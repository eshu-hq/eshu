# AGENTS.md - internal/searchbench guidance for LLM assistants

## Read first

1. `go/internal/searchbench/README.md` - package purpose, boundaries, and
   invariants.
2. `go/internal/searchbench/evidence.go` - evidence schema, validation, and
   metric scoring.
3. `go/internal/searchbench/suite.go` - query-suite validation and aggregate
   scoring.
4. `go/internal/searchbench/decay_eval.go` - decay-scoring eval gate.
5. `go/internal/searchbench/rerank_eval.go` - reranking eval gate.
6. `go/internal/searchbench/protocol_recommendation.go` - protocol decision
   gate.
7. `go/internal/searchdocs/README.md` - curated search-document contract.
8. `docs/public/reference/search-benchmark-evidence.md` - public evidence
   format and proof requirements.
9. `docs/internal/design/430-nornicdb-graph-search-split.md` - parent design for
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
- **Reranking depends on hybrid evidence** - rerank evals require a prior
  NornicDB hybrid baseline evidence id before any reranked metrics are valid.
- **Reranking is reorder-only** - rerank evals must use the same candidate set
  before and after reranking.
- **False claims cannot be buried** - rerank evals count false canonical
  candidates across the full baseline and reranked sets, not only top-K output.
- **Protocol expansion must justify user value** - protocol recommendations
  require baseline hybrid evidence, fallback behavior, preserved API/MCP
  authorization, and measured or explicitly deferred value evidence.
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
- **Change rerank evals** - cover baseline hybrid evidence, before/after
  metrics, same-candidate-set validation, latency and cost deltas, false
  canonical candidates, and rank movement.
- **Change protocol recommendations** - cover baseline hybrid proof, candidate
  protocol validation, user value evidence, fallback behavior, API/MCP
  authorization preservation, and latency/cost impact evidence.
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
- Adding live cross-encoder or protocol client calls.
- Letting a protocol recommendation bypass API/MCP authorization or fallback
  requirements.
- Treating NornicDB whole-graph search as the target benchmark.
- Making accuracy metrics optional because a run is inconvenient to score.
- Accepting a benchmark without a clear recommendation.
