# Troubleshooting

Start with the logs: `docker compose logs emby-mcp`. Configuration errors stop the server at startup with a clear message. Errors from Emby show up in the tool results the assistant sees, and you can usually ask it *"what error did you get?"*. To test the connection and the API key, run `docker compose exec emby-mcp /emby-mcp check` (or `docker compose run --rm emby-mcp check` if the server isn't running).

- [The container keeps restarting](#the-container-keeps-restarting)
- [The client can't connect to the server](#the-client-cant-connect-to-the-server)
- [The server can't reach Emby](#the-server-cant-reach-emby)
- [Emby rejects the API key](#emby-rejects-the-api-key)
- [https and certificates](#https-and-certificates)
- [Errors when changing things](#errors-when-changing-things)
- [Cast or crew linked to the wrong person](#cast-or-crew-linked-to-the-wrong-person)
- [Results look wrong](#results-look-wrong)
- [Tools are missing](#tools-are-missing)
- [Still stuck?](#still-stuck)

## The container keeps restarting

Run `docker compose logs emby-mcp`. The last line names the problem, for example:

| Message | Fix |
|---|---|
| `EMBY_URL is required: …` or `EMBY_API_KEY is required: …` | Set both in `.env`. See [Getting started](getting-started.md#2-set-up-the-server). |
| `EMBY_URL must start with http:// or https://, got "…"` | Add the scheme, such as `http://emby.lan:8096`. |
| `EMBY_URL must not contain a user name or password (user:pass@); use EMBY_API_KEY` | Remove them from the URL. emby-mcp signs in with the API key only. |
| `EMBY_ALLOW_WRITES must be true or false, got "…"` | Fix the value. Booleans accept `true`/`false`, `1`/`0`, `yes`/`no` and `on`/`off`. The same applies to the other switches. |
| `EMBY_ALLOW_DELETES=true also needs EMBY_ALLOW_WRITES=true` | Turn on writes too, or turn deletes off. |
| `unknown toolset "x" in EMBY_TOOLSETS; valid: …` | Fix the typo. The message lists the valid names. |
| `MCP_TRANSPORT must be stdio or http, got "…"` | Fix the value. |
| `MCP_HTTP_PORT must be a positive integer, got "…"` | Fix the value. The same applies to `EMBY_TIMEOUT_MS`. |
| `MCP_AUTH_TOKEN is required for the http transport …` | Set `MCP_AUTH_TOKEN`, for example to the output of `openssl rand -hex 32`. |
| `MCP_AUTH_TOKEN must be at least 16 characters …` | Use a longer token. |
| `EMBY_TLS_FINGERPRINT must be a SHA-256 fingerprint …` | Paste the whole SHA-256 fingerprint: 32 pairs of hex digits. See [https and certificates](configuration.md#https-and-certificates). |
| `EMBY_CA_FILE and EMBY_TLS_FINGERPRINT need an https:// EMBY_URL` | They only apply to https. Remove them, or use the https address. |
| `could not read EMBY_API_KEY_FILE (…)` or `could not read EMBY_CA_FILE (…)` | The file isn't mounted where the variable points. |
| `EMBY_CA_FILE contains no PEM certificates` | The file must hold the CA certificate in PEM form (`-----BEGIN CERTIFICATE-----`). |
| `EMBY_AUDIT_LOG: cannot write to …` | Create the directory and let UID `65532` write to it, or use `EMBY_AUDIT_LOG=stderr`. See [Audit log](configuration.md#audit-log). |

## The client can't connect to the server

1. **Is it running?** `curl http://<docker-host>:8080/health` should return `{"status":"ok"}`.
2. **Right host?** The port is published on the machine running Docker. If Docker runs on another machine (for example with `DOCKER_HOST`), `localhost` won't work.
3. **Right path?** The endpoint is `/mcp`, so the URL is `http://<docker-host>:8080/mcp`.
4. **`401 Unauthorized`:** the client isn't sending `Authorization: Bearer <MCP_AUTH_TOKEN>`, or the token doesn't match. Watch for stray quotes or spaces in `.env`.
5. **`403 Host … is not allowed`:** add the hostname the client uses to `MCP_ALLOWED_HOSTS`, or unset it. **`403 Origin … is not allowed`** means a browser-based client sent an `Origin` that isn't listed: add its `host` (or `host:port` if it isn't on port 80 or 443).
6. **The client expects SSE:** the server is stateless and answers `POST` requests with JSON. Make sure the client is set to *Streamable HTTP* (often called `http`), not `sse`.
7. **stdio clients show nothing:** check that the command has `-i` and not `-t`, and that `MCP_TRANSPORT=stdio` is set when using the image, which defaults to HTTP.

## The server can't reach Emby

These come from `check` or from any tool, as `… could not reach Emby at <EMBY_URL>: …`, with the cause at the end.

**`the host name … doesn't resolve`**

The container can't look up the name in `EMBY_URL`. Names that work on your computer, such as ones from your router, may not resolve inside Docker. Try the Emby machine's IP address, such as `http://192.0.2.10:8096`.

**`connection refused. Is Emby running, and is the port right (8096 for http, 8920 for https)?`**

Nothing is listening at that address and port. Check that Emby is running and that you can open the same address in a browser. If Emby runs on the same machine as Docker, `localhost` in `EMBY_URL` points at the emby-mcp container itself: use the machine's LAN address, or put both containers on one Docker network and use Emby's container name.

**`it didn't answer in time (EMBY_TIMEOUT_MS)`**

Emby is slow to answer, often on a busy NAS or with a very large library. Give it longer, for example `EMBY_TIMEOUT_MS=60000`, and ask for fewer items per call.

**`it answered in plain http; use http:// in EMBY_URL`**

The port speaks http, not https. Use `http://`, or Emby's https port (8920 by default) if https is set up.

**`Emby answered with a redirect to "…", which isn't followed (it would carry the API key)`**

The address redirects somewhere else, often from http to https, or to a different path on a reverse proxy. Set `EMBY_URL` to the address the redirect points at.

**`the response is larger than 64 MiB; ask for fewer items`**

A request asked for too much at once. Ask for a smaller page, such as a lower `limit`.

## Emby rejects the API key

**`… failed (HTTP 401): … Emby rejected the API key.`**

The key is wrong or has been deleted. Check `EMBY_API_KEY` for stray quotes or spaces, and that the key is still listed in the Emby dashboard under **Advanced → API Keys** (**Settings → API Keys** in newer versions). If in doubt, create a new key.

**`… failed (HTTP 403): … Emby refused this for the API key or user.`**

The key worked, but Emby refused the request. Some requests need an administrator user. Check which user the tools act as with `check`, or set `EMBY_USER` to an administrator.

**`there is no Emby user called "…"; the users are …`**

`EMBY_USER`, or a tool's `user` argument, doesn't match a user. Use one of the names listed.

## https and certificates

**`the certificate couldn't be verified (…). Set EMBY_TLS_FINGERPRINT or EMBY_CA_FILE for a self-signed certificate`**

Emby, or the proxy in front of it, has a certificate that the system's certificate authorities don't vouch for, such as a self-signed one, or the name in `EMBY_URL` isn't on the certificate. Pin the certificate with `EMBY_TLS_FINGERPRINT`, or trust the CA that signed it with `EMBY_CA_FILE`. See [https and certificates](configuration.md#https-and-certificates). `EMBY_VERIFY_SSL=false` also works, but leaves the API key open to interception.

**`the Emby server's certificate fingerprint is …, which doesn't match EMBY_TLS_FINGERPRINT`**

The certificate has changed, for example after it was renewed. Check the new fingerprint is really your server's, then update `EMBY_TLS_FINGERPRINT`. During a change you can list both, separated by a comma.

## Errors when changing things

**`… is not an Emby ID (they look like 12345 or a GUID); search for the item to find its ID`**

The tool was given a title or something else where it needs an ID. The assistant should find the ID with `emby_search_items` first.

**`"…" matches more than one item; give the ID of one of: …`**

A title matches several items, such as a film and its remake. Use one of the IDs listed.

**`at most 500 item IDs per call, got …; split them into several calls`** or **`at most 20 tags per call`**

Split the change into several calls. The assistant normally does this itself.

**`replace_all overwrites every unlocked field with downloaded metadata, …`**

A "replace all" refresh needs `confirm: true`, because it can remove tags and edits. Lock the fields you want to keep first, then tell the assistant to go ahead. See [Locks and metadata refreshes](usage.md#locks-and-metadata-refreshes).

**`deleting the collection "…" (id …) can't be undone. Call again with confirm: true once the user agrees`**

Deletes need an explicit confirmation. Tell the assistant you do want it deleted. The same applies to playlists.

**`there is already a collection called "…" (id …)`**

Add to the existing collection, or ask for a second one with the same name (`allow_duplicate: true`).

**`Emby didn't return the item's … field, so saving it could erase data; not changing anything`**

Emby replaces an item's metadata with what it's sent, so `emby_update_item` (and `emby_add_tags` with `lock: true`) won't save an item it couldn't read in full. Nothing was changed. Please report it with your Emby version.

**`These values were on the item before and aren't now: …`** (in a result)

Emby dropped something when the item was saved. The tool reports it so you can put it back. Please report it with your Emby version and the item type.

**`… failed (HTTP 404)`**

The item, collection or playlist doesn't exist any more, or the ID is wrong. Search again.

**`… failed (HTTP 429)`** or **`(HTTP 5xx)`**

Emby is busy or having problems. emby-mcp has already retried reads a few times; changes aren't retried after a `5xx`, in case Emby made them. Check whether the change happened, wait a minute and try again, and check Emby's log if it keeps failing.

## Cast or crew linked to the wrong person

After `emby_update_item`, or `emby_add_tags` with `lock: true`, the result can say that Emby *linked … to a different person with the same name*.

When an item is saved, Emby matches its cast and crew by name, as its own metadata editor does. If two people share a name, the item can end up linked to the wrong one. To link them by their TMDb ID again, refresh the item with `emby_refresh_item` and `mode: "replace_all"` (which needs `confirm: true`). Lock the fields you want to keep, such as Tags, first. Plain tagging without `lock` doesn't save the whole item and isn't affected. See [Cast and crew after a save](usage.md#cast-and-crew-after-a-save).

## Results look wrong

- **Watched state or favourites are someone else's.** They belong to one user. The tools use `EMBY_USER`, or the first administrator. `check` and `emby_get_server_info` show which user that is. Name the user in your request, or set `EMBY_USER`.
- **Items are missing.** The tools see what the default user can see. Parental controls, blocked tags and library access hide items from that user. Use an administrator without limits as `EMBY_USER`.
- **A change doesn't show yet.** Metadata refreshes, library scans and scheduled tasks run in the background on the server. Check on them with `emby_list_scheduled_tasks` or `emby_list_activity`.
- **A new library doesn't appear.** The list of libraries is reused for a minute. Ask again shortly.
- **Tags disappear after a refresh.** A "replace all" refresh overwrites unlocked fields with downloaded metadata. Lock the Tags field when tagging (`lock: true`). See [Locks and metadata refreshes](usage.md#locks-and-metadata-refreshes).

## Tools are missing

Tools are only registered when they're allowed:

- Write tools need `EMBY_ALLOW_WRITES=true`, and the delete tools need `EMBY_ALLOW_DELETES=true` as well.
- Each tool's toolset must be listed in `EMBY_TOOLSETS`, and the tool must not be in `EMBY_DENY_TOOLS`. The raw API tool needs `raw` listed by name.
- Restart the container after changing `.env` with `docker compose up -d`, and reconnect or restart your MCP client so it fetches the new tool list.
- Check the first log line: it shows whether writes are enabled and how many toolsets are loaded.

## Still stuck?

Search the [existing issues](https://github.com/mattoddie/emby-mcp/issues) or [open a bug report](https://github.com/mattoddie/emby-mcp/issues/new?template=bug_report.yml). Include the emby-mcp version (the first log line), the Emby Server version (from `check`), the transport, the tool or command involved and the exact error. **Remove your API key, server address, user names and file paths first.**
