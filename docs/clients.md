# Connecting MCP clients

emby-mcp supports both standard MCP transports:

| Transport | How it runs | Best for |
|---|---|---|
| **Streamable HTTP** (the default in the image) | One long-running container. Clients connect to `http://<host>:8080/mcp`. | Most setups. One server can be shared by several clients, sessions and machines. |
| **stdio** (the default for the binary) | The client starts the server for each session, with `docker run -i` or the local `emby-mcp` binary. | A single machine, no network service, or clients that only support stdio. |

The HTTP endpoint is **stateless**: each request is handled on its own, so you can restart the container at any time. When `MCP_AUTH_TOKEN` is set, every request must send `Authorization: Bearer <token>`.

In the examples below, replace `<docker-host>` with the machine running the container and `<token>` with your `MCP_AUTH_TOKEN`. The server is called `emby` in every example. If you use a different name, the prompt slash commands change to match.

- [Claude Code](#claude-code)
- [Claude Desktop](#claude-desktop)
- [VS Code (GitHub Copilot)](#vs-code-github-copilot)
- [Cursor](#cursor)
- [OpenAI Codex CLI](#openai-codex-cli)
- [Other clients](#other-clients)
- [From the MCP Registry](#from-the-mcp-registry)
- [stdio mode](#stdio-mode)

## Claude Code

```sh
claude mcp add --transport http emby http://<docker-host>:8080/mcp \
  --header "Authorization: Bearer <token>"
```

Add `--scope user` to make it available in every project, or `--scope project` to save it in a `.mcp.json` you can share. Don't commit the token: in `.mcp.json`, reference an environment variable instead:

```json
{
  "mcpServers": {
    "emby": {
      "type": "http",
      "url": "http://<docker-host>:8080/mcp",
      "headers": { "Authorization": "Bearer ${EMBY_MCP_TOKEN}" }
    }
  }
}
```

Run `claude mcp list` to check the connection, and `/mcp` inside Claude Code to see the tools. The built-in prompts are available as slash commands, such as `/mcp__emby__library_report` or `/mcp__emby__build_collection "Christopher Nolan films"`.

To use stdio instead, with Docker or the local binary:

```sh
# Docker
claude mcp add emby -- docker run -i --rm \
  -e MCP_TRANSPORT=stdio --env-file /path/to/emby.env \
  ghcr.io/mattoddie/emby-mcp:latest

# The local binary (see "Running without Docker" in the deployment guide)
claude mcp add emby -e EMBY_URL=http://emby.lan:8096 -e EMBY_API_KEY_FILE=/path/to/emby-api-key -- emby-mcp
```

Read the notes under [stdio mode](#stdio-mode) first.

## Claude Desktop

The easiest way is the **extension bundle**: a single `.mcpb` file that installs emby-mcp into Claude Desktop with a settings form. It needs no Docker, no service and no token, because Claude Desktop runs the server itself over stdio.

### Install the extension

1. Open the [latest release](https://github.com/mattoddie/emby-mcp/releases/latest) and download the bundle for your computer:

   | Computer | Bundle |
   |---|---|
   | Mac with Apple silicon (M1 and later) | `emby-mcp-<version>-darwin-arm64.mcpb` |
   | Mac with an Intel processor | `emby-mcp-<version>-darwin-amd64.mcpb` |
   | Windows on x64 (most PCs) | `emby-mcp-<version>-windows-amd64.mcpb` |
   | Windows on Arm | `emby-mcp-<version>-windows-arm64.mcpb` |

   There are Linux bundles too (`linux-amd64`, `linux-arm64`) for other clients that install MCP Bundles.

2. Double-click the file, or drag it onto **Settings → Extensions** in Claude Desktop, and click **Install**.
3. Fill in the settings and turn the extension on. The tools appear under the tools (🔨) menu, and the prompts under the **+** menu. You can allow or block each tool separately there.
4. Ask Claude *"What's in my Emby library?"* to check it works.

To update, install the bundle from a newer release the same way.

### Settings

| Setting | Variable | What to enter |
|---|---|---|
| **Emby server address** (required) | `EMBY_URL` | The address you open Emby at, with the port, such as `http://emby.lan:8096` or `https://emby.example.com`. |
| **Emby API key** (required) | `EMBY_API_KEY` | A key from the Emby dashboard: **Advanced → API Keys** (**Settings → API Keys** in newer versions), **New API Key**. |
| **Emby user** | `EMBY_USER` | Optional: whose watched state, favourites and parental limits the tools use, by name. Empty means the first administrator. |
| **Allow changes** | `EMBY_ALLOW_WRITES` | Off by default, which keeps the assistant read-only. Read [Security](security.md) before turning it on. |
| **Allow deletes** | `EMBY_ALLOW_DELETES` | Off by default. Also lets Claude delete collections and playlists, never media files. Needs **Allow changes**. |
| **Toolsets** | `EMBY_TOOLSETS` | Optional, such as `library,metadata,collections`. Empty means every toolset except `raw`. See [Keeping the tool list small](configuration.md#keeping-the-tool-list-small). |
| **Certificate fingerprint** | `EMBY_TLS_FINGERPRINT` | Optional, for an `https://` address with a self-signed certificate: its SHA-256 fingerprint (`AA:BB:...`). Leave it empty for `http://` or a normal certificate. See [https and certificates](configuration.md#https-and-certificates). |
| **Audit log file** | `EMBY_AUDIT_LOG` | Optional full path to a file, such as `/Users/you/emby-mcp-audit.jsonl`, that records every change Claude makes. The folder must exist; the file is created. `stderr` writes the entries to the extension's log instead. Off when empty. See [Audit log](configuration.md#audit-log). |

Settings you leave empty use their defaults, as described in [Configuration](configuration.md).

### If it doesn't start

- **Read the error.** Claude Desktop shows that the extension failed to start. The server's log, which says what's wrong, is in `~/Library/Logs/Claude/` on macOS and `%APPDATA%\Claude\logs\` on Windows, in a file named after the extension.
- **macOS blocks the server.** The binary in the bundle isn't notarised by Apple. If macOS says it can't check it for malicious software, open **System Settings → Privacy & Security** and click **Open Anyway** next to the message about `emby-mcp`, then turn the extension off and on again.
- **Tools say Emby can't be reached or rejected the key.** Check the address in a browser on the same computer, and create a new API key if in doubt. See [Troubleshooting](troubleshooting.md).

### Or run it with Docker

If you'd rather run the container image, use [stdio mode](#stdio-mode). Open **Settings → Developer → Edit Config** and add:

```json
{
  "mcpServers": {
    "emby": {
      "command": "docker",
      "args": [
        "run", "-i", "--rm",
        "-e", "MCP_TRANSPORT=stdio",
        "-e", "EMBY_URL", "-e", "EMBY_API_KEY",
        "ghcr.io/mattoddie/emby-mcp:latest"
      ],
      "env": {
        "EMBY_URL": "http://emby.lan:8096",
        "EMBY_API_KEY": "<your-api-key>"
      }
    }
  }
}
```

Or, with the local binary:

```json
{
  "mcpServers": {
    "emby": {
      "command": "/usr/local/bin/emby-mcp",
      "env": { "EMBY_URL": "http://emby.lan:8096", "EMBY_API_KEY_FILE": "/Users/you/.config/emby-mcp/api-key" }
    }
  }
}
```

Restart Claude Desktop. The tools appear under the tools (🔨) menu, and the prompts appear under **+ → Add from emby**.

To turn on writes, add `"-e", "EMBY_ALLOW_WRITES=true"` to `args` (or `"EMBY_ALLOW_WRITES": "true"` to `env` for the binary).

## VS Code (GitHub Copilot)

Create `.vscode/mcp.json` in your workspace, or run **MCP: Add Server** from the command palette:

```json
{
  "inputs": [
    { "id": "emby-token", "type": "promptString", "description": "emby-mcp token", "password": true }
  ],
  "servers": {
    "emby": {
      "type": "http",
      "url": "http://<docker-host>:8080/mcp",
      "headers": { "Authorization": "Bearer ${input:emby-token}" }
    }
  }
}
```

VS Code asks for the token once and stores it securely. Use the tools from Copilot Chat in **Agent** mode.

## Cursor

Add to `~/.cursor/mcp.json` for all projects, or `.cursor/mcp.json` for one project:

```json
{
  "mcpServers": {
    "emby": {
      "url": "http://<docker-host>:8080/mcp",
      "headers": { "Authorization": "Bearer <token>" }
    }
  }
}
```

## OpenAI Codex CLI

Add to `~/.codex/config.toml`:

```toml
[mcp_servers.emby]
url = "http://<docker-host>:8080/mcp"
bearer_token_env_var = "EMBY_MCP_TOKEN"
```

Then `export EMBY_MCP_TOKEN=<token>` before running `codex`.

## Other clients

Any client that supports MCP **Streamable HTTP** works. Point it at `http://<docker-host>:8080/mcp` and send the bearer token header.

If your client only supports stdio, you have two options. You can use [stdio mode](#stdio-mode), or you can keep the shared HTTP server and connect through a stdio-to-HTTP bridge such as [`mcp-remote`](https://www.npmjs.com/package/mcp-remote):

```json
{
  "command": "npx",
  "args": ["-y", "mcp-remote", "http://<docker-host>:8080/mcp", "--header", "Authorization: Bearer <token>"]
}
```

## From the MCP Registry

emby-mcp is listed in the official [MCP Registry](https://registry.modelcontextprotocol.io) as **`io.github.mattoddie/emby-mcp`**. Clients that can install servers from the registry offer two packages:

- **The Docker image** (`ghcr.io/mattoddie/emby-mcp`) in [stdio mode](#stdio-mode). The client asks for your Emby server's address and API key, and the same optional settings as the Claude Desktop extension.
- **The MCP Bundles** (`.mcpb`), one for each OS and processor, as for [Claude Desktop](#claude-desktop).

Each release is published to the registry by the release workflow, so the registry always offers the latest stable version.

## stdio mode

In stdio mode the MCP client runs the server itself and talks to it over stdin/stdout. No port is opened, and no `MCP_AUTH_TOKEN` is needed because only the client can reach the server.

With Docker, the general shape is:

```sh
docker run -i --rm \
  -e MCP_TRANSPORT=stdio \
  --env-file /path/to/emby.env \
  ghcr.io/mattoddie/emby-mcp:latest
```

Tips:

- **No volume is needed.** The server keeps no state between sessions: everything it shows comes from Emby.
- **Several sessions at once are fine.** Each one is its own process with its own short-lived caches. For many clients or machines, the [HTTP server](#claude-code) is still simpler.
- **Keep secrets out of the client config.** Pass `-e NAME` without a value so Docker copies the variable from the client's environment, as in the Claude Desktop example, or use `--env-file`.
- **Always pass `-i` and never `-t`.** A TTY corrupts the MCP message stream.
- **The container must reach Emby.** `EMBY_URL` is resolved inside the container, so `localhost` there means the container itself. Use the Emby machine's LAN address or hostname.
- **Docker on another machine** works too. Set `DOCKER_HOST` in the client's `env`. The container then runs, and connects to Emby, from that machine.
- **Pin a version** (for example `:1.2.3`) so a new `latest` doesn't surprise you. See [Deployment](deployment.md#image-tags).

### Without Docker

With a [release binary](deployment.md#download-a-release-binary), the client runs it directly. Use the binary's full path as the command and pass the settings as environment variables. In a JSON client config, that looks like:

```json
{
  "mcpServers": {
    "emby": {
      "command": "/usr/local/bin/emby-mcp",
      "env": {
        "EMBY_URL": "http://emby.lan:8096",
        "EMBY_API_KEY_FILE": "/path/to/emby-api-key"
      }
    }
  }
}
```

The binary uses stdio by default, so there's no `MCP_TRANSPORT` to set. Run `emby-mcp check` once in a terminal with the same variables to test the connection. In Claude Code:

```sh
claude mcp add emby \
  --env EMBY_URL=http://emby.lan:8096 \
  --env EMBY_API_KEY_FILE=/path/to/emby-api-key \
  -- /usr/local/bin/emby-mcp
```
