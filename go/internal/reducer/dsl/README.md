# internal/reducer/dsl

## Purpose

`internal/reducer/dsl` defines the evaluator seam for cross-source DSL
substrates and the readiness-publication helpers shared with reducer phase
publishers.

## Ownership boundary

This package owns the in-process contract: evaluator interfaces, bounded
canonical views, evaluation results, publication rows, runtime contract
templates, and conversion to `GraphProjectionPhaseState` rows.

It does not evaluate a DSL, read storage directly, write graph edges, or own
reducer domain orchestration. Concrete evaluators implement the interfaces here
and publish through reducer-owned ports.

## Exported surface

Use `go doc ./internal/reducer/dsl` for the complete exported contract. The
stable surface is:

- `Evaluator` and `DriftEvaluator`.
- `CanonicalView`, `EvaluationResult`, `Publication`, and `OutputKind`.
- `RuntimeContract`, `DefaultRuntimeContract`, and `RuntimeContractTemplate`.
- `PublishEvaluationResult`, which forwards phase states through
  `reducer.GraphProjectionPhasePublisher`.

## Dependencies

- `internal/reducer` supplies the phase-state publisher contract.
- `internal/status` state values are used indirectly through reducer phase
  publication.

Keep this package free of storage adapters and graph writers.

## Telemetry

This package emits no metrics or spans. The reducer domain that invokes an
evaluator owns runtime telemetry and status publication.

## Gotchas / invariants

- `RuntimeContractTemplate` must return defensive copies; callers can inspect
  the contract but must not mutate package globals.
- `EvaluationResult.PhaseStates` dedupes duplicate publications. Preserve
  stable state generation because phase replay depends on it.
- `PublishEvaluationResult` must reject invalid publications instead of writing
  ambiguous readiness rows.
- Keep view inputs bounded. A DSL evaluator must not default to whole-graph
  scans when a domain-specific scope exists.

## Focused tests

```bash
go test ./internal/reducer/dsl -count=1
go doc ./internal/reducer/dsl
```

## Related docs

- `go/internal/reducer/README.md`
- `docs/public/architecture.md`
- `docs/public/reference/relationship-mapping.md`
