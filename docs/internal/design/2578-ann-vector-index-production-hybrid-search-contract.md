# ANN Vector-Index Contract For Production Hybrid Search

Status: proposed design gate for issue #2578 with storage-owner follow-up
issue #2582, parent #2451. This document does not approve implementation. It
records the contract and approval checklist that must be satisfied before Eshu
lands a production ANN/vector-index search lane.

Related architecture:

- [NornicDB Canonical Graph And Search Projection Split](430-nornicdb-graph-search-split.md)
- [Search Retrieval Contract](../../public/reference/search-retrieval-contract.md)
- [Search Benchmark Evidence](../../public/reference/search-benchmark-evidence.md)
- [Semantic Enrichment Posture](../../public/reference/semantic-enrichment-posture.md)

## Problem

Eshu now has deterministic, bounded retrieval over curated search documents:

- the default public route reads persisted Postgres BM25 postings for active
  curated search documents;
- the optional local hash embedder path builds an in-process vector index capped
  at 500 documents;
- `go/internal/searchhybrid` keeps exact cosine as the correctness baseline and
  offers an explicitly selected deterministic angular-LSH candidate index with
  exact reranking;
- NornicDB search remains disabled for the canonical graph lane.

That is enough for a deterministic hybrid-search slice, but not enough for
production ANN/vector readiness. A production vector lane introduces persisted
index state, rebuild/freshness semantics, operator-visible failure modes,
schema/storage ownership, and security/retention decisions that cannot be
smuggled in through a code PR.

## Decision

Eshu will treat production ANN/vector retrieval as a separate curated search
lane. It must not reuse the canonical graph database's whole-graph search
indexes, and it must not promote vector similarity, link prediction, or search
rank to canonical graph truth.

The first implementation target is a Postgres-owned persisted vector lane over
active `EshuSearchDocument` rows, scoped by repository generation. Postgres is
the storage owner for the first production slice because it already owns durable
facts, content, read models, status, and active-generation visibility. That
keeps vector rollback as a read-model disablement instead of a canonical graph
operation, and it lets the initial implementation join through the same active
search-document scope as the existing BM25 path.

The accepted options stay narrow, but only the Postgres sidecar path is selected
for the first slice:

| Option | Use when | Required proof |
| --- | --- | --- |
| Postgres sidecar tables | **Chosen first path.** The first slice needs freshness and rollback parity with the existing BM25 index and can start with exact or staged ANN behavior behind `searchretrieval.Backend`. | DDL review, active-generation joins, rebuild idempotency, and latency/recall evidence on the benchmark suite. |
| Separate NornicDB search database or namespace | Later, if NornicDB provides measured vector/hybrid value without delaying canonical graph readiness. | Search flags, startup, preserved-volume, lazy-warm, artifact, memory, and query evidence. |
| Separate search service or store behind an adapter | Later, if a dedicated ANN engine is needed for target scale or isolation. | Security, egress, backup/restore, authorization, cost, and operator burden review before code. |

Rejected for the first slice:

- **Canonical graph NornicDB search indexes:** rejected because graph readiness
  must not depend on vector-index startup, rebuild, artifact loading, or
  embedding work. Search rank remains derived retrieval evidence, not canonical
  graph truth.
- **Separate NornicDB search database or namespace:** deferred until a measured
  proof shows vector/hybrid value that justifies separate startup, volume,
  lazy-warm, artifact, and operational evidence.
- **External vector store:** deferred because the first production slice should
  not introduce provider egress, backup/restore, tenant isolation, credential,
  or cost burden before local storage proves the search contract.

The canonical graph lane remains graph-only by default:

- `NORNICDB_SEARCH_BM25_ENABLED=false`
- `NORNICDB_SEARCH_VECTOR_ENABLED=false`
- `NORNICDB_EMBEDDING_ENABLED=false`
- `NORNICDB_PERSIST_SEARCH_INDEXES=false`

Changing those defaults is out of scope for the first production ANN slice.

## Non-Goals

- No vector index implementation in this design gate.
- No public API, MCP, OpenAPI, or wire-contract change.
- No hosted provider adapter or credential path.
- No raw prompt, response, source chunk, provider payload, trace, log, or
  dashboard retention change.
- No canonical graph write, reducer materialization, or Cypher schema change.
- No default NornicDB BM25/vector enablement for the canonical graph database.

## Storage Contract

The first implementation storage owner is `go/internal/storage/postgres`.
Implementation issues may add a narrow package under `go/internal/search*` for
ranking or adapter code, but durable vector metadata, build state, active
generation visibility, retry state, and cleanup state belong in Postgres.

The first Postgres-backed schema must include:

- vector metadata and build-state tables scoped by `scope_id` and
  `generation_id`;
- generated or stored vector values behind the existing curated search-document
  contract;
- index/build version fields so the implementation can rebuild without
  changing API semantics;
- a small status view or query surface that API, MCP, and admin status can use
  without scanning raw vectors.

NornicDB search or an external ANN store may be added later only behind
`searchretrieval.Backend` after a separate issue records why Postgres is
insufficient for the measured corpus and how the new store preserves the same
authorization, freshness, truth, and rollback semantics.

Required identity and lifecycle fields:

- `scope_id` and `generation_id`, aligned with active curated search documents;
- stable `document_id`;
- embedding model id and embedding dimensions;
- embedding content hash;
- vector/index version;
- build state: `disabled`, `queued`, `building`, `ready`, `failed`, `stale`;
- low-cardinality failure class;
- created, updated, and last-success timestamps.

All reads must join through the active generation. Superseded generation vector
rows may exist until cleanup, but they must be invisible to request-time
retrieval. Rollback must be disabling Postgres vector retrieval or dropping the
vector read path from `hybrid` without disabling keyword BM25 search.

Migration and rollback:

- add Postgres vector metadata/build-state DDL behind schema approval;
- leave existing BM25 search-document reads unchanged during migration;
- build vectors for active generations through durable queued or replayable
  work, not request-time rebuilds;
- expose vector state as disabled until the build reaches `ready`;
- rollback by setting the vector lane to `disabled` and serving BM25-only
  `hybrid_degraded` responses from the unchanged search-document index;
- cleanup superseded generation vector rows asynchronously, with bounded retry
  and failure class reporting.

## Freshness And Rebuild Contract

Vector state is derived read-model state. It must converge from durable curated
search documents and remain replayable.

The implementation must specify:

- trigger path for initial build and rebuild;
- idempotency key for each build unit;
- retry and dead-letter behavior;
- cleanup of stale generations;
- behavior when indexed documents exist but vector rows or stats are missing;
- behavior when embeddings are built with an old model id or dimensions;
- status surfaced when a rebuild is in progress.

Request-time behavior must be fail-closed and explicit:

| State | `semantic` | `hybrid` |
| --- | --- | --- |
| vector disabled | `backend_unavailable` | BM25 with `retrieval_state=hybrid_degraded` |
| vector building/stale | `backend_unavailable` or empty, with failure class | BM25 with degraded retrieval state |
| vector ready | vector ranking | RRF or equivalent fused ranking |
| vector failed | `backend_unavailable` with failure class | BM25 with degraded retrieval state |

The current persisted BM25 path must stay available when vector retrieval is
disabled, rebuilding, or failed.

## Query Contract

Production ANN retrieval must preserve the existing bounded route semantics:

- require `repo_id`, `query`, `mode`, `limit`, and `timeout_ms`;
- cap returned results at the existing route limit;
- resolve the smallest scope before retrieval;
- apply scoped-token authorization before storage access;
- return deterministic top-K ordering;
- report truncation, retrieval state, indexed document count, corpus limit,
  corpus truncation, search method, freshness, and truth labels;
- return only derived/read-model search documents with stable graph handles.

If a backend cannot pre-filter by the smallest scope anchor, the design must
record the accepted post-filter behavior, overfetch bound, and false-negative
risk before implementation. Whole-graph vector search is not allowed.

## Truth Contract

Search remains retrieval evidence:

- vector similarity is not canonical graph truth;
- a ranked candidate cannot create or delete graph edges;
- semantic hints remain provenance-only until deterministic evidence admits
  them through the owning reducer path;
- every result must carry `truth_scope.level=derived`;
- benchmark and runtime responses must count false canonical claims instead of
  suppressing them.

Any nonzero false-canonical-claim count is a correctness failure for an
implementation PR.

## Security And Retention Gate

Schema approval is required before any persisted vector table, index, queue, or
DDL lands.

Security approval is required before any of these land:

- hosted embedder or provider adapter;
- provider credential, egress, or workload identity path;
- retention of prompt text, provider response text, raw source chunks, or
  sensitive content beyond existing curated search-document fields;
- external ANN service with separate auth, network, backup, or tenant boundary.

Default retention posture for the first production slice:

- store vector rows and embedding metadata only;
- store model id, dimensions, content hash, and failure class;
- do not store provider prompts or responses;
- do not expose source identifiers or repository paths as metric labels.

## Benchmark Gate

The first implementation PR must record a measured comparison before enabling
production vector retrieval.

Required corpus evidence:

- Eshu commit;
- backend image or commit;
- schema/bootstrap state;
- clean-volume and preserved-volume startup behavior when NornicDB is involved;
- repository count, file count, entity count, document count, vector count, and
  source-kind distribution;
- embedding model id, dimensions, and vector count;
- index build duration, rebuild duration, and artifact size;
- memory high-water mark;
- query suite version and at least 15 labeled queries;
- p50 and p95 latency by mode;
- recall, precision, nDCG, and false canonical claim count;
- timeout, truncation, disabled-search, rebuild, lazy-warm, missing-artifact,
  and corruption failure classes.

Stop thresholds:

- zero false canonical claims is mandatory;
- candidate recall must improve over the current Postgres/BM25 baseline for the
  measured semantic or hybrid mode;
- p95 latency must stay within the threshold recorded in the benchmark plan or
  the PR must choose `defer_search_change`;
- canonical graph readiness must not depend on vector-index startup, rebuild, or
  artifact load;
- default BM25 keyword retrieval must not regress when vector retrieval is off.

## Observability Gate

Implementation must bridge retrieval and build state into operator-visible
signals before production enablement.

Required low-cardinality signals:

- index build state;
- build duration;
- indexed document count;
- vector count;
- artifact size;
- rebuild count and failure class;
- query duration by mode and retrieval state;
- result count;
- truncation and timeout counts;
- stale, disabled, degraded, and failed retrieval counts.

High-cardinality values such as repository ids, document ids, graph handles,
paths, source ids, provider request ids, and content hashes must stay in logs or
spans when needed, not metric labels.

## Approval Checklist

Before implementation starts:

- Schema owner approves storage owner, DDL, active-generation visibility, and
  cleanup behavior.
- Security owner approves provider/credential/egress/retention posture, or the
  implementation explicitly stays local and hosted-free.
- Query owner approves API/MCP response-state semantics.
- Runtime owner approves rebuild, rollback, and observability behavior.
- Benchmark owner approves the query suite, corpus, thresholds, and stop
  reasons.

## Follow-Up Implementation Shape

After this gate is accepted, implementation should be split into small issues:

1. Add vector-index storage metadata and active-generation read contract.
2. Add deterministic local embedding build/rebuild path over curated search
   documents.
3. Add ANN retrieval adapter behind `searchretrieval.Backend`.
4. Add runtime status and telemetry.
5. Run the benchmark suite and decide `keep_postgres_search`,
   `add_nornicdb_search_lane`, or `defer_search_change`.

Each issue must keep API/MCP behavior explicit and must cite the benchmark and
observability evidence it actually produces.
