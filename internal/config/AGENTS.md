# Config Package Guide

## Environment Contracts

- `FromEnv` always requires `PKGDEPOT_OIDC_ISSUER`. Defaults include `:8080`, `/var/lib/pkgdepot`, `http://127.0.0.1:8080`, 500 MiB uploads, 30s HTTP timeout, and 15m key-cache lifetime.
- Empty address/app/data-root/URL values select defaults; whitespace does not. Size and duration values are untrimmed, must parse, and must be positive.
- `PKGDEPOT_ROLE_SCOPES` replaces the built-in map; it does not merge. Require a nonempty map and nonblank role/scope strings, but empty scope lists are currently valid.
- A client-credentials subject template is either empty or contains exactly one literal `{client_id}`.
- URL and issuer validation allows HTTP only for `localhost` or literal loopback IPs. Keep validation in `urlpolicy` rather than duplicating it here.
- An unset audience remains empty in `Config`; server wiring defaults it to the canonical resource URL. An empty algorithm list later means RS256.
- Preserve fail-fast validation order because tests and surfaced configuration errors depend on it.

## Tests

- Tests use `t.Setenv` and must not run in parallel. Explicitly neutralize relevant host environment variables in default-sensitive cases.
- Run `go test ./internal/config`.
