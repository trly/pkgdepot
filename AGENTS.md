# Repository Guide

## Verification

- Use the Go version from `go.mod` (currently 1.27); README development text may lag it.
- Format changed Go files with `gofmt -w <files>`.
- Run focused tests with `go test ./internal/<package>` or `go test ./internal/<package> -run '^TestName$'`.
- Before finishing, run the CI-equivalent Go checks: `go vet ./...` and `go test ./...`.
- Unit tests inject fake repository commands and do not require Arch Linux or pacman tooling.
- For web changes, also run the exact HTML/CSS commands documented in `internal/httpapi/AGENTS.md`.

## Architecture

- `cmd/pkgdepot` is the only binary. Its top-level commands are `login`, `logout`, `serve`, `package`, and `repo`.
- Server wiring flows from `internal/config` to `internal/repository` to `internal/httpapi`; keep storage and package-database behavior in `repository`, not HTTP handlers.
- CLI commands use `internal/httpclient`; `internal/api` is the stable JSON boundary shared by server and client.
- `internal/alpm` parses package archives and gzip repository databases directly; there is no libalpm dependency.
- Repository state lives below the data root in `repositories/`, `staging/`, and `locks/`. Each database is `repositories/<repo>/<arch>/<repo>.db.tar.gz`.

## Protocol Boundaries

- The server is an OAuth 2.0 protected resource; list/download routes are public and mutations require operation-specific scopes. Identity-provider client restrictions, not local user/role mappings, determine who receives those scopes.
- The CLI discovers RFC 9728 protected-resource metadata and then OIDC metadata. Generic RFC 8414-only providers and direct endpoint overrides are unsupported.
- Keep OAuth mechanics in `golang.org/x/oauth2`, OIDC discovery/validation in `go-oidc`, URL trust rules in `internal/urlpolicy`, and authorization policy in `internal/auth`.
- `PKGDEPOT_OIDC_*` configures the server; `PKGDEPOT_OAUTH_*` configures CLI clients. Do not interchange them.

## Runtime Constraints

- `serve` defaults to `:8080` and `/var/lib/pkgdepot`; configure mutation access through the OIDC provider.
- Real mutations execute absolute `/usr/bin/repo-add` and `/usr/bin/repo-remove` paths with `--wait-for-lock`; changing `PATH` does not substitute them. The Docker runtime must also provide these paths.
- Call `repository.Service.Initialize` before serving or publishing so the staging and lock roots exist.
- Publish/remove operations are serialized per repository and architecture with both an in-process mutex and filesystem `flock`; preserve both layers when changing mutation flow.
- Published packages must target the requested architecture or `any`. Repository and architecture values, plus package names passed to removal, are restricted by `componentPattern` in `internal/repository/service.go`.

## Documentation

- Keep the root `README.md` focused on user installation, deployment, provider configuration, CLI use, and concise developer setup.
- Put repository-wide contributor and maintainer rules here. Put implementation contracts in the owning package's `AGENTS.md`; do not duplicate protocol mechanics in the README.

## Releases

- A valid v-prefixed SemVer tag publishes a Linux/amd64 `tar.gz` archive, checksums, and a versioned Linux/amd64 GHCR image.
- Pushes to `main` publish `ghcr.io/trly/pkgdepot:latest` for Linux/amd64. Release mechanics belong here rather than in the user-facing README.
