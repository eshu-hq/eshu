<!-- docs-catalog
title: Health Checks
description: Separates process health, runtime status, and indexed-data completeness checks for Eshu operators.
type: operate
audience: operator
entrypoint: true
landing: false
-->

# Health Checks

Health checks answer three different questions. Keep them separate.

| Question | Signal |
| --- | --- |
| Is the process alive and initialized? | `/healthz`, `/readyz`, or MCP `GET /health` |
| Is the runtime stuck, failing, or behind? | `/admin/status` on the relevant runtime |
| Is indexed data complete enough for the question? | `GET /api/v0/index-status`, `GET /api/v0/status/index`, or repository coverage |

A green process health check does not mean indexing finished.
For code graph and dead-code queries, also wait for `/admin/status` to report
no shared projection domain backlog; pending shared projection intents mean the
reducer has not finished making all accepted edges graph-visible yet.

## Runtime Endpoints

Long-running Go runtimes that mount the shared admin surface expose:

- `GET /healthz`
- `GET /readyz`
- `GET /admin/status`
- `GET /metrics` when a metrics handler is mounted

The MCP server also exposes `GET /health`, `GET /sse`, and
`POST /mcp/message`.

Use [Runtime Admin API](../reference/runtime-admin-api.md) for the exact
contract.

## Local Compose Checks

```bash
curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/readyz
curl -fsS http://localhost:8080/admin/status
curl -fsS http://localhost:8081/health
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" \
  http://localhost:8080/api/v0/index-status
```

Metrics endpoints are exposed directly by service:

- API: `http://localhost:19464/metrics`
- Ingester: `http://localhost:19465/metrics`
- Resolution Engine: `http://localhost:19466/metrics`
- MCP: `http://localhost:19468/metrics`

## Kubernetes Checks

```bash
kubectl get pods -n eshu
kubectl get services -n eshu
kubectl logs -n eshu deployment/eshu-api --tail=50
kubectl logs -n eshu statefulset/eshu --tail=50
kubectl logs -n eshu deployment/eshu-resolution-engine --tail=50
```

If API and MCP are healthy but answers are stale, check ingestion, queue drain,
Postgres, and graph projection next.

For slow indexing, queue backlog, graph write timeouts, or high memory, use the
[Tuning Playbook](tuning-playbook.md).

## Graph Backend Data Loss

With the default NornicDB backend, graph storage is rebuildable projection
state. A graph volume that cannot reopen should be handled like a lost search
index or materialized view, not like loss of the authoritative sources.

Operator sequence:

1. Confirm Postgres is healthy and the API/MCP health checks are failing
   because the graph backend cannot start.
2. Preserve the graph volume or pod logs if the failure needs forensic review.
3. Recreate only the NornicDB graph data directory or PVC.
4. Run the data-plane schema bootstrap before projection work resumes.
5. Re-run bootstrap indexing, replay projection work, or recollect from source
   systems depending on which Postgres facts and workflow rows are available.
6. Verify `GET /api/v0/index-status` reports `status=healthy`,
   `queue.pending=0`, `queue.retrying=0`, `queue.failed=0`, and
   `queue.dead_letter=0`.
7. Capture the public-safe restore and graph-rebuild summary with
   `scripts/verify-hosted-backup-restore-proof.sh` when the recovery is part of
   hosted rollout or incident proof.

Do not delete Postgres during graph recovery unless the intended plan is a full
source-system recollection. Postgres holds facts, content, queues, workflow
state, and the durable inputs that make graph rebuild possible.
