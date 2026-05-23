# AGENTS.md — internal/eshulocal guidance for LLM assistants

## Read first

1. `go/internal/eshulocal/README.md` — full ownership boundary, platform split,
   exported surface, and invariants
2. `go/internal/eshulocal/layout.go` — `Layout`, `BuildLayout`,
   `ResolveWorkspaceRoot`, `WorkspaceID`
3. `go/internal/eshulocal/startup.go` — `PrepareWorkspace`, `StartupDeps`
4. `go/internal/eshulocal/reclaim.go` — `ValidateOrReclaimOwner`, `ReclaimDeps`,
   typed error vars
5. `go/internal/eshulocal/owner_lock_unix.go` — `AcquireOwnerLock`,
   `unix.Flock` protocol; `owner_lock_windows.go` for the stub
6. `go/internal/eshulocal/postgres_unix.go` — `StartEmbeddedPostgres`,
   `ManagedPostgres`; `postgres_windows.go` for the stub
7. `go/internal/eshulocal/health_unix.go` — `ProcessAlive`, `SocketHealthy`,
   `StopEmbeddedPostgres`, `DefaultReclaimDeps`; `health_windows.go` for stubs

## Invariants this package enforces

- **Workspace ID stability** — `WorkspaceID` hashes the symlink-resolved
  absolute path with SHA-256 (first 20 bytes, hex). Case-insensitive filesystems
  (macOS HFS+, Windows NTFS) lowercase the path before hashing. Do not change
  the algorithm without a workspace migration path.

- **Non-blocking lock** — `AcquireOwnerLock` uses `unix.LOCK_EX|unix.LOCK_NB`.
  It returns `ErrWorkspaceOwned` immediately if the lock is held. Do not change
  this to a blocking lock — callers expect to get a fast failure.

- **Startup admission order** — `PrepareWorkspace` enforces: acquire lock →
  `EnsureLayoutVersion` → `ValidateOrReclaimOwner`. Reversing or skipping
  these steps creates split-brain windows.

- **Atomic file writes** — `WriteOwnerRecord` and `WriteLayoutVersion` both
  use temp-file + `Chmod(0o600)` + `Sync()` + `Rename`. Do not replace them
  with `os.WriteFile`; partial writes during crash leave the workspace in an
  inconsistent state.

- **Reclaim assumes lock is held** — `ValidateOrReclaimOwner` does not acquire
  the lock itself. Callers must hold `owner.lock` before calling it, as
  `PrepareWorkspace` does.

- **Ownerless Postgres reclaim is start-side only** — `StartEmbeddedPostgres`
  may stop a live `postmaster.pid` when `owner.json` is already gone, but only
  after `PrepareWorkspace` has acquired `owner.lock`. Keep the PID, socket, and
  Postgres protocol checks together so a stale or reused PID is not enough to
  trigger `pg_ctl stop`.

- **Windows stubs fail loudly** — `AcquireOwnerLock`, `StartEmbeddedPostgres`,
  `ProcessAlive`, `SocketHealthy`, and `StopEmbeddedPostgres` on Windows return
  errors or false. Do not add Windows implementations without also updating the
  `local-data-root-spec.md` and `local-host-lifecycle.md` docs.

- **No internal imports** — this package must remain a leaf in the dependency
  graph. It must not import any `internal/` sub-package other than itself.

## Common changes and how to scope them

- **Add a field to `OwnerRecord`** → add the JSON field in `owner.go`, update
  `WriteOwnerRecord` callers in `cmd/eshu` or `cmd/bootstrap-index`, add a test
  in `owner_test.go`. Verify that `ValidateOrReclaimOwner` still reads the field
  correctly when it is missing (new field added after existing workspaces were
  created).

- **Add a new layout directory** → add the path to `Layout` in `layout.go`,
  add the `os.MkdirAll` call in `EnsureLayoutVersion` or `BuildLayout`, add a
  test in `layout_test.go`. The VERSION file must not be inside the new dir or
  the `dirHasEntriesExcept` logic needs updating.

- **Change the workspace ID algorithm** → this is a breaking change for
  existing workspaces. Read `docs/docs/reference/local-data-root-spec.md`
  first, confirm with the team, document a migration path. Do not make this
  change speculatively.

- **Add a new reclaim condition** → add a typed error var in `reclaim.go`, add
  a branch in `ValidateOrReclaimOwner`, add health-check fields to `ReclaimDeps`
  if needed, add a test in `reclaim_test.go`. Confirm the failure message
  includes enough context (PID, path, version) for 3 AM diagnosis.

## Failure modes and how to debug

- Symptom: `ErrWorkspaceOwned` on startup → another process holds the flock. Run
  `lsof owner.lock` to find the holder, or use `ValidateOrReclaimOwner` after
  confirming the process is dead.

- Symptom: `ErrIncompatibleLayoutVersion` → the VERSION file in the workspace
  data root does not match the current binary's version. Either the binary was
  downgraded, or the data root was created by a different version. Read the
  version with `ReadLayoutVersion` and compare to the current binary.

- Symptom: `ErrWorkspaceOwnerActive` on reclaim → `ValidateOrReclaimOwner`
  found a live PID or responsive socket in `owner.json`. Check `ProcessAlive`
  output for the stored PID; if the process is truly dead, the socket file may
  be stale — clean it manually then retry.

- Symptom: embedded Postgres fails to start → `StartEmbeddedPostgres` tries up
  to three port reservations. Check `eshu` logs for `"process already listening
  on port"`. If all three attempts fail on port collision, the loopback TCP port
  range may be exhausted; check `sysctl net.inet.ip.portrange`.

- Symptom: embedded Postgres reports `postmaster.pid` already exists while
  `owner.json` is missing → a previous owner likely removed metadata before
  Postgres stopped. Startup should acquire `owner.lock`, verify the lock-file
  PID, socket, and Postgres protocol, then run `pg_ctl stop` before starting a
  fresh embedded Postgres. Do not remove `postmaster.pid` manually while a
  process is still live.

## Testing

Gate: `cd go && go test ./internal/eshulocal -count=1`

Key test files:

- `layout_test.go` — `BuildLayout`, `ResolveWorkspaceRoot`, `WorkspaceID`
- `owner_test.go` — `ReadOwnerRecord`, `WriteOwnerRecord`
- `reclaim_test.go` — `ValidateOrReclaimOwner`, error paths
- `startup_test.go` — `PrepareWorkspace` admission order
- `version_test.go` — `EnsureLayoutVersion`, `ReadLayoutVersion`,
  `WriteLayoutVersion`
- `health_unix_test.go` — `ProcessAlive`, `SocketHealthy`,
  `StopEmbeddedPostgres`, `StartEmbeddedPostgres`, ownerless Postgres startup
  reclaim
