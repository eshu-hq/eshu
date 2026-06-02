# NornicDB Tuning Evidence

This page keeps durable NornicDB performance lessons without making
[NornicDB Tuning](nornicdb-tuning.md) hard to scan.

Use it to understand why current defaults exist. Do not treat old checkpoint
numbers as current acceptance evidence unless you rerun the same proof on the
current Eshu and NornicDB commits.

## Current Baselines

| Scope | Durable result | Lesson |
| --- | --- | --- |
| Full corpus, latest-main checkpoint | `8458/8458` queue rows drained in `878s`; pending, in-flight, retrying, failed, and dead-letter rows all `0`; API/MCP relationship-evidence checks passed. | NornicDB is the default backend baseline. Later NornicDB-only full-corpus runs are regression evidence, not a substitute for Neo4j parity decisions. |
| Five-repo and 50-repo lanes | Large PHP stress repos reached `148,948` and `176,201` facts; the 50-repo subset drained in `884s` with no timeout, retry, dead-letter, panic, or fatal lines. | Prove focused problem repos and representative subsets before scaling to full corpus. |
| 23-repo medium corpus | Drained in about `3m11s`; projector `23/23`; reducer `184` succeeded; queue terminal clean. | Source-cache shaping and edge-index work had medium-corpus proof before promotion. |

The acceptance shape is more important than the raw date: schema applied before
indexing, clean terminal queues, no retry/dead-letter drift, and API/MCP truth
checks against the completed graph.

## Canonical Entity Writes

`php-large-repo-b` exposed the canonical entity write bottleneck: `74,475`
files, `176,201` facts, `Variable=131,977`, `Function=28,926`, and `Class=6`.

Durable decisions:

| Decision | Evidence |
| --- | --- |
| Keep `ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT=true`. | Cross-file batched entity containment wrote `131,977` Variable rows as `13,198` statements and `2,640` grouped executions with no singleton fallbacks. |
| Keep `Variable=100` as the built-in row cap. | Focused A/B runs improved from `196.713s` at `Variable=10` to `102.820s` at `Variable=100`; max grouped execution stayed around `0.607s` and the queue drained cleanly. |
| Keep `Variable=5` as the grouped-statement cap. | It bounds one grouped execution near 500 Variable rows. `Variable=10` stayed experimental; `Variable=25` made early chunks slower. |
| Keep direct edge-between indexes in the graph schema. | Relationship-existence lookup moved from outgoing-edge scans to indexed `(start,end,type)` checks. The same large PHP repo then wrote `118,768` Variable rows in `62.340s` with max grouped execution `0.437s`. |

The edge index fixes relationship-existence slope. High-cardinality entity
volume still needs measured label summaries before changing caps.

## Inheritance Edge Routing

Before typed inheritance routing, one 700-edge `inheritance_edges` write used
broad endpoint labels and took `392.562s` plus `159.949s`. Pprof showed
NornicDB under `executeUnwindMergeChainBatch -> findMergeNodeAnyLabel ->
GetNodesByLabel` while the reducer waited on Bolt.

Typed routing by concrete child and parent labels wrote the same 700 edges as
four concrete routes in milliseconds, and the full
`inheritance_materialization` item completed in `1.707s`.

Performance Evidence: focused hosted E2E subset, NornicDB
`nornicdb-cpu-bge:match-merge-on-create-route`, clean volumes, and
`ESHU_CANONICAL_WRITE_TIMEOUT=120s`. Before typed routing, one 700-edge
inheritance item took `554.283s`; after typed routing, the same repo's
inheritance item took `1.707s` with queue `pending=0 in_flight=0 retrying=0
dead_letter=0 failed=0`.

Observability Evidence: shared-edge logs include `statement_summaries` for
`inheritance_edges`, including relationship type, child label, parent label,
and row count. Pprof remains available for NornicDB and reducer during hosted
subset runs to verify broad-label scans versus schema-backed concrete-label
lookups.

## Content Store Lessons

After graph-write tuning, `php-large-repo-b` showed Postgres content
persistence as the largest source-local stage: `160,909` rows, `537` batches,
and `upsert_entities=158.293s`.

Durable decisions:

| Decision | Evidence |
| --- | --- |
| Treat `ESHU_CONTENT_ENTITY_BATCH_SIZE` as diagnostic. | Raising the batch size to `600` reduced statements to `269`, but `upsert_entities` stayed flat at `158.814s`. |
| Bound oversized Variable snippets at `4 KiB`. | A direct Postgres microbench isolated `content_entities_source_trgm_idx` as the cost driver: `132.174s` with trigram index versus `2.827s` with btree lookup indexes. Source-cache shaping reduced total entity `source_cache` from about `1.108 GB` to `164 MB`. |
| Keep truncation metadata. | Oversized rows record `source_cache_truncated`, `source_cache_original_bytes`, and `source_cache_limit_bytes`. |
| Keep `ESHU_LOCAL_AUTHORITATIVE_DEFER_CONTENT_SEARCH_INDEXES` local-only. | It can defer expensive trigram indexes during local-authoritative bulk load and rebuild after clean drain, but it is not a deployed Postgres schema default. |

The follow-up proof on the same repo drained healthy with
`upsert_entities=31.956s`, total content write `43.762s`, total source-local
projection `165.604s`, canonical graph write `120.248s`, and `37,288`
truncated Variable rows.

## Search Index And Embedding Defaults

Compose and Helm set:

- `NORNICDB_PERSIST_SEARCH_INDEXES=true`
- `NORNICDB_EMBEDDING_ENABLED=false`

These settings are the current graph-lane mitigation for the pinned image, not
a decision that the canonical graph is Eshu's BM25/vector search corpus. Issue
#430 separates canonical graph storage from curated search projection. Future
work that disables or lazily warms BM25/vector indexes for the canonical graph
must pin the supporting NornicDB image and record startup, memory, artifact-size,
document-count, vector-count, and failure-mode evidence before changing defaults.

No-Regression Evidence: Compose pins
`timothyswt/nornicdb-cpu-bge:v1.1.0@sha256:65855ca2c9649020f7f9e29d2e0fbedf0bf9601457de233d87160ddbe4b473f0`.

Observability Evidence: NornicDB logs expose `BuildIndexes progress` with
phase and processed-node counts. Eshu graph schema bootstrap logs each graph
DDL statement before and after execution and bounds each statement with
`ESHU_GRAPH_SCHEMA_STATEMENT_TIMEOUT`, so recurrence should name the stuck
schema phase and statement.

No-Regression Evidence: the 2026-05-15 remote full-corpus recovery completed
correctly at 896 repositories and `8344/8344` queue rows even though NornicDB
logs showed local `bge-m3` embeddings enabled and the auto-embed worker active.
Disabling embeddings by default preserves graph writes and search-index
persistence while removing background vector generation Eshu does not query
during indexing.

Observability Evidence: NornicDB startup logs print whether embeddings are
enabled, which provider/model is selected, and whether the embed worker starts.
The Prometheus surface also exposes embedding worker and processed/failed
counters.

## EKS Defaults

The Helm chart promotes proven Compose behavior to Kubernetes:

- `NORNICDB_PERSIST_SEARCH_INDEXES=true`
- `NORNICDB_EMBEDDING_ENABLED=false`
- startup probe window long enough for large graph startup
- readiness probe against `/health`
- `ESHU_CANONICAL_WRITE_TIMEOUT=120s`
- `ESHU_SHARED_PROJECTION_WORKERS=8`

Persisted search indexes plus the startup probe prevent restart loops while
NornicDB rebuilds large search indexes. `ESHU_CANONICAL_WRITE_TIMEOUT=120s`
covers EKS pod-to-pod graph-write latency under load; reducer worker count
remains runtime-owned unless explicitly set.

Observability Evidence: `graph_write_timeout` failures preserve
`failure_details` naming phase, label, and row count. Worker settings surface
in startup logs and active-worker gauges.
