# Graph Backend Operations

Use this page to start, stop, inspect, and troubleshoot the local graph backend
used by the `local_authoritative` profile. Normal local mode embeds NornicDB in
the `eshu` owner process built by `./scripts/install-local-binaries.sh`; it does
not require `nornicdb-headless`.

For install policy, use
[Graph Backend Installation](graph-backend-installation.md). For NornicDB
runtime knobs, use [NornicDB Tuning](nornicdb-tuning.md). For owner metadata
and locks, use [Local Eshu Service Lifecycle](local-host-lifecycle.md).

## Command Map

| Command | Purpose |
| --- | --- |
| `eshu graph start` | Start the local-authoritative service in the foreground. It owns embedded Postgres, embedded or process-mode NornicDB, the ingester, and the reducer. |
| `eshu graph status` | Print owner metadata, backend, PIDs, ports, data/log paths, version, and health. |
| `eshu graph logs` | Print the current workspace `graph-nornicdb.log`. |
| `eshu graph stop` | Stop the workspace owner and child runtimes, or safely reclaim stale metadata after lock checks. |
| `eshu graph upgrade` | Replace the managed process-mode NornicDB binary after the workspace is stopped. |
| `eshu install nornicdb` | Install a verified process-mode binary; embedded mode does not need it. |

All graph commands accept `--workspace-root` where workspace identity matters.

## Start

```bash
eshu graph start
eshu graph start --workspace-root ~/src
```

Foreground options:

- `--progress auto`, `plain`, or `quiet`
- `--logs file`, `terminal`, or `quiet`
- `--verbose`, equivalent to terminal child logs

`eshu graph start` execs the local-host supervisor in the foreground. Stop it
with Ctrl-C in that terminal or `eshu graph stop` from another terminal.

Process mode is explicit:

```bash
ESHU_NORNICDB_RUNTIME=process \
ESHU_NORNICDB_BINARY=/absolute/path/to/nornicdb-headless \
eshu graph start --workspace-root /path/to/repo
```

`ESHU_NORNICDB_BINARY` chooses the process-mode binary only; it does not switch
runtime mode by itself.

## Status And Health

Start with:

```bash
eshu graph status
```

Status reports owner metadata, backend, PID, Bolt and health ports, data
directory, log path, version, and health for the selected workspace.

For NornicDB, healthy means:

- recorded graph PID is alive
- HTTP health endpoint succeeds
- Bolt port completes protocol negotiation

A plain TCP connect is not enough; Eshu sends a Bolt handshake before treating
the graph as usable.

## Logs

```bash
eshu graph logs
```

The log path is:

```text
${ESHU_HOME}/local/workspaces/<workspace_id>/logs/graph-nornicdb.log
```

If the file does not exist, start the workspace in local-authoritative mode
first.

## Stop

```bash
eshu graph stop
```

Stop behavior:

1. If the owner process is alive, Eshu sends `SIGTERM`.
2. The owner stops child runtimes, NornicDB, and embedded Postgres.
3. If the owner is gone, Eshu acquires `owner.lock` before reclaiming stale
   metadata or stopping stale child processes.

Do not delete `owner.json`, `owner.lock`, or Postgres lock files manually while
a process may still be alive.

## Upgrade Process Mode

`eshu graph upgrade` replaces the managed process-mode binary only. It does not
change embedded NornicDB linked into the `eshu` binary.

```bash
eshu graph stop
eshu graph upgrade --from /absolute/path/to/nornicdb-headless
```

Remote artifacts need a checksum:

```bash
eshu graph upgrade \
  --from https://example.com/releases/nornicdb-headless-darwin-arm64.tar.gz \
  --sha256 <expected-sha256>
```

## Troubleshooting

| Symptom | First checks |
| --- | --- |
| `status` says no graph is running | Start `eshu graph start`; if you used `eshu watch`, confirm the active profile is `local_authoritative`. |
| Process mode cannot find NornicDB | Check `ESHU_NORNICDB_BINARY`, `${ESHU_HOME}/bin/nornicdb-headless`, then `PATH`. Each candidate must print a `NornicDB ...` version. |
| Queries report `backend_unavailable` | Run `eshu graph status` and `eshu graph logs`; restart the local owner if metadata points at a dead process. |
| Content search works but graph answers are degraded | Treat it as graph projection failure or timeout. Keep status/log output and check NornicDB tuning before changing timeouts or batch sizes. |

## Backend Migration

Switching a workspace between `nornicdb` and `neo4j` is a re-index, not an
in-place data migration:

1. Stop the local owner.
2. Change `ESHU_GRAPH_BACKEND`.
3. Re-index the workspace with the target backend.
4. Restart the local owner.

Graph schema differences belong to schema bootstrap dialects. Eshu does not
fork reducers, query handlers, or MCP tools per backend.

## Sizing (Neo4j, QA-Measured)

Interim guidance from the QA deployment at its current size, pending the
timed full rebuild (#10883). Do not resize the deployment from this section
alone; the rebuild peaks still have to confirm the headroom.

Measured 2026-11-21 (Neo4j Community 2026.08.1, pod uptime 35 days):

| Signal | Value |
| --- | --- |
| Nodes / relationships | 1,295,209 / 3,062,391 |
| Store (`/data`) | 7.4 GiB (strings 2.9G, indexes 1.1G, propertystore 641M, relstore 128M) |
| Pod usage | 7m CPU / ~39 GiB RSS |
| Pod reserved | 32 CPU / 224 GiB (request = limit; heap 31g, page cache 160g) |
| Graph PVC | 500 GiB, ~1.5% used |
| Postgres behind it | 12,363 scopes, 92.9M facts |

The reservation is the former NornicDB slot on the dedicated node, not a number
any graph measurement produced. At 7.4 GiB of store the 160g page cache is
roughly thirty times the data plus indexes, and steady-state writes average
single-digit milliseconds.

The sizing rule until the timed run lands:

- Page cache at or above 1.5x store data plus indexes, re-checked after the
  rebuild measures the real peak.
- Against the #10862 target (ten times one organization, about 94,000 scopes
  and 840 million facts): ten times the current store is about 74 GiB, which
  the current 160g page cache covers with margin left. Capacity is not the
  reason to touch the reservation; efficiency is, and that call waits for the
  rebuild's peak RSS and write-path CPU.
- CPU stays at 32 until the rebuild shows what the write drain actually burns.
  Idle usage rounds to zero; sizing from idle would repeat the original guess.
- The 500 GiB PVC stays. Even at ten times the store it holds six times the
  data plus room for transaction logs.

The full reference points and the procedure that produces the rebuild peaks are
in [Timed graph rebuild on QA](../operate/graph-rebuild-qa-timed.md).

## Telemetry

Graph-backed runtimes label backend activity with `graph_backend` on spans,
metrics, logs where applicable, and optional truth metadata. Use that label plus
queue-zero and API/MCP truth checks when comparing NornicDB and Neo4j.
