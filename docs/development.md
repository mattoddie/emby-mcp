# Development

This page is for people working on emby-mcp itself. Start with [CONTRIBUTING.md](../CONTRIBUTING.md) for the workflow, and use this page for how the code fits together.

- [Setup](#setup)
- [Project layout](#project-layout)
- [How a request flows](#how-a-request-flows)
- [Gates and dry runs](#gates-and-dry-runs)
- [Adding a tool](#adding-a-tool)
- [Tool design guidelines](#tool-design-guidelines)
- [Testing](#testing)
- [Running against a real Emby server](#running-against-a-real-emby-server)
- [Docs that are generated](#docs-that-are-generated)
- [The website](#the-website)

## Setup

You need Go 1.26 or later, and Docker for building the image.

```sh
git clone https://github.com/mattoddie/emby-mcp.git
cd emby-mcp
make test           # runs all tests against a mock Emby
```

| Target | What it does |
|---|---|
| `make build` | Build a static binary to `bin/emby-mcp`. Pass `VERSION=1.2.3` to stamp a version. |
| `make test` | `go test ./...` |
| `make lint` | Fail if any file needs `gofmt`, then run `go vet ./...` |
| `make docs` | Regenerate [`docs/tools.md`](tools.md) and the tools and prompts in [`mcpb/manifest.json`](../mcpb/manifest.json) from the tool definitions |
| `make docs-check` | Fail if either is out of date, or if `server.json` (the MCP Registry entry) doesn't match the manifest's settings (CI runs this) |
| `make image` | Build the Docker image as `emby-mcp:$(VERSION)` |
| `make mcpb` | Build the Claude Desktop bundles, one per OS and architecture, in `dist/`. See [Releasing](releasing.md#about-the-bundles). |

The project deliberately has only two direct dependencies: the official MCP Go SDK, [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk), and [`github.com/google/jsonschema-go`](https://github.com/google/jsonschema-go), which the SDK uses for input schemas. Everything else, including the Emby client, is the standard library. Please discuss before adding a dependency.

## Project layout

```text
cmd/
├── emby-mcp/            # the binary: serve, check, healthcheck and version commands
├── gen-tool-docs/       # writes docs/tools.md from the registered tools and prompts
└── mcpb/                # keeps mcpb/manifest.json's tools and prompts current, checks server.json, and packs the .mcpb bundles
internal/
├── config/              # environment variables → typed Config (validation lives here)
├── emby/
│   ├── client.go        # Client: the X-Emby-Token header, TLS (pinning, CA file), retries, no redirects, errors with hints,
│   │                    #   the user and library caches, finding the default user, item queries
│   ├── dryrun.go        # dry runs: records the requests a call would send instead of sending them
│   ├── format.go        # reading Emby's objects, and compact summaries of items
│   └── embytest/        # an in-memory mock Emby with a small seeded library, for the tests
├── httpserver/          # stateless Streamable HTTP, bearer auth, Host and Origin allow-list, /health
└── tools/
    ├── tools.go         # the registration framework: Def, Gate, add[In](), schemas with enums and ranges, dry-run results
    ├── server.go        # NewServer(): server instructions, registers every toolset and the prompts
    ├── helpers.go       # cleaning ID and name lists, resolving an item by ID or title, bulk workers, list results
    ├── audit.go         # the audit log (EMBY_AUDIT_LOG): middleware recording calls that change things, secret redaction
    ├── prompts.go       # the built-in MCP prompts
    ├── raw.go           # the raw API tool, with its path checks and delete gate
    └── <toolset>.go     # one file per toolset: library, metadata, collections, playlists, users, serverops (the server toolset)
```

`mcpb/` holds the MCP Bundle manifest and icon for Claude Desktop.

## How a request flows

1. An MCP client calls a tool, for example `emby_add_tags {item_ids: ["101", "102"], tags: ["Christmas"]}`.
2. The MCP SDK validates the arguments against the tool's JSON Schema, which was inferred from its Go input struct (with enums and minimum/maximum from the `Def`), and decodes them into that struct.
3. The handler registered by `add()` runs. It cleans and checks the IDs and tag names (`cleanIDs`, `cleanNames`), finds the default user (`Client.User`, cached), and reads the items' current tags in batches of 100 with `Client.ItemsByID`.
4. It skips items that already have the tag (matching without regard to case, as Emby does), then sends `POST /Items/{Id}/Tags/Add` with `{"Tags":[{"Name":"Christmas"}]}` for each remaining item, four at a time (`forEach`).
5. `emby.Client` sends the request to `<EMBY_URL>/emby/...` with the API key in `X-Emby-Token`. It never follows a redirect. On `429` it waits and retries up to three times, and on `502`, `503` or `504` too, except for a `POST`, which Emby may already have acted on. Other failures become an `emby.Error` with Emby's message and a hint, such as checking the API key on a `401`.
6. The handler returns a summary: how many items changed, which were already tagged, which weren't found, and what happened to each. `add()` serialises it to indented JSON text. A returned error becomes `isError: true` with the message, plus whatever the handler did before it failed, so the model can see what went wrong and what changed.

Tools that edit metadata (`emby_update_item`, and `emby_add_tags` with `lock`) read the whole item and send it back with `POST /Items/{Id}`, because that endpoint replaces the item's metadata with the body. `saveItem` in `metadata.go` refuses to save an item that's missing a key field, then reads the item back and warns about values that vanished and about cast and crew that Emby re-linked to a different person with the same name.

## Gates and dry runs

Every tool has a `Gate`, which decides whether `add()` registers it at all:

| Gate | Registered when | Use it for |
|---|---|---|
| `Read` (the default) | Always | Tools that only read. |
| `Write` | `EMBY_ALLOW_WRITES` | Anything that changes the server: tags, metadata, collections, playlists, watched state, refreshes, scans and tasks. |
| `Delete` | `EMBY_ALLOW_WRITES` and `EMBY_ALLOW_DELETES` | Tools that delete something. Today that's only collections and playlists, never media. |

A tool that isn't allowed isn't registered, so the model never sees it. `EMBY_TOOLSETS` and `EMBY_DENY_TOOLS` are checked in the same place. The raw API tool is a `Read` tool that gates each call itself, by method and path, and tells `add()` with `Changes` which calls change things, so they get a dry run and are audited.

Every `Write` and `Delete` tool gets a `dry_run` argument automatically. When a call passes `dry_run: true`, `add()` runs the handler with a context from `emby.WithDryRun`. In that context `Client.Do` records each request other than `GET` instead of sending it and answers as if Emby had accepted it, while `GET`s still go through, so the call is validated against the live state. A handler can call `emby.SetCurrent` (or `SetPreview` for a large body) just before a change, so the preview shows the current state and a field-by-field diff. `add()` then returns `{dry_run, would_send, current, changes, note, plan}`, where `plan` is what the handler returned. Bulk work runs one request at a time in a dry run, so the recorded requests are in order.

## Adding a tool

Tools are defined with the generic `add[In]()` in the file for their toolset. Here's a complete example from `users.go`:

```go
add(c, Def{
	Toolset: "users", Name: "emby_set_favorite", Title: "Add or remove favourites",
	Description: "Add items to a user's favourites, or take them out (up to 500 per call). Favourites are per user.",
	Gate:        Write, Idempotent: true,
}, func(ctx context.Context, a struct {
	ItemIDs  []string `json:"item_ids" jsonschema:"IDs of the items"`
	Favorite bool     `json:"favorite" jsonschema:"true to add to favourites, false to remove"`
	userArgs
}) (any, error) {
	ids, err := cleanIDs(a.ItemIDs, "item")
	if err != nil {
		return nil, err
	}
	user, err := cl.User(ctx, a.User)
	if err != nil {
		return nil, err
	}
	uid := emby.Str(user, "Id")
	return userDataChange(ctx, c, uid, ids, func(id string, ud Obj) (bool, error) {
		if emby.Bool(ud, "IsFavorite") == a.Favorite {
			return false, nil
		}
		path := "/Users/" + uid + "/FavoriteItems/" + id
		if !a.Favorite {
			path += "/Delete"
		}
		return true, cl.Post(ctx, path, nil, nil, nil)
	}, map[bool]string{true: "added to favourites", false: "removed from favourites"}[a.Favorite], emby.Str(user, "Name"))
})
```

### The input struct

The input struct **is** the tool's argument schema:

- The `json` tag names the argument. Use `snake_case`.
- `omitempty` makes it optional. Without it, the argument is required.
- The `jsonschema` tag is the argument's description, which the model reads. Every argument needs one.
- Use pointers (`*string`, `*int`, `*bool`, `*float64`) for optional values where an empty or zero value means something, so "not given" and "set to empty" differ. `emby_update_item` relies on this to clear a field.
- Embedded structs, such as `userArgs`, `rawArgs`, `confirmArgs` or `tagArgs`, add their fields to the tool.
- Tools without arguments take `NoArgs`.
- Don't add `dry_run` yourself: `add()` adds it to tools that can change things.

### The `Def`

| Field | Effect |
|---|---|
| `Toolset` | Required. The tool is only registered when this toolset is enabled. |
| `Name`, `Title`, `Description` | The tool's name (`emby_<verb>_<thing>`), a short human title, and the description the model reads. |
| `Gate` | `Read` (the default, always registered), `Write` (needs `EMBY_ALLOW_WRITES`) or `Delete` (needs `EMBY_ALLOW_WRITES` and `EMBY_ALLOW_DELETES`). See [Gates and dry runs](#gates-and-dry-runs). |
| `ReadOnly` | Overrides the `readOnlyHint` annotation, which defaults to `Gate == Read`. |
| `Destructive` | Sets the `destructiveHint` annotation for tools that aren't read-only. |
| `Idempotent` | Repeating the call with the same arguments has no further effect. Sets `idempotentHint`. |
| `OpenWorld` | The tool reaches outside the Emby server. Sets `openWorldHint`. |
| `Enums` | Restricts string arguments (or the items of a string list) to fixed values, keyed by argument name. |
| `Ranges` | Sets the minimum and maximum of number arguments, keyed by argument name. Validate in the handler too. |
| `Keys` | Restricts the keys of object arguments, keyed by argument name. |
| `Changes` | For a `Read` tool that can still change things (the raw API tool): says whether a call with these arguments does, so it gets `dry_run` and is audited. |

### Helpers

| Helper | Use it for |
|---|---|
| `cleanIDs(ids, what)` | Trim, de-duplicate and check a list of item IDs, at most 500 (`maxBatch`). |
| `cleanNames(names, what, max)` | Trim and de-duplicate names such as tags, without regard to case, and check their length. |
| `resolveItem(ctx, c, userID, ref, types...)` | Find one item by ID, or by a title that matches exactly one, with errors that list the matches. |
| `c.Client.User(ctx, ref)`, `UserID()` | The user named in a `user` argument, or the default user (`EMBY_USER`, or the first administrator). |
| `c.Client.Library(ctx, ref)` | A library by name or ID. |
| `c.Client.Items()`, `Item()`, `ItemsByID()` | Item queries as a user. |
| `forEach(ctx, jobs, fn)` | Run a bulk change with a few workers (one at a time in a dry run). |
| `listResult()`, `pagedResult()` | List output as `{total, returned, items}`, with `offset` and `next_offset` when paged. |
| `emby.Summary()`, `emby.Detail()` | Compact summaries of items for tool output. |
| `emby.SetCurrent()`, `emby.SetPreview()` | Give a dry run the state a change would replace. |

### Checklist for a new tool

1. Put it in the right `internal/tools/<toolset>.go`. If it needs a new toolset, add it to `Toolsets` in `internal/config/config.go`, describe it in `toolsetInfo` in `cmd/gen-tool-docs/main.go`, and register it in `NewServer()`.
2. Set its `Gate` correctly: anything that changes the server is `Write`, and anything that deletes is `Delete`. Add it to the lists in `gating_test.go`.
3. Validate every value before it reaches Emby, and say what's allowed in the description. Check IDs with `cleanIDs` or `emby.CheckID` before they go into a path.
4. Return a **compact summary** and offer `raw: true` for Emby's full object where it helps. Never return API keys, tokens or passwords.
5. Write descriptions for the model: what the tool does, when to use it, limits, and side effects.
6. Add tests in `internal/tools/<toolset>_test.go` against the mock, including a dry run for a tool that changes things.
7. Run `make docs` and commit the updated `docs/tools.md` and `mcpb/manifest.json`. If it's a notable feature, update the README table and [CHANGELOG](../CHANGELOG.md).

## Tool design guidelines

- **Fewer, broader tools beat many narrow ones.** One search tool with many optional filters is easier for a model than a tool per filter.
- **Built for bulk.** Curation means hundreds of items. Take lists of IDs, skip items that are already done, and report per item, so one call does a whole batch.
- **Never surprise.** Changes are explicit and validated. Anything that can lose work, such as a "replace all" refresh or a delete, needs `confirm: true`. Nothing deletes media.
- **Don't wipe data.** When an endpoint replaces a whole object, read it in full first, send it back with only the intended changes, and check the result.
- **Errors should teach.** Return messages that tell the model, or the user, how to fix the call, such as the list of user or library names.
- **Do what Emby's own apps do.** Use the documented endpoints in `/emby/openapi.json`, as the Emby web app uses them.

## Testing

Tests use Go's `testing` package against `embytest.Server` (`internal/emby/embytest`), an `httptest` server that imitates the parts of Emby's API that emby-mcp uses. It checks the `X-Emby-Token` header against `embytest.APIKey` and holds a small seeded library: five films (`ElfID`, `HomeAloneID`, `HeatID`, `ArrivalID`, `InceptionID`) with genres, tags, people and TMDb IDs, a film with no metadata (`NoMetaID`), a series, a collection, a playlist, two libraries and two users (an administrator and a guest with a parental limit and a blocked tag). Tagging, item saves, collections, playlists, played state and favourites, refreshes, sessions, the activity log and scheduled tasks all work against it.

`Requests` records everything the mock received, `Calls("POST /Items/101/Tags/Add")` returns the matching requests, `Writes()` counts requests other than `GET` (useful to prove a dry run sent nothing), and `TagNames(id)` reads an item's tags. `Fail` makes a route answer with an error status, and `DropOnUpdate` makes an item save lose a field, to test the safety checks.

`newHarness(t, opts)` in `internal/tools/helpers_test.go` starts the mock, a client connected to it and an in-memory MCP session with the tools `opts` allows: `writable()` or `readOnly()`, or `config.AllTools()` for everything. `h.ok()` runs a tool and decodes its JSON (failing on a tool error), `h.fails()` checks a tool fails with a given message, and `h.tools()` lists what's registered. A typical test:

```go
func TestDryRunTags(t *testing.T) {
	h := newHarness(t, writable())
	before := h.mock.Writes()
	out := h.ok("emby_add_tags", map[string]any{"item_ids": []string{embytest.HeatID, embytest.ArrivalID}, "tags": []string{"Must see"}, "dry_run": true})
	if h.mock.Writes() != before {
		t.Fatal("a dry run sent a change")
	}
	if out["would_send"].(map[string]any)["path"] != "/Items/"+embytest.HeatID+"/Tags/Add" {
		t.Errorf("would_send: %v", out)
	}
}
```

`TestGates` in `gating_test.go` checks every tool against its expected gate, and that a read-only server registers none of the others. `TestToolsetsAndDenyList` and `TestAnnotations` cover the rest of the registration rules.

Run one package or test with `go test ./internal/tools -run TestDryRunTags -v`.

## Running against a real Emby server

There's no Emby sandbox, so anything you try with writes on changes a real library that other people may use. Keep writes off until you're sure, use `dry_run: true` first, and try changes on a test library or a few items.

With the [MCP Inspector](https://github.com/modelcontextprotocol/inspector):

```sh
make build
export EMBY_URL=http://emby.lan:8096 EMBY_API_KEY=<your-api-key>
./bin/emby-mcp check
npx @modelcontextprotocol/inspector ./bin/emby-mcp
```

Or build the image and point Claude Code at it:

```sh
make image
docker run --rm -p 8080:8080 --env-file .env emby-mcp:0.0.0-dev
claude mcp add --transport http emby-dev http://localhost:8080/mcp --header "Authorization: Bearer $MCP_AUTH_TOKEN"
```

`.env` is in `.gitignore`. Keep it that way, and never paste real API keys, server addresses, user names or file paths into issues, tests or fixtures. If you capture a real response from Emby to improve the mock, anonymise it first.

## Docs that are generated

[`docs/tools.md`](tools.md) is generated by `cmd/gen-tool-docs`. The program builds the server with every tool enabled, records each tool's toolset and gate as it's registered, reads the tools and prompts back over MCP, and writes the reference. Don't edit `docs/tools.md` by hand. Change the tool's title, description or `jsonschema` tags and run `make docs`. CI runs `make docs-check` and fails if the committed file is stale.

`make docs` also regenerates the `tools` and `prompts` lists in [`mcpb/manifest.json`](../mcpb/manifest.json), the Claude Desktop bundle's manifest, with `cmd/mcpb`. It starts the server the way the manifest's default settings do (read-only, every toolset but `raw`), so the list matches what a fresh install shows. The rest of the manifest is edited by hand. If you add a setting there, map it to its variable in `server.mcp_config.env` and give it a `default` (`""` for optional text), because Claude Desktop passes an unfilled setting without a default through as the literal `${user_config.<key>}`. The config package treats that, and an empty value, as unset.

## The website

The site at [emby-mcp.mattoddie.dev](https://emby-mcp.mattoddie.dev) lives in `website/`. It has a hand-built landing page plus these docs rendered with Astro Starlight. It has its own `package.json`, so its dependencies never reach the server or the Docker image. It's one of the family of servers listed at [mcp.mattoddie.dev](https://mcp.mattoddie.dev).

The Markdown in `docs/` is the single source: the site copies it in at build time. It turns each page's first `# Heading` into the page title, drops hand-written "Contents" lists, rewrites links between `.md` files to site URLs and converts GitHub alerts (`> [!NOTE]`) to Starlight asides. So write docs as normal GitHub Markdown, start each page with a `# Title`, and link between pages with relative `.md` links.

To preview the site locally:

```sh
cd website
npm ci
npm run dev
```
