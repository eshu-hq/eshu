# C-14 (#4367) projection-ordering coverage evidence

Scope: burn down the two advisory `projection:*|ordering` gaps for the
shared-conflict-key reducer projections
(`projection:incident_repository_correlation`,
`projection:supply_chain_impact`) in `specs/replay-coverage-manifest.v1.yaml`,
with real `go_test` scenarios under `go/internal/replay/schedulereplay`.

## What changed

- Two new committed cassettes:
  `testdata/cassettes/replayschedule/incident-repository-correlation.json`
  (6 facts: 2 `incident.record`, 2 `work_item.record`, 2
  `work_item.external_link`) and
  `testdata/cassettes/replayschedule/supply-chain-impact.json` (5 facts: 1
  `vulnerability.cve`, 2 `vulnerability.affected_package`, 1
  `scanner_worker.analysis`, 1 `vulnerability.suppression`).
- `go/internal/replay/schedulereplay/workitem_projection.go`: a new
  cassette-driven builder (`LoadProjectionWorkItems`) mapping each recorded
  fact to one `WorkItem`, dispatching by `fact_kind`, failing loudly on an
  unrecognized kind or a missing cross-reference key.
- `go/internal/replay/schedulereplay/scenario.go`: `Config` gained a `Domain
  reducer.Domain` field, defaulting to the pre-existing
  `reducer.DomainCodeCallMaterialization` when empty. This is the only change
  to previously-shipped behavior in the package, and it is additive:
  every existing caller that does not set `Domain` gets byte-identical
  behavior to before this change.
- `go/internal/replay/schedulereplay/projection_ordering_scenario_test.go`: a
  new table-driven test file covering both domains — order-invariant
  snapshot (Workers:1, 4 scripted orders), concurrent-batch invariant
  (Workers:4, `RunScheduleReport`), and a cross-hook teeth test on the
  incident cassette.
- `specs/replay-coverage-manifest.v1.yaml`: two new coverage rows mapping
  `projection:incident_repository_correlation|ordering` and
  `projection:supply_chain_impact|ordering` to the new test file, proof_gate
  `go-test-race`.
- `AGENTS.md`, `README.md`, `doc.go`: extended to describe the new
  cassette -> projection work-item seam and the shared-conflict-key ordering
  scenarios (this file, `eshu-folder-doc-keeper` scope).

## No-Regression Evidence:

This package is additive test-only replay/gate infrastructure plus one
backward-compatible `Config` field. No production runtime path changed (no
edit under `go/internal/reducer`, `go/internal/storage`, `go/internal/queue`,
or the service binaries); `Config.Domain` defaulting to
`reducer.DomainCodeCallMaterialization` when unset means the pre-existing
`TestScheduleReplay*` tests exercise the exact same code path as before this
change (proven: they still pass unmodified, see commands below).

Conflict domain per new scenario: `reducer.DomainIncidentRepositoryCorrelation`
("incident_repository_correlation") and `reducer.DomainSupplyChainImpact`
("supply_chain_impact") — the real reducer domain constants, not placeholders.
Worker settings exercised: `Workers=1` (sequential) and `Workers=4,
BatchClaimSize=4` (concurrent batch), same as the existing scenario.

Input shape: 6 work items for `incident_repository_correlation` (2 Incident
nodes, 2 WorkItem nodes, 2 cross-hook `HAS_WORK_ITEM` edges) and 5 work items
for `supply_chain_impact` (1 Vulnerability node, 2 Package nodes, 1 Finding
node, 1 Suppression node, 1 same-hook `AFFECTS` edge, 2 cross-hook `DETECTS`/
`TARGETS_PACKAGE` edges, 1 cross-hook `SUPPRESSES` edge). Terminal state is a
fixed node/edge count, identical across every scripted delivery order (proven
by `TestProjectionScheduleReplayOrderInvariantSnapshot`) and identical between
sequential and concurrent-batch delivery (proven by
`TestProjectionScheduleReplayConcurrentBatchInvariant`). Wall time is not
asserted (ordering correctness, not throughput); the full package `go test
-race` run completes in ~1.8s (see commands below). Because no production path
changed, there is no reducer throughput, queue-depth, or row-count regression
to measure.

## No-Observability-Change:

No telemetry instruments, spans, logs, or status fields are added or
modified. The new scenarios assert the canonical graph-truth snapshot
directly (`Graph.Canonical()`), the same mechanism the existing scenario
uses; the reducer's existing claim/queue instrumentation is untouched.

## Deviations from the assigned design (flagged, not silently applied)

- `work_item.external_link`'s real schema
  (`sdk/go/factschema/schema/work_item.external_link.v1.schema.json`) has no
  dedicated incident-reference field. This fixture repurposes the schema's
  existing `global_id` field (a generic cross-system identifier) to carry the
  linked incident's `provider_incident_id`. `work_item_key` is required by
  this loader (the schema allows it to be null) because it is the only way to
  identify which WorkItem node an edge attaches to.
- `scanner_worker.analysis`'s real schema
  (`sdk/go/factschema/schema/scanner_worker.analysis.v1.schema.json`) has no
  CVE or package reference field — a real reducer forms that join through
  richer evidence-path machinery, not a flat payload field
  (`go/internal/reducer/supply_chain_impact.go`). This fixture adds two
  additional (schema-legal: `additionalProperties: true`) fields,
  `linked_cve_id` and `linked_purl`, solely to give this credential-free,
  in-memory ordering fixture a cross-hook join. This does not claim
  `scanner_worker.analysis` carries these fields in production.
- `vulnerability.suppression` has no committed JSON Schema (confirmed: no
  `sdk/go/factschema/schema/vulnerability.suppression*.json` file exists);
  its payload keys come from the reducer decode seam
  (`go/internal/reducer/supply_chain_suppression_decode.go`) per the assigned
  design. This fixture repurposes `evidence_ref` (a generic evidence
  reference) to carry the suppressed finding's `target_locator_hash`.

None of these change a schema-required field, narrow a type, or rename a real
field — every payload still validates against its committed JSON Schema
(`additionalProperties: true` on every schema checked). They are flagged here
per the executor brief's "adapt minimally and flag it" instruction rather than
silently applied.

## Commands run

```bash
export GOCACHE="$(git rev-parse --show-toplevel)/.gocache"
cd go

# RED: prove the test fails to compile without the implementation
# (workitem_projection.go moved aside) -- see PR/report for captured output.
# go test ./internal/replay/schedulereplay/ -run TestProjection -count=1
#   -> internal/replay/schedulereplay/projection_ordering_scenario_test.go:51:31:
#      undefined: schedulereplay.LoadProjectionWorkItems

# GREEN
go test ./internal/replay/schedulereplay/ -run TestProjection -v -count=1
go test ./internal/replay/schedulereplay/ -race -count=1
go test ./internal/replay/offlinetier/ ./internal/replaycoverage/ ./cmd/replay-coverage-gate/ -count=1
go vet ./internal/replay/schedulereplay/
gofumpt -l go/internal/replay/schedulereplay/*.go

# regenerate + gate
go test ./cmd/replay-coverage-gate/ -update-dashboard -count=1
bash scripts/verify-replay-coverage-gate.sh --blocking
```
