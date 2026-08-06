# pkgdepot

`pkgdepot` is an Arch Linux package repository server

## Run

Start the server with a persistent data root:

```sh
go run ./cmd/pkgdepot serve
```

Create API credentials locally against that same data root. The credential is displayed once; place it in the appropriate CI secret store rather than a configuration file.

```sh
pkgdepot token create --permission package:publish --repository stable --architecture x86_64 release-ci
```

The data root is security-sensitive. Run `serve` and local `token` commands as the pkgdepot service account, or grant equivalent administrative access. The credential store below the data root is restricted to that account and contains only Argon2id hashes, not token secrets.

Configuration is read from the environment. `PKGDEPOT_URL` is shared by the server and CLI: `serve` uses it as the canonical URL in generated repository configuration, while CLI commands use it as their API endpoint. Set it to the externally reachable URL of the server.

| Variable | Default | Purpose |
| --- | --- | --- |
| `PKGDEPOT_ADDRESS` | `:8080` | HTTP listen address |
| `PKGDEPOT_DATA_ROOT` | `/var/lib/pkgdepot` | Repository, staging, lock, and credential data |
| `PKGDEPOT_URL` | `http://localhost:8080` | Server URL |
| `PKGDEPOT_CREDENTIAL` | none | API credential |
| `PKGDEPOT_MAX_UPLOAD_SIZE` | `524288000` | Maximum multipart request size in bytes |
| `PKGDEPOT_HTTP_TIMEOUT` | `30s` | HTTP read, write, and idle timeout |

The host running `serve` must provide `/usr/bin/repo-add` and `/usr/bin/repo-remove`, part of the `pacman` package from Arch Linux.

## CLI

```sh
PKGDEPOT_CREDENTIAL=pd_<id>_<secret> pkgdepot publish stable x86_64 ./example-1.0-1-x86_64.pkg.tar.zst
PKGDEPOT_CREDENTIAL=pd_<id>_<secret> pkgdepot publish -signature ./example-1.0-1-x86_64.pkg.tar.zst.sig stable x86_64 ./example-1.0-1-x86_64.pkg.tar.zst
pkgdepot list stable x86_64
PKGDEPOT_CREDENTIAL=pd_<id>_<secret> pkgdepot remove stable x86_64 example
pkgdepot rename stable release
```

Use `pkgdepot token list`, `pkgdepot token revoke <id>`, and `pkgdepot token rotate <id>` to administer credentials locally. Publish and remove permissions can be scoped to a repository and architecture; token revocation is effective immediately.

`pkgdepot rename` is a local maintenance command. Run it with the same data root as the server (using `--data-root` or `PKGDEPOT_DATA_ROOT`). It copies a snapshot of every architecture, renames each copied database from `<old>.db.tar.gz` to `<new>.db.tar.gz`, then removes the old repository after the new snapshot is installed. Mutations that complete during copying may not be included in the new repository. Repository-scoped tokens are not updated; create replacement tokens scoped to the new repository name.

Public repository files are available at `/repos/{repository}/{architecture}/{filename}`. For example, add the following entry to `/etc/pacman.conf` to use the `stable` repository:

```ini
[pkgdepot]
Server = https://packages.example/repos/stable/$arch
```

Replace `packages.example` with the hostname of your pkgdepot server, then run `pacman -Sy` to synchronize the repository database.

## HTTP API

Management endpoints are versioned under `/api/v1`. Package listing is public; mutation endpoints require `Authorization: Bearer <locally-generated-credential>`:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/repositories` | List repositories and architectures |
| `GET` | `/api/v1/repositories/{repository}/{architecture}/packages` | List packages |
| `POST` | `/api/v1/repositories/{repository}/{architecture}/packages` | Multipart upload with `package` and optional `signature` fields |
| `DELETE` | `/api/v1/repositories/{repository}/{architecture}/packages/{package}` | Remove a package |

Successful package responses use the stable API contract defined in `internal/api`. Error responses contain a human-readable `error` and a machine-readable `code` such as `invalid_request`, `not_found`, or `unauthorized`. Clients should branch on `code`, not error text.

## Container

Build arguments allow the build and runtime bases to be pinned to immutable digests in release automation:

```sh
docker build --build-arg ARCHLINUX_IMAGE=archlinux:base@sha256:<digest> -t pkgdepot .
docker run --rm -p 8080:8080 -v pkgdepot:/var/lib/pkgdepot pkgdepot
```
