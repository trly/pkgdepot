# Command Adapter Guide

## External Tool Boundary

- Keep this package as the narrow adapter to Arch repository tools. Validation, paths, locking, rollback, and mutation ordering belong to `internal/repository`.
- Production paths are fixed at `/usr/bin/repo-add` and `/usr/bin/repo-remove`; `PATH` must not override them.
- Preserve exact invocation: `repo-add --include-sigs --wait-for-lock <database> <package>` and `repo-remove --wait-for-lock <database> <name>`.
- `--wait-for-lock` complements service-level in-process and filesystem locks; it does not replace them.
- Keep `exec.CommandContext` so request cancellation terminates subprocesses.
- The service installs package/signature files before `Add` and rolls them back on failure; `Remove` updates the database before deleting files. Command success semantics affect repository consistency.

## Tests

- There are no direct tests. Repository and HTTP tests inject the command interface and require no pacman tools; verify changes with `go test ./internal/repository ./internal/httpapi`.
