# CIMD Package Guide

## Client Metadata Contract

- Derive each client ID from the canonical resource URL by trimming trailing path slashes and appending its profile path: `/oauth/clients/cli-publisher` or `/oauth/clients/cli-admin`; preserve `RawPath` and escaped path components.
- CIMD client IDs require HTTPS even when the resource is loopback HTTP. Reject userinfo, query, fragment, and path components decoding exactly to `.` or `..`.
- Keep metadata for a native public client using authorization code, refresh token, response type `code`, and token endpoint auth method `none`.
- Redirect URIs are exactly `http://127.0.0.1/oauth/callback` and `http://[::1]/oauth/callback`. The CLI binds an ephemeral port on one of those loopback families for each transaction.
- Metadata remains under the resource path prefix, not necessarily the origin root.
- The CLI derives the selected profile ID only for delegated clients without an explicit client ID and only when provider metadata advertises CIMD support.

## Tests

- Run `go test ./internal/cimd`; also run `go test ./internal/httpclient` when changing client IDs or redirect URIs.
