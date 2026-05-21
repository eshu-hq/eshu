# Graph Backend Operations

Use this page when you need to start, stop, inspect, or troubleshoot the local
graph backend used by the `local_authoritative` profile.

For install policy, use
[Graph Backend Installation](graph-backend-installation.md). For tuning knobs,
use [NornicDB Tuning](nornicdb-tuning.md). For the local owner lifecycle, use
[Local Eshu Service Lifecycle](local-host-lifecycle.md).

## Default Local Mode

The normal local-authoritative path is embedded NornicDB inside the local
`eshu` process. That path requires an `eshu` binary built with
`-tags nolocalllm`, which is what the local installer builds for the owner
binary.

You do not need `nornicdb-headless` for normal local mode.

Process mode exists for maintainers who need to test a specific NornicDB
binary. Enable it explicitly:

```bash
ESHU_NORNICDB_RUNTIME=process \
ESHU_NORNICDB_BINARY=/absolute/path/to/nornicdb-headless \
eshu graph start --workspace-root /path/to/repo
```

`ESHU_NORNICDB_BINARY` only chooses the process-mode binary. It does not switch
the runtime mode by itself.

## Command Map

| Command | Key flags | Purpose |
| --- | --- | --- |
| `eshu graph status` | `--workspace-root` | Print workspace owner metadata, graph backend, PID, ports, data directory, log path, version, and health. |
| `eshu graph start` | `--workspace-root`, `--progress`, `--logs`, `--verbose` | Start the local-authoritative Eshu service in the foreground. This also starts embedded Postgres, embedded NornicDB, the ingester, and the reducer. |
| `eshu graph logs` | `--workspace-root` | Print the current workspace `graph-nornicdb.log` file. |
| `eshu graph stop` | `--workspace-root` | Stop the local Eshu service for the workspace. If the owner is stale, reclaim the owner record only after lock and child-process checks. |
| `eshu graph upgrade` | `--from`, `--sha256`, `--workspace-root` | Replace the managed process-mode NornicDB binary. The workspace graph must be stopped first. |
| `eshu install nornicdb` | `--from`, `--sha256`, `--force`, `--full` | Install a verified NornicDB binary for explicit process-mode testing. Normal embedded mode does not need this. |

## Start

Start the local authoritative service:

```bash
eshu graph start
```

Use `--workspace-root` when you want one explicit workspace to own multiple
repositories:

```bash
eshu graph start --workspace-root ~/src
```

Foreground behavior:

- `--progress auto` shows the terminal progress panel when stderr is a
  terminal.
- `--progress plain` prints append-only status snapshots.
- `--progress quiet` disables the progress reporter.
- `--logs file` writes child logs to the workspace `logs/` directory.
- `--logs terminal` or `--verbose` sends child logs to the terminal.
- `--logs quiet` discards child logs.

`eshu graph start` execs the hidden local-host supervisor. It does not create a
detached daemon. Stop it with Ctrl-C in the same terminal or with
`eshu graph stop` from another terminal.

## Status

Use `eshu graph status` first when the local service behaves unexpectedly.

The JSON output reports:

- workspace root and workspace ID
- whether an owner record exists
- owner PID and profile
- graph backend
- graph PID
- graph address, Bolt port, and HTTP health port
- graph data directory and log path
- discovered or running graph version
- whether the graph looks healthy

For NornicDB, healthy means:

- the recorded graph PID is alive
- the recorded HTTP health endpoint returns success
- the recorded Bolt port completes protocol negotiation

A plain TCP connect is not enough. The health probe sends a Bolt handshake so
Eshu does not treat a listening-but-not-ready graph as usable.

## Logs

NornicDB logs live under the workspace data root:

```text
${ESHU_HOME}/local/workspaces/<workspace_id>/logs/graph-nornicdb.log
```

Print the current graph log:

```bash
eshu graph logs
```

If the file does not exist, start the workspace in local-authoritative mode
first.

## Stop

Stop the workspace service:

```bash
eshu graph stop
```

The stop path is service-aware:

1. If the owner process is alive, Eshu sends it `SIGTERM`.
2. The owner stops child runtimes, then NornicDB, then embedded Postgres.
3. If the owner process is already gone, Eshu acquires `owner.lock` before
   reclaiming stale metadata or stopping stale child processes.

Do not remove `owner.json`, `owner.lock`, or Postgres lock files manually while
a process may still be alive.

## Upgrade Process-Mode Binary

`eshu graph upgrade` is only for the managed process-mode binary. It does not
change embedded NornicDB inside the `eshu` binary.

```bash
eshu graph stop
eshu graph upgrade --from /absolute/path/to/nornicdb-headless
```

Remote artifacts are accepted when you provide a checksum:

```bash
eshu graph upgrade \
  --from https://example.com/releases/nornicdb-headless-darwin-arm64.tar.gz \
  --sha256 <expected-sha256>
```

## Troubleshooting

### Status says no graph is running

Check that you started the local-authoritative owner:

```bash
eshu graph start
```

If you started with `eshu watch`, confirm the active profile is
`local_authoritative`.

### Process mode cannot find NornicDB

Process-mode binary discovery checks:

1. `ESHU_NORNICDB_BINARY`
2. the managed install at `${ESHU_HOME}/bin/nornicdb-headless`
3. `nornicdb-headless` on `PATH`
4. `nornicdb` on `PATH`

Every candidate must print a `NornicDB ...` version string from its `version`
command.

### Backend starts but queries report `backend_unavailable`

Run:

```bash
eshu graph status
eshu graph logs
```

If the owner record points at a dead service, restart the local owner. If the
graph is recovering after an unclean shutdown, wait for recovery to finish and
watch `graph-nornicdb.log`.

### Content search works but graph answers are degraded

This can happen when content indexing completed but graph projection failed or
timed out. Keep the graph log and status output; then use
[NornicDB Tuning](nornicdb-tuning.md) before changing timeouts or batch sizes.

## Backend Migration

Switching a workspace between `nornicdb` and `neo4j` is not an in-place graph
data migration. Use this sequence:

1. Stop the local owner.
2. Change `ESHU_GRAPH_BACKEND`.
3. Re-index the workspace with the target backend.
4. Restart the local owner.

Graph schema differences are handled by the schema bootstrap dialect. Eshu does
not fork reducers, query handlers, or MCP tools per backend.

## Telemetry

Graph-backed runtimes label backend activity with `graph_backend` on spans,
metrics, logs where applicable, and optional truth metadata. Use that label to
separate NornicDB and Neo4j behavior when comparing runs.
