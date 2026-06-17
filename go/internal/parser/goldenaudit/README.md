# Parser Golden Audit

## Purpose

`internal/parser/goldenaudit` compares source-language graph observations
against independent golden fixtures. It gives language-depth work a reusable
accuracy check before parser or reducer changes claim better code graph support.

## Ownership boundary

This package owns only fixture loading and deterministic comparison. It does
not parse source files, materialize facts, write graph rows, query storage, or
decide support maturity.

## Exported surface

See `doc.go` for the godoc contract.

- `Graph`, `Node`, and `Edge` describe expected or observed source graph facts.
- `LoadGoldenGraph` reads checked-in fixture truth.
- `CompareGraph` returns a `Report` with missing, unexpected, and duplicate
  nodes and edges. The `Report` also carries an `Accuracy` field populated by
  `ScoreAccuracy`, and `Report.Summary()` appends
  `accuracy_precision`/`accuracy_recall` so a failing structural audit surfaces
  precision/recall alongside the diff. `Accuracy` is informational only:
  `Report.Pass()` still gates on structural drift, not on precision/recall.
- `ScoreAccuracy` returns an `AccuracyResult` with per-relationship-type and
  overall precision/recall (`Score`, `TypeAccuracy`) plus a wrong-target vs
  missing vs extra edge breakdown. It exists because tier distribution cannot
  tell a correctly targeted edge from one resolved to the wrong callee.

## Dependencies

The package depends only on the Go standard library. Parser, reducer, and query
tests may import it, but this package must not import those parents.

## Telemetry

This package emits no metrics, spans, or logs. It is a test and verification
helper; production parse timing remains owned by the collector snapshotter.

## Gotchas / invariants

Golden fixtures must not be produced by serializing Eshu output and checking it
back in as expected truth. Expected nodes and edges should come from a human
source fixture contract, with IDs stable enough for review and CI.

Comparison output is sorted by node ID and edge key so failures are stable
across runs.

`ScoreAccuracy` reuses `Edge.Key()` ((source, type, target)) as the correct-edge
identity and the `(source, type)` prefix to split wrong-target from extra edges.
Duplicate edges (same key) are collapsed before scoring. The div-by-zero
convention: an empty denominator scores `1.0` only when the counterpart
dimension is also empty (empty observed and empty golden is a perfect score),
otherwise `0.0` — so emitting nothing against a non-empty golden set still
fails.

## Related docs

- `go/internal/parser/README.md` — parser package contract and SCIP boundary
- `docs/public/reference/local-testing.md` — verification gates for parser work
- `docs/public/reference/dead-code-reachability-spec.md` — fixture accuracy
  expectations for language-scoped graph truth
