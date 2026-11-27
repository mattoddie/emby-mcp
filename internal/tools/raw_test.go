package tools

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mattoddie/emby-mcp/internal/config"
)

func TestRawGates(t *testing.T) {
	h := newHarness(t, readOnly())
	out := h.ok("emby_api_request", map[string]any{"path": "/emby/System/Info"})
	if out["ServerName"] != "Test Emby" {
		t.Errorf("GET: %v", out)
	}
	h.fails("emby_api_request", map[string]any{"method": "POST", "path": "/Library/Refresh"}, "writes are disabled")
	for _, p := range []string{"/Items/../System", "/Items/%2e%2e/x", "/Items//x", "Items", "/Items?x=1", "/Items/%252e", "/Items/a\nb"} {
		h.fails("emby_api_request", map[string]any{"path": p}, "path")
	}

	w := newHarness(t, writable())
	w.ok("emby_api_request", map[string]any{"method": "POST", "path": "/Library/Refresh"})
	for _, p := range []string{"/System/Restart", "/Auth/Keys", "/Users/a1b2c3d4/Password", "/Library/VirtualFolders/Paths"} {
		w.fails("emby_api_request", map[string]any{"method": "POST", "path": p}, "EMBY_ALLOW_DELETES")
	}
	w.fails("emby_api_request", map[string]any{"method": "DELETE", "path": "/Collections/301/Items", "query": map[string]any{"Ids": "101"}}, "EMBY_ALLOW_DELETES")

	all := newHarness(t, config.AllTools())
	all.ok("emby_api_request", map[string]any{"method": "POST", "path": "/Collections/301/Items/Delete", "query": map[string]any{"Ids": "101"}})
	// Never items, which would delete media files, even with every switch on.
	for _, p := range []string{"/Items/101/Delete", "/emby/Items/Delete", "/items/101/delete"} {
		all.fails("emby_api_request", map[string]any{"method": "POST", "path": p, "query": map[string]any{"Ids": "101"}}, "never deletes library items")
	}
	all.fails("emby_api_request", map[string]any{"method": "DELETE", "path": "/Items/101"}, "never deletes library items")
	if _, ok := all.mock.Items["101"]; !ok {
		t.Error("the raw tool deleted an item")
	}
}

func TestRedactSecrets(t *testing.T) {
	in := map[string]any{"Items": []any{map[string]any{"AccessToken": "abc", "AppName": "Claude", "HasPassword": true, "Password": "", "Pin": "1234", "PathMappings": []any{"x"}}}}
	b, _ := json.Marshal(redactSecrets(in))
	s := string(b)
	if strings.Contains(s, "abc") || strings.Contains(s, "1234") || !strings.Contains(s, `"HasPassword":true`) || !strings.Contains(s, "PathMappings") || !strings.Contains(s, "Claude") {
		t.Errorf("redacted: %s", s)
	}
}

func TestAuditLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	opts := writable()
	opts.AuditLog = path
	h := newHarness(t, opts)
	h.ok("emby_list_users", nil)
	h.ok("emby_add_tags", map[string]any{"item_ids": []string{"103"}, "tags": []string{"Heist"}})
	h.call("emby_add_tags", map[string]any{"item_ids": []string{"bad id"}, "tags": []string{"x"}})
	h.ok("emby_api_request", map[string]any{"path": "/System/Info"})
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var entries []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e map[string]any
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, e)
	}
	if len(entries) != 2 || entries[0]["tool"] != "emby_add_tags" || entries[0]["outcome"] != "ok" || entries[1]["outcome"] != "error" {
		t.Errorf("entries: %v", entries)
	}
}

func TestPrompts(t *testing.T) {
	h := newHarness(t, readOnly())
	var got []string
	for p, err := range h.session.Prompts(t.Context(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, p.Name)
	}
	// Read-only: no prompt that relies on a write tool.
	sort.Strings(got)
	if strings.Join(got, ",") != "library_report,movie_night,suggest_collections,tidy_metadata" {
		t.Errorf("read-only prompts: %v", got)
	}
	w := newHarness(t, writable())
	n := 0
	for p, err := range w.session.Prompts(t.Context(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		n++
		_ = p
	}
	if n != len(prompts) {
		t.Errorf("writable prompts: %d of %d", n, len(prompts))
	}
}
