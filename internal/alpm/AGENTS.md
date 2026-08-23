# ALPM Package Guide

## Parsing Contracts

- `InspectPackage` parses root `.PKGINFO`; `ReadDatabase` parses `*/desc` records from gzip repository databases. These are distinct formats.
- Package compression is detected by magic bytes, not extensions. Supported inputs are tar, gzip, bzip2, xz, and zstd; inspection requires a seekable `*os.File` so detection can rewind it.
- `.PKGINFO` requires the literal ` = ` delimiter and mandatory `pkgname`, `pkgver`, and `arch`. Ignore comments, blank lines, and unknown keys; preserve repeated dependency order.
- Preserve decompression limits: all package formats are capped at 64 MiB decoded output; xz dictionary and zstd memory/window are also capped.
- Preserve package metadata limits: `.PKGINFO` is capped at 256 KiB, with at most 256 dependencies and 64 KiB of aggregate dependency values. Repository database dependency sections enforce the same dependency limits.
- `ReadDatabase` returns an empty non-nil slice for a missing database. It accepts only gzip, ignores non-`/desc` entries, requires `NAME` and `VERSION`, and sorts only by package name.
- Package `Filename` and `Size` are the source file basename and compressed on-disk size, not archive-header values.

## Tests

- Tests build archives in temporary directories; there is no checked-in fixture set.
- Run `go test ./internal/alpm`. The oversized-zstd-window test allocates roughly 65 MiB and is intentionally heavier than the other parser tests.
