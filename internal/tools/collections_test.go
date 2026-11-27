package tools

import (
	"strings"
	"testing"

	"github.com/mattoddie/emby-mcp/internal/config"
	"github.com/mattoddie/emby-mcp/internal/emby/embytest"
)

func TestCollections(t *testing.T) {
	h := newHarness(t, config.AllTools())
	out := h.ok("emby_list_collections", nil)
	if got := names(out["items"]); len(got) != 1 || got[0] != "Christmas Classics" {
		t.Errorf("list: %v", got)
	}
	out = h.ok("emby_get_collection", map[string]any{"collection": "christmas classics"})
	if got := names(out["items"]); strings.Join(got, ",") != "Elf" {
		t.Errorf("members: %v", got)
	}

	out = h.ok("emby_add_to_collection", map[string]any{"collection": embytest.BoxSetID, "item_ids": []string{embytest.ElfID, embytest.HomeAloneID}})
	if out["changed"] != float64(1) || out["already_in"] != float64(1) {
		t.Errorf("add: %v", out)
	}
	if q := h.mock.Calls("POST /Collections/" + embytest.BoxSetID + "/Items")[0].Query; q["Ids"][0] != embytest.HomeAloneID {
		t.Errorf("add sent: %v", q)
	}
	out = h.ok("emby_remove_from_collection", map[string]any{"collection": embytest.BoxSetID, "item_ids": []string{embytest.ElfID, embytest.HeatID}})
	if out["changed"] != float64(1) || out["not_in"] != float64(1) {
		t.Errorf("remove: %v", out)
	}

	out = h.ok("emby_create_collection", map[string]any{"name": "Nolan", "item_ids": []string{embytest.InceptionID}, "overview": "Films by Christopher Nolan."})
	id := out["id"].(string)
	if h.mock.Items[id]["Overview"] != "Films by Christopher Nolan." {
		t.Errorf("overview not set: %v", h.mock.Items[id])
	}
	h.fails("emby_create_collection", map[string]any{"name": "nolan"}, "already a collection")
	h.fails("emby_create_collection", map[string]any{"name": "New", "item_ids": []string{"424242"}}, "don't exist")
	h.fails("emby_add_to_collection", map[string]any{"collection": embytest.ElfID, "item_ids": []string{embytest.HeatID}}, "not a collection")

	h.fails("emby_delete_collection", map[string]any{"collection": id}, "confirm: true")
	h.ok("emby_delete_collection", map[string]any{"collection": id, "confirm": true})
	if _, still := h.mock.Items[id]; still {
		t.Error("collection not deleted")
	}
	if _, ok := h.mock.Items[embytest.InceptionID]; !ok {
		t.Error("deleting a collection deleted its film")
	}
	// Never a film.
	h.fails("emby_delete_collection", map[string]any{"collection": embytest.ElfID, "confirm": true}, "not a collection")
	if len(h.mock.Calls("POST /Items/"+embytest.ElfID+"/Delete")) != 0 {
		t.Error("tried to delete a film")
	}
}

func TestPlaylists(t *testing.T) {
	h := newHarness(t, config.AllTools())
	out := h.ok("emby_get_playlist", map[string]any{"playlist": "Sci-fi marathon"})
	if got := names(out["items"]); strings.Join(got, ",") != "Arrival,Inception" {
		t.Errorf("entries: %v", got)
	}
	if out["playlist"].(map[string]any)["total_minutes"] != float64(264) {
		t.Errorf("minutes: %v", out["playlist"])
	}
	out = h.ok("emby_add_to_playlist", map[string]any{"playlist": embytest.PlaylistID, "item_ids": []string{embytest.ArrivalID, embytest.HeatID}})
	if out["added"] != float64(1) || out["already_in"] != float64(1) {
		t.Errorf("add: %v", out)
	}
	out = h.ok("emby_remove_from_playlist", map[string]any{"playlist": embytest.PlaylistID, "item_ids": []string{embytest.InceptionID}})
	if out["removed"] != float64(1) {
		t.Errorf("remove: %v", out)
	}
	if q := h.mock.Calls("POST /Playlists/" + embytest.PlaylistID + "/Items/Delete")[0].Query; q["EntryIds"][0] != "e2" {
		t.Errorf("entry ids: %v", q)
	}
	out = h.ok("emby_create_playlist", map[string]any{"name": "Christmas Eve", "item_ids": []string{embytest.ElfID, embytest.HomeAloneID}})
	if out["media_type"] != "Video" || out["id"] == nil {
		t.Errorf("create: %v", out)
	}
	h.fails("emby_delete_playlist", map[string]any{"playlist": "Christmas Eve"}, "confirm: true")
	h.ok("emby_delete_playlist", map[string]any{"playlist": "Christmas Eve", "confirm": true})
}

func TestUsersPlayedFavourite(t *testing.T) {
	h := newHarness(t, writable())
	out := h.ok("emby_list_users", nil)
	us := items(out["items"])
	if len(us) != 2 || us[1]["default"] != true || us[0]["blocked_tags"] == nil {
		t.Errorf("users: %v", us)
	}
	out = h.ok("emby_set_played", map[string]any{"item_ids": []string{embytest.HeatID, embytest.ElfID}, "played": true})
	if out["changed"] != float64(1) || out["unchanged"] != float64(1) || !h.mock.Played[embytest.AdminID][embytest.ElfID] {
		t.Errorf("played: %v", out)
	}
	h.ok("emby_set_played", map[string]any{"item_ids": []string{embytest.ElfID}, "played": false, "user": "Guest"})
	if len(h.mock.Calls("POST /Users/"+embytest.GuestID+"/PlayedItems/"+embytest.ElfID+"/Delete")) != 0 {
		t.Error("marked unplayed something the guest hadn't played")
	}
	out = h.ok("emby_set_favorite", map[string]any{"item_ids": []string{embytest.ArrivalID}, "favorite": false})
	if out["changed"] != float64(1) || h.mock.Favorite[embytest.AdminID][embytest.ArrivalID] {
		t.Errorf("favourite: %v", out)
	}
	h.fails("emby_set_played", map[string]any{"item_ids": []string{embytest.ElfID}, "played": true, "date": "yesterday"}, "cannot parse")
}

func TestServerTools(t *testing.T) {
	h := newHarness(t, writable())
	out := h.ok("emby_list_sessions", nil)
	s := items(out["items"])
	if len(s) != 1 || s[0]["playing"].(map[string]any)["name"] != "Heat" {
		t.Fatalf("sessions: %v", s)
	}
	if tr := s[0]["playing"].(map[string]any)["transcoding"].(map[string]any); tr["reasons"].([]any)[0] != "AudioCodecNotSupported" {
		t.Errorf("transcoding: %v", tr)
	}
	if out := h.ok("emby_list_sessions", map[string]any{"include_idle": true}); out["total"] != float64(2) {
		t.Errorf("idle: %v", out)
	}
	out = h.ok("emby_list_activity", map[string]any{"min_severity": "Error"})
	if got := names(out["items"]); len(got) != 1 || !strings.Contains(got[0], "failed") {
		t.Errorf("activity: %v", got)
	}
	out = h.ok("emby_list_scheduled_tasks", nil)
	if items(out["items"])[1]["last_run"].(map[string]any)["error"] != "Disk not ready" {
		t.Errorf("tasks: %v", out)
	}
	h.ok("emby_run_scheduled_task", map[string]any{"task": "refreshlibrary"})
	if len(h.mock.Calls("POST /ScheduledTasks/Running/6330ee8fb4a957f33981f89aa78b030f")) != 1 {
		t.Error("task not started")
	}
	if out := h.ok("emby_run_scheduled_task", map[string]any{"task": "Refresh people"}); !strings.Contains(out["note"].(string), "already running") {
		t.Errorf("running: %v", out)
	}
	h.fails("emby_run_scheduled_task", map[string]any{"task": "Defrag"}, "no scheduled task")
	h.ok("emby_scan_library", nil)
	h.ok("emby_scan_library", map[string]any{"library": "TV"})
	if len(h.mock.Calls("POST /Items/"+embytest.TVLib+"/Refresh")) != 1 {
		t.Error("library scan not sent")
	}
}
