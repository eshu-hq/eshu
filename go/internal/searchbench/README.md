# Searchbench

## Purpose

`searchbench` defines the benchmark evidence contract for comparing current
Postgres content search with curated NornicDB BM25, vector, or hybrid retrieval.
It keeps measurements tied to `searchdocs.Document` inputs so benchmark,
decay, and reranking results cannot drift into whole-graph search or
canonical-truth claims.

## Ownership boundary

This package owns validation and scoring for search benchmark evidence,
semantic retrieval query suites, decay-scoring evaluation records, and
reranking evaluation records. It does not query Postgres, call NornicDB, invoke
cross-encoder rerankers, write graph state, expose API/MCP routes, or change
runtime defaults. Live benchmark adapters must measure their backend and then
feed versioned records through this package.

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
- `ScoreDecayEvaluation` records before/after metrics for one decay-scored
  query, including required-evidence visibility and per-candidate outcomes.
- `ValidateDecayEvaluation` rejects decay evidence that hides required results,
  fails to show required results after decay, or contains false canonical
  candidate claims.
- `ScoreRerankEvaluation` records baseline-hybrid and reranked metrics, latency,
  cost, and rank movement for one query.
- `ValidateRerankEvaluation` rejects rerank evidence without baseline hybrid
  proof or with false canonical candidate claims.
- `RequiredFailureClasses` returns the operator-visible failure classes every
  benchmark must report.

## Dependencies

`searchbench` imports `go/internal/searchdecay` for the decay policy contract
and `go/internal/searchdocs` for the search-document and truth contracts. It
otherwise uses only the Go standard library.

## Telemetry

None directly. The package is a pure validation and scoring layer. Live
benchmark runners must capture startup duration, query latency, memory
high-water mark, index artifact size, rebuild behavior, truncation, timeout,
disabled-search, lazy-warm, missing-artifact, corruption, and false canonical
claim evidence before writing a record. Decay evaluation records carry policy
id, evidence class, and outcome through `searchdecay.Decision`; live telemetry
bridges must keep those dimensions low-cardinality. Reranking evaluation
records carry the prior hybrid evidence id, aggregate latency, and aggregate
cost; live telemetry bridges must not promote document ids, query ids, or graph
handles into high-cardinality metric labels.

## Gotchas / invariants

- Benchmarks compare Postgres content search against curated NornicDB search
  documents, not whole-graph BM25 or vector search.
- Semantic retrieval suites must contain at least 15 scoped queries before they
  can be used as #417 baseline evidence, and every query limit must stay at or
  below 100.
- `truth_scope.level` must remain `derived`, and `truth_scope.basis` must name
  a known search-document evidence basis. Search rank, semantic similarity, and
  link prediction never become canonical graph truth.
- Decay scoring can change ranking metadata, but required evidence must remain
  visible after decay and false canonical candidate claims cannot be buried
  outside the top-K.
- Reranking can only be evaluated after a NornicDB hybrid baseline evidence
  record exists. Missing baseline evidence is a failed evaluation, not an
  implicit approval to run a reranker.
- Reranking evals compare the same candidate set before and after reranking; a
  changed candidate set is a retrieval experiment, not reranking evidence.
- Reranking eval query limits must stay at or below 100.
- Reranking false canonical candidate checks cover the full baseline and
  reranked candidate sets, not only the visible top-K.
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
