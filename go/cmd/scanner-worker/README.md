# scanner-worker

## Purpose

`scanner-worker` runs claim-driven scanner work for security analyzers that are
too CPU-heavy or memory-heavy for reducer lanes. The installed binary is
`eshu-scanner-worker`.

## Flow

```mermaid
flowchart LR
  workflow["workflow work item"] --> claim["scanner-worker claim"]
  claim --> input["scannerworker.ClaimInput"]
  input --> analyzer["bounded analyzer"]
  analyzer --> facts["scanner_worker.* source facts"]
  facts --> postgres["Postgres fact store"]
  postgres --> reducers["reducers admit findings"]
```

## Runtime Contract

- Selects one enabled, claim-capable `scanner_worker` collector instance from
  `ESHU_COLLECTOR_INSTANCES_JSON`.
- Uses `ESHU_SCANNER_WORKER_ANALYZER` or the instance `configuration.analyzer`
  to choose a scanner-worker analyzer profile.
- Applies analyzer defaults, optional instance `resource_limits`, then
  `ESHU_SCANNER_WORKER_*` resource overrides.
- Emits source facts only and rejects silent clean output before committing.
- Records retry and dead-letter payloads with locator hashes and bounded
  failure classes.
- Exposes `/healthz`, `/readyz`, `/metrics`, `/admin/status`, and optional
  private pprof through `ESHU_PPROF_ADDR`.

The current built-in analyzer emits an explicit `scanner_worker.warning` source
fact (`reason=analyzer_not_configured`). Concrete SBOM, image, secret, license,
OS package, source, and misconfiguration analyzers must plug into this boundary
instead of running in reducer lanes.

## Environment

| Variable | Purpose |
| --- | --- |
| `ESHU_SCANNER_WORKER_INSTANCE_ID` | Select one configured scanner-worker instance when more than one exists. |
| `ESHU_SCANNER_WORKER_ANALYZER` | Analyzer override such as `source_analysis` or `image_unpacking`. |
| `ESHU_SCANNER_WORKER_POLL_INTERVAL` | Claim poll interval. |
| `ESHU_SCANNER_WORKER_CLAIM_LEASE_TTL` | Workflow claim lease TTL. |
| `ESHU_SCANNER_WORKER_HEARTBEAT_INTERVAL` | Claim heartbeat interval; must be less than lease TTL. |
| `ESHU_SCANNER_WORKER_CPU_MILLIS` | Analyzer CPU budget in millicores. |
| `ESHU_SCANNER_WORKER_MEMORY_BYTES` | Analyzer memory budget in bytes. |
| `ESHU_SCANNER_WORKER_TIMEOUT` | Analyzer timeout. |
| `ESHU_SCANNER_WORKER_MAX_INPUT_BYTES` | Maximum analyzer input bytes. |
| `ESHU_SCANNER_WORKER_MAX_FILES` | Maximum files per claim. |
| `ESHU_SCANNER_WORKER_MAX_FACTS` | Maximum source facts emitted per claim. |

## Evidence

No-Regression Evidence: scanner-worker runtime behavior is covered by
`go test ./internal/collector/scannerworker ./cmd/scanner-worker -count=1`.

Observability Evidence: the runtime records scanner-worker claim, retry,
dead-letter, facts-emitted, queue-wait, scan-duration, target-count,
result-count, CPU, and memory metrics, plus `scanner_worker.*` spans and
bounded structured failure logs.
