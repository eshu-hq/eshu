# 10973: status snapshot selection trim (registry + fact sections)

## Problem

Every status snapshot runs under `statusSnapshotLimit = 5s`
(`go/internal/runtime/postgres/status_reader.go`). All flap rows named in
#10973 load through `operatorStatusSelection()` — one transaction of ~15
sequential section queries. A bare snapshot deadline is not a reader-fence
verdict, so it answers the route's fixed-message 500 instead of a 503.

## Baseline (QA cluster, core image 651b030a, authed, 2026-11-20)

- `GET /api/v0/status/freshness-causality`: six 200s at 3.29-4.08s.
- `GET /api/v0/status/operations`: 200s at 4.36-5.23s, then 500s at 5.108s
  and 5.162s (75B fixed-message bodies); causality also sampled a 500 at
  5.106s. Any slow sample crosses 5s and all four fetches 500 together.
- `EXPLAIN (ANALYZE, TIMING OFF)` on the read replica
  (`eshu-postgresql-read`, 89M-row `fact_records`):
  - `registryMetadataTargetStatusQuery` (fact_records scan for registry
    snapshots): 878ms warm.
  - Collector fact evidence aggregate over `collector_evidence_summary`
    (11,125 nested-loop probes, ~243k buffers): 731ms warm, 2019ms cold.

## What changed

- `operatorStatusSelection()` drops `IncludeRegistryCollectors` (keeps fact
  evidence): no `/api/v0` route renders `RegistryCollectors` — only
  `status.RenderJSON`/`RenderText` admin surfaces do, and they do not use
  this selection.
- New `causalityStatusSelection()` drops both fact sections for
  `GET /api/v0/status/freshness-causality`, which renders neither.
- The live evidence bundle keeps fact evidence and drops registry.

Timeouts unchanged; the snapshot simply stops paying for sections its
caller drops.

## No-Regression Evidence:

Rendered bytes are proven identical with sections omitted:
`TestFactRegistryStatusRoutesSkipUnrenderedSections` pins all 10 operator
routes' selections and asserts byte-identical bodies against fixtures with
populated fact evidence (1 row) and registry snapshots (1 row); the probe is
non-vacuous (fixture rows asserted present) and its sensitivity is proven by
`TestFactEvidenceOmissionChangesOperationsResponse` (omitting a rendered
section changes the body) and by the pre-existing Terraform omission probes.
`TestTerraformFreeStatusRoutesSkipTerraformEvidence`,
`TestEvidenceBundleSelectsFactEvidenceWithoutTerraformAndRegistry`, and the
full `go/internal/query`, `go/internal/query/evidence`, `go/internal/status`,
and `go/internal/storage/postgres` suites pass on the rebased head.

## Performance Evidence:

Before (above): snapshot cost ~3.3-4.1s (causality) and ~4.4-5.2s
(operations, which adds a second bounded `ReadLiveActivity` query), against
the 5s bound. Removing the ~0.9s registry scan from every operator snapshot
and the ~0.7-2s fact aggregate from causality moves both rows clear of the
bound at the measured QA shape; no timeout, retry, or concurrency change.
After-measurement (authed timing sweep of the five rows on QA) runs once the
new core image deploys there, and lands on #10973 as the flap-fixed signal
for the 9669 C1c retry.

## Observability Evidence:

No new spans, metrics, or logs: strictly less work under existing signals.
Effect is visible in the existing request latency/status-code metrics for
the five routes (fewer 5.1s 500s) and the unchanged
`snapshotStatusReader` phase/outcome span attributes.
