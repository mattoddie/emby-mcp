<!-- Title: use Conventional Commits, e.g. "fix(metadata): keep locks when renaming a tag". It becomes the commit on main. See CONTRIBUTING.md. -->

## What does this change?

<!-- A short description of the change and why it's needed. Link any related issue: "Fixes #123". -->

## How was it tested?

<!-- e.g. new tests against the mock Emby (embytest), `make test`, and/or tried against a real Emby server (which version?). -->

## Checklist

- [ ] `make test` and `make lint` pass
- [ ] New or changed tools have tests, and `make docs` has been run
- [ ] New tools have the right gate (`Read`, `Write` or `Delete`) and annotations
- [ ] Docs updated where behaviour changed (README / `docs/`)
- [ ] `CHANGELOG.md` has an entry under **Unreleased** for user-facing changes
- [ ] No API keys, server addresses, user names, file paths or other private data in code, tests or screenshots
