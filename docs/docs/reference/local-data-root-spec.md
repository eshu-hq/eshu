# Local Host Data Root Spec

This document defines the on-disk contract for the lightweight local host.

The data root must support:

- one owner per workspace
- clean restart
- stale-lock recovery
- explicit version migration or reset

## Root Location

Default root:

```text
${ESHU_HOME}/local/workspaces/<workspace_id>/
```

Default `ESHU_HOME`:

- macOS: `~/Library/Application Support/eshu`
- Linux: `${XDG_DATA_HOME:-~/.local/share}/eshu`
- Windows: `%LOCALAPPDATA%\\eshu`

Initial Chunk 3 support targets macOS and Linux. Windows-equivalent ownership
and transport primitives are deferred until a later implementation wave.

## Workspace ID Algorithm

Workspace root resolution must happen before the hash is computed.

Resolution order:

1. explicit `--workspace-root`, if provided
2. nearest ancestor containing `.eshu.yaml`
3. nearest ancestor containing `.git`
4. invocation working directory

This keeps single-repo usage intuitive while allowing an explicit monofolder
root to own multiple sibling repositories.

`workspace_id` is:

1. `realpath` of the workspace root
2. normalize path separators to `/`
3. on case-insensitive filesystems, lowercase the normalized path before hash
4. compute SHA-256
5. use the first 20 bytes as lowercase hex

This keeps the ID stable while avoiding long path components. Two symlinked
paths that resolve to the same real path must therefore converge to the same
workspace ID.

## Layout

```text
<workspace_root>/
  VERSION
  owner.lock
  owner.json
  postgres/
  graph/
  logs/
  cache/
```

`graph/` is the persistent data directory for the local graph backend on the
`local_authoritative` profile. On `local_lightweight`, this directory is
absent. When present, its internal layout is owned by the graph backend
(for example `graph/nornicdb/`).

### Files

- `VERSION`
  - layout version for migration decisions
- `owner.lock`
  - ownership sentinel used with `flock`
- `owner.json`
  - current owner metadata
- `postgres/`
  - embedded Postgres data directory
- `logs/`
  - local-host lifecycle and recovery logs
- `cache/`
  - optional derived local caches that can be rebuilt

## Owner Record

`owner.json` should include:

- `pid`
- `started_at`
- `hostname`
- `workspace_id`
- `version`
- `socket_path`
- `postgres_pid`
- `postgres_port`
- `postgres_data_dir`
- `postgres_socket_dir`
- `postgres_socket_path`
- `profile` — active Eshu query profile
- `graph_backend` — optional, populated only on `local_authoritative`;
  allowed values enumerated by `capability-conformance-spec.md`
- `graph_pid` — optional, graph backend PID. Embedded NornicDB records the
  workspace owner PID; process mode records the child process PID.
- `graph_address` — optional, loopback bind address for graph backends that
  use TCP listener ports
- `graph_bolt_port` — optional, loopback Bolt port for graph backends that
  speak Neo4j-compatible Bolt
- `graph_http_port` — optional, loopback HTTP health/admin port for graph
  backends that expose one
- `graph_data_dir` — optional, absolute path to the graph backend data
  directory (typically `${workspace_root}/graph/<backend>/`)
- `graph_socket_path` — optional, local socket path for graph backends that
  use Unix sockets; NornicDB does not populate this today
- `graph_version` — optional, installed graph backend binary version
- `graph_username` — optional, local graph admin username used by attach
  processes
- `graph_password` — optional, per-workspace local graph password copied
  from the persistent graph credential file while the owner is live; this
  is sensitive and relies on `owner.json` being written with `0600`
  permissions

## Ownership Rules

1. One local host process owns a workspace root at a time.
2. A second invocation must:
   - attach to the healthy owner if attach semantics are supported, or
   - fail fast with an actionable error.
3. If the recorded owner is stale, the next invocation may reclaim ownership
   after health checks fail.

## Stale-Lock Detection

Ownership must be guarded by `flock(LOCK_EX | LOCK_NB)` on `owner.lock`. The
runtime must not rely on a check-then-claim sequence without an atomic lock.

The reclaim flow should check, in order:

1. whether the recorded PID is still alive
2. whether the owner socket responds
3. whether the owner version matches the current binary
4. whether the recorded Postgres PID is still alive and serving its socket
5. if `graph_pid` is recorded, whether the graph backend process is still
   alive and serving its recorded local endpoint (`graph_socket_path` when a
   socket backend is in use, otherwise `graph_address` + `graph_bolt_port` /
   `graph_http_port`)

If the owner PID is dead but the recorded Postgres PID is still alive, the new
process must not silently adopt that Postgres instance. It should:

1. attempt `pg_ctl stop -m fast` against the recorded workspace data directory
2. re-check the Postgres PID and socket
3. only reclaim ownership if Postgres is fully stopped

If Postgres refuses to stop or still owns the workspace socket after the stop
attempt, startup must fail with an actionable operator error.

If all checks fail and Postgres is confirmed stopped, the new process may take
ownership and rewrite `owner.json`.

## Local Endpoints

The local host should use a workspace-local Unix socket contract for ownership
and health checks. Embedded Postgres also keeps a workspace-local Unix socket
for reclaim and operator diagnostics.

The current runtime additionally reserves one loopback-only TCP port per
workspace process because the embedded Postgres library performs readiness
checks over `localhost:<port>` and the local attach flows reuse that same
loopback endpoint. This port is not a public network surface and must bind
only to loopback addresses.

The current `local_authoritative` NornicDB path uses loopback-only TCP ports
for both Bolt and HTTP health instead of a Unix socket. Those ports must also
bind only to loopback addresses and must be recorded in `owner.json`.

To avoid `sun_path` length limits:

- socket paths should live under a short runtime directory such as
  `${TMPDIR}/eshu/<workspace_id>/`
- `owner.json` should record the resolved socket path
- `owner.json` should record the resolved loopback Postgres port
- `owner.json` should record the resolved graph loopback address and ports
  for backends that do not support Unix sockets
- long home-directory paths must not be used directly as socket paths
- if `${TMPDIR}` itself is too long on a given host, the runtime should fall
  back to a shorter operator-visible runtime directory

## Versioning And Migration

`VERSION` is the data-root schema version.

Rules:

- same version: open normally
- known forward-compatible upgrade path: run migration
- unknown or incompatible version: refuse to start and require explicit reset or
  migration command

The runtime must not silently open a data root from an unknown schema version.

Migration should use write-new-then-swap or backup-before-migrate semantics so
failure does not strand the workspace in a partially upgraded state.

## Crash Recovery Expectations

On crash or forced shutdown:

- Postgres WAL recovery must be allowed to run normally
- stale owner records must be recoverable
- partial derived tables must be repairable through idempotent reindex or
  rebuild paths

## Log And Cache Policy

- `logs/` should use size-bounded rotation
- `cache/` should be rebuildable and may be pruned automatically
- cache contents should be tracked by a simple manifest or version marker so
  corruption is detectable

## Permissions And Security Stance

- the data root is per-user and not intended for shared multi-UID access
- a different user should fail fast with an actionable ownership or permissions
  error
- no encryption-at-rest is provided by default; operators are responsible for
  host-level disk encryption if required

## Filesystem Limitation

Chunk 3 targets local filesystems with reliable advisory locking. The local host
should refuse to start on unsupported or non-local filesystems where `flock`
semantics are not dependable enough for workspace ownership, such as common
NFS/SMB mounts, unless and until the runtime gains a verified compatibility
story for that filesystem class.

## Non-Goals

- shared cross-workspace data roots
- concurrent multi-writer ownership of one workspace root
- hidden destructive reset behavior
