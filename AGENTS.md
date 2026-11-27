# AGENTS.md

Notes for AI coding agents working on emby-mcp, an MCP server for Emby media servers. [CONTRIBUTING.md](CONTRIBUTING.md) is the full contributor guide and [docs/development.md](docs/development.md) explains the architecture. Read them before larger changes.

## Commands

```sh
make lint         # gofmt and go vet
make test         # go test ./...
make docs         # regenerate the generated docs and manifest after changing tools
make docs-check   # CI's check that the generated files match the code
```

## Layout

- `cmd/emby-mcp/`: the server binary
- `internal/tools/`: the MCP tools, one file per toolset
- `internal/emby/`: the Emby API client; `internal/emby/embytest/` is the mock Emby the tests use
- `docs/`: the user docs. `website/` (the site at emby-mcp.mattoddie.dev) copies them in at build time, so edit `docs/`, not the website. `docs/tools.md` is generated: run `make docs` instead of editing it.

## Rules

- The server is read-only by default. Every tool that changes something needs the right gate (`Write`, or `Delete` for anything that deletes): see "Adding or changing tools" in CONTRIBUTING.md. No tool deletes media files.
- Don't add dependencies without asking.
- Never put real secrets, hostnames, IP addresses, user names or file paths in code, tests or fixtures. Use obviously fake values such as `emby.lan` and `192.0.2.10`.
- Add a line under **Unreleased** in CHANGELOG.md for anything users will notice. Tool names, arguments, prompts and configuration variables are the public API, so mark changes to them **Breaking:**.
- Commit messages and pull request titles use Conventional Commits. Pull requests are squash-merged.
