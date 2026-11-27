# Security

emby-mcp gives an AI assistant access to your Emby server with an administrator's API key. This page explains what that means and how to set it up safely. To report a vulnerability, see the [security policy](../SECURITY.md).

- [Threat model](#threat-model)
- [Layers of protection](#layers-of-protection)
- [The API key](#the-api-key)
- [Recommended setups](#recommended-setups)
- [Network exposure](#network-exposure)
- [Checklist](#checklist)

## Threat model

Five things need protecting:

1. **The MCP endpoint.** Anyone who can send requests to `/mcp` can use every registered tool with your API key. That includes browsing the whole library, seeing who is watching what right now and what each user has watched, and, if you've allowed it, retagging, editing and reorganising the library.
2. **The API key.** An Emby API key acts with administrator rights. Anyone who copies it can do anything the Emby dashboard can, through Emby's API directly, whatever emby-mcp's settings say: change users and their passwords, install plugins, and delete items, including their media files.
3. **The assistant's judgement.** A model can misunderstand a request, tag the wrong films, or be steered by malicious text it reads (*prompt injection*), such as a plot summary or title downloaded from the internet, or a document or web page pasted into the same conversation. Changes to the library are seen by everyone in the household, and a careless bulk change can take a while to undo.
4. **The library's integrity.** A "replace all" metadata refresh overwrites unlocked fields, and saving an item can re-link cast and crew who share a name (see [Cast and crew after a save](usage.md#cast-and-crew-after-a-save)). Neither loses media, but both can undo curation work.
5. **Your data, on its way to the model.** Whatever the tools return, such as titles, file paths, user names, what each person has watched and when, and the activity log, is sent to your AI provider as part of the conversation.

The best defence is to limit what's possible, not to rely on the model behaving.

## Layers of protection

| Layer | Setting | What it does |
|---|---|---|
| Write switch | `EMBY_ALLOW_WRITES` (off by default) | When off, no tools that change anything are registered and the raw API tool only allows `GET`. This is the main control: Emby's API keys have no read-only mode. |
| Delete switch | `EMBY_ALLOW_DELETES` (off by default, needs writes) | The tools that delete collections and playlists are only registered with it. They never delete media files. The raw API tool refuses to delete library items whatever the settings, because Emby deletes their files too. For everything else in that tool, it's needed for any `DELETE` and for requests that delete things, restart or shut down the server, manage API keys and sign-ins (`/Auth/...`), add users or change a user's password, policy or configuration, change library folders, install or configure plugins, or change the server configuration. |
| Toolsets and deny list | `EMBY_TOOLSETS`, `EMBY_DENY_TOOLS` | Tools outside the listed toolsets, or on the deny list, don't exist. The model can't see or call them. The raw API tool is only there if you name it. |
| Validation | Built in | Item IDs must look like Emby IDs before they go into a request path, bulk tools take at most 500 items, tag names are checked, and ratings and years are range-checked. A "replace all" refresh and every delete need `confirm: true`. Before saving an item, `emby_update_item` checks it read the whole item, since Emby replaces an item's metadata with what it's sent, and afterwards it reports any value that went missing. The raw API tool doesn't apply these checks. |
| Dry runs | Built in | Every tool that changes something takes `dry_run: true`, which validates the call and returns the first request it would send to Emby, with the current state, the fields that would change and the full plan, without sending anything. |
| Tool annotations | Built in | Each tool is marked read-only or not, and the delete tools destructive, so MCP clients can ask for your confirmation before changes. With writes on, the raw API tool is marked destructive. |
| Server instructions | Built in | With writes on, the model is told that changes affect the real library that everyone sees, to show you its plan and get your agreement before bulk changes, and to use dry runs for big ones. This helps, but it isn't a security boundary. |
| No secret output | Built in | The API key never appears in tool output or logs. `emby_list_users` never shows passwords. The raw API tool redacts access tokens, API keys and passwords from what it returns, and the audit log redacts them from the arguments it records. |
| Requests to Emby | Built in | Redirects are never followed, so the API key can't be sent somewhere else. Responses are capped at 64 MiB. A change is never retried after a `5xx`, in case Emby already made it. Bulk changes send at most four requests at a time. |
| Raw path checks | Built in | The raw API tool refuses paths with `..`, `.` or `//` segments, a query string, control characters or double percent-encoding, so a path can't reach anything but the API path it names. |
| Audit log | `EMBY_AUDIT_LOG` (off by default) | Appends a JSON line for every change the assistant makes or tries to make: the tool, its arguments (with secrets redacted) and whether it worked. It shows after the fact what was changed and when. See [Audit log](configuration.md#audit-log). |
| Endpoint auth | `MCP_AUTH_TOKEN` | Bearer token required on every request, checked in constant time. The http transport refuses to start without one, or with one shorter than 16 characters. `MCP_ALLOW_UNAUTHENTICATED=true` skips it, but only together with `MCP_ALLOWED_HOSTS`, and logs a warning. |
| Host and Origin allow-list | `MCP_ALLOWED_HOSTS` | Rejects requests with an unexpected `Host` header, and browser requests whose `Origin` isn't listed (including `null`). This blocks DNS-rebinding and cross-site attacks from browser pages. `/mcp` only accepts `POST`. |
| Container | Built in | A distroless image with no shell, running as a non-root user and keeping no state. The default `compose.yaml` uses a read-only root filesystem, `cap_drop: [ALL]` and `no-new-privileges`. |

## The API key

Emby API keys aren't tied to a user and can't be limited: every key acts with administrator rights. emby-mcp's switches decide which tools exist, but they can't make the key itself any less powerful. Treat it like an administrator's password.

- Create a key just for emby-mcp, named so you can recognise it, such as `emby-mcp`. Don't reuse one another app has.
- Prefer `EMBY_API_KEY_FILE` with Docker secrets to putting it in `.env`, and keep `.env` readable only by you. `.env` is in `.gitignore` and `.dockerignore`.
- Over plain `http://`, the key crosses the network unencrypted with every request. That's usual on a home network. If the network isn't trusted, use https to Emby, and pin a self-signed certificate with `EMBY_TLS_FINGERPRINT` or trust your CA with `EMBY_CA_FILE` rather than turning `EMBY_VERIFY_SSL` off. See [https and certificates](configuration.md#https-and-certificates).
- If you think the key has leaked, delete it in the Emby dashboard (**Advanced → API Keys**, or **Settings → API Keys**) and create a new one. It stops working straight away.

## Recommended setups

**Look, don't touch** (lowest risk):

- Writes off (the default)
- `raw` stays off unless you list it

**Curation:**

- `EMBY_ALLOW_WRITES=true`
- Only the toolsets you use, for example `library,metadata,collections`, without `raw`
- `EMBY_AUDIT_LOG` set, so you can see what changed
- Keep tool confirmations on in your MCP client, so you approve each change

**Everything** (for a single trusted user, working interactively):

- Writes and deletes on, and all toolsets (`all,raw` if you want the raw API tool too)
- Don't mix it with untrusted content, such as pasted emails or web pages, in the same conversation as instructions to change things

## Network exposure

- Set `MCP_AUTH_TOKEN` to a long random value, even on a home network.
- Publish the port only where it's needed: on a LAN address, or on `127.0.0.1` behind a reverse proxy.
- **Never expose port 8080 directly to the internet.** If traffic crosses a network you don't trust, put TLS in front of it. See [Deployment](deployment.md#tls-with-a-reverse-proxy). Prefer a VPN to a public endpoint.
- Set `MCP_ALLOWED_HOSTS` to the hostname clients use.
- The server only needs to reach your Emby server. It needs no internet access, so a firewall can limit it to that one address and port.

## Checklist

- [ ] `MCP_AUTH_TOKEN` is set to a long random value (`openssl rand -hex 32`).
- [ ] The port is only reachable from machines that need it, or published on `127.0.0.1` behind a proxy.
- [ ] TLS terminates at a reverse proxy if traffic crosses an untrusted network. Prefer a VPN to a public endpoint.
- [ ] emby-mcp has its own API key, kept in a secret file or a private `.env`, and not committed anywhere.
- [ ] Writes are only enabled if you need them, and deletes only if you need those too.
- [ ] With writes on, `EMBY_AUDIT_LOG` records the changes somewhere you can read them.
- [ ] `EMBY_TOOLSETS` lists only the toolsets you use.
- [ ] If the connection to Emby crosses an untrusted network, it uses https with the certificate checked.
- [ ] The image is pinned to a version, and you update when security releases come out (watch the repository's releases).
