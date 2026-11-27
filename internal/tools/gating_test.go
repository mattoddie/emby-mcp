package tools

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mattoddie/emby-mcp/internal/config"
)

var readTools = []string{
	"emby_get_server_info", "emby_list_libraries", "emby_search_items", "emby_get_item", "emby_list_values", "emby_get_similar",
	"emby_list_latest", "emby_list_resume", "emby_list_collections", "emby_get_collection", "emby_list_playlists", "emby_get_playlist",
	"emby_list_users", "emby_list_sessions", "emby_list_activity", "emby_list_scheduled_tasks",
}

var writeTools = []string{
	"emby_add_tags", "emby_remove_tags", "emby_rename_tag", "emby_update_item", "emby_refresh_item",
	"emby_create_collection", "emby_add_to_collection", "emby_remove_from_collection",
	"emby_create_playlist", "emby_add_to_playlist", "emby_remove_from_playlist",
	"emby_set_played", "emby_set_favorite", "emby_run_scheduled_task", "emby_scan_library",
}

var deleteTools = []string{"emby_delete_collection", "emby_delete_playlist"}

func sorted(m map[string]Info, gate Gate) []string {
	var out []string
	for name, info := range m {
		if info.Gate == gate && info.Toolset != "raw" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func same(a, b []string) bool {
	a, b = append([]string{}, a...), append([]string{}, b...)
	sort.Strings(a)
	sort.Strings(b)
	return strings.Join(a, ",") == strings.Join(b, ",")
}

// Every tool has the gate it should: a tool that changes something must not
// be registered on a read-only server.
func TestGates(t *testing.T) {
	reg := map[string]Info{}
	NewServer(nil, config.AllTools(), reg)
	if got := sorted(reg, Read); !same(got, readTools) {
		t.Errorf("read tools:\n got %v\nwant %v", got, readTools)
	}
	if got := sorted(reg, Write); !same(got, writeTools) {
		t.Errorf("write tools:\n got %v\nwant %v", got, writeTools)
	}
	if got := sorted(reg, Delete); !same(got, deleteTools) {
		t.Errorf("delete tools:\n got %v\nwant %v", got, deleteTools)
	}

	ro := map[string]Info{}
	NewServer(nil, readOnly(), ro)
	for name, info := range ro {
		if info.Gate != Read {
			t.Errorf("%s is registered read-only", name)
		}
	}
	w := map[string]Info{}
	NewServer(nil, writable(), w)
	for _, name := range deleteTools {
		if _, ok := w[name]; ok {
			t.Errorf("%s is registered without EMBY_ALLOW_DELETES", name)
		}
	}
}

func TestToolsetsAndDenyList(t *testing.T) {
	opts := config.AllTools()
	opts.Toolsets = map[string]bool{"library": true}
	opts.DenyTools = map[string]bool{"emby_list_resume": true}
	reg := map[string]Info{}
	NewServer(nil, opts, reg)
	for name, info := range reg {
		if info.Toolset != "library" {
			t.Errorf("%s (%s) registered with only library enabled", name, info.Toolset)
		}
	}
	if _, ok := reg["emby_list_resume"]; ok {
		t.Error("a denied tool was registered")
	}
	if _, ok := reg["emby_search_items"]; !ok {
		t.Error("emby_search_items missing")
	}
}

// Annotations follow the gates, and every tool that changes something takes dry_run.
func TestAnnotations(t *testing.T) {
	h := newHarness(t, config.AllTools())
	for name, tool := range h.tools() {
		a := tool.Annotations
		if a == nil || a.Title == "" {
			t.Errorf("%s: no annotations or title", name)
			continue
		}
		isRead := contains(readTools, name)
		if isRead != a.ReadOnlyHint && name != "emby_api_request" {
			t.Errorf("%s: readOnlyHint %v", name, a.ReadOnlyHint)
		}
		props, _ := tool.InputSchema.(map[string]any)["properties"].(map[string]any)
		_, hasDry := props["dry_run"]
		if !isRead && !hasDry {
			t.Errorf("%s changes things but has no dry_run", name)
		}
		if isRead && hasDry {
			t.Errorf("%s is read-only but has dry_run", name)
		}
		if contains(deleteTools, name) && (a.DestructiveHint == nil || !*a.DestructiveHint) {
			t.Errorf("%s should be destructive", name)
		}
		if len(tool.Description) < 40 {
			t.Errorf("%s: description too short", name)
		}
	}
}

func TestInstructions(t *testing.T) {
	if s := instructions(readOnly()); !strings.Contains(s, "read-only") || strings.Contains(s, "dry_run") {
		t.Errorf("read-only instructions: %s", s)
	}
	if s := instructions(config.AllTools()); !strings.Contains(s, "dry_run") || !strings.Contains(s, "never media files") {
		t.Errorf("write instructions: %s", s)
	}
}

// Every argument has a description, including fields of nested objects and array items.
func TestSchemasDescribeEveryArgument(t *testing.T) {
	h := newHarness(t, config.AllTools())
	for name, tool := range h.tools() {
		schema, _ := tool.InputSchema.(map[string]any)
		checkArgDescriptions(t, name, schema)
	}
}

func checkArgDescriptions(t *testing.T, path string, schema map[string]any) {
	t.Helper()
	if items, ok := schema["items"].(map[string]any); ok {
		checkArgDescriptions(t, path+"[]", items)
	}
	props, _ := schema["properties"].(map[string]any)
	for arg, p := range props {
		prop, _ := p.(map[string]any)
		if d, _ := prop["description"].(string); d == "" {
			t.Errorf("%s.%s has no description", path, arg)
		}
		checkArgDescriptions(t, path+"."+arg, prop)
	}
}

// A description that names a tool the client can't see is a dead end.
func TestDescriptionsNameOnlyRegisteredTools(t *testing.T) {
	toolRe := regexp.MustCompile(`\bemby_[a-z_]+`)
	deletes := config.AllTools()
	for label, o := range map[string]config.Tools{"read-only": readOnly(), "writes": writable(), "deletes": deletes} {
		h := newHarness(t, o)
		tools := h.tools()
		for name, tool := range tools {
			schema, _ := json.Marshal(tool.InputSchema)
			for _, ref := range toolRe.FindAllString(tool.Description+string(schema), -1) {
				if _, ok := tools[ref]; !ok {
					t.Errorf("%s: %s mentions %s, which isn't registered", label, name, ref)
				}
			}
		}
	}
}
