// Package emby is a client for the Emby Server REST API.
package emby

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mattoddie/emby-mcp/internal/config"
)

// UserAgent is sent with every API request; main adds the version.
var UserAgent = "emby-mcp"

// Obj is a JSON object from the API.
type Obj = map[string]any

// Error is an API error with its HTTP status (0 when Emby couldn't be reached).
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

// StatusOf returns the HTTP status of an API error, or 0.
func StatusOf(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Status
	}
	return 0
}

const maxRetries = 3

// maxResponseBytes bounds every response body read from Emby. A variable so tests can lower it.
var maxResponseBytes = 64 << 20

// Client calls the Emby REST API with an API key.
type Client struct {
	cfg  config.Emby
	http *http.Client
	now  func() time.Time

	mu        sync.Mutex // guards the caches below
	users     []Obj
	usersAt   time.Time
	libraries []Obj
	libsAt    time.Time
	defUser   Obj
}

// cacheTTL is how long the user and library lists are reused.
const cacheTTL = time.Minute

// New creates a client.
func New(cfg config.Emby) (*Client, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.CACert != "" {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM([]byte(cfg.CACert)) {
			return nil, errors.New("EMBY_CA_FILE contains no PEM certificates")
		}
		tlsCfg.RootCAs = pool
	}
	if len(cfg.TLSFingerprints) > 0 {
		// A pinned certificate is trusted on its fingerprint alone, so a
		// self-signed certificate works; the chain isn't checked.
		tlsCfg.InsecureSkipVerify = true //nolint:gosec // replaced by the fingerprint check below
		pins := cfg.TLSFingerprints
		tlsCfg.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("the Emby server sent no certificate")
			}
			got := Fingerprint(cs.PeerCertificates[0].Raw)
			for _, p := range pins {
				if p == got {
					return nil
				}
			}
			return fmt.Errorf("the Emby server's certificate fingerprint is %s, which doesn't match EMBY_TLS_FINGERPRINT", got)
		}
	} else if !cfg.VerifySSL {
		tlsCfg.InsecureSkipVerify = true //nolint:gosec // the owner's choice (EMBY_VERIFY_SSL=false); the startup warning covers it
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsCfg
	transport.MaxIdleConnsPerHost = 8
	hc := &http.Client{
		Timeout:   cfg.Timeout,
		Transport: transport,
		// Never follow redirects: every request carries the API key, and a
		// redirect could send it somewhere else.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &Client{cfg: cfg, http: hc, now: time.Now}, nil
}

// Fingerprint is a certificate's SHA-256 fingerprint as AA:BB:...
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":")
}

// BaseURL is the configured server address.
func (c *Client) BaseURL() string { return c.cfg.URL }

// Get fetches JSON into out (which may be nil).
func (c *Client) Get(ctx context.Context, path string, q url.Values, out any) error {
	return c.Do(ctx, http.MethodGet, path, q, nil, out)
}

// Post sends body (nil for none) as JSON.
func (c *Client) Post(ctx context.Context, path string, q url.Values, body, out any) error {
	return c.Do(ctx, http.MethodPost, path, q, body, out)
}

// Do makes a request to /emby<path> and decodes a JSON response into out.
// Empty bodies leave out untouched; bodies that aren't JSON are stored as a
// string when out is *any. In a dry run, requests other than GET are
// recorded and not sent.
func (c *Client) Do(ctx context.Context, method, path string, q url.Values, body, out any) error {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if d, ok := ctx.Value(dryRunKey{}).(*DryRun); ok && method != http.MethodGet {
		d.record(method, path, q, body)
		return nil
	}
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	full := c.cfg.URL + "/emby" + path
	if len(q) > 0 {
		full += "?" + q.Encode()
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, full, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("X-Emby-Token", c.cfg.APIKey)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", UserAgent)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return &Error{Message: fmt.Sprintf("%s %s: could not reach Emby at %s: %s", method, path, c.cfg.URL, networkCause(err))}
		}
		data, err := readBody(resp.Body)
		resp.Body.Close()
		if err != nil {
			return &Error{Status: resp.StatusCode, Message: fmt.Sprintf("%s %s: could not read Emby's answer: %v", method, path, err)}
		}
		if resp.StatusCode/100 == 2 {
			if out == nil || len(bytes.TrimSpace(data)) == 0 {
				return nil
			}
			if err := json.Unmarshal(data, out); err != nil {
				if p, ok := out.(*any); ok {
					*p = strings.TrimSpace(string(data))
					return nil
				}
				return fmt.Errorf("%s %s: unexpected response: %w", method, path, err)
			}
			return nil
		}
		if resp.StatusCode/100 == 3 {
			return &Error{Status: resp.StatusCode, Message: fmt.Sprintf("%s %s: Emby answered with a redirect to %q, which isn't followed (it would carry the API key). Check EMBY_URL: it may need https:// or a different path.", method, path, resp.Header.Get("Location"))}
		}
		if retryable(method, resp.StatusCode) && attempt < maxRetries {
			delay := time.Duration(500<<attempt) * time.Millisecond
			if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
				delay = min(time.Duration(s)*time.Second, 10*time.Second)
			}
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}
		return &Error{
			Status:  resp.StatusCode,
			Message: fmt.Sprintf("%s %s failed (HTTP %d): %s.%s", method, path, resp.StatusCode, errorMessage(resp.StatusCode, data), hint(resp.StatusCode)),
		}
	}
}

// readBody reads a response body, failing clearly instead of truncating it.
func readBody(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, int64(maxResponseBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxResponseBytes {
		return nil, fmt.Errorf("the response is larger than %d MiB; ask for fewer items", maxResponseBytes>>20)
	}
	return data, nil
}

// retryable reports whether a failed request may be sent again. A 429 means
// Emby didn't act on it, so any method can be retried; after a 502 or 503 a
// POST may already have changed something, so only GETs are.
func retryable(method string, status int) bool {
	switch status {
	case http.StatusTooManyRequests:
		return true
	case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		return method == http.MethodGet
	}
	return false
}

func networkCause(err error) string {
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var opErr *net.OpError
	switch {
	case errors.As(err, &certErr):
		return "the certificate couldn't be verified (" + certErr.Err.Error() + "). Set EMBY_TLS_FINGERPRINT or EMBY_CA_FILE for a self-signed certificate"
	case strings.Contains(err.Error(), "EMBY_TLS_FINGERPRINT"):
		var cause error = err
		for errors.Unwrap(cause) != nil {
			cause = errors.Unwrap(cause)
		}
		return cause.Error()
	case errors.As(err, &dnsErr):
		return "the host name " + dnsErr.Name + " doesn't resolve"
	case strings.Contains(err.Error(), "connection refused"):
		return "connection refused. Is Emby running, and is the port right (8096 for http, 8920 for https)?"
	case strings.Contains(err.Error(), "Client.Timeout") || strings.Contains(err.Error(), "deadline exceeded"):
		return "it didn't answer in time (EMBY_TIMEOUT_MS)"
	case strings.Contains(err.Error(), "server gave HTTP response to HTTPS client"):
		return "it answered in plain http; use http:// in EMBY_URL"
	case errors.As(err, &opErr):
		return opErr.Err.Error()
	}
	return err.Error()
}

// errorMessage pulls a readable message out of an error body. Emby usually
// answers errors in plain text.
func errorMessage(status int, data []byte) string {
	var j Obj
	if json.Unmarshal(data, &j) != nil {
		if s := strings.TrimSpace(string(data)); s != "" && !strings.HasPrefix(s, "<") {
			return truncate(s, 300)
		}
		return http.StatusText(status)
	}
	var parts []string
	for _, k := range []string{"Message", "message", "error", "title", "detail"} {
		if s, ok := j[k].(string); ok && s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return truncate(string(data), 300)
	}
	return strings.Join(parts, "; ")
}

func hint(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return " Emby rejected the API key. Check EMBY_API_KEY, or create a new key in the Emby dashboard under API Keys."
	case http.StatusForbidden:
		return " Emby refused this for the API key or user. Some requests need an administrator."
	case http.StatusNotFound:
		return " The item or path doesn't exist; it may have been removed, or the ID is wrong."
	}
	if status >= 500 {
		return " Emby had a problem; try again shortly, and check the Emby server log if it keeps failing."
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// idRe is what an Emby ID looks like: a number, or a GUID with or without dashes.
var idRe = regexp.MustCompile(`^[0-9A-Za-z-]{1,64}$`)

// LooksLikeID reports whether s is shaped like a real item ID: a number or
// a GUID. Titles such as "Aladdin" pass CheckID but aren't IDs.
var looksLikeIDRe = regexp.MustCompile(`^(\d{1,19}|[0-9a-fA-F]{32}|[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$`)

// LooksLikeID reports whether s is a number or a GUID.
func LooksLikeID(s string) bool { return looksLikeIDRe.MatchString(s) }

// CheckID refuses anything that isn't shaped like an Emby ID, so IDs can go
// into request paths safely.
func CheckID(what, id string) error {
	if !idRe.MatchString(id) {
		return fmt.Errorf("%s %q is not an Emby ID (they look like 12345 or a GUID); search for the item to find its ID", what, id)
	}
	return nil
}

// Users returns the server's users (cached for a minute).
func (c *Client) Users(ctx context.Context) ([]Obj, error) {
	c.mu.Lock()
	if c.users != nil && c.now().Sub(c.usersAt) < cacheTTL {
		u := c.users
		c.mu.Unlock()
		return u, nil
	}
	c.mu.Unlock()
	var res struct {
		Items []Obj
	}
	if err := c.Get(ctx, "/Users/Query", nil, &res); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.users, c.usersAt = res.Items, c.now()
	c.mu.Unlock()
	return res.Items, nil
}

// User finds a user by ID or name (case-insensitive). An empty ref is the
// configured EMBY_USER, or else the first enabled administrator.
func (c *Client) User(ctx context.Context, ref string) (Obj, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = c.cfg.User
	}
	if ref == "" {
		c.mu.Lock()
		d := c.defUser
		c.mu.Unlock()
		if d != nil {
			return d, nil
		}
	}
	users, err := c.Users(ctx)
	if err != nil {
		if StatusOf(err) == http.StatusForbidden || StatusOf(err) == http.StatusUnauthorized {
			return nil, err
		}
		return nil, fmt.Errorf("could not list Emby users: %w", err)
	}
	var names []string
	for _, u := range users {
		names = append(names, Str(u, "Name"))
	}
	if ref != "" {
		for _, u := range users {
			if Str(u, "Id") == ref || strings.EqualFold(Str(u, "Name"), ref) {
				return u, nil
			}
		}
		return nil, fmt.Errorf("there is no Emby user called %q; the users are %s", ref, strings.Join(quote(names), ", "))
	}
	// Default: the first enabled administrator by name, else the first enabled user.
	sorted := append([]Obj{}, users...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return strings.ToLower(Str(sorted[i], "Name")) < strings.ToLower(Str(sorted[j], "Name"))
	})
	var pick Obj
	for _, u := range sorted {
		p, _ := u["Policy"].(Obj)
		if Bool(p, "IsDisabled") {
			continue
		}
		if Bool(p, "IsAdministrator") {
			pick = u
			break
		}
		if pick == nil {
			pick = u
		}
	}
	if pick == nil {
		return nil, errors.New("the Emby server has no enabled users")
	}
	c.mu.Lock()
	c.defUser = pick
	c.mu.Unlock()
	return pick, nil
}

// UserID is User's ID.
func (c *Client) UserID(ctx context.Context, ref string) (string, error) {
	u, err := c.User(ctx, ref)
	if err != nil {
		return "", err
	}
	return Str(u, "Id"), nil
}

// Libraries returns the libraries (virtual folders), cached for a minute.
func (c *Client) Libraries(ctx context.Context) ([]Obj, error) {
	c.mu.Lock()
	if c.libraries != nil && c.now().Sub(c.libsAt) < cacheTTL {
		l := c.libraries
		c.mu.Unlock()
		return l, nil
	}
	c.mu.Unlock()
	var res []Obj
	if err := c.Get(ctx, "/Library/VirtualFolders", nil, &res); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.libraries, c.libsAt = res, c.now()
	c.mu.Unlock()
	return res, nil
}

// Library finds a library by name (case-insensitive) or ID and returns its
// item ID, which is what ParentId takes.
func (c *Client) Library(ctx context.Context, ref string) (Obj, error) {
	libs, err := c.Libraries(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, l := range libs {
		if Str(l, "ItemId") == ref || Str(l, "Id") == ref || strings.EqualFold(Str(l, "Name"), ref) {
			return l, nil
		}
		names = append(names, Str(l, "Name"))
	}
	return nil, fmt.Errorf("there is no library called %q; the libraries are %s", ref, strings.Join(quote(names), ", "))
}

// Items runs an item query for a user: GET /Users/{id}/Items.
func (c *Client) Items(ctx context.Context, userID string, q url.Values) ([]Obj, int, error) {
	var res struct {
		Items            []Obj
		TotalRecordCount int
	}
	if err := c.Get(ctx, "/Users/"+userID+"/Items", q, &res); err != nil {
		return nil, 0, err
	}
	return res.Items, res.TotalRecordCount, nil
}

// Item fetches one item with every field, as the user sees it.
func (c *Client) Item(ctx context.Context, userID, id string) (Obj, error) {
	if err := CheckID("item", id); err != nil {
		return nil, err
	}
	var item Obj
	if err := c.Get(ctx, "/Users/"+userID+"/Items/"+id, nil, &item); err != nil {
		// Emby answers an ID it can't parse with a 500 "Unrecognized Guid format".
		if st := StatusOf(err); st == http.StatusNotFound || st == http.StatusBadRequest || (st == http.StatusInternalServerError && strings.Contains(err.Error(), "Guid")) {
			return nil, &Error{Status: http.StatusNotFound, Message: fmt.Sprintf("there is no item with ID %s (or this user can't see it)", id)}
		}
		return nil, err
	}
	return item, nil
}

// ItemsByID fetches several items in batches with the given fields, keyed by
// ID. IDs that don't exist are missing from the map.
func (c *Client) ItemsByID(ctx context.Context, userID string, ids []string, fields string) (map[string]Obj, error) {
	out := map[string]Obj{}
	for start := 0; start < len(ids); start += 100 {
		end := min(start+100, len(ids))
		q := url.Values{"Ids": {strings.Join(ids[start:end], ",")}, "Fields": {fields}, "EnableImages": {"false"}}
		items, _, err := c.Items(ctx, userID, q)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			out[Str(it, "Id")] = it
		}
	}
	return out, nil
}

func quote(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = strconv.Quote(n)
	}
	return out
}

// Str reads a string field, following nested keys.
func Str(o Obj, keys ...string) string {
	var v any = o
	for _, k := range keys {
		m, ok := v.(Obj)
		if !ok {
			return ""
		}
		v = m[k]
	}
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case json.Number:
		return x.String()
	}
	return ""
}

// Bool reads a boolean field.
func Bool(o Obj, key string) bool {
	b, _ := o[key].(bool)
	return b
}

// Num reads a number field.
func Num(o Obj, key string) (float64, bool) {
	f, ok := o[key].(float64)
	return f, ok
}

// Objs reads an array of objects.
func Objs(v any) []Obj {
	list, _ := v.([]any)
	out := make([]Obj, 0, len(list))
	for _, x := range list {
		if o, ok := x.(Obj); ok {
			out = append(out, o)
		}
	}
	return out
}

// Strs reads an array of strings.
func Strs(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// Names reads the Name of each object in an array (TagItems, GenreItems, Studios ...).
func Names(v any) []string {
	var out []string
	for _, o := range Objs(v) {
		if n := Str(o, "Name"); n != "" {
			out = append(out, n)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out
}
