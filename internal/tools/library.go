package tools

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/mattoddie/emby-mcp/internal/emby"
)

// itemTypes are the item types the search and list tools accept.
var itemTypes = []string{"Movie", "Series", "Season", "Episode", "BoxSet", "Playlist", "Video", "MusicVideo", "Trailer", "Audio", "MusicAlbum", "MusicArtist", "AudioBook", "Book", "Photo", "PhotoAlbum", "Folder", "Person"}

var sortFields = map[string]string{
	"name": "SortName", "year": "ProductionYear,SortName", "premiere_date": "PremiereDate,SortName", "date_added": "DateCreated,SortName",
	"community_rating": "CommunityRating,SortName", "critic_rating": "CriticRating,SortName", "runtime": "Runtime,SortName",
	"date_played": "DatePlayed,SortName", "play_count": "PlayCount,SortName", "random": "Random",
}

// missingFields maps the "missing" filter to Emby's Has* query parameters.
var missingFields = map[string]string{
	"overview": "HasOverview", "tmdb_id": "HasTmdbId", "imdb_id": "HasImdbId", "tvdb_id": "HasTvdbId",
	"official_rating": "HasOfficialRating", "parental_rating": "HasParentalRating", "trailer": "HasTrailer", "subtitles": "HasSubtitles",
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type searchArgs struct {
	Query             string   `json:"query,omitempty" jsonschema:"Text to search for in titles (Emby's search: names, original titles and people)"`
	Types             []string `json:"types,omitempty" jsonschema:"Item types to include, such as [\"Movie\"] or [\"Series\"]. Default: every type except folders"`
	Library           string   `json:"library,omitempty" jsonschema:"Only this library (name or ID), such as Movies"`
	ParentID          string   `json:"parent_id,omitempty" jsonschema:"Only items inside this item: a collection, playlist, series, season or folder ID"`
	Genres            []string `json:"genres,omitempty" jsonschema:"Items with any of these genres"`
	Tags              []string `json:"tags,omitempty" jsonschema:"Items with any of these tags"`
	ExcludeTags       []string `json:"exclude_tags,omitempty" jsonschema:"Leave out items with any of these tags"`
	Untagged          bool     `json:"untagged,omitempty" jsonschema:"Only items with no tags at all (useful when tagging a library a batch at a time)"`
	Years             []int    `json:"years,omitempty" jsonschema:"Items from any of these production years"`
	YearFrom          int      `json:"year_from,omitempty" jsonschema:"Items from this year or later"`
	YearTo            int      `json:"year_to,omitempty" jsonschema:"Items from this year or earlier"`
	Ratings           []string `json:"ratings,omitempty" jsonschema:"Items with any of these parental ratings, such as [\"PG\",\"12\"]"`
	MaxRating         string   `json:"max_rating,omitempty" jsonschema:"Items rated at most this (such as PG-13 or 12), for family viewing"`
	MinCommunity      float64  `json:"min_community_rating,omitempty" jsonschema:"Minimum community (audience) rating, 0 to 10"`
	MinCritic         float64  `json:"min_critic_rating,omitempty" jsonschema:"Minimum critic rating, 0 to 100"`
	Person            string   `json:"person,omitempty" jsonschema:"Items featuring this person (actor, director, writer...)"`
	PersonTypes       []string `json:"person_types,omitempty" jsonschema:"With person: only these roles, such as [\"Director\"]"`
	Studios           []string `json:"studios,omitempty" jsonschema:"Items from any of these studios or networks"`
	Played            *bool    `json:"played,omitempty" jsonschema:"true: only watched items; false: only unwatched (for the user)"`
	Favorite          *bool    `json:"favorite,omitempty" jsonschema:"true: only the user's favourites; false: leave them out"`
	InProgress        bool     `json:"in_progress,omitempty" jsonschema:"Only items the user has started but not finished"`
	Missing           []string `json:"missing,omitempty" jsonschema:"Only items missing any of these, to find gaps in the metadata"`
	IDs               []string `json:"ids,omitempty" jsonschema:"Only these item IDs"`
	SortBy            string   `json:"sort_by,omitempty" jsonschema:"Sort order (default name; date_added for newest first with sort_order descending)"`
	SortOrder         string   `json:"sort_order,omitempty" jsonschema:"ascending (default) or descending"`
	Limit             int      `json:"limit,omitempty" jsonschema:"How many items to return (default 50)"`
	Offset            int      `json:"offset,omitempty" jsonschema:"How many matching items to skip, for paging (use next_offset from the last call)"`
	IncludeOverview   bool     `json:"include_overview,omitempty" jsonschema:"Add each item's plot summary (shortened), for example to decide tags"`
	IncludeCollection bool     `json:"group_into_collections,omitempty" jsonschema:"Show collections in place of the films they contain, as Emby's library view does"`
	userArgs
	rawArgs
}

func registerLibrary(c *Context) {
	cl := c.Client

	add(c, Def{
		Toolset: "library", Name: "emby_get_server_info", Title: "Server, libraries and counts",
		Description: "A first look at the Emby server: name, version, whether an update or restart is pending, the user the tools act as, each library with its type, and how many films, series, episodes, collections and other items there are. Also says whether changes are allowed.",
	}, func(ctx context.Context, _ NoArgs) (any, error) {
		var info Obj
		if err := cl.Get(ctx, "/System/Info", nil, &info); err != nil {
			return nil, err
		}
		out := Obj{"server": Obj{
			"name": emby.Str(info, "ServerName"), "version": emby.Str(info, "Version"), "os": emby.Str(info, "OperatingSystemDisplayName"),
			"update_available": emby.Bool(info, "HasUpdateAvailable"), "restart_pending": emby.Bool(info, "HasPendingRestart"),
			"url": cl.BaseURL(),
		}}
		user, err := cl.User(ctx, "")
		if err != nil {
			return nil, err
		}
		policy, _ := user["Policy"].(Obj)
		out["user"] = Obj{"name": emby.Str(user, "Name"), "id": emby.Str(user, "Id"), "administrator": emby.Bool(policy, "IsAdministrator")}
		if libs, err := cl.Libraries(ctx); err == nil {
			var list []Obj
			for _, l := range libs {
				list = append(list, Obj{"name": emby.Str(l, "Name"), "id": emby.Str(l, "ItemId"), "type": or(emby.Str(l, "CollectionType"), "mixed")})
			}
			out["libraries"] = list
		} else {
			out["libraries_error"] = err.Error()
		}
		var counts Obj
		if err := cl.Get(ctx, "/Items/Counts", url.Values{"UserId": {emby.Str(user, "Id")}}, &counts); err == nil {
			cs := Obj{}
			for k, v := range counts {
				if n, ok := v.(float64); ok && n > 0 {
					cs[strings.TrimSuffix(k, "Count")] = int(n)
				}
			}
			out["counts"] = cs
		}
		out["changes"] = map[bool]string{true: "allowed (EMBY_ALLOW_WRITES=true)", false: "not allowed: the server is read-only"}[c.AllowWrites]
		if c.AllowWrites {
			out["deletes"] = map[bool]string{true: "collections and playlists can be deleted", false: "off"}[c.AllowDeletes]
		}
		return out, nil
	})

	add(c, Def{
		Toolset: "library", Name: "emby_list_libraries", Title: "List libraries",
		Description: "List the Emby libraries: name, ID (use it as library or parent_id), content type (movies, tvshows, music...), the folders on disk, and the settings that matter for curation: metadata language and country, whether Emby imports collections (such as TMDb film series) automatically and the minimum size for one, whether metadata is saved next to the media (NFO files), and the automatic refresh interval.",
	}, func(ctx context.Context, a rawArgs) (any, error) {
		libs, err := cl.Libraries(ctx)
		if err != nil {
			return nil, err
		}
		var out []Obj
		for _, l := range libs {
			if a.Raw {
				out = append(out, l)
				continue
			}
			s := Obj{"name": emby.Str(l, "Name"), "id": emby.Str(l, "ItemId"), "type": or(emby.Str(l, "CollectionType"), "mixed"), "folders": emby.Strs(l["Locations"])}
			if st := emby.Str(l, "RefreshStatus"); st != "" && st != "Idle" {
				s["scan"] = st
			}
			if o, ok := l["LibraryOptions"].(Obj); ok {
				opts := Obj{}
				for k, name := range map[string]string{"PreferredMetadataLanguage": "metadata_language", "MetadataCountryCode": "metadata_country"} {
					if v := emby.Str(o, k); v != "" {
						opts[name] = v
					}
				}
				for k, name := range map[string]string{"ImportCollections": "import_collections", "SaveLocalMetadata": "save_nfo", "EnableRealtimeMonitor": "realtime_monitor", "EnableAutomaticSeriesGrouping": "automatic_series_grouping"} {
					if v, ok := o[k].(bool); ok {
						opts[name] = v
					}
				}
				if n, ok := emby.Num(o, "MinCollectionItems"); ok {
					opts["min_collection_items"] = int(n)
				}
				if n, ok := emby.Num(o, "AutomaticRefreshIntervalDays"); ok {
					opts["refresh_interval_days"] = int(n)
				}
				s["options"] = opts
			}
			out = append(out, s)
		}
		return listResult(out), nil
	})

	add(c, Def{
		Toolset: "library", Name: "emby_search_items", Title: "Search the library",
		Description: "Find items in the Emby library: films, series, episodes, collections, music and more. Combine any filters: text, type, library, genres, tags (or untagged), years, parental rating, community and critic ratings, person, studio, watched or favourite (for the user), in progress, missing metadata (such as no overview or no TMDb ID), or a list of IDs. Returns compact items with IDs, year, rating, runtime, genres, tags and watched state, sorted and paged (total, offset, next_offset). include_overview adds a short plot summary, for deciding tags or recommending something.",
		Enums:       map[string][]string{"sort_by": keys(sortFields), "sort_order": {"ascending", "descending"}},
		Ranges:      map[string][2]float64{"limit": {1, maxBatch}, "min_community_rating": {0, 10}, "min_critic_rating": {0, 100}},
	}, func(ctx context.Context, a searchArgs) (any, error) {
		return search(ctx, c, a)
	})

	add(c, Def{
		Toolset: "library", Name: "emby_get_item", Title: "Item details",
		Description: "Everything about one item: title, original title, year and premiere date, ratings, runtime, plot, tagline, genres, tags, studios, the collections it's in, directors, writers and cast, provider IDs (TMDb, IMDb, TVDb), date added, file path, media (resolution, HDR, codecs, audio and subtitle tracks, size), locked metadata fields, and the user's watched state and progress. Give the ID, or a title that matches one item.",
	}, func(ctx context.Context, a struct {
		Item string `json:"item" jsonschema:"The item's ID, or its exact title"`
		userArgs
		rawArgs
	}) (any, error) {
		uid, err := cl.UserID(ctx, a.User)
		if err != nil {
			return nil, err
		}
		item, err := resolveItem(ctx, c, uid, a.Item)
		if err != nil {
			return nil, err
		}
		if a.Raw {
			return item, nil
		}
		d := emby.Detail(item)
		// Emby doesn't always fill in Collections; ask which collections hold the item.
		if _, ok := d["collections"]; !ok && emby.Str(item, "Type") != "BoxSet" {
			if names := collectionsOf(ctx, c, uid, emby.Str(item, "Id")); len(names) > 0 {
				d["collections"] = names
			}
		}
		return d, nil
	})

	add(c, Def{
		Toolset: "library", Name: "emby_list_values", Title: "List tags, genres, studios...",
		Description: "List the values in use in the library for one kind of metadata: tags, genres, studios, parental ratings or years. Narrow it to item types or a library, or filter the names. with_counts adds how many items have each value (it reads every matching item, so it's slower on big libraries), sorted most-used first. Use it before tagging to reuse existing tag names instead of inventing near-duplicates.",
		Enums:       map[string][]string{"kind": {"tags", "genres", "studios", "ratings", "years"}},
	}, func(ctx context.Context, a struct {
		Kind       string   `json:"kind" jsonschema:"Which values to list"`
		Types      []string `json:"types,omitempty" jsonschema:"Only values used by these item types, such as [\"Movie\"]"`
		Library    string   `json:"library,omitempty" jsonschema:"Only values used in this library (name or ID)"`
		Filter     string   `json:"filter,omitempty" jsonschema:"Only names containing this text (case-insensitive)"`
		WithCounts bool     `json:"with_counts,omitempty" jsonschema:"Count the items with each value"`
		userArgs
	}) (any, error) {
		return listValues(ctx, c, a.Kind, a.Types, a.Library, a.Filter, a.WithCounts, a.User)
	})

	add(c, Def{
		Toolset: "library", Name: "emby_get_similar", Title: "Similar items",
		Description: "Items Emby considers similar to one item (by genre, people and studio), with the user's watched state: good for \"something like X\" recommendations or for finding candidates for a collection.",
		Ranges:      map[string][2]float64{"limit": {1, 100}},
	}, func(ctx context.Context, a struct {
		Item  string `json:"item" jsonschema:"The item's ID, or its exact title"`
		Limit int    `json:"limit,omitempty" jsonschema:"How many (default 12)"`
		userArgs
	}) (any, error) {
		uid, err := cl.UserID(ctx, a.User)
		if err != nil {
			return nil, err
		}
		item, err := resolveItem(ctx, c, uid, a.Item)
		if err != nil {
			return nil, err
		}
		q := url.Values{"UserId": {uid}, "Limit": {strconv.Itoa(orInt(a.Limit, 12))}, "Fields": {emby.ListFields}, "EnableImages": {"false"}}
		var res struct{ Items []Obj }
		if err := cl.Get(ctx, "/Items/"+emby.Str(item, "Id")+"/Similar", q, &res); err != nil {
			return nil, err
		}
		var out []Obj
		for _, it := range res.Items {
			out = append(out, emby.Summary(it, false))
		}
		r := listResult(out)
		r["similar_to"] = emby.Summary(item, false)
		return r, nil
	})

	add(c, Def{
		Toolset: "library", Name: "emby_list_latest", Title: "Recently added",
		Description: "The most recently added items, newest first, as on Emby's home screen: optionally only some types or one library. New episodes of a series are grouped under the series unless group is false.",
		Ranges:      map[string][2]float64{"limit": {1, 200}},
	}, func(ctx context.Context, a struct {
		Types   []string `json:"types,omitempty" jsonschema:"Only these item types, such as [\"Movie\"] or [\"Episode\"]"`
		Library string   `json:"library,omitempty" jsonschema:"Only this library (name or ID)"`
		Limit   int      `json:"limit,omitempty" jsonschema:"How many (default 20)"`
		Group   *bool    `json:"group,omitempty" jsonschema:"Group new episodes under their series (default true)"`
		userArgs
	}) (any, error) {
		uid, err := cl.UserID(ctx, a.User)
		if err != nil {
			return nil, err
		}
		q := url.Values{"Limit": {strconv.Itoa(orInt(a.Limit, 20))}, "Fields": {emby.ListFields + ",DateCreated"}, "EnableImages": {"false"}}
		if err := typesParam(q, "IncludeItemTypes", a.Types); err != nil {
			return nil, err
		}
		if a.Library != "" {
			lib, err := cl.Library(ctx, a.Library)
			if err != nil {
				return nil, err
			}
			q.Set("ParentId", emby.Str(lib, "ItemId"))
		}
		if a.Group != nil {
			q.Set("GroupItems", strconv.FormatBool(*a.Group))
		}
		var items []Obj
		if err := cl.Get(ctx, "/Users/"+uid+"/Items/Latest", q, &items); err != nil {
			return nil, err
		}
		var out []Obj
		for _, it := range items {
			s := emby.Summary(it, false)
			if v := emby.Str(it, "DateCreated"); v != "" {
				s["date_added"] = strings.SplitN(v, "T", 2)[0]
			}
			out = append(out, s)
		}
		return listResult(out), nil
	})

	add(c, Def{
		Toolset: "library", Name: "emby_list_resume", Title: "Continue watching",
		Description: "What the user has started but not finished (Emby's \"Continue watching\"), most recent first, with how far through each one they are.",
		Ranges:      map[string][2]float64{"limit": {1, 200}},
	}, func(ctx context.Context, a struct {
		Types []string `json:"types,omitempty" jsonschema:"Only these item types, such as [\"Movie\"] or [\"Episode\"]"`
		Limit int      `json:"limit,omitempty" jsonschema:"How many (default 20)"`
		userArgs
	}) (any, error) {
		uid, err := cl.UserID(ctx, a.User)
		if err != nil {
			return nil, err
		}
		q := url.Values{"Limit": {strconv.Itoa(orInt(a.Limit, 20))}, "Recursive": {"true"}, "Fields": {emby.ListFields}, "EnableImages": {"false"}}
		if err := typesParam(q, "IncludeItemTypes", a.Types); err != nil {
			return nil, err
		}
		var res struct {
			Items            []Obj
			TotalRecordCount int
		}
		if err := cl.Get(ctx, "/Users/"+uid+"/Items/Resume", q, &res); err != nil {
			return nil, err
		}
		var out []Obj
		for _, it := range res.Items {
			out = append(out, emby.Summary(it, false))
		}
		return pagedResult(out, max(res.TotalRecordCount, len(out)), 0), nil
	})
}

func orInt(v, fallback int) int {
	if v <= 0 {
		return fallback
	}
	return v
}

// typesParam validates item types and sets them on q.
func typesParam(q url.Values, key string, types []string) error {
	if len(types) == 0 {
		return nil
	}
	var out []string
	for _, t := range types {
		t = strings.TrimSpace(t)
		found := ""
		for _, known := range itemTypes {
			if strings.EqualFold(known, t) {
				found = known
			}
		}
		if found == "" {
			return fmt.Errorf("unknown item type %q; use %s", t, strings.Join(itemTypes, ", "))
		}
		out = append(out, found)
	}
	q.Set(key, strings.Join(out, ","))
	return nil
}

// searchQuery turns search arguments into Emby query parameters.
func searchQuery(ctx context.Context, c *Context, a searchArgs) (url.Values, error) {
	q := url.Values{"Recursive": {"true"}, "EnableImages": {"false"}, "EnableUserData": {"true"}}
	fields := emby.ListFields
	if a.IncludeOverview {
		fields += ",Overview"
	}
	q.Set("Fields", fields)
	if s := strings.TrimSpace(a.Query); s != "" {
		q.Set("SearchTerm", s)
	}
	if err := typesParam(q, "IncludeItemTypes", a.Types); err != nil {
		return nil, err
	}
	if len(a.Types) == 0 {
		q.Set("ExcludeItemTypes", "Folder,CollectionFolder,UserView,Person")
	}
	if a.Library != "" {
		lib, err := c.Client.Library(ctx, a.Library)
		if err != nil {
			return nil, err
		}
		q.Set("ParentId", emby.Str(lib, "ItemId"))
	}
	if a.ParentID != "" {
		if err := emby.CheckID("parent_id", a.ParentID); err != nil {
			return nil, err
		}
		q.Set("ParentId", a.ParentID)
	}
	join := func(key string, list []string) {
		var clean []string
		for _, v := range list {
			if v = strings.TrimSpace(v); v != "" {
				// Emby separates multiple values with "|" for these filters.
				clean = append(clean, v)
			}
		}
		if len(clean) > 0 {
			q.Set(key, strings.Join(clean, "|"))
		}
	}
	join("Genres", a.Genres)
	join("Tags", a.Tags)
	join("ExcludeTags", a.ExcludeTags)
	join("OfficialRatings", a.Ratings)
	join("Studios", a.Studios)
	years := append([]int{}, a.Years...)
	if a.YearFrom > 0 || a.YearTo > 0 {
		from, to := a.YearFrom, a.YearTo
		if from == 0 {
			from = 1880
		}
		if to == 0 {
			to = 2100
		}
		if to < from || to-from > 200 {
			return nil, errors.New("year_from must be before year_to")
		}
		if len(years) == 0 {
			for y := from; y <= to; y++ {
				years = append(years, y)
			}
		} else {
			var kept []int
			for _, y := range years {
				if y >= from && y <= to {
					kept = append(kept, y)
				}
			}
			years = kept
			if len(years) == 0 {
				return nil, errors.New("none of years is between year_from and year_to")
			}
		}
	}
	if len(years) > 0 {
		var ys []string
		for _, y := range years {
			ys = append(ys, strconv.Itoa(y))
		}
		q.Set("Years", strings.Join(ys, ","))
	}
	if a.MaxRating != "" {
		q.Set("MaxOfficialRating", a.MaxRating)
	}
	if a.MinCommunity > 0 {
		q.Set("MinCommunityRating", strconv.FormatFloat(a.MinCommunity, 'f', -1, 64))
	}
	if a.MinCritic > 0 {
		q.Set("MinCriticRating", strconv.FormatFloat(a.MinCritic, 'f', -1, 64))
	}
	if p := strings.TrimSpace(a.Person); p != "" {
		q.Set("Person", p)
		if len(a.PersonTypes) > 0 {
			q.Set("PersonTypes", strings.Join(a.PersonTypes, ","))
		}
	}
	if a.Played != nil {
		q.Set("IsPlayed", strconv.FormatBool(*a.Played))
	}
	if a.Favorite != nil {
		q.Set("IsFavorite", strconv.FormatBool(*a.Favorite))
	}
	if a.InProgress {
		q.Set("Filters", "IsResumable")
	}
	for _, m := range a.Missing {
		param, ok := missingFields[strings.ToLower(strings.TrimSpace(m))]
		if !ok {
			return nil, fmt.Errorf("missing must be some of %s, got %q", strings.Join(keys(missingFields), ", "), m)
		}
		q.Set(param, "false")
	}
	if len(a.IDs) > 0 {
		ids, err := cleanIDs(a.IDs, "item")
		if err != nil {
			return nil, err
		}
		q.Set("Ids", strings.Join(ids, ","))
	}
	sortBy := "name"
	if a.SortBy != "" {
		sortBy = a.SortBy
	}
	sf, ok := sortFields[sortBy]
	if !ok {
		return nil, fmt.Errorf("sort_by must be one of %s", strings.Join(keys(sortFields), ", "))
	}
	q.Set("SortBy", sf)
	switch a.SortOrder {
	case "", "ascending":
		q.Set("SortOrder", "Ascending")
	case "descending":
		q.Set("SortOrder", "Descending")
	default:
		return nil, errors.New("sort_order must be ascending or descending")
	}
	if a.IncludeCollection {
		q.Set("GroupItemsIntoCollections", "true")
	}
	return q, nil
}

func search(ctx context.Context, c *Context, a searchArgs) (any, error) {
	uid, err := c.Client.UserID(ctx, a.User)
	if err != nil {
		return nil, err
	}
	q, err := searchQuery(ctx, c, a)
	if err != nil {
		return nil, err
	}
	limit := orInt(a.Limit, 50)
	if limit > maxBatch {
		limit = maxBatch
	}
	offset := max(a.Offset, 0)
	summarize := func(items []Obj) []Obj {
		out := make([]Obj, 0, len(items))
		for _, it := range items {
			if a.Raw {
				out = append(out, it)
			} else {
				out = append(out, emby.Summary(it, a.IncludeOverview))
			}
		}
		return out
	}
	if !a.Untagged {
		q.Set("StartIndex", strconv.Itoa(offset))
		q.Set("Limit", strconv.Itoa(limit))
		items, total, err := c.Client.Items(ctx, uid, q)
		if err != nil {
			return nil, err
		}
		return pagedResult(summarize(items), total, offset), nil
	}
	// Emby can't filter on "no tags", so read the matches a page at a time
	// and keep the untagged ones. total counts every untagged match.
	var kept []Obj
	total := 0
	for start := 0; ; start += 500 {
		q.Set("StartIndex", strconv.Itoa(start))
		q.Set("Limit", "500")
		items, all, err := c.Client.Items(ctx, uid, q)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if len(emby.Tags(it)) > 0 {
				continue
			}
			if total >= offset && len(kept) < limit {
				kept = append(kept, it)
			}
			total++
		}
		if len(items) == 0 || start+len(items) >= all {
			break
		}
	}
	return pagedResult(summarize(kept), total, offset), nil
}

// collectionsOf names the collections (box sets) that hold an item.
func collectionsOf(ctx context.Context, c *Context, uid, id string) []string {
	q := url.Values{"IncludeItemTypes": {"BoxSet"}, "Recursive": {"true"}, "ListItemIds": {id}, "EnableImages": {"false"}, "Limit": {"50"}}
	items, _, err := c.Client.Items(ctx, uid, q)
	if err != nil {
		return nil
	}
	var names []string
	for _, it := range items {
		names = append(names, emby.Str(it, "Name"))
	}
	return names
}

func listValues(ctx context.Context, c *Context, kind string, types []string, library, filter string, withCounts bool, user string) (any, error) {
	cl := c.Client
	uid, err := cl.UserID(ctx, user)
	if err != nil {
		return nil, err
	}
	q := url.Values{"Recursive": {"true"}, "EnableImages": {"false"}, "UserId": {uid}}
	if err := typesParam(q, "IncludeItemTypes", types); err != nil {
		return nil, err
	}
	if library != "" {
		lib, err := cl.Library(ctx, library)
		if err != nil {
			return nil, err
		}
		q.Set("ParentId", emby.Str(lib, "ItemId"))
	}
	filter = strings.ToLower(strings.TrimSpace(filter))
	keep := func(name string) bool {
		return name != "" && (filter == "" || strings.Contains(strings.ToLower(name), filter))
	}

	if withCounts {
		// Count from the items themselves: exact, and the same for every kind.
		if len(types) == 0 {
			q.Set("ExcludeItemTypes", "Folder,CollectionFolder,UserView,Person,Season,Episode,BoxSet,Playlist")
		}
		q.Set("Fields", emby.ListFields+",Studios")
		counts := map[string]int{}
		display := map[string]string{}
		items := 0
		for start := 0; ; start += 500 {
			q.Set("StartIndex", strconv.Itoa(start))
			q.Set("Limit", "500")
			page, all, err := cl.Items(ctx, uid, q)
			if err != nil {
				return nil, err
			}
			for _, it := range page {
				items++
				var vals []string
				switch kind {
				case "tags":
					vals = emby.Tags(it)
				case "genres":
					vals = emby.Genres(it)
				case "studios":
					vals = emby.Names(it["Studios"])
				case "ratings":
					vals = []string{emby.Str(it, "OfficialRating")}
				case "years":
					if y := emby.Year(it); y > 0 {
						vals = []string{strconv.Itoa(y)}
					}
				default:
					return nil, errors.New("kind must be tags, genres, studios, ratings or years")
				}
				for _, v := range vals {
					if keep(v) {
						k := strings.ToLower(v)
						counts[k]++
						if _, ok := display[k]; !ok {
							display[k] = v
						}
					}
				}
			}
			if len(page) == 0 || start+len(page) >= all {
				break
			}
		}
		var out []Obj
		for k, n := range counts {
			out = append(out, Obj{"name": display[k], "items": n})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i]["items"].(int) != out[j]["items"].(int) {
				return out[i]["items"].(int) > out[j]["items"].(int)
			}
			return strings.ToLower(out[i]["name"].(string)) < strings.ToLower(out[j]["name"].(string))
		})
		r := listResult(out)
		r["items_read"] = items
		return r, nil
	}

	path := map[string]string{"tags": "/Tags", "genres": "/Genres", "studios": "/Studios", "ratings": "/OfficialRatings", "years": "/Years"}[kind]
	if path == "" {
		return nil, errors.New("kind must be tags, genres, studios, ratings or years")
	}
	var names []string
	if kind == "ratings" {
		// /OfficialRatings answers a plain list of names.
		var res any
		if err := cl.Get(ctx, path, q, &res); err != nil {
			return nil, err
		}
		switch x := res.(type) {
		case []any:
			for _, v := range x {
				switch y := v.(type) {
				case string:
					names = append(names, y)
				case Obj:
					names = append(names, emby.Str(y, "Name"))
				}
			}
		case Obj:
			for _, it := range emby.Objs(x["Items"]) {
				names = append(names, emby.Str(it, "Name"))
			}
		}
	} else {
		var res struct{ Items []Obj }
		if err := cl.Get(ctx, path, q, &res); err != nil {
			return nil, err
		}
		for _, it := range res.Items {
			names = append(names, emby.Str(it, "Name"))
		}
	}
	var out []Obj
	seen := map[string]bool{}
	for _, n := range names {
		if keep(n) && !seen[strings.ToLower(n)] {
			seen[strings.ToLower(n)] = true
			out = append(out, Obj{"name": n})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]["name"].(string)) < strings.ToLower(out[j]["name"].(string))
	})
	return listResult(out), nil
}
