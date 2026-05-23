# Local Eshu Service Data Root

This page defines the on-disk contract for one local Eshu service workspace:
where files live, how a workspace is identified, which stores are rebuildable,
and what `owner.json` means. Maintainer implementation details live in
`go/internal/eshulocal/README.md`.

## Root location

Default root:

```text
${ESHU_HOME}/local/workspaces/<workspace_id>/
```

Default `ESHU_HOME`:

| Platform | Default |
| --- | --- |
| macOS | `~/Library/Application Support/eshu` |
| Linux | `${XDG_DATA_HOME:-~/.local/share}/eshu` |
| Windows | `%LOCALAPPDATA%\\eshu`, falling back to `%USERPROFILE%\\AppData\\Local\\eshu` |

The local runtime targets macOS and Linux. Windows layout helpers compile, but
workspace ownership and embedded Postgres startup return explicit unsupported
errors.

## Workspace identity

Workspace root resolution happens in this order:

1. explicit `--workspace-root`
2. nearest ancestor containing `.eshu.yaml`
3. nearest ancestor containing `.git`
4. invocation working directory

`workspace_id` is the first 20 bytes of the SHA-256 hash of the symlink-resolved
workspace root, encoded as lowercase hex. Paths are normalized to `/`, and
case-insensitive filesystems lowercase the resolved path before hashing.

## Layout Contract

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
    graph-nornicdb.log
    postgres.log
  cache/
    embedded-postgres/
    repos/
```

`postgres/` belongs to the embedded Postgres runtime. `graph/nornicdb/` exists
only for `local_authoritative`. `cache/repos/` contains rebuildable filesystem
repository selections for the local child ingester.

## Rebuildable stores

Before a `local_authoritative` owner starts, Eshu removes rebuildable state:

- `postgres/data`
- `postgres/runtime`
- `graph/nornicdb`
- `cache/repos`

Those stores are rebuilt from the workspace source tree. Do not treat local
authoritative Postgres data, graph data, or repository cache entries as durable
user data. Graph credentials are recreated at
`graph/nornicdb/eshu-credentials.json` after the graph store is reset.

## Owner record

`owner.json` is written atomically with `0600` permissions while a local owner is
live and is removed during normal shutdown.

Field groups:

| Group | Fields |
| --- | --- |
| Owner identity | `pid`, `started_at`, `hostname`, `workspace_id`, `version`, `socket_path` |
| Postgres runtime | `postgres_pid`, `postgres_port`, `postgres_data_dir`, `postgres_socket_dir`, `postgres_socket_path` |
| Query profile | `profile` |
| Graph runtime | `graph_backend`, `graph_address`, `graph_pid`, `graph_bolt_port`, `graph_http_port`, `graph_data_dir`, `graph_socket_path`, `graph_version` |
| Local graph credentials | `graph_username`, `graph_password` |

`graph_password` is sensitive. It is copied into `owner.json` so attach
processes can connect to the workspace-local graph, and it relies on owner-only
file permissions.

## Startup Admission

One local Eshu service owns a workspace root at a time. Startup follows this
order:

1. acquire `owner.lock`
2. validate `VERSION`
3. validate or reclaim `owner.json`
4. reset rebuildable stores for `local_authoritative`
5. start embedded Postgres
6. start NornicDB for `local_authoritative`
7. write `owner.json`

On Unix, `owner.lock` uses non-blocking `flock(LOCK_EX | LOCK_NB)`. If another
process holds the lock, startup fails instead of waiting. A second command may
attach to a healthy owner when that command supports attach semantics, but it
must not start a competing owner for the same workspace.

Stale owner recovery is lock-first. A new owner can reclaim `owner.json` only
after holding `owner.lock` and proving the recorded owner is no longer healthy.
Ownerless live Postgres is stopped only after the lock, PID, socket, and
Postgres protocol checks agree.

## Local Endpoints

Embedded Postgres uses a short Unix socket directory plus a loopback-only TCP
port. The TCP port is for readiness checks and local attach flows; it is not a
public network surface.

The current `local_authoritative` NornicDB path uses loopback-only TCP ports for
HTTP health and Bolt. It does not use a graph Unix socket.

## Version Gate

`VERSION` is the data-root schema marker. The current implementation:

- creates `VERSION` for a new empty data root
- opens normally when `VERSION` matches the current binary version
- refuses a non-empty root that lacks `VERSION`
- refuses a mismatched version with an incompatible-version error

There is no silent forward or backward compatibility path.

## Security And Filesystem Limits

- Data roots are per-user and are not intended for shared multi-UID access.
- Owner metadata, credentials, lock files, logs, and runtime directories are
  written with owner-only permissions where the runtime creates them.
- Eshu does not provide encryption at rest; use host-level disk encryption when
  required.
- Use local filesystems with reliable advisory locking. Do not rely on this
  ownership contract over NFS or SMB unless Eshu adds verified compatibility for
  that filesystem class.

## Verification

Relevant focused tests:

```bash
cd go
go test ./internal/eshulocal -run 'TestResolveWorkspaceRoot|TestBuildLayoutUsesStableWorkspaceIDForSymlinks|TestOwnerRecordRoundTrip|TestPrepareWorkspace|TestValidateOrReclaimOwner|TestEnsureLayoutVersion' -count=1
go test ./cmd/eshu -run 'TestResetLocalAuthoritativeStatePreservesPostgresBinariesAndLogs|TestRunGraphStartExecsAuthoritativeLocalHost|TestGraphStop|TestRuntimeConfigFromOwnerRecordDefaultsAuthoritativeBackendToNornicDB' -count=1
```

## Related docs

- [Local host lifecycle](local-host-lifecycle.md)
- [Graph backend operations](graph-backend-operations.md)
- [Runtime and storage environment](environment-runtime-storage.md)
- [Local binaries](../run-locally/local-binaries.md)
