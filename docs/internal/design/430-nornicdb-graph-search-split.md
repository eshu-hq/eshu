# NornicDB Canonical Graph And Search Projection Split

Status: proposed decision for issue #430. Design only; no code, schema,
Compose YAML, Helm chart, or runtime-default change in this PR.

Owners: graph backend, runtime, query, and release-gate maintainers.

## 1. Problem

Eshu uses NornicDB as the default canonical graph backend. That graph is a
truth and relationship store. It is not automatically a useful text corpus for
BM25, vector, or hybrid search. Whole-graph search indexing can include files,
symbols, workloads, deployment facts, Terraform resources, collector metadata,
timestamps, hashes, internal ids, queue/projection bookkeeping, and provider
fact details that are useful for graph truth but noisy or unsafe as search
documents.

Issue #430 was opened after an ops-qa EKS rollout where the canonical graph lane
paid search-index startup cost even though Eshu was not using vector retrieval:

- embeddings were disabled;
- Qdrant gRPC was disabled;
- vector/HNSW work had no vectors to index;
- BM25 still built over a large graph property set;
- the persisted BM25 artifact was large enough that restart still rebuilt.

The root design issue is not just a missing runtime knob. The root design issue
is that Eshu needs two separate lanes:

1. canonical graph storage for graph truth and relationships;
2. curated search projection for BM25, vector, or hybrid retrieval.

## 2. Decision

Eshu should split the canonical graph lane from the search lane.

### 2.1 Canonical graph lane

The canonical graph lane remains NornicDB or Neo4j behind Eshu's shared
Cypher/Bolt contract. It stores graph truth projected by the reducer and is read
through bounded API/MCP handlers.

For NornicDB deployments, the canonical graph lane should not build BM25 or
vector indexes over every graph node and property unless a specific proof says
that deployment also serves a curated Eshu search lane from the same database.

Eshu pins NornicDB `v1.1.2` for Compose graph startup because that release
ships the per-database BM25/vector enable and warming controls described in
[orneryd/NornicDB#177](https://github.com/orneryd/NornicDB/pull/177).
The canonical graph lane uses this graph-only policy:

- `NORNICDB_SEARCH_BM25_ENABLED=false`;
- `NORNICDB_SEARCH_VECTOR_ENABLED=false`;
- `NORNICDB_SEARCH_BM25_WARMING=lazy`;
- `NORNICDB_SEARCH_VECTOR_WARMING=lazy`;
- `NORNICDB_EMBEDDING_ENABLED=false`;
- `NORNICDB_PERSIST_SEARCH_INDEXES=false`.

BM25 or vector indexing should be re-enabled only for a deliberate curated
search-lane proof. Search results must remain derived retrieval records, not
canonical graph truth.

### 2.2 Search lane

Search is a reducer/query read-model problem, not a side effect of graph
backend startup. The search lane should project explicit Eshu search documents
and index only those records.

Proposed document shape:

```text
EshuSearchDocument
- id
- repo_id
- source_kind
- title
- path
- context_text
- entity_refs
- graph_handles
- labels
- updated_at
- truth_scope
- freshness_state
- access_scope
- provenance
```

The search lane may be backed by Postgres content search, a separate NornicDB
database/namespace, or a separate NornicDB deployment. The choice is a benchmark
result, not a default assumption.

### 2.3 Query contract

Every search or retrieval route must preserve the Eshu query contract:

- resolve the smallest scope first (`repo_id`, `service_id`, `workload_id`, or
  environment);
- require limit and timeout;
- return deterministic top-K results;
- return `truncated`, search mode, freshness, and truth labels;
- expand graph context only from bounded candidate handles;
- never run unbounded whole-graph search;
- never promote search score, semantic similarity, or link prediction to
  canonical truth.

## 3. What Belongs In Search Documents

Search documents should be curated for user questions, not blindly copied from
graph properties.

Good candidate records:

| Source | Search value |
| --- | --- |
| repository files | path, language, bounded snippet, symbol refs |
| content entities | name, kind, bounded source cache, repo path |
| service or workload summaries | stable handle, name, owning repo, deployment refs |
| vulnerability and supply-chain summaries | package, advisory id, owned evidence refs |
| incident or work-item summaries | title, bounded summary, linked runtime artifact refs |
| observability metadata | dashboard or rule title, bounded labels, target refs |

Excluded by default:

- internal graph ids, projection bookkeeping, queue ids, retry counters, and
  timestamps that are not user-facing freshness;
- raw provider payloads, log lines, trace spans, dashboard JSON, query bodies,
  security findings bodies, credentials, secrets, and tokens;
- high-cardinality labels that would become metric-like noise;
- every node/property in the canonical graph merely because it exists.

## 4. Deployment Topology Options

### Option A: Keep search in Postgres content store

This is the current safe baseline. It avoids new graph-backend startup cost and
uses the existing content search indexes. It does not provide semantic vector
retrieval.

### Option B: Separate NornicDB database or namespace in the same process

This keeps one deployment but separates graph truth from curated search
documents. It needs proof that index build, lazy warming, persistence, memory,
and failure behavior for the search database cannot delay canonical graph
readiness.

### Option C: Separate NornicDB search deployment

This provides clearer failure isolation and resource sizing. It adds operational
complexity, separate backup/restore, and another readiness contract.

The first implementation should benchmark A against B. C should be reserved for
evidence that shared-process resource contention or failure isolation matters at
the target scale.

## 5. Benchmark And Evidence Gate

Before replacing or augmenting Postgres-backed content search with NornicDB
BM25/vector retrieval, the PR must record:

- backend image or commit, Eshu commit, and effective NornicDB search flags;
- clean-volume and preserved-volume startup times;
- memory high-water mark, index artifact size, and rebuild behavior;
- document count, vector count, and indexed source-kind distribution;
- p50 and p95 latency for keyword and semantic queries;
- recall, precision, nDCG, and false-canonical-claim count on the semantic
  evaluation suite;
- reducer and queue impact for search projection writes;
- failure classes for disabled search, lazy warming, rebuild, corruption, and
  missing artifacts.

Stop threshold: do not enable NornicDB search by default if the canonical graph
readiness path gets slower, less diagnosable, or dependent on successful search
index rebuild.

## 6. Observability Contract

Search-lane implementation must expose operator signals for:

- search-index build state (`disabled`, `lazy`, `warming`, `ready`, `failed`);
- build duration, document count, vector count, and artifact size;
- query duration by search mode and result count;
- truncation and timeout counts;
- projection input count, skipped-document count, and redaction/drop reason;
- first-query lazy-warm trigger count and failure class.

Until implementation lands, this PR has no runtime observability delta.

## 7. Implementation Breakdown

Recommended child slices:

1. Adopt a pinned NornicDB build with explicit BM25/vector disable or lazy
   warming, then wire graph-lane Compose/Helm defaults with tests.
2. Define and fixture-test `EshuSearchDocument` projection without changing API
   or MCP contracts. The first contract lives in
   `go/internal/searchdocs` and
   `docs/public/reference/search-document-projection.md`.
3. Add a benchmark harness comparing current Postgres content search with
   curated NornicDB BM25/vector retrieval. The first harness contract lives in
   `go/internal/searchbench` and
   `docs/public/reference/search-benchmark-evidence.md`.
4. Add bounded internal retrieval path for semantic-evaluation queries. The
   first request/response contract lives in `go/internal/searchretrieval` and
   `docs/public/reference/search-retrieval-contract.md`.
5. Add public API/MCP search surfaces only after retrieval evidence proves value
   and preserves truth labels, scope, limits, and truncation.

Issues #417, #418, #420, #421, and #431 should stay behind this architecture
gate. #417 can start once the bounded search document projection and backend
startup policy have proof. #431 is tracked separately in
[NornicDB Primary Knowledge Store Evaluation](431-nornicdb-primary-store-evaluation.md)
and should not evaluate replacing Postgres until the search lane has at least
one shadow-read and shadow-write comparison.

## 8. Evidence For This PR

No-Regression Evidence: design-only PR; no Go, Cypher, schema,
docker-compose YAML, Helm chart, OpenAPI, MCP, or runtime-default files are
changed.

No-Observability-Change: design-only PR; it names the telemetry required for
future runtime work and does not alter existing signals.

Source check date: 2026-06-02.

Sources used:

- [NornicDB indexing memory docs](https://raw.githubusercontent.com/orneryd/NornicDB/main/docs/architecture/indexing-memory-large-datasets.md)
- [NornicDB search methodology docs](https://raw.githubusercontent.com/orneryd/NornicDB/main/docs/performance/searching.md)
- [NornicDB graph-only mode request](https://github.com/orneryd/NornicDB/issues/175)
- [NornicDB per-database search flag PR](https://github.com/orneryd/NornicDB/pull/177)
- [Eshu NornicDB tuning](../../public/reference/nornicdb-tuning.md)
- [Eshu NornicDB tuning evidence](../../public/reference/nornicdb-tuning-evidence.md)
