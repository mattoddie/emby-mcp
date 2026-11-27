# Deployment

- [Docker Compose](#docker-compose)
- [Commands](#commands)
- [Image tags](#image-tags)
- [Upgrading](#upgrading)
- [Building from source](#building-from-source)
- [TLS with a reverse proxy](#tls-with-a-reverse-proxy)
- [Hardening](#hardening)
- [Running without Docker](#running-without-docker)

## Docker Compose

The repository's [`compose.yaml`](../compose.yaml) is a good starting point:

```yaml
services:
  emby-mcp:
    image: ghcr.io/mattoddie/emby-mcp:latest
    # To build from source instead of pulling, replace the line above with:
    # build: .
    container_name: emby-mcp
    restart: unless-stopped
    env_file: .env
    ports:
      - "8080:8080"
    read_only: true
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
```

```sh
docker compose run --rm emby-mcp check   # test the connection to Emby and the API key
docker compose up -d          # start
docker compose logs -f        # follow logs
docker compose ps             # shows "healthy" once the health check passes
docker compose down           # stop
```

emby-mcp keeps no state: everything it shows comes from Emby, and everything it changes is changed in Emby. So there's no volume to keep or back up, and the root filesystem can stay read-only. The only file it writes is the [audit log](configuration.md#audit-log), if you turn it on.

The image has a built-in health check that runs `emby-mcp healthcheck` every 30 seconds. The image has no shell or `curl`, so the binary checks its own `/health` endpoint. It only shows that the server is running, not that Emby is reachable or the API key works. For that, run `check`.

To listen on one interface only, such as a LAN address or localhost when you use a reverse proxy, change the port mapping to `"192.0.2.10:8080:8080"` or `"127.0.0.1:8080:8080"`.

If Emby runs in Docker on the same machine, you can put both containers on one Docker network and set `EMBY_URL=http://<emby-container-name>:8096`. Otherwise use the Emby machine's LAN address or hostname: `localhost` inside the container is the container itself.

## Commands

The image's entrypoint is the `emby-mcp` binary, so you can run any of its commands with `docker compose run --rm emby-mcp <command>`, or `docker compose exec emby-mcp /emby-mcp <command>` while it's running:

| Command | What it does |
|---|---|
| `serve` (the default) | Serves MCP over stdio, or Streamable HTTP when `MCP_TRANSPORT=http` |
| `check` | Connects to Emby with `EMBY_URL` and `EMBY_API_KEY`, then prints the server's name and version, the user the tools act as, the libraries, and how many films, series and episodes there are. It also says whether changes are allowed, and repeats any note about plain http or an unchecked certificate. It exits with an error, and says why, if Emby can't be reached or rejects the key. |
| `healthcheck` | Exits with `0` if the HTTP server answers `/health`. It asks `MCP_HTTP_HOST` (or loopback when that is `0.0.0.0` or `::`) on `MCP_HTTP_PORT`, `_FILE` variants included. Used by the image's `HEALTHCHECK`. |
| `version` | Prints the version |

## Image tags

Images are published to the GitHub Container Registry at **`ghcr.io/mattoddie/emby-mcp`** for `linux/amd64` and `linux/arm64`. Each [GitHub release](https://github.com/mattoddie/emby-mcp/releases) publishes these tags:

| Tag | Example | Moves? |
|---|---|---|
| `X.Y.Z` | `1.4.2` | Never. Pin this for full reproducibility. |
| `X.Y` | `1.4` | Moves to the latest patch release (bug fixes). |
| `X` | `1` | Moves to the latest minor release (new features, no breaking changes). Not published for `0.x`. |
| `latest` | | Moves to the newest stable release. |

Pre-releases, such as `1.5.0-rc.1`, are only published under their exact version, never as `latest`.

The image is built `FROM gcr.io/distroless/static-debian13:nonroot`. It contains the static `emby-mcp` binary, CA certificates and time zone data, and nothing else: no shell and no package manager. It runs as the non-root user `65532` and is about 21 MB.

Every image includes an SBOM and build provenance attestation. To verify that an image was built by this repository's release workflow:

```sh
gh attestation verify oci://ghcr.io/mattoddie/emby-mcp:1.4.2 --owner mattoddie
```

## Upgrading

```sh
docker compose pull
docker compose up -d
```

There's nothing to migrate. Check the [changelog](../CHANGELOG.md) or the release notes before upgrading across a major version. After an upgrade, reconnect or restart your MCP client so it fetches the new tool list.

When you upgrade Emby itself, run `check` afterwards. emby-mcp uses Emby's documented API, which changes rarely, but see [Compatibility](compatibility.md) for the versions it's tested with.

## Building from source

```sh
git clone https://github.com/mattoddie/emby-mcp.git
cd emby-mcp
docker build -t emby-mcp .        # or: make image
```

The tests run as part of the image build, so a broken build never produces an image. The build stage cross-compiles, so building for another architecture doesn't need emulation. To use a local build with Compose, replace `image:` in `compose.yaml` with `build: .` and run `docker compose up -d --build`.

## TLS with a reverse proxy

The server speaks plain HTTP. If it's reachable from outside a trusted network, put a TLS-terminating reverse proxy in front of it and keep `MCP_AUTH_TOKEN` set.

**Caddy**, which gets certificates automatically:

```text
emby-mcp.example.com {
    reverse_proxy emby-mcp:8080
}
```

**nginx:**

```nginx
server {
    listen 443 ssl;
    server_name emby-mcp.example.com;
    ssl_certificate     /etc/ssl/certs/emby-mcp.pem;
    ssl_certificate_key /etc/ssl/private/emby-mcp.key;

    location /mcp {
        proxy_pass http://emby-mcp:8080;
        proxy_set_header Host $host;
        proxy_read_timeout 120s;
    }
}
```

**Traefik** labels:

```yaml
services:
  emby-mcp:
    image: ghcr.io/mattoddie/emby-mcp:latest
    env_file: .env
    labels:
      - traefik.enable=true
      - traefik.http.routers.emby-mcp.rule=Host(`emby-mcp.example.com`)
      - traefik.http.routers.emby-mcp.tls.certresolver=letsencrypt
      - traefik.http.services.emby-mcp.loadbalancer.server.port=8080
```

When the server sits behind a proxy, don't publish its port. Set `MCP_ALLOWED_HOSTS=emby-mcp.example.com` so it only accepts requests for that hostname.

This is about the connection from your MCP client to emby-mcp. The connection from emby-mcp to Emby is set by `EMBY_URL`; see [https and certificates](configuration.md#https-and-certificates).

> [!WARNING]
> Don't expose emby-mcp directly to the internet. Anyone with the `MCP_AUTH_TOKEN` can browse your whole library, see what everyone has watched and, with writes on, change it. For remote access, a VPN (WireGuard, Tailscale) is safer than a public endpoint.

## Hardening

The image already runs as a non-root user with nothing in it but the binary, and the default `compose.yaml` already sets `read_only`, `cap_drop: [ALL]` and `no-new-privileges`. You can lock it down further:

```yaml
services:
  emby-mcp:
    image: ghcr.io/mattoddie/emby-mcp:1.4.2   # pinned
    read_only: true
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    mem_limit: 128m
    pids_limit: 100
    env_file: .env
    ports: ["127.0.0.1:8080:8080"]
```

The server only needs to reach your Emby server, on the address and port in `EMBY_URL`. It needs no internet access at all. See [Security](security.md) for choosing settings and toolsets.

## Running without Docker

Docker is the easiest way to run emby-mcp as a service, but the server is a single static binary with no dependencies.

### With Homebrew

On macOS or Linux, install the release binary with [Homebrew](https://brew.sh):

```sh
brew install mattoddie/tap/emby-mcp
```

`brew upgrade` picks up new releases, usually within an hour of them being published. The formula installs the same release archive described below, after checking its checksum and build provenance. See [mattoddie/homebrew-tap](https://github.com/mattoddie/homebrew-tap).

### Download a release binary

Each [release](https://github.com/mattoddie/emby-mcp/releases/latest) has an archive for macOS, Linux and Windows on `amd64` and `arm64`. Each archive holds the `emby-mcp` binary and the licence. For example, on a Raspberry Pi or another 64-bit Arm Linux machine:

```sh
VERSION=1.2.3   # the latest release, without the v
curl -fsSLO https://github.com/mattoddie/emby-mcp/releases/download/v$VERSION/emby-mcp-$VERSION-linux-arm64.tar.gz
curl -fsSLO https://github.com/mattoddie/emby-mcp/releases/download/v$VERSION/SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing   # on macOS: shasum -a 256 -c SHA256SUMS --ignore-missing
tar -xzf emby-mcp-$VERSION-linux-arm64.tar.gz
sudo install emby-mcp /usr/local/bin/
```

Use `darwin-arm64` for a Mac with Apple silicon, `darwin-amd64` for an Intel Mac, `linux-amd64` for most PCs and servers, and the `windows` `.zip` files on Windows.

Each archive also has a signed build provenance attestation. To check that a file was built by this repository's release workflow, run `gh release verify-asset v$VERSION emby-mcp-$VERSION-linux-arm64.tar.gz -R mattoddie/emby-mcp`.

> [!NOTE]
> The macOS binaries aren't notarised by Apple. A file downloaded with `curl`, as above, runs as is. If you download it in a browser, macOS blocks it the first time. Either allow it under **System Settings → Privacy & Security → Open Anyway**, or run `xattr -d com.apple.quarantine emby-mcp`.

Release binaries don't update themselves. Watch the repository's releases (**Watch → Custom → Releases**) to hear about new versions.

### Build it yourself

You need [Go](https://go.dev/dl/) 1.26 or later. With `go install`:

```sh
go install github.com/mattoddie/emby-mcp/cmd/emby-mcp@latest
```

The binary lands in `$(go env GOPATH)/bin`. Built this way it reports its version as `0.0.0-dev`.

Or from a clone, which also stamps the version:

```sh
git clone https://github.com/mattoddie/emby-mcp.git
cd emby-mcp
make build VERSION=1.2.3      # writes bin/emby-mcp
```

### Run it

```sh
export EMBY_URL=http://emby.lan:8096 EMBY_API_KEY_FILE=$HOME/.config/emby-mcp/api-key
emby-mcp check                # test the connection and the key
emby-mcp                      # stdio, for an MCP client to start
MCP_TRANSPORT=http MCP_AUTH_TOKEN=... emby-mcp   # or a long-running HTTP server
```

To run it as a service, a systemd unit that sets these variables in an `EnvironmentFile` and runs `emby-mcp` as an unprivileged user works well. It can run on the Emby machine itself, with `EMBY_URL=http://localhost:8096`.
