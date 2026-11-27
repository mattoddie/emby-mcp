package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mattoddie/emby-mcp/internal/config"
	"github.com/mattoddie/emby-mcp/internal/emby"
	"github.com/mattoddie/emby-mcp/internal/emby/embytest"
)

type harness struct {
	t       *testing.T
	mock    *embytest.Server
	session *mcp.ClientSession
}

func readOnly() config.Tools {
	t := config.AllTools()
	t.AllowWrites, t.AllowDeletes = false, false
	return t
}

func writable() config.Tools {
	t := config.AllTools()
	t.AllowDeletes = false
	return t
}

func newHarness(t *testing.T, opts config.Tools) *harness {
	t.Helper()
	mock := embytest.New(t)
	client, err := emby.New(config.Emby{URL: mock.URL, APIKey: embytest.APIKey, VerifySSL: true, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(client, opts, nil)
	st, ct := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := server.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return &harness{t: t, mock: mock, session: session}
}

// call runs a tool and returns its text and whether it failed.
func (h *harness) call(name string, args map[string]any) (string, bool) {
	h.t.Helper()
	res, err := h.session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		h.t.Fatalf("%s: %v", name, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String(), res.IsError
}

// ok runs a tool that must succeed and decodes its JSON result.
func (h *harness) ok(name string, args map[string]any) map[string]any {
	h.t.Helper()
	text, isErr := h.call(name, args)
	if isErr {
		h.t.Fatalf("%s failed: %s", name, text)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		h.t.Fatalf("%s: not JSON: %s", name, text)
	}
	return out
}

// fails runs a tool that must fail with an error containing want.
func (h *harness) fails(name string, args map[string]any, want string) {
	h.t.Helper()
	text, isErr := h.call(name, args)
	if !isErr || !strings.Contains(text, want) {
		h.t.Errorf("%s: got (error=%v) %s, want an error containing %q", name, isErr, text, want)
	}
}

func (h *harness) tools() map[string]*mcp.Tool {
	h.t.Helper()
	out := map[string]*mcp.Tool{}
	for tool, err := range h.session.Tools(context.Background(), nil) {
		if err != nil {
			h.t.Fatal(err)
		}
		out[tool.Name] = tool
	}
	return out
}

func items(v any) []map[string]any {
	list, _ := v.([]any)
	var out []map[string]any
	for _, x := range list {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func names(v any) []string {
	var out []string
	for _, it := range items(v) {
		out = append(out, it["name"].(string))
	}
	return out
}
