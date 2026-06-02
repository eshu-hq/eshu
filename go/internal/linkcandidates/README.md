# Linkcandidates

## Purpose

`linkcandidates` defines the diagnostic evidence contract for relationship
suggestions produced by future link-prediction experiments. It gives issue #420
a testable shape before NornicDB procedures, API/MCP responses, reducer
admission, or telemetry exporters use candidate suggestions.

## Ownership boundary

This package owns pure validation of candidate shape, truth labels, freshness,
decision, and low-cardinality observation dimensions. It does not call
NornicDB, Postgres, graph stores, API/MCP handlers, reducers, or OTEL
exporters. It never writes canonical relationship rows or graph edges.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `Candidate` records algorithm, score, source handle, target handle, evidence
  context, freshness, reason, truth level, and decision.
- `GraphHandle` records source and target endpoint kind/id fields.
- `TruthLevel` permits only `candidate` and `semantic_candidate`.
- `Decision` records generated, suppressed, or ambiguous candidate outcomes.
- `Freshness` records state and observed time for the candidate input.
- `ValidateCandidate` rejects incomplete, canonical, or unbounded candidate
  records.
- `ObservationFor` returns the bounded algorithm and decision dimensions for
  later metrics, spans, or logs.

## Dependencies

`linkcandidates` uses only the Go standard library.

## Telemetry

No OTEL telemetry directly. `ObservationFor` returns only algorithm and
decision. Live callers must not add source handles, target handles, repository
ids, service ids, evidence ids, or candidate ids as metric labels.

## Gotchas / invariants

- Candidate truth labels are not canonical truth labels.
- `candidate` and `semantic_candidate` suggestions are diagnostic evidence
  until a separate reducer-owned admission design accepts them.
- Ambiguous candidates remain provenance-only.
- Suppressed candidates are counted but not shown as generated suggestions.
- Algorithm ids are short lowercase tokens so observation dimensions stay
  low-cardinality.
- Scores are bounded from `0` to `1` and must be finite.

## Related docs

- `docs/public/reference/link-prediction-candidates.md`
- `docs/public/reference/relationship-mapping.md`
- `docs/public/reference/relationship-mapping-evidence.md`
- `docs/internal/design/430-nornicdb-graph-search-split.md`
