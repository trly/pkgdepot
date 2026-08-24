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
http://127.0.0.1/oauth/callback
http://[::1]/oauth/callback
```

The CLI selects IPv4 or IPv6 and binds an ephemeral port for each login. Do not
register fixed ports; the provider matches these loopback redirect URIs with a
variable port.

Authorization is controlled by the identity provider's client restrictions and
resource scopes. pkgdepot publishes two CIMD clients:

| Client | CIMD client ID path | Intended group | Scopes |
| --- | --- | --- | --- |
| Publisher | `/oauth/clients/cli-publisher` | `pkgdepot publishers` | `package:publish` |
| Admin | `/oauth/clients/cli-admin` | `pkgdepot administrators` | All mutation scopes |

Restrict each client to its corresponding provider group. pkgdepot does not
read user roles or map role claims; a validated access token with the requested
operation scope is authorized. The token audience remains the canonical
`PKGDEPOT_URL`, not either CIMD client ID.

### Delegated CLI login

Delegated login requires an OS credential store for the cached token. For an
HTTPS deployment with a provider that supports CIMD:

```sh
export PKGDEPOT_URL=https://packages.example.com

pkgdepot login --access admin --scope repo:create --scope package:publish
pkgdepot repo create stable
pkgdepot package publish stable ./example-1.0-1-x86_64.pkg.tar.zst
```

The CLI opens one authorization URL for the selected access profile. Pocket ID
performs authentication and displays the requested permissions. `--access`
selects the CIMD client, either `publisher` or `admin`; it defaults to
`publisher`. `--scope` selects the operation permissions requested from that
client and may be repeated. Without `--scope`, publisher login requests
`package:publish`, while admin login requests all scopes advertised by the
protected resource. Publisher access cannot request administrative scopes such
as `package:remove`; use `--access admin` explicitly. Run `pkgdepot logout` to
delete cached delegated tokens. For a provider without CIMD, set
`PKGDEPOT_OAUTH_CLIENT_ID` to a registered client ID before login. For the
default local HTTP URL, a pre-registered client ID is always required.

### Automation with client credentials

For headless or containerized automation, configure a confidential client. The
client needs an ID, secret, issuer pin, and the required operation scopes:

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
4. Start pkgdepot with the Pocket ID issuer.
5. Configure the CLI with the confidential client credentials as shown above.

```sh
PKGDEPOT_URL=https://packages.example.com \
PKGDEPOT_OIDC_ISSUER=https://id.example.com \
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

### CLI OAuth

| Variable | Default | Description |
| --- | --- | --- |
| `PKGDEPOT_OAUTH_CLIENT_ID` | Empty | Required for client credentials and delegated clients without CIMD. HTTPS delegated clients with CIMD derive the publisher or admin CIMD URL. |
| `PKGDEPOT_OAUTH_CLIENT_SECRET` | Empty | Enables client-credentials authentication; omit for delegated login. |
| `PKGDEPOT_OAUTH_ISSUER` | Required with a client secret | Expected issuer pin before credentials are sent. |

`PKGDEPOT_OIDC_*` configures the server; `PKGDEPOT_OAUTH_*` configures the CLI.

## Development

Go 1.27 is required. Build and test without Arch Linux or pacman tools:

```sh
go test ./...
go vet ./...
```

To seed a local instance from an existing pkgdepot repository, set the data
root and run the bootstrap script with the source origin, repository name, and
architecture. The destination must not already exist:

```sh
PKGDEPOT_DATA_ROOT=/tmp/pkgdepot \
  ./scripts/bootstrap-repository.sh \
  https://packages.trly.dev stable x86_64
```

The script downloads `<repository>.db.tar.gz` and every package named by that
database into `repositories/<repository>/<architecture>`. It expects the
source repository at `/repos/<repository>/<architecture>`.

Run real local package mutations only on a system that supplies
`/usr/bin/repo-add` and `/usr/bin/repo-remove`.
