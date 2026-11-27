package tools

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/mattoddie/emby-mcp/internal/emby"
)

// maxBatch is the most item IDs one bulk tool call takes.
const maxBatch = 500

// workers is how many requests a bulk change sends at once: enough to be
// quick, few enough not to load a small NAS.
const workers = 4

// listResult is the output of a list tool that isn't paged.
func listResult(items []Obj) Obj {
	if items == nil {
		items = []Obj{}
	}
	return Obj{"total": len(items), "returned": len(items), "items": items}
}

// pagedResult is the output of a paged list tool.
func pagedResult(items []Obj, total, offset int) Obj {
	if items == nil {
		items = []Obj{}
	}
	out := Obj{"total": total, "returned": len(items), "offset": offset, "items": items}
	if next := offset + len(items); next < total && len(items) > 0 {
		out["next_offset"] = next
	}
	return out
}

type userArgs struct {
	User string `json:"user,omitempty" jsonschema:"Emby user (name or ID) whose played state, favourites and access to use. Default: EMBY_USER, or the first administrator"`
}

type rawArgs struct {
	Raw bool `json:"raw,omitempty" jsonschema:"Return Emby's full objects instead of compact summaries (much larger)"`
}

type confirmArgs struct {
	Confirm bool `json:"confirm,omitempty" jsonschema:"Must be true. Only set it once the user has agreed to this specific change"`
}

// cleanIDs trims, de-duplicates and checks a list of item IDs.
func cleanIDs(ids []string, what string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if err := emby.CheckID(what, id); err != nil {
			return nil, err
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("give at least one %s ID", what)
	}
	if len(out) > maxBatch {
		return nil, fmt.Errorf("at most %d %s IDs per call, got %d; split them into several calls", maxBatch, what, len(out))
	}
	return out, nil
}

// cleanNames trims and de-duplicates (case-insensitively) a list of names such as tags.
func cleanNames(names []string, what string, max int) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		n = strings.Join(strings.Fields(n), " ")
		if n == "" || seen[strings.ToLower(n)] {
			continue
		}
		if len(n) > 100 {
			return nil, fmt.Errorf("%s %q is longer than 100 characters", what, n[:40]+"…")
		}
		for _, r := range n {
			if r < 0x20 || r == 0x7f {
				return nil, fmt.Errorf("%s must not contain control characters", what)
			}
		}
		seen[strings.ToLower(n)] = true
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("give at least one %s", what)
	}
	if len(out) > max {
		return nil, fmt.Errorf("at most %d %ss per call", max, what)
	}
	return out, nil
}

// hasName reports whether list contains name, ignoring case.
func hasName(list []string, name string) bool {
	for _, x := range list {
		if strings.EqualFold(x, name) {
			return true
		}
	}
	return false
}

// itemResult is one item's outcome in a bulk change.
type itemResult struct {
	ID     string   `json:"id"`
	Name   string   `json:"name,omitempty"`
	Change []string `json:"change,omitempty"`
	Error  string   `json:"error,omitempty"`
}

// forEach runs fn for each job with a few workers, in order of results.
// In a dry run it runs one at a time, so the requests are recorded in order.
func forEach[T any](ctx context.Context, jobs []T, fn func(T) error) []error {
	errs := make([]error, len(jobs))
	n := workers
	if emby.IsDryRun(ctx) {
		n = 1
	}
	var wg sync.WaitGroup
	next := make(chan int)
	for w := 0; w < n; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				if err := ctx.Err(); err != nil {
					errs[i] = err
					continue
				}
				errs[i] = fn(jobs[i])
			}
		}()
	}
	for i := range jobs {
		next <- i
	}
	close(next)
	wg.Wait()
	return errs
}

// resolveItem finds one item by ID, or by a name that matches exactly one
// item of the given types.
func resolveItem(ctx context.Context, c *Context, userID, ref string, types ...string) (Obj, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, errors.New("give an item ID or name")
	}
	if emby.LooksLikeID(ref) {
		item, err := c.Client.Item(ctx, userID, ref)
		if err == nil || emby.StatusOf(err) != 404 {
			return item, err
		}
	}
	q := url.Values{"SearchTerm": {ref}, "Recursive": {"true"}, "Limit": {"25"}, "Fields": {emby.ListFields}, "EnableImages": {"false"}}
	if len(types) > 0 {
		q.Set("IncludeItemTypes", strings.Join(types, ","))
	}
	items, _, err := c.Client.Items(ctx, userID, q)
	if err != nil {
		return nil, err
	}
	var exact []Obj
	for _, it := range items {
		if strings.EqualFold(emby.Str(it, "Name"), ref) {
			exact = append(exact, it)
		}
	}
	if len(exact) == 0 {
		exact = items // a unique partial match will do; several are listed below
	}
	what := "item"
	if len(types) == 1 {
		what = map[string]string{"BoxSet": "collection", "Playlist": "playlist"}[types[0]]
	}
	switch len(exact) {
	case 1:
		return c.Client.Item(ctx, userID, emby.Str(exact[0], "Id"))
	case 0:
		return nil, fmt.Errorf("no %s called %q", what, ref)
	}
	var choices []string
	for _, it := range exact {
		choices = append(choices, fmt.Sprintf("%s (%s, id %s)", emby.Str(it, "Name"), yearOrType(it), emby.Str(it, "Id")))
	}
	return nil, fmt.Errorf("%q matches more than one %s; give the ID of one of: %s", ref, what, strings.Join(choices, "; "))
}

func yearOrType(it Obj) string {
	if y := emby.Year(it); y > 0 {
		return fmt.Sprintf("%s %d", emby.Str(it, "Type"), y)
	}
	return emby.Str(it, "Type")
}

// toString renders a query value.
func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	}
	return fmt.Sprint(v)
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
