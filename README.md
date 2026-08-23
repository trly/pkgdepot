# pkgdepot

`pkgdepot` is an Arch Linux package repository server.

## Install the CLI

Download the Linux/amd64 archive from [GitHub Releases](https://github.com/trly/pkgdepot/releases), or build the CLI from source with Go 1.27:

```sh
go build -o pkgdepot ./cmd/pkgdepot
```

Use this host-side CLI for delegated login and repository management. Do not run
delegated login inside the server container: its loopback callback and OS
credential store are normally unavailable to the host browser.

## Deploy the server

Non-loopback deployments require HTTPS. Configure the identity provider before
starting the server, and ensure its discovery and signing-key endpoints are
reachable from the container. Save the following as `compose.yaml`, replacing
the example host and issuer URLs:

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

```sh
docker compose up -d
```

The container includes the Arch repository tools required for package mutations.
For a non-container server, install `/usr/bin/repo-add` and
`/usr/bin/repo-remove`; pkgdepot does not use substitutes from `PATH`.

### Path-prefixed deployments

`PKGDEPOT_URL` may include a path, such as `https://packages.example.com/pkgdepot`.
Configure the reverse proxy to remove that prefix before forwarding requests to
pkgdepot. Use the same prefix in pacman URLs. The OAuth protected-resource
metadata stays at the origin's `/.well-known/oauth-protected-resource` path.

## Configure authentication

pkgdepot is an OAuth 2.0 protected resource. Package and repository lists are
public; every mutation requires its operation scope:

| Mutation | Required scope |
| --- | --- |
| Publish a package | `package:publish` |
| Remove a package | `package:remove` |
| Create a repository | `repo:create` |
| Remove a repository | `repo:remove` |
| Rename a repository | `repo:rename` |

The identity provider must provide OIDC discovery and signing keys, issue signed
RFC 9068 access tokens, and support the OAuth `resource` parameter. Delegated
login uses authorization code with S256 PKCE. It can use pkgdepot's Client ID
Metadata Document (CIMD) on an HTTPS deployment, or a pre-registered public
client specified with `PKGDEPOT_OAUTH_CLIENT_ID`. Pre-registered clients must
allow these loopback callbacks:

```text
http://127.0.0.1:8085/oauth/callback
http://127.0.0.1:8086/oauth/callback
http://127.0.0.1:8087/oauth/callback
http://127.0.0.1:8088/oauth/callback
http://127.0.0.1:8089/oauth/callback
```

Delegated access tokens need both the requested OAuth scope and a role mapped to
that scope. By default, roles are read from the `pkgdepot_roles` claim:

```json
{"pkgdepot_roles":["publisher"]}
```

The built-in `admin` role grants every mutation scope. `publisher` grants only
`package:publish`. Configure a different claim or mapping with
`PKGDEPOT_ROLE_CLAIM` and `PKGDEPOT_ROLE_SCOPES`.

### Delegated CLI login

Delegated login requires an OS credential store for the cached token. For an
HTTPS deployment with a provider that supports CIMD:

```sh
export PKGDEPOT_URL=https://packages.example.com

pkgdepot login --scope repo:create --scope package:publish
pkgdepot repo create stable
pkgdepot package publish stable ./example-1.0-1-x86_64.pkg.tar.zst
```

The CLI prints URLs for identity verification and scope selection. Run
`pkgdepot logout` to delete its cached delegated token. For a provider without
CIMD, set `PKGDEPOT_OAUTH_CLIENT_ID` to a registered client ID before login.
For the default local HTTP URL, a pre-registered client ID is always required.

### Automation with client credentials

For headless or containerized automation, configure a confidential client. The
server must set `PKGDEPOT_CLIENT_CREDENTIALS_SUBJECT_TEMPLATE` to the format of
the provider's client-credentials subject, for example `client-{client_id}`.
The client needs an ID, secret, and issuer pin:

```sh
PKGDEPOT_URL=https://packages.example.com \
PKGDEPOT_OAUTH_ISSUER=https://id.example.com \
PKGDEPOT_OAUTH_CLIENT_ID=<client-id> \
PKGDEPOT_OAUTH_CLIENT_SECRET=<client-secret> \
pkgdepot package publish stable ./example-1.0-1-x86_64.pkg.tar.zst
```

The provider must support `client_secret_basic` and grant the required operation
scope. Client credentials apply to all mutation commands and cannot use
`pkgdepot login`.

### Pocket ID automation

To use Pocket ID with client credentials:

1. Create a `pkgdepot` API whose resource is the exact public `PKGDEPOT_URL`.
2. Add the required operation scopes, such as `package:publish` and `package:remove`.
3. Create a confidential OIDC client and grant its API access under **Client access**.
4. Start pkgdepot with the Pocket ID issuer and a matching subject template.
5. Configure the CLI with the confidential client credentials as shown above.

```sh
PKGDEPOT_URL=https://packages.example.com \
PKGDEPOT_OIDC_ISSUER=https://id.example.com \
PKGDEPOT_CLIENT_CREDENTIALS_SUBJECT_TEMPLATE=client-{client_id} \
pkgdepot serve
```

## Manage repositories and packages

`--url` uses `PKGDEPOT_URL` and defaults to `http://127.0.0.1:8080`.
`--architecture` defaults to `x86_64`.

```sh
# Public JSON lists
pkgdepot repo list
pkgdepot package list stable

# Protected repository lifecycle operations
pkgdepot repo create stable
pkgdepot repo rename stable production
pkgdepot repo remove production

# Protected package operations
pkgdepot package publish stable ./example-1.0-1-x86_64.pkg.tar.zst
pkgdepot package publish --signature ./example-1.0-1-x86_64.pkg.tar.zst.sig stable ./example-1.0-1-x86_64.pkg.tar.zst
pkgdepot package remove stable example
```

Publishing creates the repository and architecture directory if needed, so an
explicit `repo create` is optional. `--signature` stores a detached signature
alongside the package as `<package filename>.sig`; pkgdepot does not verify it.
`repo remove` recursively deletes every architecture, package, signature, and
database in that repository. `package remove` deletes the package, its detached
signature, and its database entry. `repo rename` rebuilds a copy under the new
name; packages published concurrently may not appear in that snapshot.

## Use the repository with pacman

For a repository named `stable`, use the same name for the pacman section so
pacman requests `stable.db`:

```ini
[stable]
Server = https://packages.example.com/repos/stable/$arch
```

For a path-prefixed resource URL, include the prefix:

```ini
[stable]
Server = https://packages.example.com/pkgdepot/repos/stable/$arch
```

```sh
sudo pacman -Syu example
```

## Configuration

### Server runtime

| Variable | Default | Description |
| --- | --- | --- |
| `PKGDEPOT_ADDRESS` | `:8080` | Server bind address. |
| `PKGDEPOT_APP_NAME` | `PKGdepot` | Name shown in the web interface. |
| `PKGDEPOT_DATA_ROOT` | `/var/lib/pkgdepot` | Repository, staging, and lock storage root. |
| `PKGDEPOT_URL` | `http://127.0.0.1:8080` | Canonical public resource URL. |
| `PKGDEPOT_MAX_UPLOAD_SIZE` | `524288000` | Maximum complete multipart upload request size in bytes. |
| `PKGDEPOT_HTTP_TIMEOUT` | `30s` | Read, write, and idle timeout. |

### Server authorization

| Variable | Default | Description |
| --- | --- | --- |
| `PKGDEPOT_OIDC_ISSUER` | Required | Trusted OIDC issuer URL. |
| `PKGDEPOT_OIDC_AUDIENCE` | `PKGDEPOT_URL` | Expected access-token audience. |
| `PKGDEPOT_OIDC_JWT_ALGORITHMS` | `RS256` | Allowed access-token signing algorithms. |
| `PKGDEPOT_OIDC_JWT_CACHE_LIFETIME` | `15m` | Maximum signing-key-set trust lifetime. |
| `PKGDEPOT_ROLE_CLAIM` | `pkgdepot_roles` | Access-token claim containing roles. |
| `PKGDEPOT_ROLE_SCOPES` | Built-in admin/publisher mapping | JSON object mapping roles to operation scopes. |
| `PKGDEPOT_CLIENT_CREDENTIALS_SUBJECT_TEMPLATE` | Disabled | Client-credentials subject format containing one `{client_id}`. |

### CLI OAuth

| Variable | Default | Description |
| --- | --- | --- |
| `PKGDEPOT_OAUTH_CLIENT_ID` | Empty | Required for client credentials and delegated clients without CIMD. HTTPS delegated clients with CIMD derive the server metadata URL when it is empty. |
| `PKGDEPOT_OAUTH_CLIENT_SECRET` | Empty | Enables client-credentials authentication; omit for delegated login. |
| `PKGDEPOT_OAUTH_ISSUER` | Required with a client secret | Expected issuer pin before credentials are sent. |

`PKGDEPOT_OIDC_*` configures the server; `PKGDEPOT_OAUTH_*` configures the CLI.

## Development

Go 1.27 is required. Build and test without Arch Linux or pacman tools:

```sh
go test ./...
go vet ./...
```

Run real local package mutations only on a system that supplies
`/usr/bin/repo-add` and `/usr/bin/repo-remove`.
