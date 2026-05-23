# Troubleshooting Reference

Use this page as the compact symptom map. The task-first runbook lives in
[Operate: Troubleshooting](../operate/troubleshooting.md).

## First Split

| Symptom | First surface to check |
| --- | --- |
| `eshu` is not on `PATH` | [Local binaries](../run-locally/local-binaries.md) |
| API or MCP process is down | Container/pod status, service port, API key, Postgres DSN, graph backend config |
| API is healthy but answers are stale | `GET /api/v0/index-status`, `GET /api/v0/status/index`, runtime `/admin/status` |
| Graph-backed reads fail | `ESHU_GRAPH_BACKEND`, graph backend health, graph query logs |
| Compose cannot see repositories | `ESHU_FILESYSTEM_HOST_ROOT` and mount path rules |
| Indexing is slow or noisy | Discovery advisory report and `.eshuignore` / `.eshu/discovery.json` |

## Install And PATH

Local source checkouts should use the local binary installer:

```bash
./scripts/install-local-binaries.sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

`go install github.com/eshu-hq/eshu/go/cmd/eshu@latest` installs only the CLI.
It does not install helper binaries such as `eshu-ingester` and
`eshu-reducer`, which `eshu graph start` discovers through `PATH`.

## Freshness Versus Health

Process health only proves a runtime can serve. It does not prove the latest
repository generation drained through projection and reducer work.

Check freshness in this order:

```bash
curl -fsS http://localhost:8080/api/v0/index-status
curl -fsS http://localhost:8080/api/v0/status/index
curl -fsS http://localhost:8080/admin/status
```

If queue depth is falling, wait. If queue depth or oldest age keeps rising,
inspect the ingester and resolution-engine logs plus
[Service Workflows](service-workflows.md).

## Compose Repository Mounts

`ESHU_FILESYSTEM_HOST_ROOT` must be an absolute path to a real directory.

- Do not rely on `~` expansion in Compose values.
- Do not point at a symlinked path.
- On macOS, avoid `/tmp`; use a real directory such as
  `$HOME/tmp/eshu-compose-repos`.
- For filesystem discovery, each repository directory should contain `.git`.

## Backend And Runtime Links

- [Docker Compose](../run-locally/docker-compose.md)
- [Graph Backend Operations](graph-backend-operations.md)
- [NornicDB Tuning](nornicdb-tuning.md)
- [Local Testing](local-testing.md)
- [Telemetry Overview](telemetry/index.md)
