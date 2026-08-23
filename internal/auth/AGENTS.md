# Auth Package Guide

## Token Validation

- Validate RFC 9068 access tokens, not generic JWTs or ID tokens. Require a three-part JWT with case-insensitive `typ` exactly `at+jwt` or `application/at+jwt`.
- Enforce issuer, audience, expiry, signature, allowed algorithms, and nonempty `sub`, `client_id`, and `jti`, plus a positive numeric `iat`. The default algorithm is RS256 only.
- `scp` must be a JSON string array; `scope` is whitespace-delimited. An absent `scp` is valid, but a scalar is not.
- The role claim defaults to `pkgdepot_roles`. Missing or `null` means no roles; any other present non-string-array value invalidates the token.
- Do not expose verifier details: log the underlying cause and return `ErrInvalidToken`.
- Signing-key trust expires from key-set creation and defaults to 15 minutes. Replace the remote key set once under its mutex; do not extend trust on successful traffic.

## Authorization

- Delegated tokens need both the requested OAuth scope and a role mapped to it. A mapped role alone is insufficient; unknown roles grant nothing.
- Role-less tokens are denied unless the configured client-credentials subject template exactly matches `sub` after `{client_id}` substitution. Tokens with roles always use role mapping.
- Bearer parsing distinguishes missing credentials from malformed credentials; HTTP integration relies on that distinction for challenges and OAuth error codes.
- Preserve the five mutation scope constants and built-in mapping: `admin` gets all; `publisher` gets only `package:publish`.

## Tests

- Run `go test ./internal/auth`. Key-set tests intentionally use the same package to inject clocks and constructors.
