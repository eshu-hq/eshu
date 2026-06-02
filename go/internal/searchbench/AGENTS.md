# AGENTS.md - internal/searchbench guidance for LLM assistants

## Read first

1. `go/internal/searchbench/README.md` - package purpose, boundaries, and
   invariants.
2. `go/internal/searchbench/evidence.go` - evidence schema, validation, and
   metric scoring.
3. `go/internal/searchbench/suite.go` - query-suite validation and aggregate
   scoring.
4. `go/internal/searchbench/decay_eval.go` - decay-scoring eval gate.
5. `go/internal/searchdocs/README.md` - curated search-document contract.
6. `docs/public/reference/search-benchmark-evidence.md` - public evidence
   format and proof requirements.
7. `docs/internal/design/430-nornicdb-graph-search-split.md` - parent design for
   keeping graph truth separate from the search lane.

## Invariants this package enforces

- **Curated documents only** - NornicDB benchmark evidence must describe
  `searchdocs.Document` inputs, not whole-graph node or property indexing.
- **Derived truth only** - benchmark output must not promote rank, similarity,
  or link prediction into canonical graph truth.
- **Comparable baselines** - evidence must include both current Postgres
  content search and at least one NornicDB search backend.
- **Scoped query suites** - #417 semantic retrieval suites must contain at least
  15 scoped queries with expected graph handles.
- **Decay is ranking metadata** - decay evals may reorder candidates, but must
  not hide required evidence or false canonical candidate claims.
- **Operational proof** - evidence must include backend identity, effective
  search flags, startup/restart behavior, memory, index artifact size, rebuild
  behavior, failure classes, accuracy metrics, and a recommendation.

## Common changes and how to scope them

- **Add a metric** - add a field to the evidence model, failing validation tests,
  docs in `search-benchmark-evidence.md`, and a fixture that proves the field is
  required.
- **Add a backend mode** - add a `Backend` or `Mode` constant, validation, tests,
  and docs. Do not add live adapter I/O to this package.
- **Change scoring** - write a red test with explicit expected handles and
  ranked documents. Keep false canonical claim counting independent from
  retrieval relevance.
- **Change decay evals** - cover ranking improvement, required-evidence
  visibility, false canonical candidate claims, and per-candidate outcomes.
- **Change query-suite validation** - cover invalid suite shape, duplicate ids,
  missing scope, and aggregate scoring before changing `suite.go`.

## Failure modes and how to debug

- Symptom: validation rejects a NornicDB run - check that effective search flags,
  startup timings, index artifact size, backend identity, and rebuild behavior
  are present.
- Symptom: validation rejects corpus details - confirm `document_count` matches
  the sum of `source_kind_distribution`.
- Symptom: false canonical claim count is nonzero - inspect result documents for
  any truth level other than `derived`; fix the producer rather than changing
  scoring.

## Anti-patterns specific to this package

- Adding Postgres, graph, HTTP, or NornicDB client calls.
- Treating NornicDB whole-graph search as the target benchmark.
- Making accuracy metrics optional because a run is inconvenient to score.
- Accepting a benchmark without a clear recommendation.
