# Storageeval

## Purpose

`internal/storageeval` defines pure evidence contracts for storage migration
evaluation gates. It currently owns the #1287 shadow-read comparison gate for
selected content/read-model families and the #1288 shadow-write comparison gate
for bounded fact-family migration proof.

The package validates records that compare current Postgres production answers
with NornicDB shadow answers. Passing evidence proves only parity for the
scoped read model or fact family; it does not change production ownership.

## Ownership boundary

This package owns value types and validation rules for storage evaluation
evidence. It must not open Postgres, call NornicDB, write graph state, expose
API/MCP routes, enqueue reducer work, or decide canonical graph truth.

Future adapters may produce `ShadowReadComparison` records, but adapters remain
outside this package. Postgres stays the baseline owner until a separate
cutover proposal proves parity, performance, backup/restore, and rollback.

## Exported surface

See `doc.go` for the godoc contract.

- `ShadowReadComparison` is the evidence record for one bounded comparison.
- `FactWriteComparison` is the evidence record for one bounded fact-family
  write/read-back comparison.
- `ReadResult`, `TruthLabel`, `Freshness`, and `Scope` describe each side of
  the comparison.
- `FactWriteResult` describes each side of a fact-store write and bounded
  read-back comparison.
- `ValidateShadowReadComparison` accepts only matching, fresh, non-truncated,
  supported, bounded evidence with explicit fallback behavior.
- `ValidateFactWriteComparison` accepts only matching fact identity,
  idempotency key, scope/generation, schema version, active generation, current
  supersession, record state, digest, fallback, and rollback evidence.
- `ReadModel`, `Backend`, `TruthLevel`, `TruthBasis`, `FreshnessState`,
  `Verdict`, `FallbackBehavior`, and `FailureClass` provide stable labels for
  future proof runners.
- `FactFamily`, `FactRecordState`, `FactGenerationState`,
  `FactSupersessionState`, `FactWriteVerdict`, `FactWriteFailureClass`, and
  `RollbackBehavior` provide fact-write proof labels.

## Dependencies

Standard library only. The package is a leaf so storage, reducer, query, and
operator tooling can consume the contract without adding runtime coupling.

## Telemetry

The package emits no metrics, spans, or logs. Future proof runners must expose
comparison count, duration, parity drift count, latest drift time, fallback
count by reason, and failure class. Repository ids, file paths, entity ids,
fact ids, graph handles, and digests belong in logs or traces, not metric
labels.

No-Observability-Change: this package defines the required evidence labels and
does not alter hosted runtime signals.

## Gotchas / invariants

- Passing evidence requires `verdict=match`; failure verdicts are useful
  diagnostics but are not passing parity proof.
- Passing evidence requires `failure_class=none` so proof records stay
  operator-diagnosable.
- Comparisons must be bounded with a positive `limit`.
- Scope kind must be one of the supported comparison scopes.
- Baseline and shadow truth labels must match exactly. A shadow result must not
  downgrade to `fallback` or upgrade derived evidence into canonical truth.
- Freshness must be explicit and `fresh` for both sides.
- Truncated output is rejected because partial equality is not parity.
- Unsupported shadow capability is rejected instead of silently falling back.
- Explicit fallback behavior is required so operators know production remains
  on Postgres, fails closed, or returns `unsupported_capability`.
- Fact-write evidence must preserve stable fact identity, idempotency key,
  scope/generation, semantic schema version, active generation, current
  supersession, active or tombstone state, and bounded read-back count.
- Shadow fact writes must be explicitly disposable through rollback behavior;
  this package must not grow queue, lease, retry, or dead-letter semantics.

## Verification

Run from the repository root:

```bash
(cd go && go test ./internal/storageeval -count=1)
(cd go && go vet ./internal/storageeval)
(cd go && golangci-lint run ./internal/storageeval)
./scripts/verify-package-docs.sh
git diff --check
```

## Related docs

- `docs/internal/design/431-nornicdb-primary-store-evaluation.md`
- `docs/internal/design/1286-postgres-ownership-inventory.md`
- `docs/internal/design/1287-shadow-read-comparison-gate.md`
- `docs/internal/design/1288-shadow-write-comparison-gate.md`
- `docs/public/reference/truth-label-protocol.md`
- `docs/public/reference/search-document-projection.md`
