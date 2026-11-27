# Configuration

emby-mcp is configured entirely through environment variables. With Docker Compose, put them in `.env`. Copy [`example.env`](../example.env) to get started.

- [Emby server](#emby-server)
- [https and certificates](#https-and-certificates)
- [What the assistant may do](#what-the-assistant-may-do)
- [Audit log](#audit-log)
- [Timeouts and retries](#timeouts-and-retries)
- [MCP transport](#mcp-transport)
- [Docker secrets (`_FILE` variables)](#docker-secrets-_file-variables)
- [Choosing toolsets](#choosing-toolsets)
- [Keeping the tool list small](#keeping-the-tool-list-small)
- [Example configurations](#example-configurations)

The server checks the configuration at startup. If something is wrong, it exits with a clear message such as `EMBY_ALLOW_WRITES must be true or false, got "maybe"`. If the container keeps restarting, check `docker compose logs`. Startup doesn't contact Emby, though: run `emby-mcp check` to test the address and the API key.

## Emby server

| Variable | Default | Description |
|---|---|---|
| `EMBY_URL` | **required** | The address you open Emby at, with the port, such as `http://emby.lan:8096` or `https://emby.example.com`. It must start with `http://` or `https://`, and can't contain a user name, password, query string or fragment. A trailing `/` or `/emby` is removed. |
| `EMBY_API_KEY` | **required** | An API key from the Emby dashboard: **Advanced → API Keys** (**Settings → API Keys** in newer versions), **New API Key**. It's sent in the `X-Emby-Token` header of every request. Use `EMBY_API_KEY_FILE` with Docker secrets where you can. |
| `EMBY_USER` | the first administrator | Whose view of the library the tools use: watched state, favourites, resume points and parental limits. A user name (matched without regard to case) or ID. When empty, the tools use the first enabled administrator in alphabetical order. Most tools also take a `user` argument for one call. |

An Emby API key isn't tied to a user and acts with administrator rights. `EMBY_USER` only chooses whose watched state and favourites the tools read and change; it doesn't limit what the key can do. See [Security](security.md#the-api-key).

When `EMBY_URL` uses plain `http://` to anything but `localhost` or a loopback address, the server logs a note at startup that the API key crosses the network unencrypted. That's normal on a home network. Use https if the network isn't trusted.

## https and certificates

| Variable | Default | Description |
|---|---|---|
| `EMBY_VERIFY_SSL` | `true` | Check the certificate of an `https://` server against the system's certificate authorities. `false` skips the check, so the API key can be intercepted, and the server logs a note at startup. Prefer `EMBY_TLS_FINGERPRINT` or `EMBY_CA_FILE`. |
| `EMBY_TLS_FINGERPRINT` | | Pin the server's certificate by its SHA-256 fingerprint, for a self-signed certificate. `AA:BB:...` (32 pairs) or 64 hex digits, optionally prefixed `sha256:`. Several can be given, separated by commas, for a certificate change. A pinned certificate is trusted on its fingerprint alone: the chain and name aren't checked. |
| `EMBY_CA_FILE` | | Path to a PEM file with the certificate authority that signed Emby's certificate, such as your own home CA. It's trusted alongside the system's authorities. |

Emby has no certificate of its own: https is usually a reverse proxy in front of it, or a certificate you gave Emby. With a certificate from a public authority, such as Let's Encrypt, nothing needs setting.

`EMBY_TLS_FINGERPRINT` and `EMBY_CA_FILE` need an `https://` `EMBY_URL`. To find a certificate's fingerprint:

```sh
openssl s_client -connect emby.lan:8920 </dev/null 2>/dev/null | openssl x509 -noout -fingerprint -sha256
```

Its output, `sha256 Fingerprint=AA:BB:...`, can be pasted into `EMBY_TLS_FINGERPRINT` as it is.

## What the assistant may do

| Variable | Default | Description |
|---|---|---|
| `EMBY_ALLOW_WRITES` | `false` | Register tools that change the library: tags, metadata, collections, playlists, watched state and favourites, metadata refreshes, library scans and scheduled tasks, and `POST` requests in the raw API tool. |
| `EMBY_ALLOW_DELETES` | `false` | Also register the tools that delete collections and playlists. They never delete media files: the films and episodes stay in the library. If you also enable the raw API tool, it allows that tool's `DELETE` requests and the other requests listed under [Choosing toolsets](#choosing-toolsets), but never one that deletes a library item. Needs `EMBY_ALLOW_WRITES=true`. |
| `EMBY_TOOLSETS` | `all` | Comma-separated list of [toolsets](#choosing-toolsets) to expose. `all` means every toolset except `raw`, which has to be named, for example `all,raw`. |
| `EMBY_DENY_TOOLS` | | Comma-separated tool names never to register, such as `emby_scan_library,emby_run_scheduled_task`. Use it to drop a few tools from a toolset you otherwise want. |

Boolean variables accept `true`/`false`, `1`/`0`, `yes`/`no` and `on`/`off`. Anything else stops the server with an error.

A variable set to an empty value counts as unset and gets its default. So does one set to exactly `${user_config.<key>}`, which is how Claude Desktop passes an [extension setting](clients.md#claude-desktop) that was left empty.

The [tool reference](tools.md) labels every tool with the setting it needs:

| Label | Registered when |
|---|---|
| 🟢 read | Always |
| 🟠 write | `EMBY_ALLOW_WRITES=true` |
| 🔴 delete | `EMBY_ALLOW_WRITES=true` and `EMBY_ALLOW_DELETES=true` |

Tools that aren't allowed are **not registered at all**. The assistant can't see them, so it can't call them or be talked into calling them. After changing `EMBY_ALLOW_WRITES`, `EMBY_ALLOW_DELETES`, `EMBY_TOOLSETS` or `EMBY_DENY_TOOLS`, restart the server and reconnect your MCP client so it fetches the new tool list.

An Emby API key has no read-only mode: whatever the switches say, the key itself can do anything an administrator can. The switches are what keep the assistant read-only. See [Security](security.md).

## Audit log

| Variable | Default | Description |
|---|---|---|
| `EMBY_AUDIT_LOG` | (off) | Record every call of a tool that changes things. A file path appends JSON Lines to that file, which is created with mode `0600` if it doesn't exist. `stderr` writes the lines to standard error, next to the server's other logs. |

Each line is one call of a write or delete tool (🟠 or 🔴 in the [tool reference](tools.md)), or of `emby_api_request` with a method other than `GET`. Calls that failed, dry runs, and calls refused for invalid arguments are logged too. Read tools aren't.

```json
{"time":"2026-11-22T20:14:03.120Z","tool":"emby_add_tags","arguments":{"item_ids":["101","102","103"],"lock":true,"tags":["Christmas"]},"outcome":"ok"}
```

| Field | Meaning |
|---|---|
| `time` | When the call finished, in UTC (RFC 3339). |
| `tool` | The tool name, such as `emby_add_tags`. |
| `arguments` | The arguments as the assistant sent them, with passwords, tokens, API keys and other secrets `[redacted]`. |
| `outcome` | `ok` or `error`. |
| `error` | The error message, when `outcome` is `error`. |

The server checks at startup that it can write to the file, and refuses to start if the directory doesn't exist or isn't writable. A write that fails later doesn't stop the tool call: the server logs one warning to standard error and carries on. The file is opened for each entry, so you can rotate or delete it while the server runs.

In Docker, put the file on a volume:

```yaml
services:
  emby-mcp:
    environment:
      EMBY_AUDIT_LOG: /audit/emby-mcp.jsonl
    volumes:
      - ./audit:/audit
```

Create the directory first and let UID 65532 write to it, for example `mkdir audit && sudo chown 65532 audit`. `EMBY_AUDIT_LOG=stderr` needs no volume: the lines appear in `docker compose logs`.

## Timeouts and retries

| Variable | Default | Description |
|---|---|---|
| `EMBY_TIMEOUT_MS` | `30000` | Timeout for each request to Emby, in milliseconds. Raise it for a slow server or a big library. |

emby-mcp retries a few times, with increasing waits, when Emby answers `429 Too Many Requests`, and for reads also `502`, `503` or `504`. A change (a `POST`) is never repeated after a `5xx`, since Emby may already have made it. It never follows redirects, because every request carries the API key, and refuses responses larger than 64 MiB.

The list of users and the list of libraries are reused for a minute, since the tools look them up often. Everything else is read from Emby on each call. Bulk changes send at most four requests at a time, so a small NAS isn't swamped.

## MCP transport

| Variable | Default | Description |
|---|---|---|
| `MCP_TRANSPORT` | `http` in the Docker image (`stdio` for the binary) | `http` for Streamable HTTP, or `stdio`. |
| `MCP_HTTP_HOST` | `127.0.0.1` (`0.0.0.0` in the Docker image) | Address to listen on (HTTP only). |
| `MCP_HTTP_PORT` | `8080` | Port to listen on (HTTP only). |
| `MCP_AUTH_TOKEN` | (required for http) | Every request to `/mcp` must send `Authorization: Bearer <token>`. The server refuses to start in http mode without it, or if it's shorter than 16 characters. Generate one with `openssl rand -hex 32`. |
| `MCP_ALLOW_UNAUTHENTICATED` | `false` | Start the http transport without `MCP_AUTH_TOKEN`. Only for a server that sits behind something else that authenticates every request. It also needs `MCP_ALLOWED_HOSTS`, and the server logs a warning at startup. |
| `MCP_ALLOWED_HOSTS` | | Comma-separated allow-list of `host` or `host:port` entries, such as `emby-mcp.lan,emby-mcp.lan:8080`. Requests whose `Host` header isn't listed are rejected (an entry without a port matches any port), and so are requests from browser pages whose `Origin` isn't listed (an entry without a port matches only the default port, 80 or 443; list `host:port` for a page on another port). `Origin: null` or an empty `Origin` is always rejected. This protects against DNS-rebinding and cross-site attacks from web pages in your browser. |

The HTTP server exposes these endpoints:

| Path | Method | Purpose |
|---|---|---|
| `/mcp` | `POST` | The MCP endpoint (stateless Streamable HTTP, JSON responses). Other methods get `405`. Bodies must be JSON and at most 1 MiB. |
| `/health`, `/healthz` | `GET` | Liveness check. Returns `{"status":"ok"}` and doesn't contact Emby. |

## Docker secrets (`_FILE` variables)

Every variable can also be read from a file: set `<NAME>_FILE` to the file's path. Leading and trailing whitespace is trimmed. This works with [Docker secrets](https://docs.docker.com/compose/how-tos/use-secrets/) and Kubernetes secret volumes:

```yaml
services:
  emby-mcp:
    image: ghcr.io/mattoddie/emby-mcp:latest
    environment:
      EMBY_URL: http://emby.lan:8096
      EMBY_API_KEY_FILE: /run/secrets/emby_api_key
      MCP_AUTH_TOKEN_FILE: /run/secrets/mcp_auth_token
    secrets: [emby_api_key, mcp_auth_token]
    ports: ["8080:8080"]

secrets:
  emby_api_key:
    file: ./secrets/emby_api_key.txt
  mcp_auth_token:
    file: ./secrets/mcp_auth_token.txt
```

If both `NAME` and `NAME_FILE` are set, the file wins. `EMBY_CA_FILE` is already a path to a file, so it doesn't need the `_FILE` form.

## Choosing toolsets

Every tool's name, description and argument schema is sent to the model, which uses up context. Exposing only what you need keeps the assistant faster and more focused:

| Toolset | Use it for |
|---|---|
| `library` | Finding and reading: the server and libraries, search, item details, the tags and genres in use, similar items, recently added and continue watching. **Always include it.** The other toolsets rely on its search to find item IDs. |
| `metadata` | Tagging, renaming and merging tags, editing metadata and locks, and metadata refreshes |
| `collections` | Collections (Emby box sets) |
| `playlists` | Playlists |
| `users` | The user list, and each user's watched state and favourites |
| `server` | What's playing now, the activity log, scheduled tasks and library scans |
| `raw` | Any path of Emby's REST API. Handy, but gives broad access, so it isn't part of `all`: list it by name to enable it. Tokens, API keys and passwords are redacted from its output. Without writes it only sends `GET`. With writes on it sends `POST` too, and needs `EMBY_ALLOW_DELETES` for `DELETE` and for paths that delete things, restart or shut down the server (`/System/Restart`, `/System/Shutdown`), manage API keys and sign-ins (`/Auth/...`), add users or change a user's password, policy or configuration, change library folders (`/Library/VirtualFolders`), install or configure plugins, or change the server configuration (`/System/Configuration`). It never deletes library items (`/Items/{Id}/Delete`, `/Items/Delete` or a `DELETE` under `/Items`), whatever the settings, because Emby deletes their media files too. |

The [tool reference](tools.md) lists the tools in each toolset, and [Keeping the tool list small](#keeping-the-tool-list-small) suggests some combinations.

## Keeping the tool list small

Most MCP clients send every tool's name, description and argument schema to the model with each request. emby-mcp's list is moderate by MCP standards: about 16 KB for the 16 read-only tools of the default `all`, and 34 KB for 31 tools with writes on. Even so, a narrower list leaves more room for your conversation, helps the model pick the right tool, and keeps the rest out of its reach.

Clients with their own tool search, such as Claude Code, load tool definitions only when the model needs them, so they handle the full list fine. `EMBY_TOOLSETS` still limits what the model can reach.

The sizes are the `tools/list` result the client receives, measured by starting the server. "With writes" is `EMBY_ALLOW_WRITES=true`.

| Goal | `EMBY_TOOLSETS` | Read-only | With writes |
|---|---|---:|---:|
| Browsing and recommendations | `library` | 8 tools, 11 KB | 8 tools, 11 KB |
| Tagging and metadata | `library,metadata` | 8 tools, 11 KB | 13 tools, 19 KB |
| Collections | `library,collections` | 10 tools, 12 KB | 13 tools, 15 KB |
| Playlists | `library,playlists` | 10 tools, 12 KB | 13 tools, 15 KB |
| Watched state and the server | `library,users,server` | 12 tools, 13 KB | 16 tools, 17 KB |
| Everything except the raw API tool (default) | `all` | 16 tools, 16 KB | 31 tools, 34 KB |
| Everything, including the raw API tool | `all,raw` | 17 tools, 17 KB | 32 tools, 35 KB |

`EMBY_ALLOW_DELETES=true` adds the 2 delete tools and about 2 KB where the `collections` and `playlists` toolsets are enabled.

## Example configurations

**Read-only** (the safest setup):

```sh
EMBY_URL=http://emby.lan:8096
EMBY_API_KEY=<your-api-key>
MCP_AUTH_TOKEN=...
```

**Curation.** The assistant can tag, edit metadata and build collections, but can't touch playlists, watched state, the server or the raw API:

```sh
EMBY_URL=http://emby.lan:8096
EMBY_API_KEY=<your-api-key>
MCP_AUTH_TOKEN=...
EMBY_ALLOW_WRITES=true
EMBY_TOOLSETS=library,metadata,collections
EMBY_AUDIT_LOG=stderr
```

**Everything**, for a single trusted user, over https with a self-signed certificate:

```sh
EMBY_URL=https://emby.lan:8920
EMBY_API_KEY=<your-api-key>
EMBY_TLS_FINGERPRINT=sha256:<fingerprint>
MCP_AUTH_TOKEN=...
EMBY_ALLOW_WRITES=true
EMBY_ALLOW_DELETES=true
```
