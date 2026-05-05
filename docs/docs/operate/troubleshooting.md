# Troubleshooting

Start with the symptom, then follow the runtime that owns it. Restarting the
API will not drain reducer backlog, and restarting the reducer will not fix
repository discovery.

## First Checks

| Symptom | Check |
| --- | --- |
| API or MCP is unavailable | pod/container status, service ports, API key or client config, storage connectivity |
| Indexing is not moving | ingester logs, repository source access, workspace persistence, Postgres fact writes |
| Queries are stale | reducer logs, queue depth, graph write errors, failed or retrying work items |
| Graph-backed query fails | `ESHU_GRAPH_BACKEND`, graph backend health, graph query logs |
| Compose cannot see repos | `ESHU_FILESYSTEM_HOST_ROOT` path and mount rules |

## Health Green, Answers Stale

Process health proves the runtime can serve. It does not prove freshness.

```bash
curl -fsS http://localhost:8080/api/v0/index-status
curl -fsS http://localhost:8080/api/v0/status/index
curl -fsS http://localhost:8080/admin/status
```

If completeness is behind and queue depth is falling, wait. If queue depth or
oldest age keeps rising, inspect resolution-engine status and logs.

## Compose Mount Problems

`ESHU_FILESYSTEM_HOST_ROOT` must be an absolute path to a real directory.

- Do not use a symlinked path.
- Do not rely on `~` expansion in Compose values.
- On macOS, avoid `/tmp`; Docker Desktop resolves it through `/private/tmp`.
- Each repository directory should contain a `.git` directory when using
  filesystem discovery.

## Slow Or Noisy Repositories

Capture a discovery advisory before changing global timeouts, worker counts, or
batch sizes:

```bash
eshu index /path/to/repo --discovery-report /tmp/eshu-discovery-advisory.json
```

Use the report to decide whether `.eshu/discovery.json` or `.eshuignore` should
exclude generated, vendored, archive, or copied third-party trees.

## Deeper Runbooks

- [Local Testing](../reference/local-testing.md)
- [Troubleshooting Reference](../reference/troubleshooting.md)
- [Graph Backend Operations](../reference/graph-backend-operations.md)
- [NornicDB Tuning](../reference/nornicdb-tuning.md)
