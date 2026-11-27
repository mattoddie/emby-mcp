<div align="center">

<img src="docs/assets/logo.svg" alt="emby-mcp logo" width="112" height="112">

# emby-mcp

**Your film library, finally in order.**

An open-source [Model Context Protocol](https://modelcontextprotocol.io) server for **Emby**, the personal media server. It lets Claude and other AI assistants search your films and series, see what's new, unwatched or playing now, and, if you allow it, tag films in bulk, build collections and playlists, fix metadata and mark things watched, all from one small Docker container.

[![CI](https://github.com/mattoddie/emby-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/mattoddie/emby-mcp/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/mattoddie/emby-mcp?sort=semver)](https://github.com/mattoddie/emby-mcp/releases)
[![Container image](https://img.shields.io/badge/ghcr.io-mattoddie%2Femby--mcp-2496ED?logo=docker&logoColor=white)](https://github.com/mattoddie/emby-mcp/pkgs/container/emby-mcp)
[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

**[emby-mcp.mattoddie.dev](https://emby-mcp.mattoddie.dev)**

[Quick start](#quick-start) · [Documentation](https://emby-mcp.mattoddie.dev/docs/) · [Tool reference](docs/tools.md) · [Contributing](CONTRIBUTING.md)

</div>

---

> [!IMPORTANT]
> emby-mcp is an independent community project. It isn't made, endorsed or supported by Emby LLC. It uses Emby's documented REST API with an API key you create on your own server.

> **"Tag every Christmas film in the library Christmas."**
> *I found 23 films that fit, from Elf to The Holiday. Four already have the tag. Here's the list: shall I tag the other 19?*
> **"Yes, and lock the tags so a refresh doesn't undo them."**
> *Done: 19 films are tagged Christmas, with the Tags field locked on each. Do you want a Christmas collection as well?*

Ask about your library in plain English. The assistant picks from **34 tools** to look things up and, if you allow it, make changes. Some examples:

- 🎬 *"What's unwatched and under two hours?"*
- 🏷️ *"Tag every heist film Heist."*
- 📚 *"Make a collection of every Christopher Nolan film."*
- 📝 *"Which films have no plot summary?"*
- 📺 *"What's playing in the living room?"*
- 🍿 *"We've got the kids tonight. Pick something for a nine-year-old."*

## Highlights

- **🐳 Docker-first and tiny.** One image (`ghcr.io/mattoddie/emby-mcp`) for `linux/amd64` and `linux/arm64`, about 21 MB. It's a single static Go binary on a distroless base, with no shell or package manager, running as a non-root user. It keeps no state, so it needs no volume. It serves Streamable HTTP for a long-running service, or stdio for `docker run -i`. For Claude Desktop there's also a one-click extension bundle (`.mcpb`) that needs no Docker, and each release has plain binaries for macOS, Linux and Windows on `amd64` and `arm64`.
- **🛡️ Safe by default.** The server is read-only unless you turn on writes, and deleting collections and playlists is a second switch on top. The delete tools never touch media files. Tools that aren't allowed aren't registered at all, a "replace all" metadata refresh needs an explicit confirmation, and every tool that changes something can show you the change first with a dry run.
- **🏷️ Made for curation.** Tag hundreds of films in one call, find what's still untagged, rename or merge tags, build collections and playlists, and lock fields so a metadata refresh doesn't undo your work.
- **🏠 Private.** It talks only to your own Emby server, with an API key you create in the Emby dashboard. No cloud service or extra account is involved.
- **🎛️ Tunable.** Expose only the toolsets you need, or deny single tools, which keeps the model's context small and focused.
- **🧪 Tested.** The test suite runs every tool against an in-memory mock Emby, and the server has been tried against a real Emby Server 4.10.

## Quick start

**Using Claude Desktop?** Skip Docker: download the extension bundle (`.mcpb`) for your computer from the [latest release](https://github.com/mattoddie/emby-mcp/releases/latest), open it to install, and fill in your Emby server's address and an API key. See [Claude Desktop](docs/clients.md#claude-desktop).

emby-mcp is listed in the [MCP Registry](https://registry.modelcontextprotocol.io) as `io.github.mattoddie/emby-mcp`, so clients that browse the registry can install it (the Docker image in stdio mode, or the bundle). See [From the MCP Registry](docs/clients.md#from-the-mcp-registry).

Otherwise, you need Docker, an Emby server you're an administrator of, and an MCP client such as [Claude Code](https://claude.com/claude-code).

**1. Create an API key.** In the Emby dashboard, open **Advanced → API Keys** (**Settings → API Keys** in newer versions) and press **New API Key**.

**2. Set up and start the server.**

```sh
mkdir emby-mcp && cd emby-mcp
curl -fsSLO https://raw.githubusercontent.com/mattoddie/emby-mcp/main/compose.yaml
curl -fsSL https://raw.githubusercontent.com/mattoddie/emby-mcp/main/example.env -o .env
# Edit .env: set EMBY_URL (such as http://emby.lan:8096), EMBY_API_KEY,
# and a random MCP_AUTH_TOKEN (e.g. `openssl rand -hex 32`)
docker compose run --rm emby-mcp check   # tests the address and the key
docker compose up -d
```

**3. Connect your assistant.**

```sh
claude mcp add --transport http emby http://<docker-host>:8080/mcp \
  --header "Authorization: Bearer <MCP_AUTH_TOKEN>"
```

Now ask *"What's in my Emby library?"* in Claude Code.

Using **Claude Desktop, VS Code, Cursor or Codex**? See [Connecting MCP clients](docs/clients.md). Prefer not to run a service? Use [stdio mode](docs/clients.md#stdio-mode) with `docker run -i`, or a [release binary](docs/deployment.md#running-without-docker) without Docker. On macOS or Linux: `brew install mattoddie/tap/emby-mcp`.

## Turning on changes

Out of the box the assistant can look but not touch. To let it tag, edit metadata, build collections and playlists and mark things watched, add this to `.env` and restart:

```sh
EMBY_ALLOW_WRITES=true
EMBY_TOOLSETS=library,metadata,collections   # optional: expose fewer tools
```

```sh
docker compose up -d
```

The API key acts with administrator rights, and everyone in the household sees the changes, so read [Security](docs/security.md) before turning on writes. To keep a record of every change the assistant makes, set `EMBY_AUDIT_LOG` (see [Audit log](docs/configuration.md#audit-log)).

## What it can do

| Toolset | What's covered |
|---|---|
| `library` | The server and its libraries, search with rich filters (genre, tag or untagged, year, rating, person, studio, watched, missing metadata), item details, the tags, genres and studios in use, similar items, recently added and continue watching |
| `metadata` | Add and remove tags on up to 500 items at once, rename or merge tags, edit metadata fields and their locks, and refresh metadata from the internet |
| `collections` | Collections (Emby box sets): list, view, create, add to, remove from and delete |
| `playlists` | Playlists: list, view, create, add to, remove from and delete |
| `users` | The users, and each user's watched state and favourites |
| `server` | What's playing now, the activity log, scheduled tasks and library scans |
| `raw` | An escape hatch that calls any path of Emby's REST API. Off unless you list it (`EMBY_TOOLSETS=all,raw`) |

See the **[tool reference](docs/tools.md)** for every tool and argument. Ready-made prompts like `library_report`, `tag_library` and `movie_night` chain the tools together for common jobs.

## Documentation

The docs are also published at **[emby-mcp.mattoddie.dev/docs](https://emby-mcp.mattoddie.dev/docs/)**, with search.

| | |
|---|---|
| 🚀 [Getting started](docs/getting-started.md) | Step-by-step setup, from the API key to your first question |
| 🔌 [Connecting MCP clients](docs/clients.md) | Claude Code, Claude Desktop, VS Code, Cursor, Codex, stdio mode |
| ⚙️ [Configuration](docs/configuration.md) | Every environment variable, https and certificates, Docker secrets, toolsets |
| 🐳 [Deployment](docs/deployment.md) | Compose, image tags, upgrades, TLS reverse proxy, hardening, running without Docker |
| 💬 [Using emby-mcp](docs/usage.md) | Example requests, curating with tags and collections, prompts, dry runs |
| 🧰 [Tool reference](docs/tools.md) | Every tool and prompt, generated from the code |
| 🛡️ [Security](docs/security.md) | Threat model, what the API key can do, safe write setups |
| 📡 [Compatibility](docs/compatibility.md) | Supported Emby versions, the API it uses, Jellyfin |
| 🩹 [Troubleshooting](docs/troubleshooting.md) | Connection and API key problems, common errors and how to fix them |
| 🛠️ [Development](docs/development.md) | Architecture, the mock Emby, adding a tool, testing |

## Contributing

Contributions are very welcome, whether that's bug reports, notes on how Emby's API behaves, docs or new tools. Read the [contributing guide](CONTRIBUTING.md) to get started, and please follow the [code of conduct](CODE_OF_CONDUCT.md). To report a vulnerability, see the [security policy](SECURITY.md). Please don't open a public issue for one.

## License

[MIT](LICENSE) © Matt Oddie

*emby-mcp is an independent community project. It is not affiliated with, endorsed by or sponsored by Emby LLC. "Emby" is a trademark of Emby LLC.*
