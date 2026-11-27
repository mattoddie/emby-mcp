// Package config reads emby-mcp's settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Toolsets are groups of tools that can be switched on and off with EMBY_TOOLSETS.
var Toolsets = []string{"library", "metadata", "collections", "playlists", "users", "server", "raw"}

// OptInToolset isn't included in "all" (the default): it has to be listed by name.
const OptInToolset = "raw"

// MinAuthTokenLength is the shortest MCP_AUTH_TOKEN accepted.
const MinAuthTokenLength = 16

// Emby holds the Emby server connection settings.
type Emby struct {
	// URL is the server's base URL without a trailing slash or /emby, such as
	// http://emby.lan:8096.
	URL    string
	APIKey string
	// User is the Emby user (name or ID) whose view of the library is used
	// for played state, favourites and resume points. Empty picks the first
	// administrator.
	User string
	// TLS: VerifySSL is false only when EMBY_VERIFY_SSL=false.
	VerifySSL       bool
	CACert          string
	TLSFingerprints []string
	Timeout         time.Duration
}

// Insecure reports whether requests go to Emby without protection for the
// API key: plain http to anything but loopback, or https without checking
// the certificate.
func (e Emby) Insecure() string {
	u, err := url.Parse(e.URL)
	if err != nil {
		return ""
	}
	switch {
	case u.Scheme == "http" && !isLoopback(u.Hostname()):
		return "EMBY_URL uses plain http, so the API key and everything the server returns cross the network unencrypted. That's usual on a home network; use https (for example through a reverse proxy) if the network isn't trusted."
	case u.Scheme == "https" && !e.VerifySSL && len(e.TLSFingerprints) == 0:
		return "EMBY_VERIFY_SSL=false: the Emby certificate isn't checked, so the API key can be intercepted. Prefer EMBY_TLS_FINGERPRINT or EMBY_CA_FILE."
	}
	return ""
}

// Tools controls which tools are registered.
type Tools struct {
	AllowWrites bool
	// AllowDeletes registers the tools that delete collections and playlists
	// (never media files). Needs AllowWrites.
	AllowDeletes bool
	Toolsets     map[string]bool
	// DenyTools are tool names that are never registered (EMBY_DENY_TOOLS).
	DenyTools map[string]bool
	// AuditLog is where calls of tools that change things are recorded
	// (EMBY_AUDIT_LOG): a file path, AuditLogStderr, or "" for no log.
	AuditLog string
}

// AuditLogStderr is the EMBY_AUDIT_LOG value that writes the audit log to standard error.
const AuditLogStderr = "stderr"

// AllTools enables every tool, for documentation and tests.
func AllTools() Tools {
	t := Tools{AllowWrites: true, AllowDeletes: true, Toolsets: map[string]bool{}, DenyTools: map[string]bool{}}
	for _, ts := range Toolsets {
		t.Toolsets[ts] = true
	}
	return t
}

// HTTP holds the Streamable HTTP transport settings.
type HTTP struct {
	Host         string
	Port         int
	AuthToken    string
	AllowedHosts []string
	// AllowUnauthenticated serves http without MCP_AUTH_TOKEN (MCP_ALLOW_UNAUTHENTICATED).
	AllowUnauthenticated bool
}

// Config is the whole configuration.
type Config struct {
	Emby      Emby
	Tools     Tools
	Transport string // "stdio" or "http"
	HTTP      HTTP
}

// Env looks up a variable; os.LookupEnv in production, a map in tests.
type Env func(string) (string, bool)

// MapEnv adapts a map for tests.
func MapEnv(m map[string]string) Env {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

// placeholder matches a ${user_config.<key>} reference that an MCP Bundle host
// such as Claude Desktop passed through unchanged because the user left an
// optional setting empty. It means "not set", not a value.
var placeholder = regexp.MustCompile(`^\$\{user_config\.[^}]*\}$`)

// lookup returns a trimmed variable, or "" when it is unset, blank or an
// unfilled bundle placeholder.
func lookup(env Env, name string) string {
	v, _ := env(name)
	v = strings.TrimSpace(v)
	if placeholder.MatchString(v) {
		return ""
	}
	return v
}

// read returns a trimmed variable (empty when unset, blank or an unfilled
// bundle placeholder), honouring the <NAME>_FILE convention for Docker secrets.
func read(env Env, name string) (string, error) {
	if file := lookup(env, name+"_FILE"); file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("could not read %s_FILE (%s): %s", name, file, errText(err))
		}
		return strings.TrimSpace(string(b)), nil
	}
	return lookup(env, name), nil
}

// errText is an OS error's message without Go's "open <path>: " prefix, which the caller already shows.
func errText(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

func boolValue(name, v string, fallback bool) (bool, error) {
	if v == "" {
		return fallback, nil
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	}
	return false, fmt.Errorf("%s must be true or false, got %q", name, v)
}

var digits = regexp.MustCompile(`^[0-9]+$`)

func positiveInt(name, v string, fallback int) (int, error) {
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if !digits.MatchString(v) || err != nil || n <= 0 || n > 1<<31-1 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", name, v)
	}
	return n, nil
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

var (
	fingerprintPrefix = regexp.MustCompile(`(?i)^(sha-?256\s*(fingerprint)?\s*[=:]?\s*)`)
	fingerprintSep    = regexp.MustCompile(`[:\s]`)
	hex64             = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
)

// ParseFingerprints parses EMBY_TLS_FINGERPRINT: one or more comma-separated
// SHA-256 certificate fingerprints ("AA:BB:...", optionally prefixed
// "sha256:", or 64 hex digits) into "AA:BB:..." form.
func ParseFingerprints(value string) ([]string, error) {
	var out []string
	for _, v := range strings.Split(value, ",") {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		hex := fingerprintSep.ReplaceAllString(fingerprintPrefix.ReplaceAllString(v, ""), "")
		if !hex64.MatchString(hex) {
			return nil, fmt.Errorf("EMBY_TLS_FINGERPRINT must be a SHA-256 fingerprint (AA:BB:... with 32 pairs, or 64 hex digits), got %q", v)
		}
		hex = strings.ToUpper(hex)
		pairs := make([]string, 0, 32)
		for i := 0; i < len(hex); i += 2 {
			pairs = append(pairs, hex[i:i+2])
		}
		out = append(out, strings.Join(pairs, ":"))
	}
	return out, nil
}

var embySuffix = regexp.MustCompile(`(?i)(/emby)?/*$`)

// checkURL validates EMBY_URL and returns it without a trailing slash or /emby.
func checkURL(v string) (string, error) {
	if v == "" {
		return "", errors.New("EMBY_URL is required: the address of your Emby server, such as http://emby.lan:8096")
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" || u.Opaque != "" {
		return "", fmt.Errorf("EMBY_URL is not a valid URL, such as http://emby.lan:8096: %q", v)
	}
	if u.User != nil {
		return "", errors.New("EMBY_URL must not contain a user name or password (user:pass@); use EMBY_API_KEY")
	}
	if s := strings.ToLower(u.Scheme); s != "http" && s != "https" {
		return "", fmt.Errorf("EMBY_URL must start with http:// or https://, got %q", v)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("EMBY_URL must not have a query string or fragment")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Path = embySuffix.ReplaceAllString(u.Path, "")
	u.RawPath = ""
	return strings.TrimRight(u.String(), "/"), nil
}

// Load builds the configuration from the environment.
func Load(env Env) (*Config, error) {
	var firstErr error
	get := func(name string) string {
		v, err := read(env, name)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return v
	}
	or := func(v, fallback string) string {
		if v == "" {
			return fallback
		}
		return v
	}
	getBool := func(name string, fallback bool) bool {
		b, err := boolValue(name, get(name), fallback)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return b
	}

	cfg := &Config{Transport: or(get("MCP_TRANSPORT"), "stdio")}
	if cfg.Transport != "stdio" && cfg.Transport != "http" {
		return nil, fmt.Errorf("MCP_TRANSPORT must be stdio or http, got %q", cfg.Transport)
	}

	t := &cfg.Tools
	t.AllowWrites = getBool("EMBY_ALLOW_WRITES", false)
	t.AllowDeletes = getBool("EMBY_ALLOW_DELETES", false)
	t.AuditLog = get("EMBY_AUDIT_LOG")
	toolsets := or(get("EMBY_TOOLSETS"), "all")
	deny := get("EMBY_DENY_TOOLS")
	if firstErr != nil {
		return nil, firstErr
	}
	if t.AllowDeletes && !t.AllowWrites {
		return nil, errors.New("EMBY_ALLOW_DELETES=true also needs EMBY_ALLOW_WRITES=true")
	}
	if err := CheckAuditLog(t.AuditLog); err != nil {
		return nil, err
	}
	t.Toolsets = map[string]bool{}
	for _, name := range strings.Split(toolsets, ",") {
		name = strings.ToLower(strings.TrimSpace(name))
		switch {
		case name == "":
		case name == "all":
			// Every toolset except raw, which reaches any API path and has to be named to be enabled.
			for _, ts := range Toolsets {
				if ts != OptInToolset {
					t.Toolsets[ts] = true
				}
			}
		case contains(Toolsets, name):
			t.Toolsets[name] = true
		default:
			return nil, fmt.Errorf("unknown toolset %q in EMBY_TOOLSETS; valid: all (every toolset but raw), %s", name, strings.Join(Toolsets, ", "))
		}
	}
	t.DenyTools = map[string]bool{}
	for _, name := range strings.Split(deny, ",") {
		if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
			t.DenyTools[name] = true
		}
	}

	e := &cfg.Emby
	var err error
	if e.URL, err = checkURL(get("EMBY_URL")); err != nil {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, err
	}
	e.APIKey = get("EMBY_API_KEY")
	e.User = get("EMBY_USER")
	if firstErr != nil {
		return nil, firstErr
	}
	if e.APIKey == "" {
		return nil, errors.New("EMBY_API_KEY is required: create one in the Emby dashboard under Advanced > API Keys (or Settings > API Keys)")
	}
	https := strings.HasPrefix(e.URL, "https://")
	if caFile := get("EMBY_CA_FILE"); caFile != "" {
		b, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("could not read EMBY_CA_FILE (%s): %s", caFile, errText(err))
		}
		e.CACert = string(b)
	}
	if fp := get("EMBY_TLS_FINGERPRINT"); fp != "" {
		if e.TLSFingerprints, err = ParseFingerprints(fp); err != nil {
			return nil, err
		}
	}
	if (e.CACert != "" || len(e.TLSFingerprints) > 0) && !https {
		return nil, errors.New("EMBY_CA_FILE and EMBY_TLS_FINGERPRINT need an https:// EMBY_URL")
	}
	// Emby has no self-signed certificate of its own: https is normally a
	// reverse proxy or a certificate the owner installed, so it's verified
	// against the system CAs unless the owner says otherwise.
	e.VerifySSL = getBool("EMBY_VERIFY_SSL", true)
	ms, err := positiveInt("EMBY_TIMEOUT_MS", get("EMBY_TIMEOUT_MS"), 30000)
	if err != nil {
		return nil, err
	}
	e.Timeout = time.Duration(ms) * time.Millisecond

	hs := &cfg.HTTP
	hs.Host = or(get("MCP_HTTP_HOST"), "127.0.0.1")
	if hs.Port, err = positiveInt("MCP_HTTP_PORT", get("MCP_HTTP_PORT"), 8080); err != nil {
		return nil, err
	}
	if hs.Port > 65535 {
		return nil, fmt.Errorf("MCP_HTTP_PORT must be at most 65535, got %d", hs.Port)
	}
	hs.AuthToken = get("MCP_AUTH_TOKEN")
	for _, host := range strings.Split(get("MCP_ALLOWED_HOSTS"), ",") {
		if host = strings.TrimSpace(host); host != "" {
			hs.AllowedHosts = append(hs.AllowedHosts, strings.ToLower(host))
		}
	}
	hs.AllowUnauthenticated = getBool("MCP_ALLOW_UNAUTHENTICATED", false)
	if firstErr != nil {
		return nil, firstErr
	}
	if hs.AuthToken != "" && len(hs.AuthToken) < MinAuthTokenLength {
		return nil, fmt.Errorf("MCP_AUTH_TOKEN must be at least %d characters (generate one with: openssl rand -hex 32)", MinAuthTokenLength)
	}
	if cfg.Transport == "http" && hs.AuthToken == "" {
		// An HTTP server without a token would hand every enabled tool to anyone who can reach the port.
		if !hs.AllowUnauthenticated {
			return nil, errors.New("MCP_AUTH_TOKEN is required for the http transport (generate one with: openssl rand -hex 32); set MCP_ALLOW_UNAUTHENTICATED=true only behind something else that authenticates")
		}
		// Without a token, the Host and Origin checks are all that stop a web page
		// the user visits from reaching the server through DNS rebinding.
		if len(hs.AllowedHosts) == 0 {
			return nil, errors.New("MCP_ALLOW_UNAUTHENTICATED=true needs MCP_ALLOWED_HOSTS: without a token, the Host and Origin allow-list is what stops web pages in your browser reaching the server through DNS rebinding. Set it to the hostname(s) clients use, such as emby-mcp.lan")
		}
	}
	return cfg, nil
}

// AuditLogMode is the permission an audit log file is created with: only the
// server's user can read it, since it records what was changed and when.
const AuditLogMode = 0o600

// OpenAuditLog opens an audit log file for appending, creating it if needed.
func OpenAuditLog(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, AuditLogMode)
}

// CheckAuditLog checks that EMBY_AUDIT_LOG can be written, so a mistake is
// reported at startup rather than on the first change.
func CheckAuditLog(path string) error {
	if path == "" || path == AuditLogStderr {
		return nil
	}
	f, err := OpenAuditLog(path)
	if err != nil {
		return fmt.Errorf("EMBY_AUDIT_LOG: cannot write to %s: %s. Its directory must exist and be writable by the server (in Docker, mount a writable directory; the image runs as UID 65532)", path, errText(err))
	}
	return f.Close()
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
