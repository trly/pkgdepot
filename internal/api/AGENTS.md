# API Package Guide

## Wire Contract

- These DTOs are the stable JSON boundary shared by `httpapi`, `httpclient`, and CLI JSON output. Do not expose `alpm` or `repository` types directly.
- Keep explicit server-side conversions in sync with DTO changes; changing tags or omission behavior changes both HTTP responses and command output.
- Empty repository architecture lists must encode as `[]`, not `null` or an omitted field.
- Preserve both error fields: `error` is the compatibility-preserved display message; clients branch on `code`.
- Repository rename is currently exactly `{"name":"..."}`. Keep server decoding and client encoding synchronized if it changes.

## Verification

- There are no package-local tests. Verify producer and consumer contracts with `go test ./internal/httpapi ./internal/httpclient`.
