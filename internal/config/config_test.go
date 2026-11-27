package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// base is the least configuration that starts the server.
func base(extra ...string) map[string]string {
	m := map[string]string{"EMBY_URL": "http://192.0.2.10:8096", "EMBY_API_KEY": "0123456789abcdef"}
	for _, kv := range extra {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return m
}

func TestDefaults(t *testing.T) {
	cfg, err := Load(MapEnv(base()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Transport != "stdio" || cfg.HTTP.Port != 8080 || cfg.HTTP.Host != "127.0.0.1" {
		t.Errorf("transport defaults: %+v %+v", cfg.Transport, cfg.HTTP)
	}
	e := cfg.Emby
	if e.URL != "http://192.0.2.10:8096" || e.APIKey != "0123456789abcdef" || e.User != "" || !e.VerifySSL || e.Timeout != 30*time.Second {
		t.Errorf("emby: %+v", e)
	}
	if cfg.Tools.AllowWrites || cfg.Tools.AllowDeletes || len(cfg.Tools.Toolsets) != len(Toolsets)-1 || cfg.Tools.Toolsets["raw"] {
		t.Errorf("tools: %+v", cfg.Tools)
	}
	if !strings.Contains(e.Insecure(), "plain http") {
		t.Errorf("http to a LAN address should warn: %q", e.Insecure())
	}
}

func TestURLNormalised(t *testing.T) {
	for in, want := range map[string]string{
		"http://emby.lan:8096/":           "http://emby.lan:8096",
		"http://emby.lan:8096/emby":       "http://emby.lan:8096",
		"HTTP://emby.lan:8096/emby/":      "http://emby.lan:8096",
		"https://media.example.com/emby":  "https://media.example.com",
		"https://example.com/media/emby/": "https://example.com/media",
		"https://example.com/embyserver":  "https://example.com/embyserver",
		"http://localhost:8096":           "http://localhost:8096",
	} {
		cfg, err := Load(MapEnv(base("EMBY_URL=" + in)))
		if err != nil {
			t.Errorf("%s: %v", in, err)
			continue
		}
		if cfg.Emby.URL != want {
			t.Errorf("%s: got %s, want %s", in, cfg.Emby.URL, want)
		}
	}
	cfg, _ := Load(MapEnv(base("EMBY_URL=http://localhost:8096")))
	if cfg.Emby.Insecure() != "" {
		t.Errorf("loopback http shouldn't warn: %q", cfg.Emby.Insecure())
	}
}

func TestValidation(t *testing.T) {
	for env, want := range map[string]string{
		"EMBY_URL=":                           "EMBY_URL is required",
		"EMBY_URL=emby.lan:8096":              "EMBY_URL",
		"EMBY_URL=ftp://emby.lan":             "http:// or https://",
		"EMBY_URL=http://user:pw@emby.lan":    "must not contain a user name",
		"EMBY_URL=http://emby.lan/?x=1":       "query string",
		"EMBY_API_KEY=":                       "EMBY_API_KEY is required",
		"EMBY_ALLOW_WRITES=maybe":             "must be true or false",
		"EMBY_ALLOW_DELETES=true":             "also needs EMBY_ALLOW_WRITES",
		"EMBY_TOOLSETS=library,heating":       `unknown toolset "heating"`,
		"EMBY_TIMEOUT_MS=30s":                 "positive integer",
		"EMBY_TLS_FINGERPRINT=AB:CD":          "SHA-256 fingerprint",
		"EMBY_TLS_FINGERPRINT=" + fp64:        "need an https://",
		"EMBY_CA_FILE=/nonexistent/ca.pem":    "could not read EMBY_CA_FILE",
		"MCP_TRANSPORT=sse":                   "stdio or http",
		"MCP_TRANSPORT=http":                  "MCP_AUTH_TOKEN is required",
		"MCP_AUTH_TOKEN=short":                "at least 16",
		"MCP_HTTP_PORT=70000":                 "at most 65535",
		"EMBY_API_KEY_FILE=/nonexistent/key":  "could not read EMBY_API_KEY_FILE",
		"EMBY_AUDIT_LOG=/nonexistent/dir/log": "cannot write",
	} {
		_, err := Load(MapEnv(base(env)))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want an error containing %q", env, err, want)
		}
	}
	// Unauthenticated HTTP needs an allow-list.
	if _, err := Load(MapEnv(base("MCP_TRANSPORT=http", "MCP_ALLOW_UNAUTHENTICATED=true"))); err == nil || !strings.Contains(err.Error(), "MCP_ALLOWED_HOSTS") {
		t.Errorf("unauthenticated without hosts: %v", err)
	}
	if _, err := Load(MapEnv(base("MCP_TRANSPORT=http", "MCP_ALLOW_UNAUTHENTICATED=true", "MCP_ALLOWED_HOSTS=emby-mcp.lan"))); err != nil {
		t.Errorf("unauthenticated with hosts: %v", err)
	}
}

const fp64 = "ab:cd:ef:01:23:45:67:89:ab:cd:ef:01:23:45:67:89:ab:cd:ef:01:23:45:67:89:ab:cd:ef:01:23:45:67:89"

func TestTLS(t *testing.T) {
	cfg, err := Load(MapEnv(base("EMBY_URL=https://192.0.2.10:8920", "EMBY_TLS_FINGERPRINT=sha256:"+fp64)))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Emby.TLSFingerprints) != 1 || !strings.HasPrefix(cfg.Emby.TLSFingerprints[0], "AB:CD:EF") || cfg.Emby.Insecure() != "" {
		t.Errorf("fingerprint: %+v", cfg.Emby)
	}
	cfg, err = Load(MapEnv(base("EMBY_URL=https://192.0.2.10:8920", "EMBY_VERIFY_SSL=false")))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Emby.VerifySSL || !strings.Contains(cfg.Emby.Insecure(), "EMBY_VERIFY_SSL=false") {
		t.Errorf("verify off: %+v %q", cfg.Emby, cfg.Emby.Insecure())
	}
	if fps, err := ParseFingerprints(strings.ReplaceAll(fp64, ":", "") + ", " + fp64); err != nil || len(fps) != 2 || fps[0] != fps[1] {
		t.Errorf("parse: %v %v", fps, err)
	}
}

func TestToolsetsAndDeny(t *testing.T) {
	cfg, err := Load(MapEnv(base("EMBY_TOOLSETS=all,raw", "EMBY_ALLOW_WRITES=yes", "EMBY_ALLOW_DELETES=1", "EMBY_DENY_TOOLS= emby_delete_playlist ,EMBY_SCAN_LIBRARY")))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tools.Toolsets) != len(Toolsets) || !cfg.Tools.AllowWrites || !cfg.Tools.AllowDeletes {
		t.Errorf("tools: %+v", cfg.Tools)
	}
	if !cfg.Tools.DenyTools["emby_delete_playlist"] || !cfg.Tools.DenyTools["emby_scan_library"] || len(cfg.Tools.DenyTools) != 2 {
		t.Errorf("deny: %v", cfg.Tools.DenyTools)
	}
	cfg, err = Load(MapEnv(base("EMBY_TOOLSETS=library, metadata")))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tools.Toolsets) != 2 || !cfg.Tools.Toolsets["metadata"] {
		t.Errorf("toolsets: %v", cfg.Tools.Toolsets)
	}
}

// Claude Desktop passes unfilled optional settings through as ${user_config.x}.
func TestBundlePlaceholders(t *testing.T) {
	cfg, err := Load(MapEnv(base("EMBY_USER=${user_config.user}", "EMBY_TOOLSETS=${user_config.toolsets}", "EMBY_AUDIT_LOG=${user_config.audit_log}", "EMBY_TLS_FINGERPRINT=${user_config.tls_fingerprint}")))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Emby.User != "" || cfg.Tools.AuditLog != "" || len(cfg.Tools.Toolsets) != len(Toolsets)-1 {
		t.Errorf("placeholders weren't treated as unset: %+v", cfg)
	}
}

func TestFileSecrets(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "key")
	if err := os.WriteFile(key, []byte("from-a-file-0123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(MapEnv(base("EMBY_API_KEY=", "EMBY_API_KEY_FILE="+key)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Emby.APIKey != "from-a-file-0123" {
		t.Errorf("key: %q", cfg.Emby.APIKey)
	}
	log := filepath.Join(dir, "audit.jsonl")
	if _, err := Load(MapEnv(base("EMBY_AUDIT_LOG=" + log))); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(log); err != nil || st.Mode().Perm() != AuditLogMode {
		t.Errorf("audit log: %v %v", st, err)
	}
}
