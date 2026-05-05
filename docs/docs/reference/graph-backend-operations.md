# Graph Backend Operations

This page covers day-two operations for the local graph backend on the
`local_authoritative` profile. Embedded NornicDB is the default local mode;
process mode is an explicit maintainer/test override. For install, see
[Graph Backend Installation](graph-backend-installation.md). For the
lifecycle contract that governs startup / shutdown ordering, see
[Local Eshu Service Lifecycle](local-host-lifecycle.md). For the consolidated
NornicDB environment-variable map, see
[NornicDB Tuning](nornicdb-tuning.md).

## Current command group

```text
eshu graph status
eshu graph logs
eshu graph stop
eshu graph start
eshu graph upgrade
```

`eshu graph status`, `eshu graph logs`, `eshu graph stop`, and
`eshu graph start` are wired today. `eshu graph upgrade --from <source>` is also
wired for explicit-source replacement of the managed process-mode binary from
a local binary path, local tar archive, macOS package, or URL. Bare
no-argument NornicDB install is intentionally unavailable while Eshu tracks
latest NornicDB `main` through explicit `--from` binaries. Signature
verification remains planned.

`eshu graph stop` is service-aware. If a healthy local Eshu service manages the
workspace, the command signals that service process so shutdown follows the
documented order: child runtimes stop first, then NornicDB, then embedded
Postgres. It only stops the recorded graph process directly when the service is dead
and an explicit process-mode graph backend is stale.

`eshu graph start` is intentionally foreground. It execs the same local service
supervisor used by `ESHU_QUERY_PROFILE=local_authoritative eshu watch .` and
does not create a detached daemon. Use Ctrl-C to stop it from the same terminal
or `eshu graph stop` from another terminal.

`eshu graph upgrade` refuses to replace the managed process-mode binary while
the local Eshu service or process-mode graph backend is still healthy. Stop the
workspace service first:

```bash
eshu graph stop
eshu graph upgrade --from /absolute/path/to/nornicdb-headless
eshu graph upgrade --from https://example.com/releases/nornicdb-headless-darwin-arm64.tar.gz --sha256 <expected-sha256>
```

The local-authoritative runtime starts embedded NornicDB automatically when you
run a local Eshu service entrypoint such as:

```bash
ESHU_QUERY_PROFILE=local_authoritative eshu watch .
```

That path requires a `eshu` binary built with `-tags nolocalllm`. A discoverable
NornicDB binary is required only for explicit process mode. See
[Graph Backend Installation](graph-backend-installation.md).

With the NornicDB default backend, Eshu keeps local content search isolated from
graph projection stalls. The local-authoritative ingester writes the
embedded-Postgres content index before attempting canonical graph writes, and
NornicDB canonical writes now run in bounded phase-group transactions instead
of one global grouped write. The timeout defaults to `30s` and can be tuned
for diagnostics with `ESHU_CANONICAL_WRITE_TIMEOUT=2s`. The default
phase-group window is `500` statements and can be tuned with
`ESHU_NORNICDB_PHASE_GROUP_STATEMENTS=<positive integer>` during repo-scale
dogfood runs. Neo4j production writes keep the grouped canonical path and are
not affected by this local-authoritative guardrail.
Timed-out graph writes are persisted as `graph_write_timeout` failures with
the sanitized statement summary in `failure_details`, which keeps the failure
diagnosable without automatically retrying a potentially partial phase group.
See [NornicDB Tuning](nornicdb-tuning.md) for the full row-batch versus
grouped-statement matrix before adding another phase-specific override.

The current local-authoritative canonical entity path uses the narrowest shape
that the active backend has proven correct. Backends with correct node-only
batched `MERGE` support can separate entity node upserts from
`phase=entity_containment`. Older NornicDB builds may still require the
file-scoped combined entity write where each statement matches the `File`
anchor with `$file_path`, unwinds entity rows for that file, upserts nodes,
and attaches `CONTAINS` in the same statement. Builds from latest NornicDB
`main` that include the row-safe generalized `UNWIND/MERGE` hot path can opt
into `ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT=true`, which batches entity rows
across files with `MERGE (n {uid: row.entity_id}) ... MATCH (f {path:
row.file_path}) ... MERGE (f)-[:CONTAINS]->(n)`. Use that switch only with the
exact NornicDB binary under evaluation. The `nornicdb entity label summary`
log includes `phase` so operators can tell which entity-write lane is active
and where repo-scale time is going.

When patched-binary evaluation still shows steady per-label growth after
schema-backed `MERGE` lookup is active, do not keep shrinking Eshu batches from
chunk logs alone. Rebuild NornicDB with explicit profiling enabled and rerun
the same repo with `NORNICDB_ENABLE_PPROF=true`, then collect CPU and heap
profiles during the hot label. That is the required next step for separating
Badger write cost, uniqueness-index maintenance, Cypher execution, and
Bolt/transaction overhead.

NornicDB exposes explicit Bolt transaction hooks, but Eshu does not enable
grouped canonical writes for normal laptop runs until the Eshu Neo4j-driver
conformance matrix proves rollback, timeout, and no-partial-write behavior on
the Eshu canonical workload. For adapter conformance only, set:

```bash
ESHU_NORNICDB_CANONICAL_GROUPED_WRITES=true
```

That switch exposes the same grouped-write surface used by Neo4j while still
bounding the call with `ESHU_CANONICAL_WRITE_TIMEOUT`. Leave it unset for
day-to-day `local_authoritative` coding. If you must use it for manual
conformance, use a disposable `ESHU_HOME` / workspace data root.

The current safety probe is deliberately conservative:

```bash
ESHU_NORNICDB_BINARY=/tmp/nornicdb-headless \
  go test ./cmd/eshu -run TestNornicDBGroupedWriteSafetyProbe -count=1 -v
```

The 2026-04-23 run against a rebuilt headless NornicDB binary proved that
grouped writes can commit a Eshu repository/file/function shape, grouped
rollback, clean explicit rollback, and failed-statement explicit rollback all
report marker count `0` on the Neo4j-driver path, and the timeout probe leaves
no partial write. Re-run that proof against the latest NornicDB `main` binary
you are evaluating. The stricter rollback promotion gate is:

```bash
ESHU_NORNICDB_BINARY=/tmp/nornicdb-headless-eshu-rollback \
ESHU_NORNICDB_REQUIRE_GROUPED_ROLLBACK=true \
  go test ./cmd/eshu -run TestNornicDBGroupedWriteRollbackConformance -count=1 -v
```

Do not use NornicDB grouped canonical writes for normal laptop runs. Use the
default phase-group path with the latest accepted NornicDB `main` build until
release-backed binary policy is settled. Neo4j production grouped writes are
unaffected.

### `eshu graph status`

Reports, for the current workspace:

- whether a local NornicDB binary was discovered
- binary path
- discovered version
- whether a local Eshu service is present
- backend PID
- loopback bind address
- Bolt port
- HTTP health port
- data directory path
- graph log path
- whether the backend currently looks healthy

For the current NornicDB-backed `local_authoritative` path, health means:

- the recorded graph PID is alive
- `GET /health` on the recorded loopback HTTP port succeeds
- the recorded loopback Bolt port accepts TCP connections

NornicDB writes logs under `${ESHU_HOME}/local/workspaces/<workspace_id>/logs/graph-nornicdb.log`.
Use `eshu graph logs [--workspace-root <path>]` to print that file without
manually deriving the workspace ID.

Eshu generates a random graph admin password per workspace data root and
persists it under `graph/nornicdb/eshu-credentials.json` with `0600`
permissions. The live service also copies it to `owner.json` so attach
processes can connect; `eshu graph status` does not print the secret.

## Health probe

`eshu doctor` probes the graph backend as part of the local Eshu service check
suite when the active profile is `local_authoritative`. A failing probe
prints the backend-specific failure (bolt timeout, health failure, version
mismatch, data directory not writable) and returns a non-zero exit code.

## Troubleshooting

### Process-mode backend installed but not starting

Check, in order:

1. `eshu graph status` — did Eshu discover the expected NornicDB binary from
   `ESHU_NORNICDB_BINARY`, `${ESHU_HOME}/bin/nornicdb-headless`, or `PATH`?
   If discovery reports not installed, verify the candidate binary prints a
   `NornicDB ...` version string or install it with
   `eshu install nornicdb --from <source>`.
2. open `${ESHU_HOME}/local/workspaces/<workspace_id>/logs/graph-nornicdb.log`
   — did the backend emit an error?
3. `ls -la ${workspace_root}/graph/` — is the data directory writable by
   the current user?
4. Loopback ports — verify that the recorded Bolt and HTTP ports are still
   free before startup and still bound to the graph PID after startup.

### Backend running but queries return `backend_unavailable`

- The Eshu process may be out of sync with `owner.json`. Run
  `eshu graph status`; if the Eshu host thinks the backend is absent,
  restart the lightweight host: `eshu watch` will re-read owner state.
- Graph backend may be in recovery. On restart after an unclean
  shutdown, NornicDB runs Badger + MVCC recovery. Wait; tail
  `logs/graph-nornicdb.log`.

### Content search works but graph-backed answers are degraded

This is expected during NornicDB evaluation if a canonical graph write times
out. MCP/CLI code-search tools that can answer from the content index should
still return results with a truth envelope such as
`basis=content_index` and `profile=local_authoritative`. Graph-backed
capabilities remain degraded until the graph projection succeeds or the
workspace is re-indexed after the backend issue is fixed.

### Backend stuck after crash

- Check `owner.json` for `graph_pid`. If that PID is dead but data
  directory locks remain, the graph backend may require manual cleanup.
  Remove stale lock files in the data directory only after confirming no
  live process holds them.
- The local Eshu service reclaim flow includes a best-effort internal stop
  before reclaim; see
  [Local Eshu Service Lifecycle](local-host-lifecycle.md).

## Telemetry

Every query response from the graph backend is labeled with
`graph_backend` (`neo4j` or `nornicdb`) on:

- telemetry spans (`graph_backend` attribute)
- query latency histograms (`graph_backend` label)
- error counters (`graph_backend` label)
- optional `truth.backend` field in responses

## Migration between backends

Switching the active graph backend for a workspace requires:

1. Stop the lightweight host and any running `eshu watch`.
2. Flip `ESHU_GRAPH_BACKEND` in the environment.
3. Either:
   - Re-index the workspace with `eshu index <path> --force` so the new
     backend receives fresh canonical writes, or
   - Run migration tooling if available (see the ADR §Migration Path for
     current status).
4. Restart `eshu watch`.

`owner.json` should record the active `graph_backend` so downstream
diagnostics can see which backend owned the last successful run.

## Schema dialect routing

`ESHU_GRAPH_BACKEND` also controls graph schema bootstrap. Eshu does not fork
the reducer, query handlers, or MCP tools per backend; it routes only the DDL
surface through a backend schema dialect:

- `neo4j` receives the shared production schema unchanged.
- `nornicdb` receives the NornicDB-compatible schema rendering. Current
  NornicDB rejects Eshu's composite `IS UNIQUE` constraints, and Eshu does not
  translate those constraints to `IS NODE KEY` because node keys require every
  participating property while some semantic labels are intentionally sparse.
  The NornicDB dialect therefore skips unsupported composite uniqueness DDL and
  relies on the separate `uid` uniqueness constraints for canonical merge
  identity.
- NornicDB intentionally skips Neo4j's multi-label
  `CREATE FULLTEXT INDEX` fallback because NornicDB only verified the
  procedure-based multi-label fulltext path.
- NornicDB also receives explicit property indexes for hot `MERGE` lookup
  identities such as `Repository.id`, `Directory.path`, `File.path`,
  `Workload.id`, `WorkloadInstance.id`, `Platform.id`, `Endpoint.id`,
  `EvidenceArtifact.id`, `Environment.name`, and canonical `uid` labels
  because its `MERGE` lookup path uses property indexes before falling back to
  label scans. Neo4j does not receive these duplicate indexes because
  uniqueness constraints already create backing indexes there.

The opt-in verification gate is:

```bash
ESHU_NORNICDB_BINARY=/absolute/path/to/nornicdb-headless \
  go test ./cmd/eshu -run TestNornicDBSchemaAdapterVerification -count=1 -v
```

That test executes the rendered NornicDB schema against a real process-mode
NornicDB runtime. It is not part of the default unit-test suite because it
requires an installed graph binary.

## Non-goals

- Running multiple graph backends simultaneously on one workspace. The
  workspace lock admits exactly one owner and exactly one graph backend
  at a time.
- Running the graph backend without a Eshu owner. Embedded mode is owned by the
  `eshu` process, and process mode is still owned by the lightweight host, not
  by the user shell.
