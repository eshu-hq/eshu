# Searchbench

## Purpose

`searchbench` defines the benchmark evidence contract for comparing current
Postgres content search with curated NornicDB BM25, vector, or hybrid retrieval.
It keeps measurements tied to `searchdocs.Document` inputs so benchmark results
cannot drift into whole-graph search or canonical-truth claims.

## Ownership boundary

This package owns validation and scoring for search benchmark evidence and
semantic retrieval query suites. It does not query Postgres, call NornicDB,
write graph state, expose API/MCP routes, or change runtime defaults. Live
benchmark adapters must measure their backend and then feed versioned records
through this package.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `Evidence` is the versioned benchmark record.
- `QuerySuite` is the versioned #417 semantic retrieval suite contract.
- `BackendRun`, `CorpusSummary`, `LatencySummary`, `StartupSummary`,
  `RetrievalMetrics`, and `Recommendation` capture the required #1264 evidence.
- `ValidateEvidence` enforces required backend identity, corpus shape, failure
  classes, accuracy metrics, truth scope, and recommendation.
- `ValidateQuerySuite` enforces the 15-case query-suite baseline shape.
- `ScoreQueryResults` computes recall, precision, nDCG, and false canonical
  claim count from ranked `searchdocs.Document` results.
- `ScoreQuerySuite` macro-averages per-query metrics and sums false canonical
  claims in suite order.
- `RequiredFailureClasses` returns the operator-visible failure classes every
  benchmark must report.

## Dependencies

`searchbench` imports `go/internal/searchdocs` for the search-document and truth
contracts. It otherwise uses only the Go standard library.

## Telemetry

None directly. The package is a pure validation and scoring layer. Live
benchmark runners must capture startup duration, query latency, memory
high-water mark, index artifact size, rebuild behavior, truncation, timeout,
disabled-search, lazy-warm, missing-artifact, corruption, and false canonical
claim evidence before writing a record.

## Gotchas / invariants

- Benchmarks compare Postgres content search against curated NornicDB search
  documents, not whole-graph BM25 or vector search.
- Semantic retrieval suites must contain at least 15 scoped queries before they
  can be used as #417 baseline evidence.
- `truth_scope.level` must remain `derived`, and `truth_scope.basis` must name
  a known search-document evidence basis. Search rank, semantic similarity, and
  link prediction never become canonical graph truth.
- NornicDB runs must record the effective search flags and both clean-volume and
  preserved-volume startup behavior.
- Backend and mode pairs are fixed: Postgres and NornicDB BM25 use `keyword`,
  NornicDB vector uses `semantic`, and NornicDB hybrid uses `hybrid`.
- Evidence must include a recommendation: keep Postgres search, add NornicDB as
  a separate search lane, or defer adoption.

## Related docs

- `docs/public/reference/search-benchmark-evidence.md`
- `docs/public/reference/search-document-projection.md`
- `docs/internal/design/430-nornicdb-graph-search-split.md`
