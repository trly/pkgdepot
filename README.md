# pkgdepot

`pkgdepot` is an Arch Linux package repository server.

## Run the server

Non-loopback deployments require HTTPS. Run `pkgdepot` behind a TLS
reverse proxy. This example uses [Caddy](https://caddyserver.com), which obtains and renews a
certificate automatically. Set `packages.example.com` to a DNS name pointing
to the host running `pkgdepot`, and make ports 80 and 443 reachable from the
internet.

```yaml
services:
  pkgdepot:
    image: ghcr.io/trly/pkgdepot:latest
    environment:
      PKGDEPOT_URL: https://packages.example.com
      PKGDEPOT_OIDC_ISSUER: https://id.example.com
    volumes:
      - pkgdepot-data:/var/lib/pkgdepot

  caddy:
    image: caddy:2
    depends_on:
      - pkgdepot
    ports:
      - "80:80"
      - "443:443"
    command: caddy reverse-proxy --from packages.example.com --to pkgdepot:8080
    volumes:
      - caddy-data:/data
      - caddy-config:/config

volumes:
  pkgdepot-data:
  caddy-data:
  caddy-config:
```

Start the server with:

```sh
docker compose up -d
```

### Create a repository

```sh
docker compose exec pkgdepot pkgdepot repo create stable
```

### Authenticate and publish a package

Configure an OAuth provider with the `package:publish` scope, then authenticate
the CLI against the server. With an HTTPS `PKGDEPOT_URL`, the CLI uses a Client
ID Metadata Document served by pkgdepot by default, so no client ID is required.
For the default local HTTP URL, set `PKGDEPOT_OAUTH_CLIENT_ID` to a
pre-registered client instead.

```sh
export PKGDEPOT_URL=https://packages.example.com
export PKGDEPOT_OAUTH_CLIENT_ID=<client-id>

pkgdepot login
pkgdepot package publish stable ./example-1.0-1-x86_64.pkg.tar.zst
```

Confidential clients that set `PKGDEPOT_OAUTH_CLIENT_SECRET` skip `login` and
authenticate automatically during publish and remove. They require
`PKGDEPOT_OAUTH_ISSUER` to pin the expected provider. See the
[Pocket ID Example](#pocket-id-example-automation) for a working setup.

The default architecture is `x86_64`. Use `--architecture` for another target,
such as `pkgdepot package publish --architecture aarch64 stable ./example-1.0-1-aarch64.pkg.tar.zst`.

### Add the repository to `/etc/pacman.conf`

```ini
[pkgdepot]
Server = https://packages.example.com/repos/stable/$arch
```

### Install the package from the pkgdepot server

```sh
pacman -Syu example
```

## Authentication and Authorization

pkgdepot is an OAuth 2.0 protected resource. The provider authenticates 
users or clients, decides which permissions they receive, and issues
access tokens. pkgdepot only validates those tokens and enforces
the permission required by each mutation:

| Mutation | Required scope |
| --- | --- |
| Publish a package | `package:publish` |
| Remove a package | `package:remove` |

The OIDC/OAuth provider must satisfy every item below:

- **Discovery.** Publish OpenID Connect discovery metadata at the standard path
  for the configured issuer, including a `jwks_uri`.
- **RFC 9068 access tokens.** Issue signed JWT access tokens whose JOSE header
  sets `typ` to `at+jwt` or `application/at+jwt`.
- **Required claims.** Each access token must carry `iss`, `aud`, `exp`, `sub`,
  `client_id`, `jti`, and a positive `iat`. Scopes must appear in the `scp`
  array claim, the `scope` space-delimited claim, or both.
- **Signing algorithm.** Default RS256; override with
  `PKGDEPOT_OIDC_JWT_ALGORITHMS`.
- **RFC 8707 resource indicators.** Accept a `resource` parameter in
  authorization and token requests and use it as the access token audience.
- **Client credentials (optional).** Support `client_secret_basic` token
  endpoint authentication, or omit `token_endpoint_auth_methods_supported`
  entirely.
- **Authorization code with PKCE (optional).** Support PKCE with S256 for
  delegated CLI login. Advertise `client_id_metadata_document_supported: true`
  so the CLI can register loopback redirect URIs on ports 8085 through 8089.
- **HTTPS.** All issuer and endpoint URLs must use HTTPS, except loopback
  addresses for local development.

pkgdepot has no local users, groups, roles, OAuth clients, or permission
policy. Opaque access tokens and token introspection are not supported.

Signing keys are refreshed at least every 15 minutes by default, including keys
that still verify successfully. Set `PKGDEPOT_OIDC_JWT_CACHE_LIFETIME` to a
positive Go duration to change this interval. Emergency key revocation can
therefore take up to the configured interval to reach pkgdepot; providers should
keep old and new keys published during normal rotation for at least that long.

### Pocket ID Example: Automation

This example creates one confidential client that can publish and remove
packages using the client-credentials flow:

1. In Pocket ID, open **Settings > APIs**, select **Add API**, and create an API
   named `pkgdepot`. Set **Resource** to the exact public `PKGDEPOT_URL`, for
   example `https://packages.example.com`.
2. Add two API permissions. Use `package:publish` and `package:remove` as their
   permission keys.
3. Open **Settings > OIDC Clients**, select **Add OIDC Client**, and create a
   confidential client (leave **Public Client** disabled). Save its client ID
   and secret. A client-credentials client does not need a callback URL.
4. Open the new client's **API Access** section. For the `pkgdepot` API, grant
   `package:publish` and `package:remove` under **Client access**. These are
   machine-to-machine grants, not **User-delegated access** grants.
5. Start pkgdepot with the Pocket ID issuer and the same API resource:

   ```sh
   PKGDEPOT_URL=https://packages.example.com \
   PKGDEPOT_OIDC_ISSUER=https://id.example.com \
   pkgdepot serve
   ```

6. Configure the CLI with that confidential client. Pinning the issuer prevents
   credentials from being sent to an issuer other than Pocket ID:

   ```sh
   PKGDEPOT_URL=https://packages.example.com \
   PKGDEPOT_OAUTH_ISSUER=https://id.example.com \
   PKGDEPOT_OAUTH_CLIENT_ID=<client-id> \
   PKGDEPOT_OAUTH_CLIENT_SECRET=<client-secret> \
   pkgdepot package publish stable ./example-1.0-1-x86_64.pkg.tar.zst
   ```

Pocket ID now issues a JWT whose audience is the API resource and whose scope
contains the permission requested by the command. The same pkgdepot server
configuration works with any provider that meets the requirements above.

### CLI Flows

List operations are public. `package publish` and `package remove` discover
RFC 9728 protected-resource metadata from `PKGDEPOT_URL`, then use standard
OpenID Connect provider discovery.

**Automation (client credentials)**

Configure a confidential client. The CLI requests the operation scope and
`resource=PKGDEPOT_URL`. An issuer pin is required before it sends a client
secret.

```sh
PKGDEPOT_URL=https://packages.example.com \
PKGDEPOT_OAUTH_ISSUER=https://id.example.com \
PKGDEPOT_OAUTH_CLIENT_ID=<client-id> \
PKGDEPOT_OAUTH_CLIENT_SECRET=<client-secret> \
pkgdepot package publish stable ./example-1.0-1-x86_64.pkg.tar.zst
```

**Delegated CLI user (authorization code with PKCE)**

The CLI uses the CIMD document at
`<PKGDEPOT_URL>/oauth/client-metadata.json`. The provider must support Client ID
Metadata Documents and allow that URL. The document registers the loopback
callbacks `http://127.0.0.1:<port>/oauth/callback` for ports 8085 through 8089.
The CLI prints an authorization URL, requests `openid` plus the operation scope
and resource, receives the local callback, and exchanges its authorization code
using PKCE. Tokens are kept in the keyring and refreshed when the provider
supplies a refresh token.

```sh
PKGDEPOT_URL=https://packages.example.com \
pkgdepot package remove stable example
```

### Environment Variables

| Variable | Default | Description |
| --- | --- | --- |
| `PKGDEPOT_ADDRESS` | `:8080` | Server listen address. |
| `PKGDEPOT_APP_NAME` | `PKGdepot` | Application name shown in the web interface. |
| `PKGDEPOT_DATA_ROOT` | `/var/lib/pkgdepot` | Root directory for repositories, staging files, and locks. |
| `PKGDEPOT_URL` | `http://127.0.0.1:8080` | Public resource URL and default token audience. |
| `PKGDEPOT_MAX_UPLOAD_SIZE` | `524288000` | Maximum package upload size in bytes. |
| `PKGDEPOT_HTTP_TIMEOUT` | `30s` | HTTP server read, write, and idle timeout. |
| `PKGDEPOT_OIDC_ISSUER` | Required | Trusted OIDC issuer URL. |
| `PKGDEPOT_OIDC_AUDIENCE` | `PKGDEPOT_URL` | Expected access-token audience. |
| `PKGDEPOT_OIDC_JWT_ALGORITHMS` | `RS256` | Comma- or space-separated signing algorithms allowed by the provider. |
| `PKGDEPOT_OIDC_JWT_CACHE_LIFETIME` | `15m` | Maximum time a successfully fetched OIDC signing-key set is trusted before refresh. |
| `PKGDEPOT_OAUTH_CLIENT_ID` | CIMD URL derived from HTTPS `PKGDEPOT_URL` | Pre-registered OIDC client ID override; required for delegated login when `PKGDEPOT_URL` is HTTP. |
| `PKGDEPOT_OAUTH_CLIENT_SECRET` | Empty | Enables client-credentials automation; omit for delegated CLI login. |
| `PKGDEPOT_OAUTH_ISSUER` | Required with client secret | Expected issuer pin for client credentials. |

The server publishes protected-resource metadata at
`<origin>/.well-known/oauth-protected-resource<path>`, such as
`https://packages.example/.well-known/oauth-protected-resource/pkgdepot` for
`https://packages.example/pkgdepot`.

## Development

Requirements: Go 1.26 and an Arch Linux environment with `repo-add` and
`repo-remove` available.

```sh
go test ./...
go vet ./...
```

## Releases

Pushing a tag beginning with `v` publishes platform-specific archives to
GitHub Releases.
