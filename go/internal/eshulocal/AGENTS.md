# internal/eshulocal Agent Rules

This package owns local data-root and embedded-Postgres coordination for
`eshu graph start`. It MUST stay a leaf package: no collector, parser, reducer,
query, storage, or graph-backend imports.

## Read First

MUST read these before editing:

1. `README.md` and `doc.go`.
2. `layout.go`, `startup.go`, `reclaim.go`, `owner.go`, `version.go`.
3. Platform files for the touched behavior: `*_unix.go` and `*_windows.go`.
4. `docs/public/reference/local-data-root-spec.md` and
   `docs/public/reference/local-host-lifecycle.md` before changing layout,
   ownership, reclaim, or platform support.

## Local Invariants

- Workspace IDs MUST remain stable: symlink-resolved absolute path, lowercased
  on case-insensitive filesystems, SHA-256 first 20 bytes as hex.
- `AcquireOwnerLock` MUST use non-blocking exclusive flock on Unix and return
  `ErrWorkspaceOwned` quickly when held.
- Startup order MUST stay: acquire `owner.lock`, ensure layout version,
  validate or reclaim owner.
- Owner and layout files MUST use atomic temp-file, `0600`, sync, and rename
  writes.
- `ValidateOrReclaimOwner` assumes the caller already holds `owner.lock`.
- Ownerless embedded Postgres reclaim MUST require PID, socket, and protocol
  checks to agree before `pg_ctl stop`.
- Windows stubs MUST fail loudly until the local data-root specs and tests cover
  real Windows behavior.
- Embedded Postgres logs MUST stay in workspace `postgres.log`; callers own
  operator-facing status and telemetry.

## Change Rules

- New owner fields MUST tolerate old records missing the field and update
  writer callers.
- New layout paths MUST update layout tests and version/spec docs if the
  on-disk contract changes.
- Workspace ID or reclaim semantics are compatibility changes; do not make them
  without a migration path and local-host docs updates.
- New reclaim conditions MUST use typed errors and include enough PID/path/
  version context for diagnosis.

## Proof

Run the focused gate for any edit:

```bash
cd go
go test ./internal/eshulocal -count=1
go vet ./internal/eshulocal
go doc ./internal/eshulocal
```

Docs-only edits also need the package-doc verifier for this directory and
`git diff --check`.
