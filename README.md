# pkgdepot

`pkgdepot` is an Arch Linux package repository server

## Run

The server requires a management token:

```sh
PKGDEPOT_TOKEN=change-me go run ./cmd/pkgdepot serve
```

Configuration is read from the environment:

| Variable | Default | Purpose |
| --- | --- | --- |
| `PKGDEPOT_ADDRESS` | `:8080` | HTTP listen address |
| `PKGDEPOT_DATA_ROOT` | `/var/lib/pkgdepot` | Repository, staging, and lock data |
| `PKGDEPOT_TOKEN` | none | Required management bearer token |
| `PKGDEPOT_URL` | `http://localhost:8080` | CLI server URL |

The host running `serve` must provide `/usr/bin/repo-add` and `/usr/bin/repo-remove`.

The public repository index is available at `/`. Select a repository architecture to browse and download its packages.

## CLI

```sh
PKGDEPOT_TOKEN=change-me pkgdepot publish stable x86_64 ./example-1.0-1-x86_64.pkg.tar.zst
PKGDEPOT_TOKEN=change-me pkgdepot publish -signature ./example-1.0-1-x86_64.pkg.tar.zst.sig stable x86_64 ./example-1.0-1-x86_64.pkg.tar.zst
PKGDEPOT_TOKEN=change-me pkgdepot list stable x86_64
PKGDEPOT_TOKEN=change-me pkgdepot remove stable x86_64 example
```

Public repository files are available at `/repos/{repository}/{architecture}/{filename}`. For example, add the following entry to `/etc/pacman.conf` to use the `stable` repository:

```ini
[pkgdepot]
Server = https://packages.example/repos/stable/$arch
```

Replace `packages.example` with the hostname of your pkgdepot server, then run `pacman -Sy` to synchronize the repository database.

## HTTP API

Management endpoints are versioned under `/api/v1` and require `Authorization: Bearer <token>`:

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
docker run --rm -p 8080:8080 -e PKGDEPOT_TOKEN=change-me -v pkgdepot:/var/lib/pkgdepot pkgdepot
```
