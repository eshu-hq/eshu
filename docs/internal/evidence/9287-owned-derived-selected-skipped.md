# Owned-derived selected/skipped counters: runtime cost and signals (#9287)

## What changed at runtime

`scheduleVulnerabilityIntelligenceWork` calls `recordOwnedDerivedPage` once
per in-tree planning pass with the owned derivation enabled
(`vulnerability_intelligence_service.go`, `service_observations.go`). The
call records the read's per-ecosystem selection plus the owned-package skip
evidence on two OTEL counters
(`eshu_dp_workflow_coordinator_owned_derived_selected_total`,
`eshu_dp_workflow_coordinator_owned_derived_skipped_total`).

No query text changed. Proof, run against the base `8fa400413ceb`:

- `git diff --name-only 8fa400413ceb..HEAD` lists no file under
  `go/internal/storage`.
- `git diff -U0 8fa400413ceb..HEAD -- 'go/**/*.go' ':!*_test.go'` has no added
  line matching `SELECT`, `INSERT INTO`, `UPDATE `, `DELETE FROM`, `MATCH (`,
  `MERGE (`, `QueryContext`, or `ExecContext` (`rg` exit 1).
- Admission is unchanged: the record call sits after planning beside
  `reportEPSSDerived` and before `createWorkflowWork`, and reads only the
  already-fetched page and plan observation.

The cost is two OTEL counter Adds with bounded labels per pass. This note
measures the pass.

## Evidence

No-Regression Evidence: `go test ./internal/coordinator/ -count=1` on
baseline `8fa400413ceb` (0.088s, 0.067s, 0.065s) vs this head (0.095s,
0.130s, 0.079s, 0.069s, 0.058s), go1.26.6 linux/arm64, same host and command:
unit-suite wall times overlap in the 0.06-0.13s noise band, no regression
signal. The pass adds no I/O, locks, or store calls; it changes no Cypher,
queue, lease, claim, worker-count, or batching behavior.

Observability Evidence: the record path is pinned by
`TestNewMetricsPublishesOwnedDerivedSignals` (SDK-collected values with
leak/exclusion cases) and
`TestServiceRecordsOwnedDerivedSelectionPerPass` (production schedule path
with on/off gating); operators read the two counters beside the existing
owned-package window log line.

## Activation-path partition (follow-up)

`scheduleVulnerabilityActivationDerivedScopes`
(`component_extension_service.go`) performs the same owned read and window
log but never entered the in-tree scheduler, so it never recorded: on an
instance that plans exclusively through activations both counters
stayed dark while the read selected targets every pass. The activation now
calls `recordOwnedDerivedPage` right after planning with an empty skip
observation. Contract: selected covers both paths; skipped stays
in-tree-only (the activation planner returns no skip evidence), pinned by
`TestActivationDerivedScopesRecordOwnedDerivedSelection` (fails with zero
observations before the call, passes with npm=1 maven=1 and empty skipped
after). No query text changed; the call reads only the already-fetched
page.
