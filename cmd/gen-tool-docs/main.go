// Command gen-tool-docs writes docs/tools.md from the registered tools and
// prompts, so the reference can't drift from the code. With -check it fails
// when the committed file is out of date.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mattoddie/emby-mcp/internal/config"
	"github.com/mattoddie/emby-mcp/internal/emby"
	"github.com/mattoddie/emby-mcp/internal/tools"
)

var toolsetInfo = map[string]string{
	"library":     "Find and read: server and library overview, search with rich filters, item details, the tags, genres and studios in use, similar items, recently added and continue watching.",
	"metadata":    "Curate: add and remove tags on many items at once, rename or merge tags, edit metadata fields and locks, and refresh metadata from the internet.",
	"collections": "Collections (Emby box sets): list, view, create, add to, remove from and delete.",
	"playlists":   "Playlists: list, view, create, add to, remove from and delete.",
	"users":       "Users, and each user's watched state and favourites.",
	"server":      "The server: what's playing now, the activity log, scheduled tasks and library scans.",
	"raw":         "A raw API escape hatch for anything the other tools don't cover. It isn't part of `EMBY_TOOLSETS=all`, so list it by name (for example `all,raw`). It is limited to GET requests unless writes are enabled, needs `EMBY_ALLOW_DELETES` for deletes and changes to users, API keys and the server, and redacts secrets.",
}

var badge = map[tools.Gate]string{tools.Read: "🟢 read", tools.Write: "🟠 write", tools.Delete: "🔴 delete"}

type schema struct {
	Type        any                `json:"type"`
	Description string             `json:"description"`
	Enum        []any              `json:"enum"`
	Items       *schema            `json:"items"`
	Properties  map[string]*schema `json:"properties"`
	Required    []string           `json:"required"`
	Minimum     *float64           `json:"minimum"`
	Maximum     *float64           `json:"maximum"`
}

// typeWithRange adds an argument's minimum and maximum to its type.
func typeWithRange(s *schema) string {
	t := typeOf(s)
	if s != nil && s.Minimum != nil && s.Maximum != nil {
		t += fmt.Sprintf(" (%g–%g)", *s.Minimum, *s.Maximum)
	}
	return t
}

func typeOf(s *schema) string {
	if s == nil {
		return "any"
	}
	if len(s.Enum) > 0 {
		var parts []string
		for _, v := range s.Enum {
			b, _ := json.Marshal(v)
			parts = append(parts, "`"+string(b)+"`")
		}
		return strings.Join(parts, " | ")
	}
	switch t := s.Type.(type) {
	case string:
		if t == "array" {
			return typeOf(s.Items) + "[]"
		}
		return t
	case []any:
		var parts []string
		for _, x := range t {
			parts = append(parts, fmt.Sprint(x))
		}
		return strings.Join(parts, " | ")
	}
	return "any"
}

// rows renders arguments, required first, with the fields of nested objects (one level) as name.field rows.
func rows(prefix string, s *schema) []string {
	required := map[string]bool{}
	for _, r := range s.Required {
		required[r] = true
	}
	names := make([]string, 0, len(s.Properties))
	for n := range s.Properties {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if required[names[i]] != required[names[j]] {
			return required[names[i]]
		}
		return names[i] < names[j]
	})
	var out []string
	for _, n := range names {
		p := s.Properties[n]
		req := ""
		if required[n] {
			req = "yes"
		}
		out = append(out, fmt.Sprintf("| `%s%s` | %s | %s | %s |", prefix, n, cell(typeWithRange(p)), req, cell(p.Description)))
		if prefix != "" {
			continue
		}
		switch {
		case p.Items != nil && len(p.Items.Properties) > 0:
			out = append(out, rows(n+"[].", p.Items)...)
		case len(p.Properties) > 0:
			out = append(out, rows(n+".", p)...)
		}
	}
	return out
}

var spaces = regexp.MustCompile(`\s*\n\s*`)

func cell(s string) string { return spaces.ReplaceAllString(strings.ReplaceAll(s, "|", "\\|"), " ") }

func main() {
	check := flag.Bool("check", false, "fail if docs/tools.md is out of date instead of writing it")
	out := flag.String("o", "docs/tools.md", "output file")
	flag.Parse()

	// A client that is never used: listing tools doesn't touch Emby.
	client, err := emby.New(config.Emby{URL: "http://emby.invalid:8096", APIKey: "unused", VerifySSL: true, Timeout: 1e9})
	if err != nil {
		log.Fatal(err)
	}
	registered := map[string]tools.Info{}
	server := tools.NewServer(client, config.AllTools(), registered)
	st, ct := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := server.Connect(ctx, st, nil); err != nil {
		log.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "gen-tool-docs", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer session.Close()

	var all []*mcp.Tool
	for t, err := range session.Tools(ctx, nil) {
		if err != nil {
			log.Fatal(err)
		}
		all = append(all, t)
	}
	var prompts []*mcp.Prompt
	for p, err := range session.Prompts(ctx, nil) {
		if err != nil {
			log.Fatal(err)
		}
		prompts = append(prompts, p)
	}
	counts := map[tools.Gate]int{}
	for _, t := range all {
		counts[registered[t.Name].Gate]++
	}

	var b strings.Builder
	w := func(lines ...string) {
		for _, l := range lines {
			b.WriteString(l + "\n")
		}
	}
	w("# Tool reference", "",
		"<!-- Generated by `make docs` (go run ./cmd/gen-tool-docs) from the tool definitions. Do not edit by hand. -->", "",
		fmt.Sprintf("emby-mcp has **%d tools** in **%d toolsets**: %d read, %d write and %d delete. It also has **%d prompts**.",
			len(all), len(config.Toolsets), counts[tools.Read], counts[tools.Write], counts[tools.Delete], len(prompts)), "",
		"Each tool is labelled with the setting it needs:", "",
		"| Label | Registered when |", "|---|---|",
		"| 🟢 read | Always |",
		"| 🟠 write | `EMBY_ALLOW_WRITES=true` |",
		"| 🔴 delete | `EMBY_ALLOW_WRITES=true` and `EMBY_ALLOW_DELETES=true` (they delete collections and playlists, never media files) |", "",
		"Every tool also needs its toolset to be enabled in `EMBY_TOOLSETS`, which enables every toolset except `raw` by default, and not to be listed in `EMBY_DENY_TOOLS`. Items are addressed by their Emby ID, which the search and list tools return; tools that take one item also accept its exact title. Bulk tools take up to 500 item IDs per call. Read tools return compact summaries, and many take `raw: true` for Emby's full objects. Played state and favourites belong to one user: tools use the default user (`EMBY_USER`, or the first administrator) unless given `user`. Every tool that changes something takes `dry_run: true` to preview the change.", "",
		"## Contents", "")
	for _, ts := range config.Toolsets {
		w(fmt.Sprintf("- [`%s`](#%s): %s", ts, ts, toolsetInfo[ts]))
	}
	w("- [Prompts](#prompts)", "")

	for _, ts := range config.Toolsets {
		w("## "+ts, "", toolsetInfo[ts], "", "| Tool | Access | Summary |", "|---|---|---|")
		var inSet []*mcp.Tool
		for _, t := range all {
			if registered[t.Name].Toolset == ts {
				inSet = append(inSet, t)
			}
		}
		for _, t := range inSet {
			w(fmt.Sprintf("| [`%s`](#%s) | %s | %s |", t.Name, t.Name, badge[registered[t.Name].Gate], cell(t.Title)))
		}
		w("")
		for _, t := range inSet {
			label := badge[registered[t.Name].Gate]
			if a := t.Annotations; a != nil && !a.ReadOnlyHint && a.DestructiveHint != nil && *a.DestructiveHint {
				label += " · destructive"
			}
			w("### "+t.Name, "", label, "", t.Description, "")
			raw, _ := json.Marshal(t.InputSchema)
			var s schema
			_ = json.Unmarshal(raw, &s)
			if len(s.Properties) == 0 {
				w("_No arguments._", "")
				continue
			}
			w("| Argument | Type | Required | Description |", "|---|---|---|---|")
			for _, row := range rows("", &s) {
				w(row)
			}
			w("")
		}
	}

	w("## Prompts", "", "Prompts are ready-made requests that tell the model which tools to call and what to report. In Claude Code they appear as slash commands, such as `/mcp__emby__library_report`.", "")
	for _, p := range prompts {
		w("### "+p.Name, "", p.Description, "")
		if len(p.Arguments) > 0 {
			w("| Argument | Required | Description |", "|---|---|---|")
			for _, a := range p.Arguments {
				req := ""
				if a.Required {
					req = "yes"
				}
				w(fmt.Sprintf("| `%s` | %s | %s |", a.Name, req, cell(a.Description)))
			}
			w("")
		}
	}

	text := regexp.MustCompile(`\n{3,}`).ReplaceAllString(b.String(), "\n\n")
	text = strings.TrimRight(text, "\n") + "\n"
	if *check {
		current, _ := os.ReadFile(*out)
		if string(current) != text {
			log.Fatalf("%s is out of date. Run `make docs` and commit the result.", *out)
		}
		fmt.Printf("%s is up to date.\n", *out)
		return
	}
	if err := os.WriteFile(*out, []byte(text), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Wrote %s (%d tools, %d prompts).\n", *out, len(all), len(prompts))
}
