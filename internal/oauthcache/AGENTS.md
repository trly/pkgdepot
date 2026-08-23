# OAuth Cache Package Guide

## Storage Contract

- Production uses the OS keyring service `pkgdepot-oauth`; the account is the lowercase SHA-256 hex digest of the logical key, never the raw key.
- Records store the complete exported `oauth2.Token` plus granted scopes. Reject missing access tokens; propagate malformed JSON and backend errors.
- Normalize both keyring and package not-found errors to `ErrNotFound`. Delete is idempotent only for those errors.
- The HTTP client's logical key is normalized resource URL, client ID, and issuer separated by NUL bytes. Scopes are record data, not cache identity.
- `Store` does no locking. Client-side delegated refresh is serialized in-process per raw logical key, not across processes.
- Client-credentials tokens are not persisted. Logout deletes persistent storage but does not clear an existing client's in-memory token sources.

## Tests

- Always inject `NewWithBackend`; tests must not depend on a desktop keyring. The existing map fake is not concurrency-safe.
- Run `go test ./internal/oauthcache`.
