# Repository Guide

## Toolchain and verification

- Format changed Go files with `gofmt -w <files>`.
- Before finishing, run `go vet ./...` and `go test ./...`.
- Run one package with `go test ./internal/repository`; run one test with `go test ./internal/repository -run '^TestPublishRejectsWrongArchitecture$'`.
- Unit tests inject fake repository commands and do not require Arch Linux or pacman tooling.

## Architecture

- `cmd/pkgdepot` is the only binary. It contains the `serve`, `publish`, `list`, and `remove` commands.
- Server wiring flows from `internal/config` to `internal/repository` to `internal/httpapi`; keep storage and package-database behavior in `repository`, not HTTP handlers.
- `internal/httpclient` is the CLI-side API client. Package uploads are streamed as multipart data rather than buffered in memory.
- `internal/alpm` parses Arch package archives and gzip repository databases directly; there is no libalpm dependency.
- Repository state lives below the data root in `repositories/`, `staging/`, and `locks/`. Each database is `repositories/<repo>/<arch>/<repo>.db.tar.gz`.

## OAuth and OIDC

- The server is an OAuth 2.0 protected resource. Signed JWT access tokens must carry roles in a configurable claim (`pkgdepot_roles` by default). Authorization is determined by mapping those roles to permitted scopes through `PKGDEPOT_ROLE_SCOPES`. Client credentials tokens can alternatively authorize from their OAuth `scope`/`scp` claims when `PKGDEPOT_CLIENT_CREDENTIALS_SUBJECT_TEMPLATE` is configured for the provider's subject format.
- The CLI discovers its provider through RFC 9728 protected-resource metadata, then requires standard OpenID Connect discovery metadata for the selected issuer. Generic RFC 8414-only providers and direct endpoint overrides are not supported.
- Keep OAuth protocol mechanics in `golang.org/x/oauth2` and OIDC discovery/token validation in `github.com/coreos/go-oidc/v3/oidc`. Use client credentials when a client secret is configured and authorization code with PKCE otherwise.
- Use `oauth2.TokenSource` and `oauth2.Transport` for token acquisition, refresh, caching, concurrency control, and bearer headers. The CLI's loopback callback tries ports 8085-8089 and uses the first available one.
- `PKGDEPOT_OAUTH_ISSUER` pins the expected OIDC issuer for confidential clients before credentials can be sent. The selected issuer must match the metadata advertised by the protected resource.

## Runtime constraints

- `serve` defaults to `:8080` and `/var/lib/pkgdepot`; configure mutation access through the OIDC provider.
- Real mutations execute absolute `/usr/bin/repo-add` and `/usr/bin/repo-remove` paths with `--wait-for-lock`; changing `PATH` does not substitute them. The Docker runtime must also provide these paths.
- Call `repository.Service.Initialize` before serving or publishing so the staging and lock roots exist.
- Publish/remove operations are serialized per repository and architecture with both an in-process mutex and filesystem `flock`; preserve both layers when changing mutation flow.
- Published packages must target the requested architecture or `any`. Repository and architecture values, plus package names passed to removal, are restricted by `componentPattern` in `internal/repository/service.go`.
