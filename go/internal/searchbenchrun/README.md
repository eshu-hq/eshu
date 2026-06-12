# Searchbenchrun

## Purpose

`searchbenchrun` is the live execution layer for the design-430 search benchmark
gate (issue #2235). The `searchbench` package owns the pure evidence, suite, and
scoring contracts and performs no I/O. This package drives a bounded
`searchretrieval.Backend` across a `searchbench.QuerySuite`, measures query
latency from runner observations, scores the normalized results, and assembles
validated `searchbench.Evidence` so the Postgres-vs-NornicDB decision can be
recorded before any runtime search change.

## Ownership boundary

This package owns only the benchmark execution loop and evidence assembly. It
does not own:

- the evidence, suite, or scoring schema (`internal/searchbench`),
- the bounded retrieval contract or runner (`internal/searchretrieval`),
- any backend adapter (`internal/searchpostgres`, `internal/searchnornicdb`),
- the curated document projection (`internal/searchdocs`),
- process-level startup, memory, or index-artifact measurement. Those values are
  supplied by the operator harness through `BackendDescriptor`.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `BackendDescriptor` carries operator-measured backend metadata (build identity,
  NornicDB search flags, startup, memory, rebuild behavior, query timeout).
- `SuiteRun` is the measured output: the assembled `searchbench.BackendRun`, the
  `searchbench.QuerySuiteScore`, and the per-query retrieval observations.
- `RunSuite` executes a suite against one backend and produces a `SuiteRun`.
- `EvidenceInput` and `AssembleEvidence` combine backend runs, corpus shape, and
  the recorded decision into validated `searchbench.Evidence`.

## What it measures and what it does not

`RunSuite` derives, from real query execution:

- query count,
- p50/p95 latency by nearest-rank percentile over every retrieval attempt
  (including timed-out and failed attempts),
- recall, precision, nDCG, and false-canonical-claim count via
  `searchbench.ScoreQuerySuite`.

The operator harness supplies, through `BackendDescriptor`, the values the query
loop cannot observe: backend image/commit, NornicDB search flags, clean and
preserved-volume startup time, memory high-water mark, index artifact size, and
rebuild behavior. The Postgres-vs-NornicDB recommendation is a human decision
recorded in `EvidenceInput.Recommendation`, not inferred by this package.

## Mode selection

Each backend serves exactly one retrieval mode, so `RunSuite` derives the
request mode from the backend identity (`modeForBackend`) and overrides each
suite query's mode. This holds the query set and ground-truth handles constant
while varying the backend under test, and guarantees the assembled run passes
`searchbench` backend/mode compatibility validation.

## Telemetry

None directly. This is benchmark plumbing, not a runtime route. `RunSuite`
captures the `searchretrieval.Observation` for every query (mode, scope anchor,
duration, candidate and result counts, truncation, timeout, candidate
truth-level counts, failure classes, error class) and returns them on `SuiteRun`
for diagnosis. A live harness must bridge those observations and the assembled
evidence to the operator signals named in design 430 §6 before any production
search surface exists.

## Gotchas / invariants

- A per-query backend or timeout error is recorded as a failed query (zero
  results, scored as a miss); only parent context cancellation aborts the run.
- `RunSuite` requires a valid suite (`searchbench.MinimumQuerySuiteSize`
  queries), a non-nil backend, a positive query timeout, and a known backend.
- `AssembleEvidence` stamps `searchbench.EvidenceVersion`, derived truth scope,
  and the complete required failure-class contract, then returns the
  `ValidateEvidence` error rather than emitting an invalid record.
- Search rank and score stay derived retrieval evidence, never canonical graph
  truth. Nothing here enables NornicDB search or adds an API or MCP route.

## Related docs

- `docs/internal/design/430-nornicdb-graph-search-split.md`
- `docs/public/reference/search-benchmark-evidence.md`
- `docs/public/reference/search-retrieval-contract.md`
- `docs/public/reference/search-document-projection.md`
