package tools

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/mattoddie/emby-mcp/internal/emby"
)

func registerUsers(c *Context) {
	cl := c.Client

	add(c, Def{
		Toolset: "users", Name: "emby_list_users", Title: "List users",
		Description: "List the Emby users: name, ID, whether each is an administrator, disabled or hidden, when they last signed in and were last active, and their parental control limit. Shows which one the tools use by default. Never shows passwords.",
	}, func(ctx context.Context, _ NoArgs) (any, error) {
		users, err := cl.Users(ctx)
		if err != nil {
			return nil, err
		}
		def, _ := cl.User(ctx, "")
		var out []Obj
		for _, u := range users {
			p, _ := u["Policy"].(Obj)
			s := Obj{"name": emby.Str(u, "Name"), "id": emby.Str(u, "Id"), "administrator": emby.Bool(p, "IsAdministrator")}
			if emby.Bool(p, "IsDisabled") {
				s["disabled"] = true
			}
			if emby.Bool(p, "IsHidden") {
				s["hidden"] = true
			}
			if v := emby.Str(u, "LastLoginDate"); v != "" {
				s["last_login"] = v
			}
			if v := emby.Str(u, "LastActivityDate"); v != "" {
				s["last_active"] = v
			}
			if n, ok := emby.Num(p, "MaxParentalRating"); ok && n > 0 {
				s["max_parental_rating_value"] = int(n)
			}
			if tags := emby.Strs(p["BlockedTags"]); len(tags) > 0 {
				s["blocked_tags"] = tags
			}
			if def != nil && emby.Str(def, "Id") == emby.Str(u, "Id") {
				s["default"] = true
			}
			out = append(out, s)
		}
		return listResult(out), nil
	})

	add(c, Def{
		Toolset: "users", Name: "emby_set_played", Title: "Mark played or unplayed",
		Description: "Mark items as watched or unwatched for a user (up to 500 per call). Marking a series or season marks all its episodes. Watched state is per user.",
		Gate:        Write, Idempotent: true,
	}, func(ctx context.Context, a struct {
		ItemIDs []string `json:"item_ids" jsonschema:"IDs of the items"`
		Played  bool     `json:"played" jsonschema:"true to mark watched, false to mark unwatched"`
		Date    string   `json:"date,omitempty" jsonschema:"When it was watched, YYYY-MM-DD (default now; only with played true)"`
		userArgs
	}) (any, error) {
		ids, err := cleanIDs(a.ItemIDs, "item")
		if err != nil {
			return nil, err
		}
		user, err := cl.User(ctx, a.User)
		if err != nil {
			return nil, err
		}
		uid := emby.Str(user, "Id")
		q := url.Values{}
		if a.Played && strings.TrimSpace(a.Date) != "" {
			t, err := time.Parse("2006-01-02", strings.TrimSpace(a.Date))
			if err != nil {
				return nil, err
			}
			q.Set("DatePlayed", t.Format("20060102150405"))
		}
		return userDataChange(ctx, c, uid, ids, func(id string, ud Obj) (bool, error) {
			if emby.Bool(ud, "Played") == a.Played {
				return false, nil
			}
			path := "/Users/" + uid + "/PlayedItems/" + id
			if !a.Played {
				path += "/Delete"
			}
			return true, cl.Post(ctx, path, q, nil, nil)
		}, map[bool]string{true: "marked played", false: "marked unplayed"}[a.Played], emby.Str(user, "Name"))
	})

	add(c, Def{
		Toolset: "users", Name: "emby_set_favorite", Title: "Add or remove favourites",
		Description: "Add items to a user's favourites, or take them out (up to 500 per call). Favourites are per user.",
		Gate:        Write, Idempotent: true,
	}, func(ctx context.Context, a struct {
		ItemIDs  []string `json:"item_ids" jsonschema:"IDs of the items"`
		Favorite bool     `json:"favorite" jsonschema:"true to add to favourites, false to remove"`
		userArgs
	}) (any, error) {
		ids, err := cleanIDs(a.ItemIDs, "item")
		if err != nil {
			return nil, err
		}
		user, err := cl.User(ctx, a.User)
		if err != nil {
			return nil, err
		}
		uid := emby.Str(user, "Id")
		return userDataChange(ctx, c, uid, ids, func(id string, ud Obj) (bool, error) {
			if emby.Bool(ud, "IsFavorite") == a.Favorite {
				return false, nil
			}
			path := "/Users/" + uid + "/FavoriteItems/" + id
			if !a.Favorite {
				path += "/Delete"
			}
			return true, cl.Post(ctx, path, nil, nil, nil)
		}, map[bool]string{true: "added to favourites", false: "removed from favourites"}[a.Favorite], emby.Str(user, "Name"))
	})
}

// userDataChange applies fn to each item with its current user data.
func userDataChange(ctx context.Context, c *Context, uid string, ids []string, fn func(id string, ud Obj) (bool, error), verb, userName string) (any, error) {
	found, err := c.Client.ItemsByID(ctx, uid, ids, "UserData")
	if err != nil {
		return nil, err
	}
	var missing []string
	var present []string
	for _, id := range ids {
		if _, ok := found[id]; ok {
			present = append(present, id)
		} else {
			missing = append(missing, id)
		}
	}
	changed := make([]bool, len(present))
	idx := map[string]int{}
	for i, id := range present {
		idx[id] = i
	}
	errs := forEach(ctx, present, func(id string) error {
		ud, _ := found[id]["UserData"].(Obj)
		if emby.IsDryRun(ctx) {
			emby.SetCurrent(ctx, ud)
		}
		ch, err := fn(id, ud)
		changed[idx[id]] = ch
		return err
	})
	var done []itemResult
	var failed []itemResult
	unchanged := 0
	for i, id := range present {
		r := itemResult{ID: id, Name: emby.Str(found[id], "Name")}
		switch {
		case errs[i] != nil:
			r.Error = errs[i].Error()
			failed = append(failed, r)
		case changed[i]:
			r.Change = []string{verb}
			done = append(done, r)
		default:
			unchanged++
		}
	}
	out := Obj{"done": len(failed) == 0 && len(missing) == 0, "user": userName, "changed": len(done), "unchanged": unchanged}
	if len(done) > 0 && len(done) <= 100 {
		out["items"] = done
	}
	if len(failed) > 0 {
		out["failed"] = failed
	}
	if len(missing) > 0 {
		out["not_found"] = missing
	}
	return out, nil
}
