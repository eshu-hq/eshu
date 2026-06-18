# Search Benchmark Evidence

Search benchmark evidence compares current Postgres content search with
curated NornicDB retrieval over `EshuSearchDocument` records. It is not evidence
for whole-graph BM25, whole-graph vector search, or canonical graph truth.

Use this page when preparing issue #1264 benchmark records or reviewing a PR
that proposes NornicDB as a search lane.

## Evidence Version

The first evidence version is:

```text
search-benchmark-evidence/v1
```

Records are validated by `go/internal/searchbench.ValidateEvidence`.

## Required Shape

Every benchmark record must include:

| Field | Meaning |
| --- | --- |
| `version` | Evidence schema version, currently `search-benchmark-evidence/v1`. |
| `eshu_commit` | Eshu commit used for the benchmark run. |
| `schema_bootstrap_state` | Schema and bootstrap state before query timing. |
| `truth_scope` | Must use `level=derived` and a known basis such as `content_index` or `read_model`; search rank never becomes canonical truth. |
| `corpus` | Repository, file, entity, document, vector, and source-kind counts. |
| `backends` | Current Postgres content search plus at least one NornicDB search backend. |
| `failure_classes` | Required operator-visible failure classes. |
| `recommendation` | One of the approved benchmark decisions with rationale. |

NornicDB backend records must also include:

- backend image or commit;
- effective BM25/vector/embedding/search-persistence flags;
- clean-volume and preserved-volume startup durations in nanoseconds;
- query count, p50 latency, and p95 latency in nanoseconds;
- recall, precision, nDCG, and false canonical claim count;
- memory high-water mark;
- index artifact size when search-index persistence is enabled, or `0` when
  persistence is disabled;
- rebuild behavior.

## Backend And Mode Matrix

The current baseline backend is `postgres_content_search`. It measures the
existing content-store search path backed by `content_files` and
`content_entities`.

Supported NornicDB benchmark backends are:

| Backend | Mode |
| --- | --- |
| `nornicdb_bm25` | `keyword` |
| `nornicdb_vector` | `semantic` |
| `nornicdb_hybrid` | `hybrid` |

Benchmark only the modes supported by the measured backend. A vector or hybrid
run must still report whether embedding generation was enabled and whether
vector indexing was enabled.

Validation rejects backend and mode mismatches. Postgres content search and
NornicDB BM25 are keyword runs, NornicDB vector is semantic, and NornicDB
hybrid is hybrid.

## Corpus Requirements

The NornicDB side must index curated `EshuSearchDocument` records. Do not index
every canonical graph node and property as the target architecture.

The corpus section must record:

- repository count;
- file count;
- entity count;
- search document count;
- vector count;
- source-kind distribution.

The source-kind distribution must sum to the document count. Source kinds come
from `go/internal/searchdocs`, such as `code_entity`, `repository_file`, and
`runtime_summary`. For issue #417 semantic retrieval, `semantic_context`
records are allowed only when they are explicit derived/read-model labels with
bounded graph handles.

## Failure Classes

Every benchmark must report the known classes below when applicable. Unknown
classes are rejected so typos cannot weaken the operator-facing contract.

| Failure class | Meaning |
| --- | --- |
| `truncation` | A bounded top-K or page limit hid more results. |
| `timeout` | Query, readiness, startup, or restart exceeded the budget. |
| `disabled_search` | A backend returned disabled-search behavior. |
| `lazy_warm` | First query triggered index warmup. |
| `rebuild` | Search index rebuilt instead of loading a usable artifact. |
| `missing_artifact` | Expected persisted search artifact was absent. |
| `corruption` | Persisted search artifact was corrupt or unreadable. |

## Accuracy Metrics

Score benchmark queries against expected graph handles, not against raw result
text. `ScoreQueryResults` computes:

- recall;
- precision;
- nDCG;
- false canonical claim count.

False canonical claim count is the number of ranked documents that claim a
truth level other than `derived`. The correct fix for a nonzero count is to fix
the producer or projection, not to suppress the metric.

Decay scoring, when used, is ranking metadata only. It must follow the
[Search Decay Scoring](search-decay-scoring.md) contract and must not hide
evidence or promote ranking output to canonical graph truth.

## Decay Evaluation Gate

Issue #418 decay evidence uses `ScoreDecayEvaluation` and
`ValidateDecayEvaluation` from `go/internal/searchbench`. The gate compares the
original ranked candidates with the same candidates after applying a
`searchdecay.Scorer`.

Each decay evaluation records:

- query id and decay policy id;
- before and after recall, precision, nDCG, and false canonical claim counts;
- recall, precision, and nDCG deltas;
- whether required evidence was visible before and after decay;
- false canonical candidate count across the full candidate set;
- per-candidate original rank, decayed rank, original score, decayed score,
  decay outcome, and required-evidence marker.

`ValidateDecayEvaluation` rejects evidence when decay hides required evidence
that was visible before decay, when required evidence is not visible after
decay, or when the candidate set contains false canonical claims. False
canonical claims are counted across all candidates, not only the bounded top-K,
so decay cannot bury the failure outside the visible result window.

## Reranking Evaluation Gate

Issue #1282 reranking evidence uses `ScoreRerankEvaluation` and
`ValidateRerankEvaluation` from `go/internal/searchbench`. The gate compares a
prior NornicDB hybrid baseline with the same candidate set after reranking. It
is a measurement contract, not a live cross-encoder, GraphQL, gRPC, Postgres,
NornicDB, graph, API, MCP, or telemetry integration.

Each reranking evaluation records:

- query id;
- baseline hybrid evidence id, backend, and mode;
- baseline and reranked recall, precision, nDCG, and false canonical claim
  counts;
- recall, precision, nDCG, and false-canonical deltas;
- baseline and reranked latency in nanoseconds;
- latency delta in nanoseconds;
- baseline and reranked cost in micro USD;
- cost delta in micro USD;
- false canonical candidate count across the full baseline and reranked sets;
- per-document baseline rank, reranked rank, and required-evidence marker.

The gate rejects reranked output that changes the candidate set. Final
validation rejects evidence when the prior baseline hybrid record is missing,
when the baseline is not `nornicdb_hybrid` with `hybrid` mode, when latency or
cost values are negative, when the query top-K limit exceeds `100`, or when any
candidate claims truth outside `derived`. False canonical claims are counted
across the full candidate sets, not only the bounded top-K, so reranking cannot
bury a bad truth claim outside the visible result window.

## Protocol Recommendation Gate

Issue #1284 protocol evidence uses `ValidateProtocolRecommendation` from
`go/internal/searchbench`. The gate decides whether a protocol option has
enough user value to justify follow-on implementation. It is not a live
GraphQL, gRPC, Qdrant, NornicDB, Postgres, graph, API, MCP, reducer, or
telemetry integration.

Candidate protocols are:

| Candidate | Meaning |
| --- | --- |
| `current_api_mcp_search` | Keep the current API/MCP-backed search path. |
| `graphql_query_protocol` | Evaluate a GraphQL query protocol. |
| `grpc_query_protocol` | Evaluate a generic gRPC query protocol. |
| `qdrant_grpc_adapter` | Evaluate a Qdrant gRPC adapter path. |
| `nornic_native_protocol` | Evaluate a Nornic native protocol path. |
| `defer_protocol_expansion` | Record that no protocol candidate is ready. |

Each recommendation records:

- baseline hybrid evidence id, backend, and mode;
- candidate protocol;
- decision: keep current path, add the candidate protocol, or defer expansion;
- rationale;
- expected user value with measured evidence or an explicit deferred-evidence
  reason;
- migration risk;
- security risk;
- operator burden;
- latency impact;
- cost impact;
- fallback behavior;
- whether API/MCP authorization is preserved.

`migration_risk`, `security_risk`, and `operator_burden` are fixed
low-cardinality categories. Valid values are `none`, `low`, `medium`, `high`,
and `unknown`.

Validation rejects recommendations that lack prior NornicDB hybrid baseline
evidence, name an unsupported candidate protocol, bypass API/MCP
authorization, use an unknown risk or burden category, omit fallback behavior,
omit latency or cost impact evidence, or claim user value without measured
evidence or a deferred-evidence reason.

## Reranking And Protocol Close-Out Gate

Issue #421 uses `ValidateRerankProtocolEvaluation` from
`go/internal/searchbench`. The gate ties the reranking comparison and protocol
recommendation to the same measured NornicDB hybrid baseline evidence, or
records why phase 5 stopped before reranking or protocol expansion could start.

The first evidence version is:

```text
rerank-protocol-evaluation/v1
```

Measured issue #421 evidence must include:

- baseline hybrid evidence id, backend, and mode;
- rerank evaluation evidence;
- protocol recommendation evidence;
- no accepted stop reason.

Validation rejects measured issue #421 evidence when the baseline hybrid
evidence is missing, when the rerank evaluation and protocol recommendation do
not reference the same `nornicdb_hybrid` / `hybrid` evidence id, or when either
child record fails its own guardrails.

A stopped issue #421 record must include only an `accepted_stop_reason` that
references both issue #421 and the blocking issue #417 state. Stopped records
cannot include baseline, rerank, or protocol evidence because they do not prove
recall improvement, rerank quality, latency, cost, or protocol adoption value.

Issue #421 currently records the stopped artifact at
[`searchbench-evidence/issue-421-rerank-protocol-evaluation-v1.json`](searchbench-evidence/issue-421-rerank-protocol-evaluation-v1.json).
The exact blocker is the issue #1298 stopped proof for #417: the tree has the
bounded retrieval contract and proof gate, but no live Postgres content-search
adapter or NornicDB hybrid adapter has produced measured `nornicdb_hybrid`
baseline evidence. Reranking and protocol expansion must not start until that
baseline exists.

## Query Suite Gate

Issue #417 semantic retrieval evidence must use a versioned query suite before a
backend run can be treated as comparable baseline evidence. The first suite
version is:

```text
semantic-retrieval-query-suite/v1
```

`ValidateQuerySuite` requires at least 15 queries. Each query must include:

- stable id;
- query text;
- one scope anchor: service, workload, repository, or environment;
- mode;
- explicit top-K limit no greater than `100`;
- expected graph handles.

`ScoreQuerySuite` scores queries in suite order. It macro-averages recall,
precision, and nDCG across the suite, and sums false canonical claim counts.
Missing results count as zero-score queries, so partial backend output cannot
inflate recall.

## Semantic Retrieval Proof Gate

Issue #417 also requires a versioned proof before a NornicDB hybrid retrieval
candidate can be treated as better than the current Postgres content-search
baseline. The first proof version is:

```text
semantic-retrieval-proof/v1
```

`ValidateRetrievalProof` requires:

- the versioned 15-query suite;
- either a stop-only accepted reason that references issue #417, or the
  measured run evidence below;
- a `postgres_content_search` / `keyword` baseline run;
- a `nornicdb_hybrid` / `hybrid` candidate run;
- candidate recall greater than baseline recall for measured runs;
- zero false canonical claims on both measured runs;
- p95 latency within the recorded threshold, or an accepted reason for the
  threshold miss;
- per-run observation summaries with query count, mode, result-count range,
  truncation count, timeout count, and candidate truth-level counts.

The proof remains an internal evidence gate. It does not call NornicDB, add a
public search route, expose an MCP tool, or change runtime defaults.

Issue #1298 records the first stopped proof artifact at
[`searchbench-evidence/issue-1298-semantic-retrieval-proof-v1.json`](searchbench-evidence/issue-1298-semantic-retrieval-proof-v1.json).
That file commits the public-safe 15-query suite and names why measured
Postgres/NornicDB runs did not execute in this stack. It does not claim recall
improvement, latency, false-canonical safety, or adoption readiness. Accepted
stop reasons are exclusive with measured runs and latency evidence; a record
with baseline or candidate run evidence must satisfy the normal recall,
latency, false-canonical, and observation guardrails.

Issue #417 records the adapter-stage proof artifact at
[`searchbench-evidence/issue-417-nornicdb-hybrid-retrieval-prototype-v1.json`](searchbench-evidence/issue-417-nornicdb-hybrid-retrieval-prototype-v1.json).
That file keeps the same 15-query suite and records the remaining blocker:
bounded semantic-context projection and the hybrid-only NornicDB adapter exist,
but no live projected corpus plus Postgres baseline run has been measured. It
also does not claim recall improvement, p95 latency, or readiness.

## Link Prediction Candidate Evaluation Gate

Issue #420 uses `go/internal/searchbench.LinkPredictionEvaluation` for the
internal proof path. The proof is diagnostic evidence only. It does not add an
API route, expose an MCP tool, write candidate edges to NornicDB, write
canonical relationships, or change public truth-envelope levels.

The first evidence version is:

```text
link-prediction-evaluation/v1
```

`ValidateLinkPredictionEvaluation` requires:

- NornicDB backend commit or image evidence and the procedure source used for
  the evaluation;
- `procedure_mode=gds_stream`, meaning GDS-style stream procedures only;
- at least one candidate for each decision: `positive`, `negative`, and
  `ambiguous`;
- every candidate to include algorithm, score, source handle, target handle,
  evidence context, freshness, reason, and explicit `candidate` or
  `semantic_candidate` truth level;
- zero canonical relationship claims and zero false canonical claim count;
- positive relationship-gap discovery improvement over the recorded baseline;
- telemetry counts keyed by bounded `algorithm` and `decision`.

Supported diagnostic algorithms are the stream-only NornicDB procedures:

| Algorithm | Procedure family |
| --- | --- |
| `common_neighbors` | `gds.linkPrediction.commonNeighbors.stream` |
| `jaccard` | `gds.linkPrediction.jaccard.stream` |
| `adamic_adar` | `gds.linkPrediction.adamicAdar.stream` |
| `resource_allocation` | `gds.linkPrediction.resourceAllocation.stream` |
| `preferential_attachment` | `gds.linkPrediction.preferentialAttachment.stream` |
| `predict` | `gds.linkPrediction.predict.stream` |

Auto-TLP is rejected for this Eshu evidence gate because it can materialize
edges. Issue #420 only evaluates candidate suggestions; any future canonical
admission remains a separate reducer-owned design.

The first public-safe fixture artifact is
[`searchbench-evidence/issue-420-link-prediction-evaluation-v1.json`](searchbench-evidence/issue-420-link-prediction-evaluation-v1.json).
It records one positive candidate that improves relationship-gap discovery, one
negative candidate, one ambiguous provenance-only candidate, candidate truth
labels, and generation counts by algorithm and decision. It uses upstream
NornicDB source commit `2ff4e099c5aa1263c1655523f15564db243c00d9` as procedure
support evidence. The artifact is a fixture/proof-contract record, not a live
backend performance claim.

No-Regression Evidence: issue #420 adds a pure validation and scoring contract
under `go/internal/searchbench`; it performs no graph, Postgres, HTTP, MCP, or
NornicDB I/O and does not touch canonical writers or existing traversal
handlers.

Observability Evidence: `link-prediction-evaluation/v1` requires
`telemetry_counts` by bounded `algorithm` and `decision`. Candidate handles,
shared neighbors, source handles, and target handles stay in evidence records,
logs, or spans rather than metric labels.

## Live Benchmark Executor

`go/internal/searchbench` is pure: it owns the evidence, suite, and scoring
contracts and performs no I/O. The live execution layer is
`go/internal/searchbenchrun`, which turns a backend adapter and a query suite
into a measured, validated evidence record.

`searchbenchrun.RunSuite` drives a `searchretrieval.Backend` across every query
in a `searchbench.QuerySuite` through the bounded retrieval runner. From that
execution it derives the query count, p50/p95 latency (nearest-rank percentile
over every retrieval attempt, including timeouts and failures), and the recall,
precision, nDCG, and false-canonical-claim metrics computed by
`searchbench.ScoreQuerySuite`. Each backend serves exactly one mode, so the
executor derives the request mode from the backend identity and holds the query
set and expected handles constant across backends.

The executor does not invent the numbers it cannot observe from the query loop.
Backend image or commit, NornicDB search flags, clean- and preserved-volume
startup times, memory high-water mark, index artifact size, and rebuild
behavior are supplied by the operator harness through
`searchbenchrun.BackendDescriptor`. The Postgres-vs-NornicDB recommendation is a
recorded human decision passed to `searchbenchrun.AssembleEvidence`, which
stamps the schema version, derived truth scope, and the full required
failure-class contract before returning a record that has passed
`searchbench.ValidateEvidence`.

A per-query backend or timeout error is recorded as a missed query rather than
aborting the run; only parent context cancellation stops a run. The returned
`SuiteRun.Observations` carry the per-query mode, scope anchor, duration,
candidate and result counts, truncation, timeout, candidate truth-level counts,
and failure classes for operator diagnosis.

Recording a benchmark decision in this document still requires a live run over a
representative corpus with both the Postgres baseline and at least one NornicDB
backend wired to real stores. The executor is the harness that produces that
record; it does not lower the requirement for measured runs before any runtime
search change.

The operator entrypoint `go/cmd/search-bench` runs the comparison over a live
Eshu content corpus and emits real latency and corpus-shape numbers. When a
validated `searchbench.QuerySuite` JSON is supplied with `--suite`, it also runs
cap sweeps over the same live corpus and reports measured indexed-document
count, overflow, build time, p50/p95 latency, recall, precision, nDCG, and
false-canonical-claim count for each cap. The command refuses to infer recall
from unlabeled queries.

## Recorded Runs

- [Issue #2235 search-lane latency (2026-06-13)](searchbench-evidence/issue-2235-search-lane-latency-2026-06-13.md)
  — first measured run over a 27,822-document corpus: Postgres baseline vs the
  in-process `searchhybrid` lane; decision `defer_search_change`. The NornicDB
  search arm was not measured (no search-enabled curated deployment), and recall
  needs a labeled query suite.
- [Issue #2237 inverted index (2026-06-13)](searchbench-evidence/issue-2237-inverted-index-2026-06-13.md)
  — before/after for the `searchhybrid` inverted index: in-process hybrid p50
  ~19.5 ms → ~0.53 ms (~37×) over the same corpus, now faster than the Postgres
  baseline at the median with a tighter tail.
- [Issue #2355 search cap sweep (2026-06-13)](searchbench-evidence/issue-2355-search-cap-sweep-2026-06-13.md)
  — live 227,196-document cap sweep with a 20-query content-handle suite; the
  500-document placeholder recorded 0.000 recall, while the full corpus recorded
  1.000 recall with 24 us p95 in-process BM25 latency.

## Synthetic Package Benchmarks

- Issue #2596 pure-Go vector retrieval (2026-06-14): Benchmark Evidence:
  `cd go && go test ./internal/searchhybrid -run '^$' -bench 'BenchmarkBackendVectorRetrieval' -benchtime=100ms -count=1`
  on a deterministic 10,000-document synthetic corpus with 64 one-hot vector
  buckets and semantic `limit=20`. Exact cosine scored every valid in-scope
  vector at 9,539,657 ns/op, 2,036,187 B/op, and 282 allocs/op. Approximate
  retrieval bucketed by dominant vector dimension/sign, filtered by scope, and
  scored exact cosine for candidates at 4,684,271 ns/op, 1,082,762 B/op, and
  185 allocs/op. This is package-level scale evidence only; it is not a live
  corpus search-lane recommendation. No-Observability-Change: the change stays
  inside the in-process `searchhybrid` backend, emits no new runtime spans,
  metrics, or logs, and continues to rely on `searchretrieval.Runner`
  observations for operator-facing request duration, counts, truncation, and
  failure classes.
- Issue #3043 in-process angular-LSH vector retrieval (2026-06-18): Benchmark
  Evidence: `go test ./internal/searchhybrid -run '^$' -bench
  BenchmarkBackendVectorRetrieval -benchmem -count=3` on Apple M4 Pro over the
  same deterministic 10,000-document / 64-dimension synthetic corpus. Exact
  cosine measured 37.54 / 33.70 / 38.02 ms/op, 2,036,176-2,036,184 B/op, and
  282 allocs/op. The approximate path now uses deterministic multi-table
  angular LSH with one-bit neighbor probing plus exact cosine rerank over ANN
  candidates; it measured 16.71 / 17.85 / 19.41 ms/op, 1,092,306-1,092,312
  B/op, and 197 allocs/op. This remains package-level synthetic evidence only. It
  supports the explicitly enabled persisted local vector path and does not add a
  hosted provider, external vector store, canonical graph write, or new runtime
  telemetry surface.

## Recommendation

Each completed evidence record must recommend exactly one decision:

| Decision | Meaning |
| --- | --- |
| `keep_postgres_search` | Postgres content search remains the search lane. |
| `add_nornicdb_search_lane` | NornicDB is worth adding as a separate search lane. |
| `defer_search_change` | Accuracy, performance, or operability is not good enough. |

Do not recommend default NornicDB search if canonical graph readiness becomes
slower, less diagnosable, or dependent on successful search index rebuild.

## Verification Gate

Focused package gate:

```bash
cd go && go test ./internal/searchbench ./internal/searchbenchrun ./internal/searchdocs ./internal/searchnornicdb -count=1
```

Docs changes must also pass:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

## Related Docs

- [Search Retrieval Contract](search-retrieval-contract.md)
- [Search Document Projection](search-document-projection.md)
- [NornicDB Tuning](nornicdb-tuning.md)
- [NornicDB Tuning Evidence](nornicdb-tuning-evidence.md)
- [Truth Label Protocol](truth-label-protocol.md)
