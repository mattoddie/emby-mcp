package emby

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mattoddie/emby-mcp/internal/config"
)

func client(t *testing.T, url string, mod ...func(*config.Emby)) *Client {
	t.Helper()
	cfg := config.Emby{URL: url, APIKey: "k3y", VerifySSL: true, Timeout: 5 * time.Second}
	for _, m := range mod {
		m(&cfg)
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRequestShape(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	var out Obj
	if err := client(t, srv.URL).Get(context.Background(), "/Items", map[string][]string{"Ids": {"1,2"}}, &out); err != nil {
		t.Fatal(err)
	}
	if got.URL.Path != "/emby/Items" || got.URL.Query().Get("Ids") != "1,2" || got.Header.Get("X-Emby-Token") != "k3y" || !strings.HasPrefix(got.Header.Get("User-Agent"), "emby-mcp") {
		t.Errorf("request: %s %v %v", got.URL, got.Header, out)
	}
	if strings.Contains(got.URL.RawQuery, "k3y") {
		t.Error("API key sent in the URL")
	}
}

func TestErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/emby/auth":
			http.Error(w, "Access token is invalid or expired.", 401)
		case "/emby/busy":
			w.WriteHeader(503)
		case "/emby/redirect":
			http.Redirect(w, r, "https://elsewhere.example/", http.StatusFound)
		default:
			http.Error(w, "Item not found", 404)
		}
	}))
	defer srv.Close()
	c := client(t, srv.URL)
	ctx := context.Background()
	err := c.Get(ctx, "/auth", nil, nil)
	if StatusOf(err) != 401 || !strings.Contains(err.Error(), "EMBY_API_KEY") || !strings.Contains(err.Error(), "invalid or expired") {
		t.Errorf("401: %v", err)
	}
	if err := c.Get(ctx, "/redirect", nil, nil); err == nil || !strings.Contains(err.Error(), "isn't followed") {
		t.Errorf("redirect: %v", err)
	}
	// A GET is retried after a 503; a POST isn't.
	calls.Store(0)
	_ = c.Get(ctx, "/busy", nil, nil)
	if calls.Load() != 4 {
		t.Errorf("GET retries: %d", calls.Load())
	}
	calls.Store(0)
	_ = c.Post(ctx, "/busy", nil, nil, nil)
	if calls.Load() != 1 {
		t.Errorf("POST retried: %d", calls.Load())
	}
	if _, err := c.Item(ctx, "u", "123"); err == nil || !strings.Contains(err.Error(), "no item with ID 123") {
		t.Errorf("missing item: %v", err)
	}
	if !LooksLikeID("12345") || !LooksLikeID("0f1e2d3c4b5a69788796a5b4c3d2e1f0") || LooksLikeID("Aladdin") || LooksLikeID("1917x") {
		t.Error("LooksLikeID")
	}
	if err := CheckID("item", "1/../2"); err == nil {
		t.Error("bad ID accepted")
	}
	unreachable := client(t, "http://127.0.0.1:1")
	if err := unreachable.Get(ctx, "/x", nil, nil); err == nil || !strings.Contains(err.Error(), "could not reach Emby") {
		t.Errorf("unreachable: %v", err)
	}
}

func TestResponseCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`"` + strings.Repeat("x", 2048) + `"`))
	}))
	defer srv.Close()
	old := maxResponseBytes
	maxResponseBytes = 1024
	defer func() { maxResponseBytes = old }()
	var out any
	if err := client(t, srv.URL).Get(context.Background(), "/x", nil, &out); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("cap: %v", err)
	}
}

func TestDryRunRecords(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			posts.Add(1)
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := client(t, srv.URL)
	ctx, d := WithDryRun(context.Background())
	SetCurrent(ctx, Obj{"a": 1})
	if err := c.Post(ctx, "/Items/1/Tags/Add", map[string][]string{"x": {"y"}}, Obj{"Tags": []any{}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, "/System/Info", nil, nil); err != nil {
		t.Fatal(err)
	}
	reqs := d.Requests()
	if posts.Load() != 0 || len(reqs) != 1 || reqs[0].Path != "/Items/1/Tags/Add" || reqs[0].Query != "x=y" || reqs[0].Current == nil {
		t.Errorf("dry run: %d posts, %+v", posts.Load(), reqs)
	}
}

func TestTLSPinning(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }))
	defer srv.Close()
	fp := Fingerprint(srv.Certificate().Raw)
	ctx := context.Background()
	if err := client(t, srv.URL, func(c *config.Emby) { c.TLSFingerprints = []string{fp} }).Get(ctx, "/x", nil, nil); err != nil {
		t.Errorf("pinned: %v", err)
	}
	wrong := strings.Repeat("AB:", 31) + "AB"
	err := client(t, srv.URL, func(c *config.Emby) { c.TLSFingerprints = []string{wrong} }).Get(ctx, "/x", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "doesn't match EMBY_TLS_FINGERPRINT") {
		t.Errorf("wrong pin: %v", err)
	}
	if err := client(t, srv.URL).Get(ctx, "/x", nil, nil); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Errorf("unverified self-signed: %v", err)
	}
	if err := client(t, srv.URL, func(c *config.Emby) { c.VerifySSL = false }).Get(ctx, "/x", nil, nil); err != nil {
		t.Errorf("verify off: %v", err)
	}
	_ = tls.VersionTLS12
}

func TestFormat(t *testing.T) {
	item := Obj{"Id": "1", "Name": "Heat", "Type": "Movie", "ProductionYear": 1995.0, "RunTimeTicks": 170 * 600_000_000.0, "CommunityRating": 8.2999,
		"TagItems": []any{Obj{"Name": "Heist"}}, "Genres": []any{"Crime"}, "UserData": Obj{"Played": false, "PlayedPercentage": 41.6},
		"Overview": strings.Repeat("word ", 200), "MediaSources": []any{Obj{"Container": "mkv", "Size": 12.5e9, "MediaStreams": []any{
			Obj{"Type": "Video", "DisplayTitle": "4K HEVC", "Codec": "hevc", "Width": 3840.0, "Height": 2160.0, "VideoRange": "HDR10"},
			Obj{"Type": "Audio", "DisplayTitle": "English DTS-HD MA 5.1"}, Obj{"Type": "Subtitle", "DisplayTitle": "English (SRT)"}}}}}
	s := Summary(item, true)
	if s["year"] != 1995 || s["minutes"] != 170 || s["community_rating"] != 8.3 || s["progress_percent"] != 42 || len(s["overview"].(string)) > 410 {
		t.Errorf("summary: %v", s)
	}
	d := Detail(item)
	m := d["media"].(Obj)
	if m["resolution"] != "3840x2160" || m["hdr"] != "HDR10" || m["size_gb"] != 12.5 {
		t.Errorf("media: %v", m)
	}
}
