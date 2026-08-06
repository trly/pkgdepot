# Repository Guide

## Toolchain and verification

- This is one Go 1.26 module with no task runner, CI workflow, or repository-specific lint configuration.
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

## Runtime constraints

- `serve` defaults to `:8080` and `/var/lib/pkgdepot`; use local `pkgdepot token` commands against the data root to manage mutation credentials.
- Real mutations execute absolute `/usr/bin/repo-add` and `/usr/bin/repo-remove` paths with `--wait-for-lock`; changing `PATH` does not substitute them. The Docker runtime must also provide these paths.
- Call `repository.Service.Initialize` before serving or publishing so the staging and lock roots exist.
- Publish/remove operations are serialized per repository and architecture with both an in-process mutex and filesystem `flock`; preserve both layers when changing mutation flow.
- Published packages must target the requested architecture or `any`. Repository and architecture values, plus package names passed to removal, are restricted by `componentPattern` in `internal/repository/service.go`.
