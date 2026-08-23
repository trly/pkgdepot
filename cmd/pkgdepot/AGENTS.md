# CLI And Server Wiring Guide

## Command Contracts

- Keep top-level commands `login`, `logout`, `serve`, `package`, and `repo`; package subcommands are `publish`, `remove`, `list`, and repository subcommands are `create`, `remove`, `rename`, `list`.
- Client commands share `--url`/`PKGDEPOT_URL`; package commands add `--architecture` defaulting to `x86_64`, and publish alone adds `--signature`.
- Preserve positional argument counts. Publish takes repository/package path; package remove repository/package name; package list one repository; repository create/remove one name; rename old/new names.
- Publish outputs one `internal/api.Package`; list commands output JSON arrays. Successful create/rename/remove are silent. Login/logout status and prompts go to stderr.
- Top-level errors print `pkgdepot: <error>` to stderr and exit 1.

## Wiring

- Keep `serve` order: parse config, construct and initialize repository service, discover/build OIDC resource auth, construct HTTP server, install signal shutdown, then listen.
- `PKGDEPOT_ADDRESS` is only the bind address. `PKGDEPOT_URL` is the canonical public resource and default audience; never derive it from the bind address or request host.
- The configured HTTP timeout covers read/write/idle and startup OIDC discovery. Graceful shutdown has a separate fixed 10s deadline.
- Client `PKGDEPOT_OAUTH_*` settings are distinct from server `PKGDEPOT_OIDC_*` settings.
- List commands stay anonymous. Mutations use operation scopes; login is delegated-only and must fail when a client secret is configured.
- A cached delegated token missing an operation scope must tell the user to run `pkgdepot login`; do not silently request broader permissions.

## Tests

- Direct package tests cover only resource-server audience/authorization wiring. OAuth, streaming, cache, API lifecycle, and public-list behavior are tested in `internal/httpclient` and `internal/httpapi`.
- Run `go test ./cmd/pkgdepot ./internal/httpclient ./internal/httpapi` for command/wiring changes.
