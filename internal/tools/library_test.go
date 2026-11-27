package tools

import (
	"strings"
	"testing"

	"github.com/mattoddie/emby-mcp/internal/emby/embytest"
)

func TestServerInfo(t *testing.T) {
	h := newHarness(t, readOnly())
	out := h.ok("emby_get_server_info", nil)
	srv := out["server"].(map[string]any)
	if srv["name"] != "Test Emby" || srv["version"] != "4.10.2.0" {
		t.Errorf("server: %v", srv)
	}
	// The default user is the first administrator, not the first user.
	if u := out["user"].(map[string]any); u["name"] != "Sam" || u["administrator"] != true {
		t.Errorf("user: %v", u)
	}
	if got := names(out["libraries"]); strings.Join(got, ",") != "Movies,TV" {
		t.Errorf("libraries: %v", got)
	}
	if c := out["counts"].(map[string]any); c["Movie"] != float64(6) || c["Episode"] != nil {
		t.Errorf("counts: %v", c)
	}
	if !strings.Contains(out["changes"].(string), "read-only") {
		t.Errorf("changes: %v", out["changes"])
	}
}

func TestListLibraries(t *testing.T) {
	h := newHarness(t, readOnly())
	out := h.ok("emby_list_libraries", nil)
	libs := items(out["items"])
	if len(libs) != 2 || libs[0]["id"] != embytest.MoviesLib || libs[0]["type"] != "movies" {
		t.Fatalf("libraries: %v", libs)
	}
	opts := libs[0]["options"].(map[string]any)
	if opts["import_collections"] != true || opts["min_collection_items"] != float64(2) || opts["metadata_country"] != "GB" {
		t.Errorf("options: %v", opts)
	}
}

func TestSearch(t *testing.T) {
	h := newHarness(t, readOnly())
	out := h.ok("emby_search_items", map[string]any{"types": []string{"movie"}, "tags": []string{"Christmas"}})
	if got := names(out["items"]); len(got) != 1 || got[0] != "Elf" {
		t.Errorf("by tag: %v", got)
	}
	elf := items(out["items"])[0]
	if elf["year"] != float64(2003) || elf["minutes"] != float64(97) || elf["rating"] != "PG" || elf["id"] != embytest.ElfID {
		t.Errorf("summary: %v", elf)
	}
	if _, has := elf["overview"]; has {
		t.Error("overview without include_overview")
	}

	// Pipe-separated multi-value filters, and the query sent.
	h.ok("emby_search_items", map[string]any{"genres": []string{"Comedy", "Crime"}, "library": "movies", "year_from": 1990, "year_to": 1992, "sort_by": "date_added", "sort_order": "descending"})
	calls := h.mock.Calls("GET /Users/" + embytest.AdminID + "/Items")
	q := calls[len(calls)-1].Query
	if q["Genres"][0] != "Comedy|Crime" || q["ParentId"][0] != embytest.MoviesLib || q["Years"][0] != "1990,1991,1992" || q["SortBy"][0] != "DateCreated,SortName" || q["SortOrder"][0] != "Descending" {
		t.Errorf("query: %v", q)
	}

	// Untagged, with paging over the matches.
	out = h.ok("emby_search_items", map[string]any{"types": []string{"Movie"}, "untagged": true, "limit": 1, "include_overview": true})
	if out["total"] != float64(3) || out["returned"] != float64(1) || out["next_offset"] != float64(1) {
		t.Errorf("untagged paging: %v", out)
	}
	if first := items(out["items"])[0]; first["name"] != "Home Alone" || first["overview"] == nil {
		t.Errorf("untagged first: %v", first)
	}

	// Watched state is per user.
	out = h.ok("emby_search_items", map[string]any{"types": []string{"Movie"}, "played": true})
	if got := names(out["items"]); len(got) != 1 || got[0] != "Heat" {
		t.Errorf("played for Sam: %v", got)
	}
	out = h.ok("emby_search_items", map[string]any{"types": []string{"Movie"}, "played": true, "user": "guest"})
	if out["total"] != float64(0) {
		t.Errorf("played for Guest: %v", out)
	}

	out = h.ok("emby_search_items", map[string]any{"missing": []string{"tmdb_id"}, "types": []string{"Movie"}})
	if got := names(out["items"]); len(got) != 1 || got[0] != "home_video_2019" {
		t.Errorf("missing tmdb: %v", got)
	}

	h.fails("emby_search_items", map[string]any{"types": []string{"Film"}}, "unknown item type")
	h.fails("emby_search_items", map[string]any{"missing": []string{"poster"}}, "missing must be")
	h.fails("emby_search_items", map[string]any{"library": "Music"}, `no library called "Music"`)
	h.fails("emby_search_items", map[string]any{"ids": []string{"../System"}}, "not an Emby ID")
	h.fails("emby_search_items", map[string]any{"user": "nobody"}, `no Emby user called "nobody"`)
}

func TestGetItem(t *testing.T) {
	h := newHarness(t, readOnly())
	out := h.ok("emby_get_item", map[string]any{"item": embytest.ElfID})
	if out["name"] != "Elf" || out["overview"] == nil || out["path"] == nil {
		t.Errorf("detail: %v", out)
	}
	people := out["people"].(map[string]any)
	if people["directors"].([]any)[0] != "A Director" || people["cast"].([]any)[0] != "An Actor (Lead)" {
		t.Errorf("people: %v", people)
	}
	if got := out["collections"]; got == nil || got.([]any)[0] != "Christmas Classics" {
		t.Errorf("collections: %v", got)
	}
	// By title.
	if out := h.ok("emby_get_item", map[string]any{"item": "inception"}); out["id"] != embytest.InceptionID {
		t.Errorf("by title: %v", out["id"])
	}
	h.fails("emby_get_item", map[string]any{"item": "Nonexistent Film"}, "no item called")
	h.fails("emby_get_item", map[string]any{"item": "home"}, "more than one") // Home Alone and home_video_2019
}

func TestListValues(t *testing.T) {
	h := newHarness(t, readOnly())
	out := h.ok("emby_list_values", map[string]any{"kind": "tags", "with_counts": true, "types": []string{"Movie"}})
	list := items(out["items"])
	// "Mind-bending" and "mind-bending" count as one tag.
	if len(list) != 3 || list[0]["name"] != "Mind-bending" || list[0]["items"] != float64(2) {
		t.Errorf("tag counts: %v", list)
	}
	out = h.ok("emby_list_values", map[string]any{"kind": "genres", "filter": "sci"})
	if got := names(out["items"]); len(got) != 1 || got[0] != "Science Fiction" {
		t.Errorf("genres: %v", got)
	}
	out = h.ok("emby_list_values", map[string]any{"kind": "ratings"})
	if out["total"] != float64(3) {
		t.Errorf("ratings: %v", out)
	}
}

func TestSimilarLatestResume(t *testing.T) {
	h := newHarness(t, readOnly())
	out := h.ok("emby_get_similar", map[string]any{"item": "Arrival"})
	if got := names(out["items"]); len(got) != 1 || got[0] != "Inception" {
		t.Errorf("similar: %v", got)
	}
	out = h.ok("emby_list_latest", map[string]any{"types": []string{"Movie"}, "limit": 3})
	if out["total"] == float64(0) {
		t.Errorf("latest: %v", out)
	}
	h.ok("emby_list_resume", nil)
}
