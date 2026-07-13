# NornicDB Canonical Graph And Search Projection Split

Status: proposed decision for issue #430. Phase-1 graph-only startup
stabilization is implemented in Compose, Helm, runtime contract tests, and
operator docs. The curated search projection remains design- and
benchmark-gated before any public API, MCP, schema, or graph-write change.

Phase-1 stabilization status: Compose and Helm now pin NornicDB `v1.1.11` and
set the canonical graph lane to graph-only startup controls. Runtime contract
tests enforce the graph-only NornicDB controls in Compose, Helm, and the public
environment reference.

Owners: graph backend, runtime, query, and release-gate maintainers.

## 1. Problem

Eshu uses NornicDB as the default canonical graph backend. That graph is a
truth and relationship store. It is not automatically a useful text corpus for
BM25, vector, or hybrid search. Whole-graph search indexing can include files,
symbols, workloads, deployment facts, Terraform resources, collector metadata,
timestamps, hashes, internal ids, queue/projection bookkeeping, and provider
fact details that are useful for graph truth but noisy or unsafe as search
documents.

Issue #430 was opened after a platform-qa EKS rollout where the canonical graph lane
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

Eshu pins NornicDB `v1.1.11` for Compose graph startup. The per-database
BM25/vector enable and warming controls Eshu depends on shipped in v1.1.2
([orneryd/NornicDB#177](https://github.com/orneryd/NornicDB/pull/177)) and are
preserved in later releases; v1.1.11 is the latest published multi-arch Docker Hub
manifest for the `nornicdb-cpu-bge` image line. Releases v1.1.4–v1.1.6 are
maintenance/compatibility releases (Cypher/Bolt correctness, storage resilience,
vector-search performance, Neo4j/Graphiti compatibility) with no on-disk format
change, so tracking the latest keeps the same graph-only startup policy.
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

### 5.1 Recorded benchmark (2026-06-13)

First measured run via `go/cmd/search-bench` over a live content corpus
(`repository:r_9a84f5f1`, 27,822 curated documents), Eshu commit `3d8dbb0e1`.
Full record:
[searchbench-evidence/issue-2235-search-lane-latency-2026-06-13.md](../evidence/searchbench-evidence/issue-2235-search-lane-latency-2026-06-13.md).

Measured keyword latency:

| Backend | p50 | p95 | max |
| --- | --- | --- | --- |
| `postgres_content_search` | ~1.1 ms | ~3.9 ms | ~132 ms |
| `in_process_hybrid_bm25` (`searchhybrid`) | ~19.5 ms | ~22.3 ms | ~38.9 ms |

Decision: **defer_search_change**. The Postgres baseline keeps a faster median
with a variable tail; the in-process hybrid lane was latency-consistent but, in
that first run, scored documents linearly (~20 ms at 28k docs). The NornicDB
search arm was not measured — the canonical NornicDB runs search-disabled and no
search-enabled curated deployment exists — so the stop threshold is not cleared
and Postgres content search remains the search lane.

Update (inverted index): the in-process lane now serves BM25 from an inverted
index, cutting p50 from ~19.5 ms to ~0.53 ms (~37×) over the same corpus — now
faster than the Postgres baseline at the median with a tighter tail (record:
[searchbench-evidence/issue-2237-inverted-index-2026-06-13.md](../evidence/searchbench-evidence/issue-2237-inverted-index-2026-06-13.md)).
The decision stays `defer_search_change` pending a quality-based comparison; the
remaining gaps are a labeled query suite to measure recall/ranking quality, a
search-enabled NornicDB to measure that arm, and an ANN index for the vector
path. The `searchhybrid` backend (#2237) and `cmd/search-bench` make them
achievable.

## 6. Observability Contract

Search-lane implementation must expose operator signals for:

- search-index build state (`disabled`, `lazy`, `warming`, `ready`, `failed`);
- build duration, document count, vector count, and artifact size;
- query duration by search mode and result count;
- truncation and timeout counts;
- projection input count, skipped-document count, and redaction/drop reason;
- first-query lazy-warm trigger count and failure class.

Until curated search-lane implementation lands, phase-1 stabilization has no
new Eshu runtime observability signal; it removes ambient NornicDB search-index
work from canonical graph startup and keeps the future search-lane telemetry
requirements explicit.

## 7. Implementation Breakdown

Recommended child slices:

1. Done: adopt a pinned NornicDB build with explicit BM25/vector disable and
   lazy warming fallback, then wire graph-lane Compose/Helm defaults with
   tests.
2. Define and fixture-test `EshuSearchDocument` projection without changing API
   or MCP contracts. The projection contract lives in `go/internal/searchdocs`
   and `docs/public/reference/search-document-projection.md`. The reducer
   read-model now persists curated documents as generation-scoped derived facts
   (`reducer_eshu_search_document`): `reducer.ProjectSearchDocuments` curates the
   bounded source set, `reducer.EshuSearchDocumentHandler` drives one intent,
   `reducer.PostgresEshuSearchDocumentWriter` upserts idempotently and retires
   stale documents, and `postgres.EshuSearchDocumentStore` reads back only the
   active generation. The `eshu_search_document` reducer domain is registered and
   its handler is wired; a decoupled periodic sweeper
   (`projector.SearchDocumentProjectionSweeper`, wired in `cmd/reducer`) enqueues
   one projection intent per repository generation that has indexed content but
   no projection yet (`postgres.EshuSearchDocumentPendingStore`). The content
   source loader (`postgres.EshuSearchDocumentSourceLoader`) reads the
   repository's current indexed content; the writer tags facts with the
   generation and the active-generation reader plus generation-scoped retirement
   keep the read model converged.

   Concurrency Evidence: the only contested resource is the reducer queue work
   item, keyed by `scope_id+generation_id+domain+entity` and inserted
   `ON CONFLICT (work_item_id) DO NOTHING`; re-enqueuing a still-pending scope
   each tick is a no-op and an advanced active generation yields a fresh work
   item, so the sweeper holds no lease and concurrent reducers converge on the
   same idempotent inserts. The handler's per-generation retire-not-in-set write
   is idempotent under retry. No-Regression Evidence: the projector
   per-generation projection path and its tests are unchanged; the sweeper is an
   additive background loop. Observability Evidence: the sweeper emits a
   structured `eshu search document projection sweep completed` log with
   pending-scope and enqueued-intent counts and duration, and the handler emits
   the canonical-write counter/duration plus its cycle log. Round-trip proof
   against the live corpus (env-gated `TestEshuSearchDocumentProjectionRoundTripLive`):
   2148 entities + 82 files loaded, 2183 documents curated, written, and read
   back through the active-generation store.
3. Partially done: add a benchmark harness comparing current Postgres content
   search with curated NornicDB BM25/vector retrieval. The pure evidence, suite,
   and scoring contract lives in `go/internal/searchbench` and
   `docs/public/reference/search-benchmark-evidence.md`. The Postgres keyword
   baseline adapter lives in `go/internal/searchpostgres`. The live execution
   layer now lives in `go/internal/searchbenchrun`: `RunSuite` drives a backend
   adapter across the query suite through the bounded retrieval runner, measures
   p50/p95 latency and accuracy, and `AssembleEvidence` produces a validated
   `search-benchmark-evidence/v1` record. The operator entrypoint
   `go/cmd/search-bench` runs the comparison over a live content corpus; a first
   measured run is recorded in §5.1 (decision: `defer_search_change`). A measured
   NornicDB-search arm still needs a search-enabled curated deployment, and a
   labeled query suite is still needed to record recall/ranking quality rather
   than latency alone.
4. Add bounded internal retrieval path for semantic-evaluation queries. The
   request/response contract lives in `go/internal/searchretrieval` and
   `docs/public/reference/search-retrieval-contract.md`. Backends: the Postgres
   keyword baseline (`go/internal/searchpostgres`), the NornicDB hybrid prototype
   (`go/internal/searchnornicdb`), and a pure-Go in-process hybrid backend
   (`go/internal/searchhybrid`) implementing BM25 plus optional local-embedding
   vectors fused with Reciprocal Rank Fusion, bounded with overflow signaling and
   deterministic top-K. The local embedding model is supplied through an
   `Embedder` port (hosted-free) and is the remaining follow-up.
5. Add public API/MCP search surfaces only after retrieval evidence proves value
   and preserves truth labels, scope, limits, and truncation.
6. Issue #2343 moves the public API/MCP surface off request-local index rebuilds
   and onto reducer-maintained Postgres BM25 postings for active curated search
   documents. Issue #2355 records a 227,196-document live cap sweep with a
   content-handle suite; the run rejects the old 500-document placeholder and
   supports leaving the persisted BM25 corpus uncapped for this corpus. A vector
   or NornicDB-search lane still needs separate backend evidence. Issue #2578
   records the production ANN/vector-index contract and approval checklist in
   [ANN Vector-Index Contract For Production Hybrid Search](2578-ann-vector-index-production-hybrid-search-contract.md);
   implementation must satisfy that gate before adding persisted vector state or
   changing runtime search behavior.

Issues #417, #418, #420, #421, and #431 should stay behind this architecture
gate. #417 can start once the bounded search document projection and backend
startup policy have proof. #431 is tracked separately in
[NornicDB Primary Knowledge Store Evaluation](431-nornicdb-primary-store-evaluation.md)
and should not evaluate replacing Postgres until the search lane has at least
one shadow-read and shadow-write comparison.

## 8. Evidence For This PR

No-Regression Evidence: the phase-1 stabilization is a graph backend runtime
contract change, not a fact, reducer, Cypher, schema, OpenAPI, MCP, or query
truth change. Compose and Helm now pin NornicDB `v1.1.11`, disable BM25/vector
search and embedding generation for the canonical graph lane, leave BM25/vector
warming lazy for deliberate proof runs, and disable search-index persistence.
Runtime package tests enforce those defaults and the public environment
reference so the old whole-graph BM25 startup policy cannot drift back
silently. The operational smoke and image evidence live in
[NornicDB Tuning Evidence](../../public/reference/nornicdb-tuning-evidence.md).

No-Observability-Change: this phase does not add an Eshu runtime signal because
it removes ambient NornicDB search-index work from the canonical graph startup
path. Operators diagnose the path with existing NornicDB search-build logs,
`/admin/stats`, `/nornicdb/embed/stats`, Eshu graph schema bootstrap logs,
graph write timeout details, and runtime queue/admin status. Future curated
BM25/vector retrieval must add the search-lane build, document-count,
vector-count, artifact-size, duration, and failure-class signals named in this
design before it becomes a supported production search surface.

No-Regression Evidence: the Postgres baseline adapter is internal benchmark
plumbing. It adds no API/MCP route, graph write, NornicDB call, schema change,
worker, queue, runtime flag, or default search behavior. It reuses the existing
Postgres content-store search methods and projects returned rows through
`searchdocs` so candidates remain repository-scoped, derived content-search
documents.

No-Observability-Change: the Postgres baseline adapter is not a steady-state
runtime path. Benchmark callers diagnose it through `searchretrieval.Runner`
observations for mode, scope anchor, duration, result count, truncation,
timeout, candidate truth-level counts, and failure classes, plus the existing
Postgres query instrumentation on the content store. Production search-lane
telemetry remains gated by Section 6 before any public surface exists.

Source check date: 2026-06-02.

Sources used:

- [NornicDB indexing memory docs](https://raw.githubusercontent.com/orneryd/NornicDB/main/docs/architecture/indexing-memory-large-datasets.md)
- [NornicDB search methodology docs](https://raw.githubusercontent.com/orneryd/NornicDB/main/docs/performance/searching.md)
- [NornicDB graph-only mode request](https://github.com/orneryd/NornicDB/issues/175)
- [NornicDB per-database search flag PR](https://github.com/orneryd/NornicDB/pull/177)
- [Eshu NornicDB tuning](../../public/reference/nornicdb-tuning.md)
- [Eshu NornicDB tuning evidence](../../public/reference/nornicdb-tuning-evidence.md)
