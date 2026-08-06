# pkgdepot

`pkgdepot` is an Arch Linux package repository server.

## Docker

Start the server

```sh
docker pull ghcr.io/trly/pkgdepot:latest
docker run -d --name pkgdepot \
  -p 8080:8080 \
  -e PKGDEPOT_URL=https://packages.example.com \
  -v pkgdepot-data:/var/lib/pkgdepot \
  ghcr.io/trly/pkgdepot:latest
```

`PKGDEPOT_URL` must be the externally reachable URL of the server. The data
volume contains repositories, staging files, locks, and credentials.

Create an initial admin token:

```sh
docker run --rm -it \
  -v pkgdepot-data:/var/lib/pkgdepot \
  ghcr.io/trly/pkgdepot:latest \
  token create --permission package:publish release-token
```

`PKGDEPOT_CREDENTIAL` should be set with the generated value to authenticate with the CLI.

```sh
PKGDEPOT_URL=https://packages.example.com \
PKGDEPOT_CREDENTIAL=<generated_token> \
pkgdepot package publish stable ./example-1.0-1-x86_64.pkg.tar.zst
```

Add the repository to `/etc/pacman.conf` on Arch clients:

```ini
[pkgdepot]
Server = https://packages.example.com/repos/stable/$arch
```

## Binary

Download the binary for your platform from the [GitHub Releases](https://github.com/trly/pkgdepot/releases)

```sh
sudo install -m 0755 pkgdepot-linux-amd64 /usr/local/bin/pkgdepot
sudo mkdir -p /var/lib/pkgdepot
```

The server must be Arch Linux, or otherwise provide `/usr/bin/repo-add` and
`/usr/bin/repo-remove` from the `pacman` package. Start it with:

```sh
PKGDEPOT_URL=https://packages.example.com \
PKGDEPOT_DATA_ROOT=/var/lib/pkgdepot \
pkgdepot serve
```

Create credentials locally against the same data root:

```sh
PKGDEPOT_DATA_ROOT=/var/lib/pkgdepot \
pkgdepot token create --permission package:publish release-ci
```

## Environment Variables

| Variable | Default | Description |
| --- | --- | --- |
| `PKGDEPOT_ADDRESS` | `:8080` | Server listen address. |
| `PKGDEPOT_APP_NAME` | `PKGdepot` | Application name shown in the web interface. |
| `PKGDEPOT_DATA_ROOT` | `/var/lib/pkgdepot` | Root directory for repositories, staging files, locks, and credentials. |
| `PKGDEPOT_URL` | `http://localhost:8080` | Public server URL used by the server and CLI. Must be an absolute HTTP(S) URL. |
| `PKGDEPOT_CREDENTIAL` | None | API credential used by package publish, list, and remove commands. |
| `PKGDEPOT_MAX_UPLOAD_SIZE` | `524288000` | Maximum package upload size in bytes. |
| `PKGDEPOT_HTTP_TIMEOUT` | `30s` | HTTP server read, write, and idle timeout. Uses Go duration syntax. |

## Development

Requirements: Go 1.26 and an Arch Linux environment with `repo-add` and
`repo-remove` available.

```sh
git clone https://github.com/trly/pkgdepot.git
cd pkgdepot
go mod download
go run ./cmd/pkgdepot serve
```

The development server uses `http://localhost:8080` and
`/var/lib/pkgdepot` by default. Use a writable local data root when needed:

```sh
PKGDEPOT_DATA_ROOT="$PWD/.data" go run ./cmd/pkgdepot serve
```

Run the checks with:

```sh
go test ./...
go vet ./...
```

## Releases

Pushing a tag beginning with `v` publishes platform-specific archives to GitHub Releases:

```sh
git tag v1.0.0
git push origin v1.0.0
```

Download the archive for the target platform and place the `pkgdepot` binary on `PATH`. A Linux host still needs `/usr/bin/repo-add` and `/usr/bin/repo-remove` from the Arch Linux `pacman` package when running the server.
