# Repository Package Guidance

## Locking

- Package mutations (`PublishUpload` and `Remove`) acquire a shared lock on `locks/repositories.lock`, then a per-repository/architecture in-process mutex and filesystem lock.
- Repository lifecycle mutations (`Create`, `Rename`, and `RemoveRepository`) acquire an exclusive lock on `locks/repositories.lock`.
- Preserve both the in-process locks and filesystem `flock` calls. The filesystem lock coordinates separate `pkgdepot` processes; the in-process locks coordinate goroutines in one process.
- Keep lock acquisition ordered consistently: acquire the global lifecycle lock before the per-repository/architecture lock.
- Release every lock and close its file descriptor on all error paths.

## Testing

- Existing concurrency tests verify lifecycle operations wait for package mutations within one process.
- The test suite does not currently exercise the OS-level lock boundary with separate subprocesses. This is a testing gap, not a known implementation defect.
- If changing lock acquisition or mutation flow, add or run a subprocess-based test against a shared data root to verify that lifecycle and package mutations remain serialized across processes.

## Verification

- Format changed Go files with `gofmt`.
- Run `go test ./internal/repository` and `go test ./...`.
- Run `go vet ./...` before completing repository changes.
