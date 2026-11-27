# Changelog

All notable changes to this project are documented here. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

For emby-mcp, the "public API" is the set of tool names, their arguments, the prompts and the configuration variables. Renaming or removing any of these is a breaking change.

## [Unreleased]

## [0.1.1] - 2026-11-27

### Fixed

- Tools that take item IDs now find collections and playlists too. Emby leaves collections out of an ID lookup unless the request names them, so `emby_add_tags` and `emby_remove_tags` reported a collection as not found, and the collection, playlist and watched-state tools couldn't use one.

## [0.1.0] - 2026-11-27

First release, tested against Emby Server 4.10.2. emby-mcp has 34 tools in 7 toolsets (17 read, 15 write and 2 delete) and 6 prompts.

### Added

- MCP server for Emby media servers, using Emby's documented REST API with an API key (`EMBY_URL`, `EMBY_API_KEY`), and toolsets `library`, `metadata`, `collections`, `playlists`, `users`, `server` and `raw`.
- Read tools for the server and its libraries, a search with filters for text, type, library, genres, tags (or `untagged`), years, ratings, people, studios, watched state, favourites and missing metadata, item details, the tags, genres, studios, ratings and years in use (with counts), similar items, recently added, continue watching, collections, playlists, users, what's playing now, the activity log and scheduled tasks.
- Curation tools, behind `EMBY_ALLOW_WRITES`: add and remove tags on up to 500 items per call, rename or merge a tag, edit an item's metadata and lock fields against refreshes, refresh metadata (a "replace all" refresh needs `confirm: true`), create and change collections and playlists, mark items watched or unwatched and add or remove favourites per user, scan libraries and run scheduled tasks.
- `emby_delete_collection` and `emby_delete_playlist`, behind `EMBY_ALLOW_DELETES` as well. They delete only the collection or playlist, never media files, and need `confirm: true`.
- Saving an item checks it was read in full first, and reports values that went missing and cast or crew that Emby linked to a different person with the same name.
- `dry_run: true` on every tool that changes something, returning the first request it would send, the current state, the fields that would change and the full plan.
- Six built-in prompts: `library_report`, `tag_library`, `build_collection`, `suggest_collections`, `movie_night` and `tidy_metadata`.
- `emby_api_request` (toolset `raw`, off unless listed) for any API path: `GET` only without writes, and `EMBY_ALLOW_DELETES` needed for deletes, server restarts and changes to users, API keys, plugins, library folders and the server configuration. It never deletes library items, since Emby would delete their files. Secrets are redacted from its output.
- `EMBY_USER` to choose whose watched state and favourites the tools use, `EMBY_DENY_TOOLS` to drop single tools, `EMBY_AUDIT_LOG` to record every change as JSON Lines, and `EMBY_TIMEOUT_MS`.
- https to Emby verified against the system's certificate authorities, with `EMBY_TLS_FINGERPRINT` to pin a self-signed certificate, `EMBY_CA_FILE` for your own CA, and `EMBY_VERIFY_SSL`. Redirects are never followed, so the API key can't be sent elsewhere.
- Streamable HTTP (stateless) and stdio transports, with bearer-token auth and a `Host` and `Origin` allow-list for HTTP.
- `serve`, `check`, `healthcheck` and `version` commands. `check` tests the connection and the API key and shows the server, the user the tools act as, the libraries and the item counts.
- `_FILE` variants of every variable for Docker secrets.
- Multi-arch distroless Docker image (`linux/amd64`, `linux/arm64`) that keeps no state, published to GHCR with SBOM and provenance attestation, Claude Desktop extension bundles (`.mcpb`), release binaries for macOS, Linux and Windows, a Homebrew formula (`mattoddie/tap/emby-mcp`) and a listing in the MCP Registry.
- An in-memory mock Emby for tests, and documentation including a tool reference generated from the code.

[Unreleased]: https://github.com/mattoddie/emby-mcp/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/mattoddie/emby-mcp/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/mattoddie/emby-mcp/releases/tag/v0.1.0
