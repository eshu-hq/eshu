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
  analyzer --> facts["scanner source facts\nscanner_worker.* / sbom.* / vulnerability.os_package"]
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
- Runs the concrete `sbom_generation` analyzer when configured with
  `sbom_targets`; this source walks a configured repository root, reads bounded
  `package-lock.json`, `npm-shrinkwrap.json`, and `go.mod` manifests, and emits
  `sbom.document`, `sbom.component`, and `sbom.warning` facts.
- Runs the concrete `os_package_extraction` analyzer when configured with
  `os_package_targets`; this parser consumes already-extracted Alpine or Debian
  rootfs metadata and emits `vulnerability.os_package` /
  `vulnerability.warning` facts.
- Records retry and dead-letter payloads with locator hashes and bounded
  failure classes.
- Exposes `/healthz`, `/readyz`, `/metrics`, `/admin/status`, and optional
  private pprof through `ESHU_PPROF_ADDR`.

The fallback analyzer emits an explicit `scanner_worker.warning` source fact
(`reason=analyzer_not_configured`). `sbom_generation` falls back to
`reason=sbom_generator_source_not_configured` when no `sbom_targets` are
configured. Concrete image, secret, license, source, and misconfiguration
analyzers must plug into this boundary instead of running in reducer lanes.

`sbom_generation` repository targets are configured inside the selected
`scanner_worker` collector instance:

```json
{
  "analyzer": "sbom_generation",
  "sbom_targets": [
    {
      "scope_id": "scanner-worker://repository/team-api",
      "root_path": "/var/lib/eshu/scanner/repositories/team-api",
      "subject_digest": "sha256:..."
    }
  ]
}
```

The repository path is runtime-local configuration. It must not appear in
retry, dead-letter, metric, log, or public documentation payloads. If no usable
components are found, the analyzer emits a document fact plus an
`sbom.warning` fact instead of returning silent clean output.

`os_package_extraction` targets are configured inside the selected
`scanner_worker` collector instance:

```json
{
  "analyzer": "os_package_extraction",
  "os_package_targets": [
    {
      "scope_id": "image://registry.example/team/app@sha256:...",
      "rootfs_path": "/var/lib/eshu/scanner/rootfs/...",
      "source_uri": "oci://registry.example/team/app@sha256:...",
      "source_record_id": "sha256:..."
    }
  ]
}
```

The rootfs path is runtime-local configuration. It must not appear in retry,
dead-letter, metric, log, or public documentation payloads.

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
`go test ./internal/collector/scannerworker ./internal/collector/scannerworker/sbomgenerator ./internal/collector/ospackagevulnerability/osruntime ./cmd/scanner-worker -count=1`.

Observability Evidence: the runtime records scanner-worker claim, retry,
dead-letter, facts-emitted, queue-wait, scan-duration, target-count,
result-count, CPU, and memory metrics, plus `scanner_worker.*` spans and
bounded structured failure logs. Configured `sbom_generation` sources return
measured manifest input bytes as peak memory usage and CPU seconds from the Go
runtime counters, so operators can distinguish queue wait, manifest read cost,
source fact volume, retries, and terminal resource-limit failures.
