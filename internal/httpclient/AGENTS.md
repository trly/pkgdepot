# HTTP Client Package Guide

## API And Streaming

- Reads are anonymous and must not trigger OAuth discovery; each mutation requests its exact operation scope.
- Validate the base resource before component-wise path escaping. Accept HTTPS or literal loopback HTTP only.
- Keep publish streaming through `io.Pipe` and multipart parts `package` plus optional `signature`. Close both pipe ends on setup failures and wait for the writer so early server errors neither deadlock nor lose `APIError` details.
- Preserve `APIError` status/code/message for valid JSON errors and status-only fallback for malformed responses.
- `New` adds no client timeout. Request deadlines come from contexts, and discovery/OAuth uses the context passed to `New`.

## OAuth

- Discovery is RFC 9728 protected-resource metadata followed by OIDC discovery. The normalized advertised resource must exactly match `BaseURL`; do not add generic RFC 8414 or endpoint overrides.
- Client credentials require client ID plus issuer pin before discovery, use `client_secret_basic`, and send the operation scope and RFC 8707 `resource`.
- Delegated auth uses authorization code, S256 PKCE, state, `resource`, and callback ports 8085-8089. Keep ports synchronized with `internal/cimd`.
- Preserve token-source reuse, 30s expiry margin, refresh `resource`, secure cache writes, per-cache-key refresh locking, and delegated reauthorization after refresh failure.
- Login verifies OIDC identity/nonce before a CSRF-protected local scope selector. Escape identity data and cache scopes actually granted, not merely requested.

## Tests

- Inject HTTP servers, OAuth options, authorization prompts, and in-memory cache backends; never require a browser, keyring, or real provider.
- Loopback tests bind ports 8085-8089 and must not run in parallel.
- Run `go test ./internal/httpclient`.
