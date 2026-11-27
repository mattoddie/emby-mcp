# Getting started

This guide takes you from nothing to asking your first question about your Emby library. It takes about ten minutes. Have the Emby dashboard open in a browser: you need it once, to create an API key.

- [What you need](#what-you-need)
- [1. Create an API key](#1-create-an-api-key)
- [2. Set up the server](#2-set-up-the-server)
- [3. Check the connection](#3-check-the-connection)
- [4. Start the server](#4-start-the-server)
- [5. Connect your assistant](#5-connect-your-assistant)
- [6. Ask something](#6-ask-something)
- [Next steps](#next-steps)

## What you need

> [!TIP]
> **No Docker?** Each release also has plain binaries for macOS, Linux and Windows. See [Running without Docker](deployment.md#running-without-docker) and [connecting a client to the binary](clients.md#without-docker). On macOS or Linux, `brew install mattoddie/tap/emby-mcp` installs it with Homebrew.

- **An Emby server** that you're an administrator of, so you can create an API key. See [Compatibility](compatibility.md) for the versions emby-mcp works with.
- **A machine that runs Docker** and can reach the Emby server. This could be the NAS Emby runs on, a home server, a small VM or your laptop. The image supports both `amd64` and `arm64`. If you'd rather not use Docker, there's a single binary too. See [Running without Docker](deployment.md#running-without-docker).
- **An MCP client**, such as [Claude Code](https://claude.com/claude-code), Claude Desktop, VS Code, Cursor or Codex.

> [!NOTE]
> emby-mcp is an independent community project. It isn't made or supported by Emby LLC, so please don't contact Emby's support about it. Questions and bug reports go to the [GitHub repository](https://github.com/mattoddie/emby-mcp).

## 1. Create an API key

In the Emby dashboard, open **Advanced → API Keys** (in newer versions, **Settings → API Keys**), press **New API Key** and give it a name you'll recognise, such as `emby-mcp`. Copy the key.

An Emby API key acts with administrator rights: it can see every library and user, and change anything the dashboard can. That's why emby-mcp starts read-only. Keep the key private. See [Security](security.md#the-api-key).

## 2. Set up the server

Create a folder and download the compose file and the example settings:

```sh
mkdir emby-mcp && cd emby-mcp
curl -fsSLO https://raw.githubusercontent.com/mattoddie/emby-mcp/main/compose.yaml
curl -fsSL https://raw.githubusercontent.com/mattoddie/emby-mcp/main/example.env -o .env
```

Edit `.env` and set at least:

```sh
EMBY_URL=http://emby.lan:8096            # the address you open Emby at, with the port
EMBY_API_KEY=<your-api-key>              # from step 1
MCP_AUTH_TOKEN=a-long-random-string      # generate one with: openssl rand -hex 32
```

`EMBY_URL` is the address you'd open Emby at in a browser: usually `http://<host>:8096`, or `https://<host>:8920` (or your reverse proxy's address) for https. A trailing `/emby` is fine; it's removed. If Emby runs on the same machine as Docker, use the machine's LAN address or hostname, not `localhost`, which inside the container is the container itself.

Leave everything else for now. The server starts **read-only**, which is the right way to begin. Keep `.env` private: it holds the API key.

## 3. Check the connection

Before starting the server, check that it can reach Emby and that the key works:

```sh
docker compose run --rm emby-mcp check
```

It connects with `EMBY_URL` and `EMBY_API_KEY` and shows what the tools will see:

```text
Connected to Media: Emby Server 4.10.2 (http://emby.lan:8096).
Acting as the user Sam (the first administrator; set EMBY_USER to choose).
3 libraries:
  - Films (movies)
  - TV (tvshows)
  - Music (music)
1250 films, 85 series, 3400 episodes.
Note: EMBY_URL uses plain http, so the API key and everything the server returns cross the network unencrypted. That's usual on a home network; use https (for example through a reverse proxy) if the network isn't trusted.
Read-only: set EMBY_ALLOW_WRITES=true to let the assistant tag, edit and build collections.
```

The *user* is whose watched state, favourites and resume points the tools use. By default that's the first administrator. To use someone else's, set `EMBY_USER` to their user name. Tools can also be given a user for one call.

If `check` fails, its message says what's wrong, such as the host not resolving, the connection being refused, or Emby rejecting the key. [Troubleshooting](troubleshooting.md) covers the common errors.

Without Compose, the same check with plain Docker is:

```sh
docker run --rm --env-file .env ghcr.io/mattoddie/emby-mcp:latest check
```

## 4. Start the server

```sh
docker compose up -d
docker compose logs -f emby-mcp
```

You should see lines like these:

```text
emby-mcp 0.1.0: http://emby.lan:8096, writes disabled, toolsets 6, transport http
emby-mcp: note: EMBY_URL uses plain http, so the API key and everything the server returns cross the network unencrypted. ...
emby-mcp listening on http://0.0.0.0:8080/mcp (bearer auth enabled)
```

The note about plain http is normal on a home network. See [Emby server](configuration.md#emby-server) for https.

Check that it's up from another machine:

```sh
curl http://<docker-host>:8080/health
# {"status":"ok"}
```

> [!TIP]
> `/health` only shows that the server is running. It doesn't contact Emby. To check the connection again, run `docker compose exec emby-mcp /emby-mcp check`, or ask the assistant to call `emby_get_server_info`. Step 6 is the real test.

## 5. Connect your assistant

For **Claude Code**:

```sh
claude mcp add --transport http emby http://<docker-host>:8080/mcp \
  --header "Authorization: Bearer <MCP_AUTH_TOKEN>"
```

Run `claude mcp list` to check that `emby` shows as connected. For other clients, see [Connecting MCP clients](clients.md).

## 6. Ask something

Start a new conversation and try:

- *"What's in my Emby library?"*
- *"What was added this week?"*
- *"Which films have no plot summary?"*

The assistant calls tools like `emby_get_server_info`, `emby_list_latest` and `emby_search_items` and summarises the results. In Claude Code you can also run the built-in prompts as slash commands, for example `/mcp__emby__library_report`.

If something goes wrong, the tool's error message usually says what to fix. [Troubleshooting](troubleshooting.md) covers the common errors.

## Next steps

- **Let it make changes.** Set `EMBY_ALLOW_WRITES=true` and restart with `docker compose up -d`. Read [Security](security.md) first.
- **Curate the library.** [Using emby-mcp](usage.md) explains tagging in bulk, collections, locks and dry runs.
- **Expose fewer tools** with `EMBY_TOOLSETS` for a faster, more focused assistant. See [Configuration](configuration.md#choosing-toolsets).
- **Put it behind HTTPS** if it's reachable outside your LAN. See [Deployment](deployment.md#tls-with-a-reverse-proxy).
