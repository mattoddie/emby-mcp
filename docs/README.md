<div align="center">
<img src="assets/logo.svg" alt="emby-mcp logo" width="80" height="80">

# emby-mcp documentation
</div>

**New here?** Start with [Getting started](getting-started.md). It goes from nothing to your first question in about ten minutes, with the Emby dashboard open in a browser to create an API key.

emby-mcp is an independent community project, not made, endorsed or supported by Emby LLC. It uses Emby's documented REST API. See [Compatibility](compatibility.md) for the versions it works with.

## Using emby-mcp

| Guide | What's in it |
|---|---|
| [Getting started](getting-started.md) | Creating an API key, running the container, connecting Claude Code, first questions |
| [Connecting MCP clients](clients.md) | Claude Code, Claude Desktop, VS Code, Cursor, Codex, generic clients, stdio mode |
| [Configuration](configuration.md) | Every environment variable, https and certificates, Docker secrets, choosing toolsets, example configs |
| [Using emby-mcp](usage.md) | Example requests, tagging and collections, built-in prompts, dry runs, the raw API tool |
| [Tool reference](tools.md) | Every tool and argument, generated from the code |

## Running it well

| Guide | What's in it |
|---|---|
| [Deployment](deployment.md) | Compose, image tags, upgrades, reverse proxies with TLS, hardening, running without Docker |
| [Security](security.md) | Threat model, layers of protection, the API key, recommended setups, checklist |
| [Compatibility](compatibility.md) | Supported Emby versions, the API it uses, Jellyfin |
| [Troubleshooting](troubleshooting.md) | Connection, certificate and API key problems, error messages and how to fix them |

## Contributing

| Guide | What's in it |
|---|---|
| [Contributing guide](../CONTRIBUTING.md) | How to report issues, set up, and open pull requests |
| [Development](development.md) | Architecture, adding a tool, the mock Emby, testing |
| [Releasing](releasing.md) | Versioning, cutting a release, what the release workflow does |
| [Changelog](../CHANGELOG.md) | What changed in each version |
