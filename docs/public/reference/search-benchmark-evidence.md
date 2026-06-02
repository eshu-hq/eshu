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
`runtime_summary`.

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
- a `postgres_content_search` / `keyword` baseline run;
- a `nornicdb_hybrid` / `hybrid` candidate run;
- candidate recall greater than baseline recall;
- zero false canonical claims on both runs;
- p95 latency within the recorded threshold, or an accepted reason for the
  threshold miss;
- per-run observation summaries with query count, mode, result-count range,
  truncation count, timeout count, and candidate truth-level counts.

The proof remains an internal evidence gate. It does not call NornicDB, add a
public search route, expose an MCP tool, or change runtime defaults.

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
cd go && go test ./internal/searchbench ./internal/searchdocs -count=1
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
