# Compatibility

> [!IMPORTANT]
> emby-mcp is an independent community project. It isn't made, endorsed or supported by Emby LLC. It uses Emby's documented REST API, the same one Emby's own apps use, with an API key you create on your server.

## Emby versions

| Emby Server | Status |
|---|---|
| 4.10 | ✅ Tested against 4.10.2 on Linux (a QNAP NAS). |
| 4.8 and 4.9 | Expected to work, but not tested. |
| Older than 4.8 | Not expected to work. Bulk tagging needs the tag add and remove endpoints (`/Items/{Id}/Tags/Add` and `/Items/{Id}/Tags/Delete`). |

The server describes its own API at `/emby/openapi.json`, for example `http://emby.lan:8096/emby/openapi.json`. That's the reference for the [raw API tool](usage.md#the-raw-api-tool), and the place to check if a tool fails on your version. Please [report](https://github.com/mattoddie/emby-mcp/issues/new/choose) anything that doesn't work on yours, with the version from `emby-mcp check`.

## What it covers

| Area | Read | Change |
|---|---|---|
| Libraries | ✅ name, type, folders, metadata language and country, whether collections are imported, NFO saving, refresh interval | ✅ scan for new files |
| Films, series, episodes, music and other items | ✅ search with filters, full details, media streams, similar items, recently added, continue watching | ✅ metadata fields and locks, metadata refreshes |
| Tags | ✅ the tags in use, with counts | ✅ add and remove in bulk, rename and merge |
| Genres, studios, parental ratings and years | ✅ the values in use, with counts | ✅ genres and studios per item |
| Collections (box sets) | ✅ list and contents | ✅ create, add to, remove from, rename, describe; delete with `EMBY_ALLOW_DELETES` |
| Playlists | ✅ list and contents | ✅ create, add to, remove from; delete with `EMBY_ALLOW_DELETES` |
| Users | ✅ name, administrator, disabled, hidden, last sign-in and activity, parental limit | ✅ watched state and favourites per user |
| The server | ✅ name, version, pending update or restart, sessions and now playing, activity log, scheduled tasks | ✅ run a scheduled task |

### Not covered

- **Deleting media.** There's no tool that deletes films, episodes or files, on purpose, and the [raw API tool](usage.md#the-raw-api-tool) refuses the endpoints that would. Delete media in Emby itself.
- **Images and artwork.** Refreshes can download them, but there's no tool to choose or upload one.
- **Editing cast and crew** by hand. They come from metadata providers.
- **Managing users, API keys, plugins and the server's settings.** Use the Emby dashboard. The raw API tool can reach them with `EMBY_ALLOW_DELETES`, which is deliberately a separate switch.
- **Controlling playback** on a device, and Live TV.

## Jellyfin

Jellyfin began as a fork of Emby, but its API has since diverged, in paths, authentication and fields. emby-mcp isn't tested with Jellyfin and isn't expected to work with it. Jellyfin isn't supported.

## How emby-mcp talks to Emby

| | Detail |
|---|---|
| API | `<EMBY_URL>/emby/...`, Emby's REST API. Items are read as the default user (`/Users/{UserId}/Items`), so watched state and parental limits are that user's. |
| Authentication | The API key in the `X-Emby-Token` header of every request. |
| Tags | Added and removed with `POST /Items/{Id}/Tags/Add` and `/Tags/Delete`, which change only the tags. Locking a field, or editing metadata, saves the whole item with `POST /Items/{Id}`, as Emby's metadata editor does. |
| Caching | The list of users and the list of libraries are reused for a minute. Everything else is read on each call. |
| Rate limits | Emby doesn't document any. emby-mcp sends at most four requests at a time for bulk changes, and retries a few times with increasing waits on `429`, and on `502`, `503` or `504` for reads. |
| Transport | `http://` or `https://`, as in `EMBY_URL`. https is verified against the system's certificate authorities, unless a certificate is pinned with `EMBY_TLS_FINGERPRINT`, a CA is given with `EMBY_CA_FILE`, or `EMBY_VERIFY_SSL=false`. Redirects aren't followed. |

## Running emby-mcp

| Component | Requirement |
|---|---|
| Docker image | `linux/amd64` or `linux/arm64` (for example Raspberry Pi 4/5) |
| Go (to build without Docker) | 1.26 or later |
| Operating system for the binary | Linux and macOS. Windows should work, but isn't tested. |
| Network | A connection to the Emby server, on its port (8096 for http and 8920 for https by default, or your reverse proxy's). No internet access is needed. |
| MCP clients | Any client supporting Streamable HTTP or stdio. See [Connecting MCP clients](clients.md). |

The server is built on the official [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) and supports tools, prompts and tool annotations.

## Help improve this page

Tried emby-mcp on another Emby version, found a field that's missing, or a tool that behaves differently on your server? [Open an issue](https://github.com/mattoddie/emby-mcp/issues/new/choose) or start a [discussion](https://github.com/mattoddie/emby-mcp/discussions). Please remove your API key, server address and user names first.
