package tools

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/mattoddie/emby-mcp/internal/emby"
)

func findPlaylist(ctx context.Context, c *Context, uid, ref string) (Obj, error) {
	if strings.TrimSpace(ref) == "" {
		return nil, errNoPlaylist
	}
	item, err := resolveItem(ctx, c, uid, ref, "Playlist")
	if err != nil {
		return nil, err
	}
	if emby.Str(item, "Type") != "Playlist" {
		return nil, fmt.Errorf("%s (%s) is a %s, not a playlist", emby.Str(item, "Name"), emby.Str(item, "Id"), emby.Str(item, "Type"))
	}
	return item, nil
}

// playlistEntries reads a playlist's entries in order, with their entry IDs.
func playlistEntries(ctx context.Context, c *Context, uid, id string) ([]Obj, error) {
	var res struct{ Items []Obj }
	q := url.Values{"UserId": {uid}, "Fields": {emby.ListFields}, "EnableImages": {"false"}}
	if err := c.Client.Get(ctx, "/Playlists/"+id+"/Items", q, &res); err != nil {
		return nil, err
	}
	return res.Items, nil
}

func registerPlaylists(c *Context) {
	cl := c.Client

	add(c, Def{
		Toolset: "playlists", Name: "emby_list_playlists", Title: "List playlists",
		Description: "List the user's playlists (video and music) with how many items each holds.",
		Ranges:      map[string][2]float64{"limit": {1, maxBatch}},
	}, func(ctx context.Context, a struct {
		Query string `json:"query,omitempty" jsonschema:"Only playlists whose name contains this"`
		Limit int    `json:"limit,omitempty" jsonschema:"How many (default 100)"`
		userArgs
	}) (any, error) {
		return search(ctx, c, searchArgs{Query: a.Query, Types: []string{"Playlist"}, Limit: orInt(a.Limit, 100), userArgs: a.userArgs})
	})

	add(c, Def{
		Toolset: "playlists", Name: "emby_get_playlist", Title: "Playlist contents",
		Description: "Show a playlist's items in order, with each one's ID, runtime and watched state, and the total running time.",
	}, func(ctx context.Context, a struct {
		Playlist string `json:"playlist" jsonschema:"The playlist's ID or exact name"`
		userArgs
	}) (any, error) {
		uid, err := cl.UserID(ctx, a.User)
		if err != nil {
			return nil, err
		}
		pl, err := findPlaylist(ctx, c, uid, a.Playlist)
		if err != nil {
			return nil, err
		}
		entries, err := playlistEntries(ctx, c, uid, emby.Str(pl, "Id"))
		if err != nil {
			return nil, err
		}
		var list []Obj
		minutes := 0
		for _, it := range entries {
			list = append(list, emby.Summary(it, false))
			minutes += emby.Minutes(it)
		}
		out := listResult(list)
		out["playlist"] = Obj{"id": emby.Str(pl, "Id"), "name": emby.Str(pl, "Name"), "media_type": emby.Str(pl, "MediaType"), "total_minutes": minutes}
		return out, nil
	})

	add(c, Def{
		Toolset: "playlists", Name: "emby_create_playlist", Gate: Write, Title: "Create a playlist",
		Description: "Create a playlist from items in the order given, such as a film marathon or a run of episodes. It belongs to the user (default: the server's default user).",
		Enums:       map[string][]string{"media_type": {"Video", "Audio"}},
	}, func(ctx context.Context, a struct {
		Name      string   `json:"name" jsonschema:"The playlist's name"`
		ItemIDs   []string `json:"item_ids,omitempty" jsonschema:"IDs of the items, in order (up to 500)"`
		MediaType string   `json:"media_type,omitempty" jsonschema:"Video or Audio (default: from the items, else Video)"`
		userArgs
	}) (any, error) {
		names, err := cleanNames([]string{a.Name}, "playlist name", 1)
		if err != nil {
			return nil, err
		}
		uid, err := cl.UserID(ctx, a.User)
		if err != nil {
			return nil, err
		}
		q := url.Values{"Name": {names[0]}, "UserId": {uid}}
		mediaType := a.MediaType
		if len(a.ItemIDs) > 0 {
			ids, err := cleanIDs(a.ItemIDs, "item")
			if err != nil {
				return nil, err
			}
			found, err := cl.ItemsByID(ctx, uid, ids, "MediaType")
			if err != nil {
				return nil, err
			}
			for _, id := range ids {
				it, ok := found[id]
				if !ok {
					return nil, fmt.Errorf("item %s doesn't exist", id)
				}
				if mediaType == "" {
					mediaType = emby.Str(it, "MediaType")
				}
			}
			q.Set("Ids", strings.Join(ids, ","))
		}
		if mediaType != "Audio" {
			mediaType = "Video"
		}
		q.Set("MediaType", mediaType)
		var res Obj
		if err := cl.Post(ctx, "/Playlists", q, nil, &res); err != nil {
			return nil, err
		}
		out := Obj{"done": true, "name": names[0], "items": len(a.ItemIDs), "media_type": mediaType}
		if id := emby.Str(res, "Id"); id != "" {
			out["id"] = id
		}
		return out, nil
	})

	add(c, Def{
		Toolset: "playlists", Name: "emby_add_to_playlist", Gate: Write, Title: "Add to a playlist",
		Description: "Add items to the end of a playlist, in the order given (up to 500 per call). Items already in it are skipped unless allow_repeats is true.",
	}, func(ctx context.Context, a struct {
		Playlist     string   `json:"playlist" jsonschema:"The playlist's ID or exact name"`
		ItemIDs      []string `json:"item_ids" jsonschema:"IDs of the items to add"`
		AllowRepeats bool     `json:"allow_repeats,omitempty" jsonschema:"Add items even if they're already in the playlist"`
		userArgs
	}) (any, error) {
		ids, err := cleanIDs(a.ItemIDs, "item")
		if err != nil {
			return nil, err
		}
		uid, err := cl.UserID(ctx, a.User)
		if err != nil {
			return nil, err
		}
		pl, err := findPlaylist(ctx, c, uid, a.Playlist)
		if err != nil {
			return nil, err
		}
		entries, err := playlistEntries(ctx, c, uid, emby.Str(pl, "Id"))
		if err != nil {
			return nil, err
		}
		in := map[string]bool{}
		for _, e := range entries {
			in[emby.Str(e, "Id")] = true
		}
		var todo []string
		skipped := 0
		for _, id := range ids {
			if in[id] && !a.AllowRepeats {
				skipped++
				continue
			}
			todo = append(todo, id)
		}
		out := Obj{"playlist": emby.Str(pl, "Name"), "id": emby.Str(pl, "Id"), "already_in": skipped}
		if len(todo) == 0 {
			out["done"], out["added"] = true, 0
			return out, nil
		}
		found, err := cl.ItemsByID(ctx, uid, todo, "MediaType")
		if err != nil {
			return nil, err
		}
		for _, id := range todo {
			if _, ok := found[id]; !ok {
				return nil, fmt.Errorf("item %s doesn't exist", id)
			}
		}
		if err := cl.Post(ctx, "/Playlists/"+emby.Str(pl, "Id")+"/Items", url.Values{"Ids": {strings.Join(todo, ",")}, "UserId": {uid}}, nil, nil); err != nil {
			return nil, err
		}
		out["done"], out["added"] = true, len(todo)
		return out, nil
	})

	add(c, Def{
		Toolset: "playlists", Name: "emby_remove_from_playlist", Gate: Write, Title: "Remove from a playlist",
		Description: "Take items out of a playlist (every entry of each item given). The items stay in the library.",
		Idempotent:  true,
	}, func(ctx context.Context, a struct {
		Playlist string   `json:"playlist" jsonschema:"The playlist's ID or exact name"`
		ItemIDs  []string `json:"item_ids" jsonschema:"IDs of the items to remove (item IDs, not playlist entry IDs)"`
		userArgs
	}) (any, error) {
		ids, err := cleanIDs(a.ItemIDs, "item")
		if err != nil {
			return nil, err
		}
		uid, err := cl.UserID(ctx, a.User)
		if err != nil {
			return nil, err
		}
		pl, err := findPlaylist(ctx, c, uid, a.Playlist)
		if err != nil {
			return nil, err
		}
		entries, err := playlistEntries(ctx, c, uid, emby.Str(pl, "Id"))
		if err != nil {
			return nil, err
		}
		want := map[string]bool{}
		for _, id := range ids {
			want[id] = true
		}
		var entryIDs, names []string
		for _, e := range entries {
			if want[emby.Str(e, "Id")] {
				if eid := emby.Str(e, "PlaylistItemId"); eid != "" {
					entryIDs = append(entryIDs, eid)
					names = append(names, emby.Str(e, "Name"))
				}
			}
		}
		out := Obj{"playlist": emby.Str(pl, "Name"), "id": emby.Str(pl, "Id")}
		if len(entryIDs) == 0 {
			out["done"], out["removed"] = true, 0
			out["note"] = "None of those items are in the playlist."
			return out, nil
		}
		if err := cl.Post(ctx, "/Playlists/"+emby.Str(pl, "Id")+"/Items/Delete", url.Values{"EntryIds": {strings.Join(entryIDs, ",")}}, nil, nil); err != nil {
			return nil, err
		}
		out["done"], out["removed"], out["items"] = true, len(entryIDs), names
		return out, nil
	})

	add(c, Def{
		Toolset: "playlists", Name: "emby_delete_playlist", Title: "Delete a playlist",
		Description: "Delete a playlist. The items in it are not touched. Needs confirm: true.",
		Gate:        Delete, Destructive: true,
	}, func(ctx context.Context, a struct {
		Playlist string `json:"playlist" jsonschema:"The playlist's ID or exact name"`
		confirmArgs
		userArgs
	}) (any, error) {
		uid, err := cl.UserID(ctx, a.User)
		if err != nil {
			return nil, err
		}
		pl, err := findPlaylist(ctx, c, uid, a.Playlist)
		if err != nil {
			return nil, err
		}
		return deleteContainer(ctx, c, pl, a.Confirm)
	})
}
