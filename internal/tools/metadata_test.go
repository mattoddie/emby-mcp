package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mattoddie/emby-mcp/internal/emby/embytest"
)

func TestAddAndRemoveTags(t *testing.T) {
	h := newHarness(t, writable())
	out := h.ok("emby_add_tags", map[string]any{"item_ids": []string{embytest.ElfID, embytest.HomeAloneID, embytest.HomeAloneID}, "tags": []string{"christmas", "Family night"}})
	// Elf already has Christmas (any case), so it only gets the new tag.
	if out["changed"] != float64(2) {
		t.Errorf("changed: %v", out)
	}
	if got := h.mock.TagNames(embytest.HomeAloneID); strings.Join(got, ",") != "christmas,Family night" {
		t.Errorf("Home Alone tags: %v", got)
	}
	if got := h.mock.TagNames(embytest.ElfID); strings.Join(got, ",") != "Christmas,Family night" {
		t.Errorf("Elf tags: %v", got)
	}
	adds := h.mock.Calls("POST /Items/" + embytest.ElfID + "/Tags/Add")
	if len(adds) != 1 || !strings.Contains(adds[0].Body, `"Family night"`) || strings.Contains(adds[0].Body, "hristmas") {
		t.Errorf("Elf add body: %v", adds)
	}

	// Doing it again changes nothing.
	out = h.ok("emby_add_tags", map[string]any{"item_ids": []string{embytest.ElfID}, "tags": []string{"Family night"}})
	if out["changed"] != float64(0) || out["already_tagged"] != float64(1) {
		t.Errorf("repeat: %v", out)
	}

	// Removing uses the item's own spelling.
	out = h.ok("emby_remove_tags", map[string]any{"item_ids": []string{embytest.InceptionID, embytest.HeatID}, "tags": []string{"MIND-BENDING"}})
	if out["changed"] != float64(1) || out["already_untagged"] != float64(1) {
		t.Errorf("remove: %v", out)
	}
	if got := h.mock.TagNames(embytest.InceptionID); strings.Join(got, ",") != "Heist" {
		t.Errorf("Inception tags: %v", got)
	}

	h.fails("emby_add_tags", map[string]any{"item_ids": []string{}, "tags": []string{"x"}}, "at least one item")
	h.fails("emby_add_tags", map[string]any{"item_ids": []string{"1"}, "tags": []string{" "}}, "at least one tag")
	h.fails("emby_add_tags", map[string]any{"item_ids": []string{"1/../2"}, "tags": []string{"x"}}, "not an Emby ID")
	out = h.ok("emby_add_tags", map[string]any{"item_ids": []string{"99999"}, "tags": []string{"x"}})
	if out["done"] != false || out["not_found"] == nil {
		t.Errorf("missing item: %v", out)
	}
}

func TestAddTagsLock(t *testing.T) {
	h := newHarness(t, writable())
	h.ok("emby_add_tags", map[string]any{"item_ids": []string{embytest.HeatID}, "tags": []string{"Heist"}, "lock": true})
	locked := h.mock.Items[embytest.HeatID]["LockedFields"].([]any)
	if len(locked) != 1 || locked[0] != "Tags" {
		t.Errorf("locked: %v", locked)
	}
	// The save kept everything else.
	if got := h.mock.TagNames(embytest.HeatID); len(got) != 1 || got[0] != "Heist" {
		t.Errorf("tags after lock: %v", got)
	}
	if h.mock.Items[embytest.HeatID]["Overview"] == nil || len(h.mock.Items[embytest.HeatID]["People"].([]any)) != 2 {
		t.Error("locking lost metadata")
	}
}

func TestDryRunTags(t *testing.T) {
	h := newHarness(t, writable())
	before := h.mock.Writes()
	out := h.ok("emby_add_tags", map[string]any{"item_ids": []string{embytest.HeatID, embytest.ArrivalID}, "tags": []string{"Must see"}, "dry_run": true})
	if h.mock.Writes() != before {
		t.Fatal("a dry run sent a change")
	}
	if out["dry_run"] != true || out["would_send"].(map[string]any)["path"] != "/Items/"+embytest.HeatID+"/Tags/Add" {
		t.Errorf("would_send: %v", out)
	}
	if !strings.Contains(out["note"].(string), "1 more request") {
		t.Errorf("note: %v", out["note"])
	}
	plan := out["plan"].(map[string]any)
	if plan["changed"] != float64(2) {
		t.Errorf("plan: %v", plan)
	}
}

func TestRenameTag(t *testing.T) {
	h := newHarness(t, writable())
	// Emby treats tags case-insensitively, so both spellings are renamed.
	out := h.ok("emby_rename_tag", map[string]any{"from": "Mind-bending", "to": "Cerebral"})
	if out["items"] != float64(2) {
		t.Errorf("rename: %v", out)
	}
	if got := h.mock.TagNames(embytest.ArrivalID); strings.Join(got, ",") != "Cerebral" {
		t.Errorf("Arrival: %v", got)
	}
	if got := h.mock.TagNames(embytest.InceptionID); strings.Join(got, ",") != "Heist,Cerebral" {
		t.Errorf("Inception: %v", got)
	}
	// A change of case only.
	h.ok("emby_rename_tag", map[string]any{"from": "Heist", "to": "HEIST"})
	if got := h.mock.TagNames(embytest.InceptionID); strings.Join(got, ",") != "Cerebral,HEIST" {
		t.Errorf("case rename: %v", got)
	}
	out = h.ok("emby_rename_tag", map[string]any{"from": "Nope", "to": "Other"})
	if out["changed"] != float64(0) {
		t.Errorf("unused tag: %v", out)
	}
	h.fails("emby_rename_tag", map[string]any{"from": "A", "to": "A"}, "two different")
}

func TestUpdateItem(t *testing.T) {
	h := newHarness(t, writable())
	out := h.ok("emby_update_item", map[string]any{"item": embytest.HeatID, "name": "Heat (Director's Cut)", "genres": []string{"Crime", "Drama"}, "community_rating": 8.5, "lock_fields": []string{"Cast"}})
	it := h.mock.Items[embytest.HeatID]
	if it["Name"] != "Heat (Director's Cut)" || it["CommunityRating"] != 8.5 {
		t.Errorf("saved: %v %v", it["Name"], it["CommunityRating"])
	}
	locked, _ := json.Marshal(it["LockedFields"])
	if string(locked) != `["Name","Genres","Cast","CommunityRating"]` {
		t.Errorf("locks: %s", locked)
	}
	// Unedited fields survive the save.
	if it["Overview"] == nil || len(it["People"].([]any)) != 2 || it["ProviderIds"].(map[string]any)["Tmdb"] == nil {
		t.Error("the save lost metadata")
	}
	if out["warning"] != nil {
		t.Errorf("unexpected warning: %v", out["warning"])
	}
	changed, _ := json.Marshal(out["changed"])
	if !strings.Contains(string(changed), "Name") || !strings.Contains(string(changed), "LockedFields") {
		t.Errorf("changed: %s", changed)
	}

	// Nothing to change.
	out = h.ok("emby_update_item", map[string]any{"item": embytest.HeatID, "name": "Heat (Director's Cut)"})
	if !strings.Contains(out["note"].(string), "Nothing to change") {
		t.Errorf("no-op: %v", out)
	}

	// A dry run shows the fields that would change, and sends nothing.
	before := h.mock.Writes()
	out = h.ok("emby_update_item", map[string]any{"item": "Arrival", "overview": "New plot.", "lock_edited": false, "dry_run": true})
	if h.mock.Writes() != before {
		t.Fatal("dry run wrote")
	}
	ch := out["changes"].(map[string]any)["changed"].(map[string]any)
	if len(ch) != 1 || ch["Overview"] == nil {
		t.Errorf("dry-run changes: %v", ch)
	}

	// A save that loses data is reported.
	h.mock.DropOnUpdate = "TagItems"
	out = h.ok("emby_update_item", map[string]any{"item": embytest.InceptionID, "tagline": "Your mind is the scene of the crime"})
	if w, _ := out["warning"].(string); !strings.Contains(w, `tag "Heist"`) {
		t.Errorf("lost-data warning: %v", out["warning"])
	}

	// So is cast or crew that Emby re-links by name.
	h.mock.DropOnUpdate, h.mock.RelinkOnUpdate = "", true
	out = h.ok("emby_update_item", map[string]any{"item": embytest.ElfID, "custom_rating": "U"})
	if w, _ := out["warning"].(string); !strings.Contains(w, "A Director (Director)") || !strings.Contains(w, "different person") {
		t.Errorf("relink warning: %v", out["warning"])
	}
	out = h.ok("emby_add_tags", map[string]any{"item_ids": []string{embytest.HeatID}, "tags": []string{"x"}, "lock": true})
	if b, _ := json.Marshal(out); !strings.Contains(string(b), "different person") {
		t.Errorf("relink note on lock: %s", b)
	}
	h.mock.RelinkOnUpdate = false

	h.fails("emby_update_item", map[string]any{"item": embytest.HeatID, "premiere_date": "13/12/1995"}, "YYYY-MM-DD")
	h.fails("emby_update_item", map[string]any{"item": embytest.HeatID, "lock_fields": []string{"Poster"}}, "lock_fields")
	h.fails("emby_update_item", map[string]any{"item": embytest.HeatID, "name": " "}, "can't be empty")
}

func TestRefresh(t *testing.T) {
	h := newHarness(t, writable())
	h.ok("emby_refresh_item", map[string]any{"item_ids": []string{embytest.ElfID}})
	q := h.mock.Calls("POST /Items/" + embytest.ElfID + "/Refresh")[0].Query
	if q["MetadataRefreshMode"][0] != "FullRefresh" || q["ReplaceAllMetadata"][0] != "false" {
		t.Errorf("missing mode: %v", q)
	}
	h.fails("emby_refresh_item", map[string]any{"item_ids": []string{embytest.ElfID}, "mode": "replace_all"}, "confirm: true")
	h.ok("emby_refresh_item", map[string]any{"item_ids": []string{embytest.ElfID}, "mode": "replace_all", "confirm": true})
	calls := h.mock.Calls("POST /Items/" + embytest.ElfID + "/Refresh")
	if q := calls[len(calls)-1].Query; q["ReplaceAllMetadata"][0] != "true" {
		t.Errorf("replace_all: %v", q)
	}
	h.mock.Fail["POST /Items/"+embytest.HeatID+"/Refresh"] = 500
	out := h.ok("emby_refresh_item", map[string]any{"item_ids": []string{embytest.ElfID, embytest.HeatID}})
	if out["done"] != false || out["refreshing"] != float64(1) || len(items(out["failed"])) != 1 {
		t.Errorf("partial failure: %v", out)
	}
}

// Emby leaves collections out of an Ids query unless the request names BoxSet,
// so tagging one used to report it as not found.
func TestAddTagsToCollection(t *testing.T) {
	h := newHarness(t, writable())
	out := h.ok("emby_add_tags", map[string]any{"item_ids": []string{embytest.BoxSetID, embytest.HeatID}, "tags": []string{"kids"}})
	if out["changed"] != float64(2) || out["not_found"] != nil {
		t.Errorf("tagging a collection and a film: %v", out)
	}
	if got := h.mock.TagNames(embytest.BoxSetID); strings.Join(got, ",") != "kids" {
		t.Errorf("collection tags: %v", got)
	}
}
