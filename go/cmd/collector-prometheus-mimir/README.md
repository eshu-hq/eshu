# collector-prometheus-mimir

`collector-prometheus-mimir` is the hosted live Prometheus-compatible metric
metadata collector. It selects an enabled, claim-capable `prometheus_mimir`
collector instance from `ESHU_COLLECTOR_INSTANCES_JSON`, claims metric target
work, reads bounded scrape-target and rule metadata, and commits
`observability.*` source facts.

```mermaid
flowchart LR
  config["ESHU_COLLECTOR_INSTANCES_JSON"] --> load["loadClaimedRuntimeConfig"]
  load --> source["prometheusmimir.ClaimedSource"]
  source --> service["collector.ClaimedService"]
  service --> postgres["Postgres ingestion store"]
```

`token_env` is optional because some Prometheus endpoints are unauthenticated.
When configured, it must resolve to a non-empty secret. `tenant_id_env` is also
optional and overrides `tenant_id` when set. Source-controlled IaC/GitOps
evidence remains preferred when current; live metric facts are fallback and
validation evidence.

## Environment

| Variable | Purpose |
| --- | --- |
| `ESHU_COLLECTOR_INSTANCES_JSON` | Desired collector instances with one enabled `prometheus_mimir` instance. |
| `ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID` | Required when more than one enabled Prometheus/Mimir instance exists. |
| `ESHU_PROMETHEUS_MIMIR_COLLECTOR_POLL_INTERVAL` | Delay between empty claim polls. Defaults to `1s`. |
| `ESHU_PROMETHEUS_MIMIR_COLLECTOR_CLAIM_LEASE_TTL` | Lease TTL for workflow claims. |
| `ESHU_PROMETHEUS_MIMIR_COLLECTOR_HEARTBEAT_INTERVAL` | Heartbeat interval; must be less than the lease TTL. |
| `ESHU_PROMETHEUS_MIMIR_COLLECTOR_OWNER_ID` | Optional claim owner label. |

Target shape:

```json
{
  "provider": "mimir",
  "scope_id": "mimir:tenant:prod",
  "instance_id": "prod",
  "base_url": "https://mimir.example.test",
  "path_prefix": "/prometheus",
  "token_env": "MIMIR_TOKEN",
  "tenant_id_env": "MIMIR_TENANT",
  "resource_limit": 100,
  "stale_after": "24h",
  "declared_ids": ["rule/api-latency"],
  "observed_only_hint": true,
  "enabled": true
}
```

## Telemetry

The binary exposes `/healthz`, `/readyz`, `/metrics`, and `/admin/status`
through the shared hosted runtime. Provider request counters, emitted fact
counters, rate-limit counters, retries, redactions, stale counters, and fetch
duration use the shared collector instruments.

## Related Docs

- `go/internal/collector/prometheusmimir/README.md`
- `docs/public/reference/environment-collectors.md`
- `docs/public/deployment/service-runtimes-collectors.md`
