# Auth Package Guide

## Token Validation

- Validate RFC 9068 access tokens, not generic JWTs or ID tokens. Require a three-part JWT with case-insensitive `typ` exactly `at+jwt` or `application/at+jwt`.
- Enforce issuer, audience, expiry, signature, allowed algorithms, and nonempty `sub`, `client_id`, and `jti`, plus a positive numeric `iat`. The default algorithm is RS256 only.
- `scp` must be a JSON string array; `scope` is whitespace-delimited. An absent `scp` is valid, but a scalar is not.
- Do not expose verifier details: log the underlying cause and return `ErrInvalidToken`.
- Signing-key trust expires from key-set creation and defaults to 15 minutes. Replace the remote key set once under its mutex; do not extend trust on successful traffic.

## Authorization

- Authorization is scope-only: a validated token is allowed when it contains the requested operation scope. User/group and client restrictions belong to the identity provider.
- Bearer parsing distinguishes missing credentials from malformed credentials; HTTP integration relies on that distinction for challenges and OAuth error codes.
- Preserve the five mutation scope constants.

## Tests

- Run `go test ./internal/auth`. Key-set tests intentionally use the same package to inject clocks and constructors.
