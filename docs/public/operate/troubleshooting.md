<!-- docs-catalog
title: Troubleshooting
description: Symptom-first support guide that maps common Eshu problems to the owning runtime, exact signals, and recovery runbooks.
type: operate
audience: operator, practitioner
entrypoint: true
landing: false
-->

# Troubleshooting

Start from the symptom you see, then inspect the runtime that owns it.
Restarting the API will not drain a backed-up resolution engine queue, and
restarting the resolution engine will not fix repository discovery.

## Quick triage map

| Symptom | Owning runtime | First command or signal | Recovery guide |
| --- | --- | --- | --- |
| Answers look stale even though health is green | Ingester or Resolution Engine | `GET /api/v0/index-status` and `GET /admin/status` | [Health green, answers stale](#health-green-answers-stale) |
| Reducer queue depth or oldest age keeps growing | Resolution Engine | `GET /admin/status` queue depth & dead-letter counts | [Stuck Reducer Queue Runbook](stuck-reducer-queue-runbook.md) |
| Indexing is slow or skips repositories | Ingester | `eshu index <repo> --discovery-report` + ingester logs | [Slow or noisy repositories](#slow-or-noisy-repositories) |
| API or MCP returns `401`, `403`, or fails startup | API / MCP Server | Auth env vars (`ESHU_API_KEY`, OIDC/scoped tokens) | [API or MCP auth errors](#api-or-mcp-auth-errors) |
| Pod stays NotReady or `/readyz` fails | Schema Bootstrap / Target Pod | `GET /readyz` (`status_schema` check) | [Health or readiness check fails](#health-or-readiness-check-fails) |
| Graph-backed query returns `503`, `504`, or `500` | Graph backend (Neo4j / NornicDB) | `ESHU_GRAPH_BACKEND` + `query.graph_read.error` logs | [Graph query timeouts or failures](#graph-query-timeouts-or-failures) |
| Local Compose cannot see repositories | Local Compose mount | `ESHU_FILESYSTEM_HOST_ROOT` absolute path check | [Compose mount problems](#compose-mount-problems) |

## Health green, answers stale

Process health (`/healthz`) proves the service can answer HTTP requests. It
does not prove that indexing finished or that cross-source correlations caught
up. Check completeness directly:

```bash
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" \
  http://localhost:8080/api/v0/index-status
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" \
  http://localhost:8080/api/v0/status/index
curl -fsS http://localhost:8080/admin/status
```

- If completeness is behind and queue depth is **falling**, Eshu is actively
  catching up — wait for the drain.
- If queue depth or oldest queued age keeps **rising**, follow the
  [Stuck Reducer Queue Runbook](stuck-reducer-queue-runbook.md).
- If a cross-source answer (such as supply-chain impact or cloud drift) is
  behind while own-scope indexing succeeded, check
  [Cross-Source Correlations Flow](../concepts/flows/correlations.md) and
  [Freshness and Convergence](freshness-convergence.md).

## Slow or noisy repositories

Capture a discovery advisory before changing global timeouts, worker counts, or
batch sizes:

```bash
eshu index /path/to/repo --discovery-report /tmp/eshu-discovery-advisory.json
```

Use the advisory to exclude generated, vendored, archive, or copied third-party
trees via `.eshu/discovery.json` or `.eshuignore`. For persistent ingestion or
database bottlenecks, follow the [Tuning Playbook](tuning-playbook.md).

## API or MCP auth errors

- **HTTP `401` on MCP `initialize`, `tools/list`, or `GET /sse`:** Hosted MCP
  HTTP requires valid credentials on every transport route, not only
  `tools/call`. Verify `ESHU_MCP_TOKEN` or regenerate the client config with
  `eshu mcp setup --hosted --platform <client>`. See
  [MCP Client Authentication](mcp-client-auth.md).
- **MCP server refuses to start over HTTP:** Configure `ESHU_API_KEY`, a
  scoped-token file, or an IdP bearer resolver (local `stdio` does not require
  HTTP auth). See [Service Runtimes](../deployment/service-runtimes.md).
- **Scoped caller sees fewer rows than expected:** Scoped tokens only return
  entities linked to granted repositories or public visibility. Verify token
  grants via [User Management Runbook](user-management-runbook.md).

## Health or readiness check fails

Every long-running Eshu pod probes `/readyz`, which verifies that the Postgres
`eshu_schema_migrations` ledger matches the binary's embedded migrations:

- If pods stay **NotReady** during a rollout, check whether the Schema
  Bootstrap `Job` finished cleanly.
- If the Schema Bootstrap `Job` refuses to run due to an open long-running
  transaction (`xmin` hold), follow the
  [Park-Holder Runbook](park-holder-runbook.md).

## Graph query timeouts or failures

- **`503 backend_unavailable` or `504 backend_timeout`:** Transient backend or
  Postgres read-replica lag. Retry after the `Retry-After` hint if present;
  verify Neo4j/graph connectivity and replica lag. See
  [Postgres Read Routing](../deployment/postgres-read-routing.md) and
  [Graph-Read Safety](../reference/telemetry/graph-read-safety.md).
- **`500 ... graph query failed`:** Match the failure timestamp to the
  `query.graph_read.error` log record (which carries the query name and
  statement fingerprint without leaking query literals into HTTP responses).
  Check [Graph Backend Operations](../reference/graph-backend-operations.md).

## Compose mount problems

`ESHU_FILESYSTEM_HOST_ROOT` must be an absolute path to a real directory:

- Do not use a symlinked path or `~` expansion in Compose values.
- On macOS, avoid `/tmp` (Docker Desktop resolves it through `/private/tmp`).
- Each repository directory needs a `.git` directory for filesystem discovery.

## Deeper runbooks

- [3 AM Operator Runbook](3am-operator-runbook.md)
- [Troubleshooting Reference](../reference/troubleshooting.md)
- [Tuning Playbook](tuning-playbook.md)
- [Graph Backend Operations](../reference/graph-backend-operations.md)
- [Local Testing](../reference/local-testing.md)
