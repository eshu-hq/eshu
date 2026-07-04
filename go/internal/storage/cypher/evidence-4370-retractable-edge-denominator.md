# Evidence: #4370 Retractable Edge Replay Denominator

Scope: `retractable_edge_types.go` and the replay-depth lockstep that consumes
`RetractableEdgeTypes()`.

## No-Regression Evidence:

This change adds a static, sorted, defensive-copy registry of registered
relationship type constants that existing canonical and reducer edge retract
paths can remove. It does not change any Cypher query string, graph writer,
batching behavior, retry policy, transaction boundary, worker claim, queue
ordering, backend knob, or runtime service path.

Baseline: before this change, `specs/replay-depth-requirements.v1.yaml` had
machine-readable `delta_tombstone` requirements for retractable graph node
labels only; retractable edge types were prose-only follow-up work, so the
replay gate could not list per-edge delete/retract gaps.

After: the replay gate derives `retractable_edge:<TYPE>` depth surfaces from a
52-entry static code registry mirrored into
`specs/replay-depth-requirements.v1.yaml`; the generated dashboard reports
`retractable_edge_type 0/52` advisory gaps and total coverage `214/389`.

Backend/version: backend-neutral. The new function performs no Bolt, Cypher,
Postgres, NornicDB, or Neo4j call and relies on no planner behavior. It only
returns string constants already registered in `go/internal/graph/edgetype`.

Input shape: one in-memory slice of 52 registered static relationship types.
Terminal queue or row counts: not applicable; no queue, graph rows, or
persistence path is touched. Runtime row count remains zero because the change
does not execute a graph query.

Commands:

```bash
go test ./internal/storage/cypher -run TestRetractableEdgeTypes -count=1
go test ./internal/replaycoverage -run 'TestLoadDepthRequirementsValid|TestLoadDepthRequirementsRejects|TestEnumerateDepthSurfaces|TestDeriveRequirementsPerApplicableSurface|TestNewRetractableEdgeReportedUncovered|TestRetractableEdgeTypesLockstep' -count=1
bash scripts/verify-replay-coverage-gate.sh --blocking
make pre-pr
```

Why safe: the only production behavior change is that coverage accounting now
lists missing edge delta scenarios instead of hiding them. The advisory-to-
blocking policy is unchanged: non-baseline depth gaps remain advisory even when
the gate runs with `--blocking`.

## No-Observability-Change:

No runtime path is added or changed, so no new operator metric, span, status
field, or log line is needed. Existing replay-coverage output is the operator
signal for this change: the committed dashboard and JSON report enumerate the
new `retractable_edge:*` advisory gaps, and `make pre-pr` verifies the generated
dashboard/report path.
