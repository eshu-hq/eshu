# Local Eshu Service Data Root Spec

This document defines the on-disk contract for one local Eshu service
workspace. It covers layout, ownership, stale-process recovery, and the
version gate used before embedded Postgres or the local graph backend starts.

## Root Location

Default root:

```text
${ESHU_HOME}/local/workspaces/<workspace_id>/
```

Default `ESHU_HOME`:

- macOS: `~/Library/Application Support/eshu`
- Linux: `${XDG_DATA_HOME:-~/.local/share}/eshu`
- Windows: `%LOCALAPPDATA%\\eshu`

The current local runtime targets macOS and Linux. Windows stubs compile, but
workspace ownership and embedded Postgres startup return explicit unsupported
errors.

## Workspace ID

Workspace root resolution happens before hashing:

1. explicit `--workspace-root`, if provided
2. nearest ancestor containing `.eshu.yaml`
3. nearest ancestor containing `.git`
4. invocation working directory

The `workspace_id` is the first 20 bytes of the SHA-256 hash of the
symlink-resolved workspace root, encoded as lowercase hex. Path separators are
normalized to `/`; case-insensitive filesystems lowercase the resolved path
before hashing. Symlinks that resolve to the same directory therefore converge
to the same workspace ID.

## Layout

```text
${ESHU_HOME}/local/workspaces/<workspace_id>/
  VERSION
  owner.lock
  owner.json
  postgres/
    data/
    runtime/
    binaries/
  graph/
    nornicdb/
  logs/
  cache/
```

`postgres/` is owned by the embedded Postgres runtime. Its `data/` and
`runtime/` subdirectories are runtime state; `binaries/` holds the managed
embedded Postgres tools.

`graph/nornicdb/` exists only for `local_authoritative`. The current
local-authoritative owner rebuilds that directory from the workspace source tree
on owner start, so clients must not treat graph data files as durable user data.
Graph logs live under `logs/`, and local graph credentials are recreated inside
the graph data directory when the graph store is reset.

`cache/` contains rebuildable derived state such as filesystem repository
selectors and embedded-Postgres package cache.

## Owner Record

`owner.json` is written atomically with `0600` permissions while the local owner
is live. The current record shape is:

| Field | Meaning |
| --- | --- |
| `pid` | Local Eshu owner process PID. |
| `started_at` | Owner start timestamp. |
| `hostname` | Host that wrote the record. |
| `workspace_id` | Workspace ID for this data root. |
| `version` | Eshu binary/data-root version used for admission. |
| `socket_path` | Reserved owner socket path. It exists in the JSON shape but is usually empty today. |
| `postgres_pid` | Embedded Postgres PID. |
| `postgres_port` | Loopback TCP port used by embedded Postgres readiness and local attach flows. |
| `postgres_data_dir` | Absolute path to `postgres/data`. |
| `postgres_socket_dir` | Short Unix-socket directory, normally under `${TMPDIR}/eshu/<workspace_id>` with fallback to `/tmp`. |
| `postgres_socket_path` | Workspace-local Postgres Unix socket path. |
| `profile` | Active query profile: `local_lightweight` or `local_authoritative`. |
| `graph_backend` | Graph backend for `local_authoritative`; currently `nornicdb`. Omitted for `local_lightweight`. |
| `graph_address` | Loopback graph bind address, currently `127.0.0.1`. |
| `graph_pid` | Graph backend PID. Embedded NornicDB records the owner PID; process mode records the child PID. |
| `graph_bolt_port` | Loopback Bolt port for Neo4j-compatible graph access. |
| `graph_http_port` | Loopback HTTP health/admin port. |
| `graph_data_dir` | Absolute graph backend data directory, normally `graph/nornicdb`. |
| `graph_socket_path` | Reserved for socket-capable graph backends. NornicDB does not populate it today. |
| `graph_version` | Graph backend version. |
| `graph_username` | Local graph admin username. |
| `graph_password` | Per-workspace local graph password copied into `owner.json` for attach processes; this is sensitive. |

## Ownership And Startup Admission

One local Eshu service owns a workspace root at a time. Startup admission is:

1. acquire `owner.lock`
2. validate `VERSION`
3. validate or reclaim `owner.json`
4. start embedded Postgres
5. start the local graph backend only for `local_authoritative`
6. write the new `owner.json`

On Unix, `owner.lock` uses non-blocking `flock(LOCK_EX | LOCK_NB)`. If another
process holds the lock, startup fails instead of waiting. Windows ownership is
not implemented.

A second invocation should attach to a healthy owner when that command supports
attach semantics, or fail fast with an actionable error. It must not start a
competing owner for the same workspace.

## Stale-Record Recovery

Reclaim assumes the caller already holds `owner.lock`.

If `owner.json` points at a live owner PID or healthy reserved owner socket,
startup fails with `workspace owner still active`. If the owner looks dead but
embedded Postgres is still alive by PID or socket, the new process runs
`pg_ctl stop -m fast` against `postgres_data_dir`, rechecks PID and socket
health, and only then removes the stale owner record.

If a graph backend still appears healthy, the new owner asks the recorded graph
runtime to stop and verifies it is no longer healthy before reclaiming. For the
current NornicDB path, healthy means:

- recorded `graph_pid` is alive
- recorded `graph_http_port` answers `GET /health`
- recorded `graph_bolt_port` completes Neo4j Bolt negotiation

If `owner.json` was already removed but `postgres/data/postmaster.pid` remains
live, startup may stop that ownerless Postgres only after holding
`owner.lock` and proving PID liveness, Unix-socket health, and Postgres protocol
readiness from the lock file.

## Local Endpoints

Embedded Postgres uses both a short Unix socket and a loopback-only TCP port.
The embedded Postgres library performs readiness checks over
`localhost:<port>`, and local attach flows reuse the recorded loopback endpoint.
This port is not a public network surface and must bind only to loopback.

The current `local_authoritative` NornicDB path uses loopback-only TCP ports for
HTTP health and Bolt. It does not use a graph Unix socket today.

To avoid Unix `sun_path` limits, Postgres socket paths should live under a
short runtime directory such as `${TMPDIR}/eshu/<workspace_id>`. If that path is
too long, the runtime falls back to shorter `/tmp`-based paths and records the
resolved path in `owner.json`.

## Version Gate

`VERSION` is the data-root schema/version marker. The current implementation:

- creates `VERSION` for a new empty data root
- opens normally when the file matches the current binary version
- refuses a non-empty root that lacks `VERSION`
- refuses a mismatched version with an explicit incompatible-version error

There is no silent forward or backward compatibility path. A future migration
must be explicit and should use write-new-then-swap or backup-before-migrate
semantics so a failure cannot strand the workspace halfway between versions.

## Crash Recovery Expectations

On crash or forced shutdown:

- Postgres WAL recovery runs normally on the next start
- stale owner records are recoverable through the lock-first reclaim path
- ownerless live Postgres is stopped only after lock, PID, socket, and protocol
  checks agree
- local-authoritative graph state is rebuildable from the workspace source tree
- derived caches under `cache/` may be pruned and rebuilt

## Permissions And Security

- Data roots are per-user and are not intended for shared multi-UID access.
- A different user should fail with an actionable ownership or permissions
  error.
- `owner.json`, `VERSION`, lock files, logs, credentials, and runtime
  directories are created with owner-only permissions where the runtime writes
  them.
- Eshu does not provide encryption at rest by default. Operators who need it
  should use host-level disk encryption.
- `graph_password` in `owner.json` is sensitive and relies on the owner-only
  permissions above.

## Filesystem Limitation

The local runtime targets local filesystems with reliable advisory locking. Do
not use this data-root contract on non-local filesystems where `flock`
semantics are not dependable enough for workspace ownership, such as common NFS
or SMB mounts, unless Eshu gains a verified compatibility story for that
filesystem class.

## Non-Goals

- shared cross-workspace data roots
- concurrent multi-writer ownership of one workspace root
- treating rebuilt local graph data or caches as durable user data
- hidden destructive reset behavior

## Related References

- [Local Eshu Service Lifecycle](local-host-lifecycle.md)
- [Graph Backend Operations](graph-backend-operations.md)
- [Runtime And Storage Environment](environment-runtime-storage.md)
