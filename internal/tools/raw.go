package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

type rawRequestArgs struct {
	Method string         `json:"method,omitempty" jsonschema:"HTTP method: GET (default), POST or DELETE"`
	Path   string         `json:"path" jsonschema:"Path of Emby's REST API (as in the server's /emby/openapi.json), without /emby, such as /Items/Counts or /Users/{UserId}/Items"`
	Query  map[string]any `json:"query,omitempty" jsonschema:"Query string parameters"`
	Body   any            `json:"body,omitempty" jsonschema:"JSON request body for POST"`
}

// rawDangerous are requests the raw tool only sends with EMBY_ALLOW_DELETES:
// ones that delete things, restart or stop the server, or change users, API
// keys or libraries' folders.
var rawDangerous = []*regexp.Regexp{
	regexp.MustCompile(`(?i)/delete$`),
	regexp.MustCompile(`(?i)^/system/(restart|shutdown)$`),
	regexp.MustCompile(`(?i)^/auth/`),
	regexp.MustCompile(`(?i)^/users/[^/]+/(password|policy|configuration|easypassword|connect)`),
	regexp.MustCompile(`(?i)^/users/new$`),
	regexp.MustCompile(`(?i)^/library/virtualfolders`),
	regexp.MustCompile(`(?i)^/packages/installed`),
	regexp.MustCompile(`(?i)^/plugins/`),
	regexp.MustCompile(`(?i)^/system/configuration`),
}

// rawItemDelete matches requests that delete library items, which takes the
// media files with them. The raw tool never sends them: the delete tools
// remove collections and playlists, and nothing in emby-mcp deletes media.
var rawItemDelete = regexp.MustCompile(`(?i)^/items(/[^/]+)?/delete$`)

// rawMaxOutput is the most JSON the raw tool returns.
const rawMaxOutput = 400 << 10

func registerRaw(c *Context) {
	desc := "Call any path of Emby's REST API directly, for anything the other tools don't cover. Paths are listed in the server's own API documentation at /emby/openapi.json. Secrets in responses (access tokens, API keys, passwords) are redacted. "
	switch {
	case c.AllowWrites && c.AllowDeletes:
		desc += "All methods are allowed, including restarts and changes to users and API keys, but never deleting library items (which would delete media files). Changes go straight to Emby with no checks: confirm them with the user first."
	case c.AllowWrites:
		desc += "GET and POST are allowed; deleting, restarting the server and changing users, API keys, plugins or library folders need EMBY_ALLOW_DELETES. Changes go straight to Emby: confirm them with the user first."
	default:
		desc += "Only GET is allowed because writes are disabled (EMBY_ALLOW_WRITES=false)."
	}
	add(c, Def{
		Toolset: "raw", Name: "emby_api_request", Title: "Raw Emby API request",
		Description: desc,
		ReadOnly:    boolPtr(!c.AllowWrites), Destructive: c.AllowWrites,
		Changes: rawRequestChanges,
		Enums:   map[string][]string{"method": {"GET", "POST", "DELETE"}},
	}, func(ctx context.Context, a rawRequestArgs) (any, error) {
		method := strings.ToUpper(strings.TrimSpace(a.Method))
		if method == "" {
			method = "GET"
		}
		p, err := url.PathUnescape(a.Path)
		if err != nil {
			return nil, errors.New("path is not validly percent-encoded")
		}
		if len(p) >= 5 && strings.EqualFold(p[:5], "/emby") && (len(p) == 5 || p[5] == '/') {
			p = p[5:]
		}
		if err := checkRawPath(p); err != nil {
			return nil, err
		}
		if method != "GET" && (rawItemDelete.MatchString(p) || (method == "DELETE" && strings.HasPrefix(strings.ToLower(p), "/items"))) {
			return nil, errors.New("emby-mcp never deletes library items, because Emby deletes their media files too. To delete a collection or playlist, use emby_delete_collection or emby_delete_playlist")
		}
		switch {
		case method == "GET":
		case method != "POST" && method != "DELETE":
			return nil, errors.New("method must be GET, POST or DELETE")
		case !c.AllowWrites:
			return nil, errors.New("writes are disabled. Set EMBY_ALLOW_WRITES=true to allow requests other than GET")
		case (method == "DELETE" || dangerousPath(p)) && !c.AllowDeletes:
			return nil, fmt.Errorf("%s %s deletes something, restarts the server or changes users, API keys, plugins or library folders, which needs EMBY_ALLOW_DELETES=true", method, p)
		}
		q := url.Values{}
		for k, v := range a.Query {
			if list, ok := v.([]any); ok {
				var parts []string
				for _, x := range list {
					parts = append(parts, toString(x))
				}
				q.Set(k, strings.Join(parts, ","))
				continue
			}
			q.Set(k, toString(v))
		}
		if method == "GET" && a.Body != nil {
			return nil, errors.New("a GET request has no body; use query")
		}
		escaped := strings.ReplaceAll(url.PathEscape(p), "%2F", "/")
		var out any
		if err := c.Client.Do(ctx, method, escaped, q, a.Body, &out); err != nil {
			return nil, err
		}
		out = redactSecrets(out)
		if b, err := json.Marshal(out); err == nil && len(b) > rawMaxOutput {
			return Obj{"truncated": true, "bytes": len(b), "note": fmt.Sprintf("The response is %d KiB, too big to return whole; this is its start. Ask for less (Limit, StartIndex, Fields) to see all of it.", len(b)>>10), "start": string(b[:rawMaxOutput/2])}, nil
		}
		return out, nil
	})
}

func dangerousPath(p string) bool {
	for _, re := range rawDangerous {
		if re.MatchString(p) {
			return true
		}
	}
	return false
}

// checkRawPath refuses a decoded path that could reach somewhere other than
// the API path it appears to name.
func checkRawPath(p string) error {
	if !strings.HasPrefix(p, "/") || strings.Contains(p, "..") || strings.ContainsAny(p, "?#\\") || strings.Contains(p, "//") ||
		strings.Contains(p, "/./") || strings.HasSuffix(p, "/.") {
		return errors.New("path must start with /, must not contain '..', '.' or '//' segments, and must not include a query string (use query)")
	}
	if strings.Contains(p, "%") {
		return errors.New("path must not be percent-encoded twice")
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return errors.New("path must not contain control characters")
		}
	}
	return nil
}
