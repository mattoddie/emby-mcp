package tools

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/mattoddie/emby-mcp/internal/emby"
)

// members lists the items directly inside a collection or playlist (up to limit; 0 for all).
func members(ctx context.Context, c *Context, uid, parentID string, overview bool, limit int) ([]Obj, int, error) {
	fields := emby.ListFields
	if overview {
		fields += ",Overview"
	}
	q := url.Values{"ParentId": {parentID}, "Fields": {fields}, "EnableImages": {"false"}, "SortBy": {"ProductionYear,PremiereDate,SortName"}}
	var all []Obj
	total := 0
	for start := 0; ; start += 500 {
		q.Set("StartIndex", strconv.Itoa(start))
		q.Set("Limit", "500")
		page, n, err := c.Client.Items(ctx, uid, q)
		if err != nil {
			return nil, 0, err
		}
		total = n
		all = append(all, page...)
		if len(page) == 0 || start+len(page) >= n || (limit > 0 && len(all) >= limit) {
			break
		}
	}
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, total, nil
}

// findCollection resolves a collection by ID or name.
func findCollection(ctx context.Context, c *Context, uid, ref string) (Obj, error) {
	item, err := resolveItem(ctx, c, uid, ref, "BoxSet")
	if err != nil {
		return nil, err
	}
	if emby.Str(item, "Type") != "BoxSet" {
		return nil, fmt.Errorf("%s (%s) is a %s, not a collection", emby.Str(item, "Name"), emby.Str(item, "Id"), emby.Str(item, "Type"))
	}
	return item, nil
}

func registerCollections(c *Context) {
	cl := c.Client

	add(c, Def{
		Toolset: "collections", Name: "emby_list_collections", Title: "List collections",
		Description: "List the collections (Emby box sets, such as \"Star Wars Collection\" or your own \"Christmas Classics\"), with how many items each holds. Filter by name.",
		Ranges:      map[string][2]float64{"limit": {1, maxBatch}},
	}, func(ctx context.Context, a struct {
		Query           string `json:"query,omitempty" jsonschema:"Only collections whose name contains this"`
		IncludeOverview bool   `json:"include_overview,omitempty" jsonschema:"Add each collection's description"`
		Limit           int    `json:"limit,omitempty" jsonschema:"How many (default 100)"`
		Offset          int    `json:"offset,omitempty" jsonschema:"How many to skip, for paging"`
	}) (any, error) {
		return search(ctx, c, searchArgs{Query: a.Query, Types: []string{"BoxSet"}, IncludeOverview: a.IncludeOverview, Limit: orInt(a.Limit, 100), Offset: a.Offset})
	})

	add(c, Def{
		Toolset: "collections", Name: "emby_get_collection", Title: "Collection contents",
		Description: "Show a collection and everything in it, in release order, with each item's ID, year, rating and the user's watched state.",
	}, func(ctx context.Context, a struct {
		Collection      string `json:"collection" jsonschema:"The collection's ID or exact name"`
		IncludeOverview bool   `json:"include_overview,omitempty" jsonschema:"Add each item's plot summary"`
		userArgs
	}) (any, error) {
		uid, err := cl.UserID(ctx, a.User)
		if err != nil {
			return nil, err
		}
		col, err := findCollection(ctx, c, uid, a.Collection)
		if err != nil {
			return nil, err
		}
		items, total, err := members(ctx, c, uid, emby.Str(col, "Id"), a.IncludeOverview, 0)
		if err != nil {
			return nil, err
		}
		var list []Obj
		for _, it := range items {
			list = append(list, emby.Summary(it, a.IncludeOverview))
		}
		d := emby.Detail(col)
		delete(d, "media")
		out := pagedResult(list, total, 0)
		out["collection"] = d
		return out, nil
	})

	add(c, Def{
		Toolset: "collections", Name: "emby_create_collection", Gate: Write, Title: "Create a collection",
		Description: "Create a collection (an Emby box set) from a list of items, such as \"Christmas Classics\" or \"Best of Studio Ghibli\". Emby shows the films grouped under it. It refuses a name that's already a collection, unless allow_duplicate is true: add to the existing one with emby_add_to_collection instead. lock: true stops metadata refreshes renaming it or changing its description.",
	}, func(ctx context.Context, a struct {
		Name           string   `json:"name" jsonschema:"The collection's name"`
		ItemIDs        []string `json:"item_ids,omitempty" jsonschema:"IDs of the items to put in it (up to 500)"`
		Overview       string   `json:"overview,omitempty" jsonschema:"A description for the collection"`
		Lock           bool     `json:"lock,omitempty" jsonschema:"Lock its metadata against refreshes"`
		AllowDuplicate bool     `json:"allow_duplicate,omitempty" jsonschema:"Create it even if a collection already has this name"`
	}) (any, error) {
		names, err := cleanNames([]string{a.Name}, "collection name", 1)
		if err != nil {
			return nil, err
		}
		name := names[0]
		var ids []string
		if len(a.ItemIDs) > 0 {
			if ids, err = cleanIDs(a.ItemIDs, "item"); err != nil {
				return nil, err
			}
		}
		uid, err := cl.UserID(ctx, "")
		if err != nil {
			return nil, err
		}
		if !a.AllowDuplicate {
			existing, _, err := cl.Items(ctx, uid, url.Values{"IncludeItemTypes": {"BoxSet"}, "Recursive": {"true"}, "SearchTerm": {name}, "EnableImages": {"false"}})
			if err != nil {
				return nil, err
			}
			for _, e := range existing {
				if strings.EqualFold(emby.Str(e, "Name"), name) {
					return nil, fmt.Errorf("there is already a collection called %q (id %s). Add to it with emby_add_to_collection, or pass allow_duplicate: true", emby.Str(e, "Name"), emby.Str(e, "Id"))
				}
			}
		}
		var found map[string]Obj
		if len(ids) > 0 {
			if found, err = cl.ItemsByID(ctx, uid, ids, "ProductionYear"); err != nil {
				return nil, err
			}
			var missing []string
			for _, id := range ids {
				if _, ok := found[id]; !ok {
					missing = append(missing, id)
				}
			}
			if len(missing) > 0 {
				return nil, fmt.Errorf("these item IDs don't exist: %s", strings.Join(missing, ", "))
			}
		}
		q := url.Values{"Name": {name}, "IsLocked": {strconv.FormatBool(a.Lock)}}
		if len(ids) > 0 {
			q.Set("Ids", strings.Join(ids, ","))
		}
		var res Obj
		if err := cl.Post(ctx, "/Collections", q, nil, &res); err != nil {
			return nil, err
		}
		out := Obj{"done": true, "name": name, "items": len(ids)}
		if emby.IsDryRun(ctx) {
			return out, nil
		}
		id := emby.Str(res, "Id")
		out["id"] = id
		if ov := strings.TrimSpace(a.Overview); ov != "" && id != "" {
			if item, err := cl.Item(ctx, uid, id); err == nil {
				body := cloneObj(item)
				body["Overview"] = ov
				if err := cl.Post(ctx, "/Items/"+id, nil, body, nil); err != nil {
					out["note"] = "Created, but setting the description failed: " + err.Error()
				}
			}
		}
		return out, nil
	})

	add(c, Def{
		Toolset: "collections", Name: "emby_add_to_collection", Gate: Write, Title: "Add to a collection",
		Description: "Add items to an existing collection (up to 500 per call). Items already in it are skipped.",
		Idempotent:  true,
	}, func(ctx context.Context, a struct {
		Collection string   `json:"collection" jsonschema:"The collection's ID or exact name"`
		ItemIDs    []string `json:"item_ids" jsonschema:"IDs of the items to add"`
	}) (any, error) {
		return changeMembers(ctx, c, a.Collection, a.ItemIDs, true)
	})

	add(c, Def{
		Toolset: "collections", Name: "emby_remove_from_collection", Gate: Write, Title: "Remove from a collection",
		Description: "Take items out of a collection (up to 500 per call). The items stay in the library; only their place in the collection goes.",
		Idempotent:  true,
	}, func(ctx context.Context, a struct {
		Collection string   `json:"collection" jsonschema:"The collection's ID or exact name"`
		ItemIDs    []string `json:"item_ids" jsonschema:"IDs of the items to remove"`
	}) (any, error) {
		return changeMembers(ctx, c, a.Collection, a.ItemIDs, false)
	})

	add(c, Def{
		Toolset: "collections", Name: "emby_delete_collection", Title: "Delete a collection",
		Description: "Delete a collection. The films and episodes in it are not touched: only the collection itself goes. Needs confirm: true.",
		Gate:        Delete, Destructive: true,
	}, func(ctx context.Context, a struct {
		Collection string `json:"collection" jsonschema:"The collection's ID or exact name"`
		confirmArgs
	}) (any, error) {
		uid, err := cl.UserID(ctx, "")
		if err != nil {
			return nil, err
		}
		col, err := findCollection(ctx, c, uid, a.Collection)
		if err != nil {
			return nil, err
		}
		return deleteContainer(ctx, c, col, a.Confirm)
	})
}

// deleteContainer deletes a collection or playlist, after checking its type.
func deleteContainer(ctx context.Context, c *Context, item Obj, confirm bool) (any, error) {
	typ, id, name := emby.Str(item, "Type"), emby.Str(item, "Id"), emby.Str(item, "Name")
	if typ != "BoxSet" && typ != "Playlist" {
		// Deleting anything else would delete media files from disk.
		return nil, fmt.Errorf("%s is a %s; only collections and playlists can be deleted", name, typ)
	}
	what := map[string]string{"BoxSet": "collection", "Playlist": "playlist"}[typ]
	if !confirm {
		return nil, fmt.Errorf("deleting the %s %q (id %s) can't be undone. Call again with confirm: true once the user agrees", what, name, id)
	}
	emby.SetCurrent(ctx, emby.Summary(item, false))
	if err := c.Client.Post(ctx, "/Items/"+id+"/Delete", nil, nil, nil); err != nil {
		return nil, err
	}
	return Obj{"done": true, "deleted": what, "name": name, "id": id, "note": "Only the " + what + " was deleted; its items are still in the library."}, nil
}

// changeMembers adds items to, or removes them from, a collection.
func changeMembers(ctx context.Context, c *Context, ref string, rawIDs []string, adding bool) (any, error) {
	cl := c.Client
	ids, err := cleanIDs(rawIDs, "item")
	if err != nil {
		return nil, err
	}
	uid, err := cl.UserID(ctx, "")
	if err != nil {
		return nil, err
	}
	col, err := findCollection(ctx, c, uid, ref)
	if err != nil {
		return nil, err
	}
	colID := emby.Str(col, "Id")
	current, _, err := members(ctx, c, uid, colID, false, 0)
	if err != nil {
		return nil, err
	}
	in := map[string]bool{}
	var currentNames []any
	for _, it := range current {
		in[emby.Str(it, "Id")] = true
		currentNames = append(currentNames, emby.Str(it, "Name"))
	}
	var todo, skipped []string
	for _, id := range ids {
		if in[id] == adding {
			skipped = append(skipped, id)
		} else {
			todo = append(todo, id)
		}
	}
	out := Obj{"collection": emby.Str(col, "Name"), "id": colID}
	if adding {
		out["already_in"] = len(skipped)
	} else {
		out["not_in"] = len(skipped)
	}
	if len(todo) == 0 {
		out["done"] = true
		out["changed"] = 0
		return out, nil
	}
	found, err := cl.ItemsByID(ctx, uid, todo, "ProductionYear")
	if err != nil {
		return nil, err
	}
	var missing []string
	var names []string
	for _, id := range todo {
		if it, ok := found[id]; ok {
			names = append(names, emby.Str(it, "Name"))
		} else if adding {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("these item IDs don't exist: %s", strings.Join(missing, ", "))
	}
	q := url.Values{"Ids": {strings.Join(todo, ",")}}
	path := "/Collections/" + colID + "/Items"
	if !adding {
		path += "/Delete"
	}
	emby.SetCurrent(ctx, Obj{"items": currentNames})
	if err := cl.Post(ctx, path, q, nil, nil); err != nil {
		return nil, err
	}
	out["done"] = true
	out["changed"] = len(todo)
	out[map[bool]string{true: "added", false: "removed"}[adding]] = names
	return out, nil
}

var errNoPlaylist = errors.New("give a playlist ID or name")
