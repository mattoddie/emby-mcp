# Security policy

emby-mcp holds an Emby API key, which acts with administrator rights on someone's media server, so security reports are taken seriously and handled as a priority.

## Supported versions

Security fixes go into the latest release. Please upgrade to the newest version before reporting.

| Version | Supported |
|---|---|
| Latest release | ✅ |
| Older releases | ❌ |

## Reporting a vulnerability

**Please don't report security vulnerabilities in public issues, discussions or pull requests.**

Report them privately through GitHub's **[private vulnerability reporting](https://github.com/mattoddie/emby-mcp/security/advisories/new)**: go to the repository's **Security** tab and click **Report a vulnerability**.

Please include:

- A description of the issue and its impact
- Steps to reproduce, or a proof of concept
- The affected version(s) and configuration, such as transport, `EMBY_ALLOW_WRITES`, `EMBY_ALLOW_DELETES` and toolsets
- Any suggested fix, if you have one

Please don't include real API keys or server addresses. Use the mock Emby in the test suite where you can.

## What to expect

- An acknowledgement within **3 working days**.
- An initial assessment within **7 days**, including whether the report is accepted and a rough timeline.
- A fix released as soon as practical, with a GitHub security advisory and credit to you, unless you'd rather stay anonymous.

Please give us a reasonable amount of time to fix the issue before you disclose it publicly.

## Scope

In scope:

- Bypassing `MCP_AUTH_TOKEN`, `MCP_ALLOWED_HOSTS`, or the write, delete or toolset gating. Examples would be changing the library through the raw API tool without `EMBY_ALLOW_WRITES`, or deleting something or changing a user through it without `EMBY_ALLOW_DELETES`.
- Getting the raw API tool to reach a path other than the one its checks allow, such as through encoding tricks
- Leaking the API key through tool output, logs, the audit log, or a request sent somewhere other than `EMBY_URL`
- Leaking passwords, access tokens or other API keys from Emby through tool output
- A tool that deletes media files, or that wipes an item's metadata when it should only change part of it
- Weaknesses in certificate checking, such as `EMBY_TLS_FINGERPRINT` or `EMBY_CA_FILE` not being enforced
- Vulnerabilities in the Docker image or the release pipeline

Out of scope:

- Issues that need an attacker who already holds a valid `MCP_AUTH_TOKEN` and uses only the tools that configuration allows. That's intended behaviour.
- Anything an attacker can do with the Emby API key itself. It acts with administrator rights by Emby's design.
- Prompt injection that leads the model to call tools the configuration allows. This is a known limitation of AI assistants. [docs/security.md](docs/security.md) explains how to limit the impact.
- Vulnerabilities in Emby Server, its apps or its API. Report those to Emby directly. emby-mcp is an independent project, not affiliated with Emby LLC.

For guidance on deploying emby-mcp securely, see [docs/security.md](docs/security.md).
