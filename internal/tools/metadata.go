package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mattoddie/emby-mcp/internal/emby"
)

// lockableFields are Emby's MetadataFields: fields a metadata refresh won't overwrite once locked.
var lockableFields = []string{"Name", "SortName", "OriginalTitle", "Overview", "Tagline", "Genres", "Tags", "Studios", "Cast", "OfficialRating", "CommunityRating", "CriticRating", "Runtime", "ProductionLocations", "Collections"}

type tagArgs struct {
	ItemIDs []string `json:"item_ids" jsonschema:"IDs of the items to change (up to 500), from emby_search_items"`
	Tags    []string `json:"tags" jsonschema:"Tag names, such as [\"Christmas\",\"Heist\"]. Reuse existing names (emby_list_values kind tags) rather than near-duplicates"`
}

// fullItemKeys must be in an item read back from Emby before it can be
// written with POST /Items/{id}: that replaces the item's metadata with the
// body, so a field missing from the read would be wiped.
var fullItemKeys = []string{"Name", "Id", "Type", "Genres", "TagItems", "Studios", "People", "ProviderIds", "LockedFields"}

func registerMetadata(c *Context) {
	cl := c.Client

	add(c, Def{
		Toolset: "metadata", Name: "emby_add_tags", Gate: Write, Title: "Add tags to items",
		Description: "Add up to 20 tags to up to 500 items in one call: the way to tag a library, such as tagging every Christmas film \"Christmas\". Items that already have a tag are skipped for it, and existing tags are kept. Group items by tag and make one call per tag (or set of tags). lock: true also locks the Tags field on each item, so a metadata refresh with 'replace all' can't remove the tags. Tags are shared by every user. Returns what changed per item.",
		Idempotent:  true,
	}, func(ctx context.Context, a struct {
		tagArgs
		Lock bool `json:"lock,omitempty" jsonschema:"Also lock each item's Tags field against metadata refreshes"`
	}) (any, error) {
		return changeTags(ctx, c, a.ItemIDs, a.Tags, true, a.Lock)
	})

	add(c, Def{
		Toolset: "metadata", Name: "emby_remove_tags", Gate: Write, Title: "Remove tags from items",
		Description: "Remove up to 20 tags from up to 500 items in one call. Items without the tag are skipped. Other tags are kept. To remove a tag everywhere, find its items with emby_search_items (tags: [name]) first, or use emby_rename_tag to merge it into another tag.",
		Idempotent:  true,
	}, func(ctx context.Context, a tagArgs) (any, error) {
		return changeTags(ctx, c, a.ItemIDs, a.Tags, false, false)
	})

	add(c, Def{
		Toolset: "metadata", Name: "emby_rename_tag", Gate: Write, Title: "Rename or merge a tag",
		Description: "Rename a tag on every item that has it (or only items of some types, or in one library), for example to fix \"xmas\" to \"Christmas\" or merge \"Sci Fi\" into \"Science Fiction\". If the new name already exists, the items join it. Returns the items changed.",
	}, func(ctx context.Context, a struct {
		From    string   `json:"from" jsonschema:"The tag to rename"`
		To      string   `json:"to" jsonschema:"Its new name (an existing tag to merge into, or a new one)"`
		Types   []string `json:"types,omitempty" jsonschema:"Only items of these types"`
		Library string   `json:"library,omitempty" jsonschema:"Only items in this library (name or ID)"`
	}) (any, error) {
		names, err := cleanNames([]string{a.From, a.To}, "tag", 2)
		if err != nil || len(names) < 2 {
			if strings.TrimSpace(a.From) != "" && strings.EqualFold(strings.TrimSpace(a.From), strings.TrimSpace(a.To)) && a.From != a.To {
				names = []string{strings.TrimSpace(a.From), strings.Join(strings.Fields(a.To), " ")}
			} else {
				return nil, errors.New("give two different tag names: from and to")
			}
		}
		from, to := names[0], names[1]
		uid, err := cl.UserID(ctx, "")
		if err != nil {
			return nil, err
		}
		q, err := searchQuery(ctx, c, searchArgs{Tags: []string{from}, Types: a.Types, Library: a.Library})
		if err != nil {
			return nil, err
		}
		var ids []string
		for start := 0; ; start += 500 {
			q.Set("StartIndex", strconv.Itoa(start))
			q.Set("Limit", "500")
			items, all, err := cl.Items(ctx, uid, q)
			if err != nil {
				return nil, err
			}
			for _, it := range items {
				// Emby matches tags case-insensitively; only take exact holders.
				if hasName(emby.Tags(it), from) {
					ids = append(ids, emby.Str(it, "Id"))
				}
			}
			if len(items) == 0 || start+len(items) >= all {
				break
			}
		}
		if len(ids) == 0 {
			return Obj{"done": true, "changed": 0, "note": fmt.Sprintf("No items have the tag %q.", from)}, nil
		}
		out := Obj{"from": from, "to": to, "items": len(ids)}
		var all []itemResult
		for start := 0; start < len(ids); start += maxBatch {
			batch := ids[start:min(start+maxBatch, len(ids))]
			// A change of case only: remove first, or the add would be a no-op.
			first, second := func() (any, error) { return changeTags(ctx, c, batch, []string{to}, true, false) },
				func() (any, error) { return changeTags(ctx, c, batch, []string{from}, false, false) }
			if strings.EqualFold(from, to) {
				first, second = second, first
			}
			r1, err := first()
			if err != nil {
				return out, err
			}
			r2, err := second()
			if err != nil {
				return out, err
			}
			all = append(all, mergeResults(r1, r2)...)
		}
		out["done"] = true
		out["changed"] = all
		return out, nil
	})

	add(c, Def{
		Toolset: "metadata", Name: "emby_update_item", Gate: Write, Title: "Edit item metadata",
		Description: "Edit an item's metadata, as in Emby's metadata editor: title, sort title, original title, plot, tagline, genres, studios, parental and custom rating, community and critic rating, production year and premiere date. It also locks or unlocks fields, so later metadata refreshes leave them alone. Only the fields given change. Edited fields are locked automatically unless lock_edited is false. It also works on collections and playlists, for example to rename a collection or give it a description. To change tags, use emby_add_tags and emby_remove_tags. Saving an item makes Emby match its cast and crew by name, as its own metadata editor does; if that links someone to a different person with the same name, the result says so.",
		Enums:       map[string][]string{"lock_fields": lockableFields, "unlock_fields": lockableFields},
		Ranges:      map[string][2]float64{"community_rating": {0, 10}, "critic_rating": {0, 100}, "production_year": {1850, 2200}},
		Idempotent:  true,
	}, func(ctx context.Context, a struct {
		Item            string   `json:"item" jsonschema:"The item's ID, or its exact title"`
		Name            *string  `json:"name,omitempty" jsonschema:"New title"`
		SortName        *string  `json:"sort_name,omitempty" jsonschema:"Title to sort by, such as \"Godfather, The\" (empty to clear)"`
		OriginalTitle   *string  `json:"original_title,omitempty" jsonschema:"Original-language title"`
		Overview        *string  `json:"overview,omitempty" jsonschema:"Plot summary or description"`
		Tagline         *string  `json:"tagline,omitempty" jsonschema:"Tagline (empty to clear)"`
		Genres          []string `json:"genres,omitempty" jsonschema:"The complete new list of genres (replaces the current ones)"`
		Studios         []string `json:"studios,omitempty" jsonschema:"The complete new list of studios (replaces the current ones)"`
		OfficialRating  *string  `json:"official_rating,omitempty" jsonschema:"Parental rating, such as PG-13 or 12A (empty to clear)"`
		CustomRating    *string  `json:"custom_rating,omitempty" jsonschema:"Custom rating, used by parental controls instead of the official one (empty to clear)"`
		CommunityRating *float64 `json:"community_rating,omitempty" jsonschema:"Community rating, 0 to 10"`
		CriticRating    *float64 `json:"critic_rating,omitempty" jsonschema:"Critic rating, 0 to 100"`
		ProductionYear  *int     `json:"production_year,omitempty" jsonschema:"Production year"`
		PremiereDate    *string  `json:"premiere_date,omitempty" jsonschema:"Premiere or release date, YYYY-MM-DD"`
		LockFields      []string `json:"lock_fields,omitempty" jsonschema:"Fields to lock against metadata refreshes"`
		UnlockFields    []string `json:"unlock_fields,omitempty" jsonschema:"Fields to unlock"`
		LockEdited      *bool    `json:"lock_edited,omitempty" jsonschema:"Lock the fields this call edits (default true), so a refresh doesn't undo them"`
		LockAll         *bool    `json:"lock_all,omitempty" jsonschema:"Lock (true) or unlock (false) all of the item's metadata, so refreshes don't touch it"`
	}) (any, error) {
		uid, err := cl.UserID(ctx, "")
		if err != nil {
			return nil, err
		}
		item, err := resolveItem(ctx, c, uid, a.Item)
		if err != nil {
			return nil, err
		}
		for _, k := range fullItemKeys {
			if _, ok := item[k]; !ok && k != "LockedFields" && k != "TagItems" {
				return nil, fmt.Errorf("Emby didn't return the item's %s field, so saving it could erase data; not changing anything. Please report this with your Emby version", k)
			}
		}
		id := emby.Str(item, "Id")
		body := cloneObj(item)
		locks := map[string]bool{}
		for _, f := range emby.Strs(item["LockedFields"]) {
			locks[f] = true
		}
		var edited []string
		setStr := func(field, lock string, v *string) {
			if v == nil {
				return
			}
			body[field] = strings.TrimSpace(*v)
			edited = append(edited, lock)
		}
		setStr("Name", "Name", a.Name)
		setStr("SortName", "SortName", a.SortName)
		if a.SortName != nil {
			body["ForcedSortName"] = strings.TrimSpace(*a.SortName)
		}
		setStr("OriginalTitle", "OriginalTitle", a.OriginalTitle)
		setStr("Overview", "Overview", a.Overview)
		setStr("OfficialRating", "OfficialRating", a.OfficialRating)
		if a.CustomRating != nil {
			body["CustomRating"] = strings.TrimSpace(*a.CustomRating)
		}
		if a.Name != nil && strings.TrimSpace(*a.Name) == "" {
			return nil, errors.New("name can't be empty")
		}
		if a.Tagline != nil {
			if t := strings.TrimSpace(*a.Tagline); t == "" {
				body["Taglines"] = []any{}
			} else {
				body["Taglines"] = []any{t}
			}
			edited = append(edited, "Tagline")
		}
		if a.Genres != nil {
			g, err := cleanNamesAllowEmpty(a.Genres, "genre", 50)
			if err != nil {
				return nil, err
			}
			body["Genres"] = toAny(g)
			body["GenreItems"] = nameItems(g, item["GenreItems"])
			edited = append(edited, "Genres")
		}
		if a.Studios != nil {
			s, err := cleanNamesAllowEmpty(a.Studios, "studio", 50)
			if err != nil {
				return nil, err
			}
			body["Studios"] = nameItems(s, item["Studios"])
			edited = append(edited, "Studios")
		}
		if a.CommunityRating != nil {
			if *a.CommunityRating < 0 || *a.CommunityRating > 10 || math.IsNaN(*a.CommunityRating) {
				return nil, errors.New("community_rating must be from 0 to 10")
			}
			body["CommunityRating"] = *a.CommunityRating
			edited = append(edited, "CommunityRating")
		}
		if a.CriticRating != nil {
			if *a.CriticRating < 0 || *a.CriticRating > 100 || math.IsNaN(*a.CriticRating) {
				return nil, errors.New("critic_rating must be from 0 to 100")
			}
			body["CriticRating"] = *a.CriticRating
			edited = append(edited, "CriticRating")
		}
		if a.ProductionYear != nil {
			body["ProductionYear"] = *a.ProductionYear
		}
		if a.PremiereDate != nil {
			d := strings.TrimSpace(*a.PremiereDate)
			if d == "" {
				delete(body, "PremiereDate")
			} else {
				t, err := time.Parse("2006-01-02", d)
				if err != nil {
					return nil, fmt.Errorf("premiere_date must be YYYY-MM-DD, got %q", d)
				}
				body["PremiereDate"] = t.Format("2006-01-02T15:04:05.0000000Z")
			}
		}
		if a.LockEdited == nil || *a.LockEdited {
			for _, f := range edited {
				if contains(lockableFields, f) {
					locks[f] = true
				}
			}
		}
		for _, f := range a.LockFields {
			if !contains(lockableFields, f) {
				return nil, fmt.Errorf("lock_fields: unknown field %q; use %s", f, strings.Join(lockableFields, ", "))
			}
			locks[f] = true
		}
		for _, f := range a.UnlockFields {
			if !contains(lockableFields, f) {
				return nil, fmt.Errorf("unlock_fields: unknown field %q; use %s", f, strings.Join(lockableFields, ", "))
			}
			delete(locks, f)
		}
		var lockList []any
		for _, f := range lockableFields {
			if locks[f] {
				lockList = append(lockList, f)
			}
		}
		for _, f := range emby.Strs(item["LockedFields"]) {
			if !contains(lockableFields, f) && locks[f] {
				lockList = append(lockList, f) // keep locks this tool doesn't know about
			}
		}
		if lockList == nil {
			lockList = []any{}
		}
		body["LockedFields"] = lockList
		if a.LockAll != nil {
			body["LockData"] = *a.LockAll
		}
		changed := changedKeys(item, body)
		if len(changed) == 0 {
			return Obj{"done": true, "changed": []string{}, "note": "Nothing to change: the item already has these values.", "item": emby.Summary(item, false)}, nil
		}
		warnings, after, err := saveItem(ctx, c, uid, item, body)
		if err != nil {
			return nil, err
		}
		out := Obj{"done": true, "id": id, "name": emby.Str(body, "Name"), "changed": changed}
		if emby.IsDryRun(ctx) {
			return out, nil
		}
		if len(warnings) > 0 {
			out["warning"] = strings.Join(warnings, " ")
		}
		if after == nil {
			return out, nil
		}
		out["now"] = emby.Detail(after)
		return out, nil
	})

	add(c, Def{
		Toolset: "metadata", Name: "emby_refresh_item", Gate: Write, Title: "Refresh metadata",
		Description: "Ask Emby to refresh items' metadata and images from the internet (TMDb, TVDb...), as \"Refresh metadata\" in Emby. mode missing (default) fills in what's missing and keeps everything else; scan only rescans the files; replace_all downloads all metadata again and overwrites every field that isn't locked, which can remove tags and edits made in Emby, so it needs confirm: true. A series or collection refreshes its contents too. Refreshing runs in the background on the server.",
		Enums:       map[string][]string{"mode": {"missing", "scan", "replace_all"}},
	}, func(ctx context.Context, a struct {
		ItemIDs       []string `json:"item_ids" jsonschema:"IDs of the items to refresh (up to 500)"`
		Mode          string   `json:"mode,omitempty" jsonschema:"missing (default), scan or replace_all"`
		ReplaceImages bool     `json:"replace_images,omitempty" jsonschema:"Download all images again too (with replace_all)"`
		confirmArgs
	}) (any, error) {
		ids, err := cleanIDs(a.ItemIDs, "item")
		if err != nil {
			return nil, err
		}
		q := url.Values{"Recursive": {"true"}}
		switch a.Mode {
		case "", "missing":
			q.Set("MetadataRefreshMode", "FullRefresh")
			q.Set("ImageRefreshMode", "FullRefresh")
			q.Set("ReplaceAllMetadata", "false")
			q.Set("ReplaceAllImages", "false")
		case "scan":
			q.Set("MetadataRefreshMode", "Default")
			q.Set("ImageRefreshMode", "Default")
		case "replace_all":
			if !a.Confirm {
				return nil, errors.New("replace_all overwrites every unlocked field with downloaded metadata, which can remove tags, edited titles and other changes made in Emby. Lock the fields to keep first (emby_update_item lock_fields, or emby_add_tags lock), and call again with confirm: true once the user agrees")
			}
			q.Set("MetadataRefreshMode", "FullRefresh")
			q.Set("ImageRefreshMode", "FullRefresh")
			q.Set("ReplaceAllMetadata", "true")
			q.Set("ReplaceAllImages", strconv.FormatBool(a.ReplaceImages))
		default:
			return nil, errors.New("mode must be missing, scan or replace_all")
		}
		results := make([]itemResult, len(ids))
		errs := forEach(ctx, ids, func(id string) error {
			return cl.Post(ctx, "/Items/"+id+"/Refresh", q, nil, nil)
		})
		failed := 0
		for i, id := range ids {
			results[i] = itemResult{ID: id}
			if errs[i] != nil {
				results[i].Error = errs[i].Error()
				failed++
			}
		}
		out := Obj{"done": failed == 0, "refreshing": len(ids) - failed, "mode": or(a.Mode, "missing")}
		if failed > 0 {
			out["failed"] = onlyFailed(results)
		}
		out["note"] = "Emby refreshes in the background; changes appear within a minute or so (longer for big series or collections)."
		return out, nil
	})
}

// changeTags adds (or removes) tags on items, skipping items that already
// have (or don't have) them. lock also locks the Tags field.
func changeTags(ctx context.Context, c *Context, rawIDs, rawTags []string, adding, lock bool) (any, error) {
	cl := c.Client
	ids, err := cleanIDs(rawIDs, "item")
	if err != nil {
		return nil, err
	}
	tags, err := cleanNames(rawTags, "tag", 20)
	if err != nil {
		return nil, err
	}
	uid, err := cl.UserID(ctx, "")
	if err != nil {
		return nil, err
	}
	items, err := cl.ItemsByID(ctx, uid, ids, "Tags,TagItems")
	if err != nil {
		return nil, err
	}
	type job struct {
		i        int
		id, name string
		tags     []string
		lock     bool
		current  []string
	}
	var jobs []job
	var missing []string
	unchanged := 0
	for _, id := range ids {
		it, ok := items[id]
		if !ok {
			missing = append(missing, id)
			continue
		}
		have := emby.Tags(it)
		var todo []string
		for _, t := range tags {
			if adding {
				if !hasName(have, t) {
					todo = append(todo, t)
				}
				continue
			}
			// Removing: use the item's own spelling of the tag.
			for _, h := range have {
				if strings.EqualFold(h, t) {
					todo = append(todo, h)
				}
			}
		}
		if len(todo) == 0 && !lock {
			unchanged++
			continue
		}
		jobs = append(jobs, job{i: len(jobs), id: id, name: emby.Str(it, "Name"), tags: todo, lock: lock, current: have})
	}
	verb, path := "added", "/Tags/Add"
	if !adding {
		verb, path = "removed", "/Tags/Delete"
	}
	// One result per job, each written only by the worker handling that job.
	results := make([]itemResult, len(jobs))
	errs := forEach(ctx, jobs, func(j job) error {
		r := &results[j.i]
		r.ID, r.Name = j.id, j.name
		if len(j.tags) > 0 {
			pairs := make([]Obj, len(j.tags))
			for i, t := range j.tags {
				pairs[i] = Obj{"Name": t}
			}
			if emby.IsDryRun(ctx) {
				emby.SetCurrent(ctx, Obj{"Tags": toAny(j.current)})
			}
			if err := cl.Post(ctx, "/Items/"+j.id+path, nil, Obj{"Tags": pairs}, nil); err != nil {
				return err
			}
			for _, t := range j.tags {
				r.Change = append(r.Change, verb+" "+strconv.Quote(t))
			}
		}
		if j.lock {
			locked, err := lockField(ctx, c, uid, j.id, "Tags")
			var w *warning
			if errors.As(err, &w) {
				r.Change = append(r.Change, "locked Tags (note: "+w.msg+")")
				return nil
			}
			if err != nil {
				return fmt.Errorf("tags %s, but locking the Tags field failed: %w", verb, err)
			}
			if locked {
				r.Change = append(r.Change, "locked Tags")
			}
		}
		return nil
	})
	var changed []itemResult
	failed := 0
	for i, j := range jobs {
		r := &results[i]
		r.ID, r.Name = j.id, j.name
		if errs[i] != nil {
			r.Error = errs[i].Error()
			failed++
		}
		if len(r.Change) > 0 || r.Error != "" {
			changed = append(changed, *r)
		}
	}
	out := Obj{"done": failed == 0 && len(missing) == 0, "tags": tags, "changed": len(changed) - failed, "already_" + map[bool]string{true: "tagged", false: "untagged"}[adding]: unchanged}
	if len(changed) > 0 {
		if len(changed) > 100 {
			out["items"] = changed[:100]
			out["items_note"] = fmt.Sprintf("Showing the first 100 of %d changed items.", len(changed))
		} else {
			out["items"] = changed
		}
	}
	if failed > 0 {
		out["failed"] = failed
	}
	if len(missing) > 0 {
		out["not_found"] = missing
	}
	return out, nil
}

// lockField adds field to an item's LockedFields, if it isn't there already.
func lockField(ctx context.Context, c *Context, uid, id, field string) (bool, error) {
	item, err := c.Client.Item(ctx, uid, id)
	if err != nil {
		return false, err
	}
	locked := emby.Strs(item["LockedFields"])
	if contains(locked, field) {
		return false, nil
	}
	for _, k := range fullItemKeys {
		if _, ok := item[k]; !ok && k != "LockedFields" && k != "TagItems" {
			return false, fmt.Errorf("Emby didn't return the item's %s field, so saving it could erase data", k)
		}
	}
	body := cloneObj(item)
	body["LockedFields"] = toAny(append(locked, field))
	warnings, _, err := saveItem(ctx, c, uid, item, body)
	if err != nil {
		return false, err
	}
	if len(warnings) > 0 {
		return true, &warning{strings.Join(warnings, " ")}
	}
	return true, nil
}

// warning is a change that succeeded with something to report.
type warning struct{ msg string }

func (w *warning) Error() string { return w.msg }

// saveItem writes a whole item back (POST /Items/{id}), which replaces its
// metadata with body. In a dry run the preview shows only the fields that
// change. Otherwise it reads the item back and returns warnings for values
// that vanished, and for cast and crew that Emby linked to a different
// person (it matches people by name when an item is saved, as its own
// metadata editor does).
func saveItem(ctx context.Context, c *Context, uid string, before, body Obj) ([]string, Obj, error) {
	id := emby.Str(before, "Id")
	if emby.IsDryRun(ctx) {
		keys := changedKeys(before, body)
		cur, shown := Obj{}, Obj{}
		for _, k := range keys {
			cur[k], shown[k] = before[k], body[k]
		}
		emby.SetPreview(ctx, cur, shown)
		return nil, nil, c.Client.Post(ctx, "/Items/"+id, nil, body, nil)
	}
	if err := c.Client.Post(ctx, "/Items/"+id, nil, body, nil); err != nil {
		return nil, nil, err
	}
	after, err := c.Client.Item(ctx, uid, id)
	if err != nil {
		return []string{"Saved, but reading the item back failed: " + err.Error()}, nil, nil
	}
	var warnings []string
	if lost := lostValues(before, after, !sameJSON(before["Genres"], body["Genres"]), !sameJSON(before["Studios"], body["Studios"])); len(lost) > 0 {
		warnings = append(warnings, "These values were on the item before and aren't now: "+strings.Join(lost, "; ")+".")
	}
	if moved := relinkedPeople(before, after); len(moved) > 0 {
		warnings = append(warnings, "Emby matches cast and crew by name when an item is saved, and linked "+strings.Join(moved, ", ")+" to a different person with the same name. A metadata refresh (emby_refresh_item mode replace_all) links them by their TMDb ID again.")
	}
	return warnings, after, nil
}

func sameJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// relinkedPeople names the people whose entry kept its name and role but changed ID.
func relinkedPeople(before, after Obj) []string {
	key := func(p Obj) string {
		return emby.Str(p, "Name") + "\x00" + emby.Str(p, "Type") + "\x00" + emby.Str(p, "Role")
	}
	ids := map[string]string{}
	for _, p := range emby.Objs(before["People"]) {
		ids[key(p)] = emby.Str(p, "Id")
	}
	var out []string
	for _, p := range emby.Objs(after["People"]) {
		if old, ok := ids[key(p)]; ok && old != "" && emby.Str(p, "Id") != "" && old != emby.Str(p, "Id") {
			out = append(out, fmt.Sprintf("%s (%s)", emby.Str(p, "Name"), or(emby.Str(p, "Role"), emby.Str(p, "Type"))))
		}
	}
	return out
}

func mergeResults(results ...any) []itemResult {
	byID := map[string]*itemResult{}
	var order []string
	for _, r := range results {
		o, _ := r.(Obj)
		list, _ := o["items"].([]itemResult)
		for _, it := range list {
			if byID[it.ID] == nil {
				cp := it
				byID[it.ID] = &cp
				order = append(order, it.ID)
				continue
			}
			byID[it.ID].Change = append(byID[it.ID].Change, it.Change...)
			if it.Error != "" {
				byID[it.ID].Error = it.Error
			}
		}
	}
	out := make([]itemResult, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out
}

func onlyFailed(rs []itemResult) []itemResult {
	var out []itemResult
	for _, r := range rs {
		if r.Error != "" {
			out = append(out, r)
		}
	}
	return out
}

func cloneObj(o Obj) Obj {
	b, _ := json.Marshal(o)
	var out Obj
	_ = json.Unmarshal(b, &out)
	return out
}

func toAny(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}

// nameItems builds [{Name, Id}] pairs, keeping the IDs of names the item already had.
func nameItems(names []string, current any) []any {
	ids := map[string]any{}
	for _, o := range emby.Objs(current) {
		ids[strings.ToLower(emby.Str(o, "Name"))] = o["Id"]
	}
	out := make([]any, len(names))
	for i, n := range names {
		p := Obj{"Name": n}
		if id, ok := ids[strings.ToLower(n)]; ok && id != nil {
			p["Id"] = id
		}
		out[i] = p
	}
	return out
}

func cleanNamesAllowEmpty(names []string, what string, max int) ([]string, error) {
	nonEmpty := false
	for _, n := range names {
		if strings.TrimSpace(n) != "" {
			nonEmpty = true
		}
	}
	if !nonEmpty {
		return []string{}, nil
	}
	return cleanNames(names, what, max)
}

// changedKeys lists the top-level fields whose values differ.
func changedKeys(before, after Obj) []string {
	var out []string
	for k, v := range after {
		b1, _ := json.Marshal(before[k])
		b2, _ := json.Marshal(v)
		if string(b1) != string(b2) {
			if _, had := before[k]; !had && isEmpty(v) {
				continue
			}
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	}
	return false
}

// lostValues compares the tags, genres, studios, people and provider IDs
// before and after a save, and lists any that vanished without being asked to.
func lostValues(before, after Obj, genresEdited, studiosEdited bool) []string {
	var lost []string
	check := func(label string, b, a []string) {
		for _, v := range b {
			if !hasName(a, v) {
				lost = append(lost, label+" "+strconv.Quote(v))
			}
		}
	}
	check("tag", emby.Tags(before), emby.Tags(after))
	if !genresEdited {
		check("genre", emby.Genres(before), emby.Genres(after))
	}
	if !studiosEdited {
		check("studio", emby.Names(before["Studios"]), emby.Names(after["Studios"]))
	}
	check("person", emby.Names(before["People"]), emby.Names(after["People"]))
	bids, _ := before["ProviderIds"].(Obj)
	aids, _ := after["ProviderIds"].(Obj)
	for k, v := range bids {
		if s, _ := v.(string); s != "" && emby.Str(aids, k) == "" {
			lost = append(lost, "provider ID "+k)
		}
	}
	return lost
}
