# URL Policy Package Guide

## Trust Rules

- `Validate` is for resource/issuer identifiers: require an absolute HTTP(S) URL and reject userinfo, query, fragment, CR, and LF. Paths and ports are allowed.
- `ValidateEndpoint` deliberately permits query parameters for discovered authorization, token, and JWKS endpoints; do not replace it with `Validate` at endpoint call sites.
- Plain HTTP is accepted only for hostname exactly `localhost` or a literal loopback IPv4/IPv6 address. Do not allow private addresses, `.local`, or names that resolve to loopback.
- Keep the literal-host check without DNS resolution to avoid rebinding trust changes.
- Endpoint fields may be empty here; protocol callers decide which discovered endpoints are mandatory.
- Apply policy at every trust boundary: local config, advertised resource/issuer, and discovered endpoints.

## Tests

- `httptest` HTTP URLs pass because `127.0.0.1` is explicitly allowed; this is not evidence that production HTTP should pass.
- Run `go test ./internal/urlpolicy` and relevant auth/client tests after changing shared policy.
