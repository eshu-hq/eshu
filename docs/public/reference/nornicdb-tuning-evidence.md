# NornicDB Tuning Evidence

This page keeps durable NornicDB performance lessons without making
[NornicDB Tuning](nornicdb-tuning.md) hard to scan.

Use it to understand why current defaults exist. Do not treat old checkpoint
numbers as current acceptance evidence unless you rerun the same proof on the
current Eshu and NornicDB commits.

## Current Baselines

| Date / commit | Scope | Result | Durable lesson |
| --- | --- | --- | --- |
| 2026-05-04 latest-main | Full corpus | `8458/8458` queue rows drained in `878s`; pending, in-flight, retrying, failed, and dead-letter rows all `0`; API/MCP relationship-evidence drilldowns passed. | NornicDB is the default backend baseline; later NornicDB-only full-corpus runs are regression evidence, not a substitute for Neo4j parity decisions. |
| Eshu `c598000d` | Targeted five-repo lane | Drained healthy in `854s`; largest PHP stress repos projected `148,948` and `176,201` facts; semantic reducers completed in `6.33s` and `15.76s`. | Use focused problem-repo proof before scaling to representative subsets. |
| Eshu `5c9b169a`, NornicDB `86e78f1` | 50-repo subset | Drained in `884s`; no graph timeout, semantic failure, acceptance cap, retry, dead-letter, panic, or fatal lines. | Remaining pain was high-cardinality source-local canonical entity writes, not semantic batch caps. |
| Eshu `a7078ddf`, NornicDB `v1.0.43` | 23-repo medium corpus | Drained in about `3m11s`; projector `23/23`; reducer `184` succeeded; queue terminal clean. | Source-cache shaping promoted from focused proof to medium-corpus proof; next target returned to canonical graph Cypher shape and NornicDB lookup behavior. |

## Canonical Entity Writes

`php-large-repo-b` exposed the canonical entity write bottleneck. It discovered
`74,475` files, emitted `176,201` facts, and reached `Variable=131,977`,
`Function=28,926`, `Class=6`.

The durable defaults came from focused A/B runs:

| Setting | Result | Decision |
| --- | --- | --- |
| `ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT=true` | Variable writes used `batch_across_files=true`; `131,977` rows wrote as `13,198` statements and `2,640` grouped executions with no singleton fallbacks. | Cross-file batched entity containment became the default. |
| `Variable=10` row cap | `131,977` rows completed in `196.713s`. | Too conservative after file-scoped entity batching. |
| `Variable=25` row cap | Completed in `130.082s`. | Better, but still fragmented. |
| `Variable=50` row cap | Completed in `118.136s`. | Better, still not best. |
| `Variable=100` row cap | Completed in `102.820s`; max grouped execution `0.607s`; queue terminal clean. | `Variable=100` became the built-in default. |
| `Variable=5` grouped-statement cap | Kept grouped execution around 500 Variable rows. | Best proven default. |
| `Variable=10` grouped-statement cap | Safe but marginally faster on the focused repo. | Experiment candidate only. |
| `Variable=25` grouped-statement cap | Early Variable chunks were clearly slower. | Do not promote. |

Later direct edge-between index proof moved relationship-existence lookup from
outgoing-edge scans to indexed `(start,end,type)` checks. The same large PHP
repo then drained healthy; canonical files completed in `1.496s`, Function in
`6.053s`, and `118,768` Variable rows in `62.340s` with max grouped execution
`0.437s`.

The medium-corpus edge-index proof drained 23 repos cleanly. File relationship
writes stayed bounded: canonical file phase completed 52 statements in
`1.683s`. The lesson is specific: the edge index fixes relationship-existence
slope, but high-cardinality entity volume still needs measured label summaries.

## Inheritance Edge Routing

A 2026-05-21 hosted E2E subset first proved an OCI registry timeout fix but
exposed a separate inheritance edge tail. Before typed inheritance routing, one
700-edge `inheritance_edges` write used broad endpoint labels and completed as
two grouped writes in `392.562s` and `159.949s`. Pprof showed NornicDB spending
the active write under `executeUnwindMergeChainBatch -> findMergeNodeAnyLabel
-> GetNodesByLabel` while the reducer waited on Bolt.

After routing inheritance rows by concrete child and parent labels, the same
one-repo subset drained queue-zero and wrote the same 700 inheritance edges as
four concrete routes in `0.005917s`, `0.005129s`, `0.003794s`, and
`0.000529s`; the full `inheritance_materialization` item completed in
`1.707s`.

Performance Evidence: focused hosted E2E subset, NornicDB
`nornicdb-cpu-bge:match-merge-on-create-route`, clean volumes, and
`ESHU_CANONICAL_WRITE_TIMEOUT=120s`. Before typed routing, one 700-edge
inheritance item took `554.283s`; after typed routing, the same repo's
inheritance item took `1.707s` with queue `pending=0 in_flight=0 retrying=0
dead_letter=0 failed=0`.

Observability Evidence: shared-edge logs include `statement_summaries` for
`inheritance_edges`, including relationship type, child label, parent label,
and row count. Pprof remains available for NornicDB and reducer during hosted
subset runs to verify whether the backend is scanning broad labels or using
schema-backed concrete-label lookups.

## Content Store Lessons

After graph-write tuning, `php-large-repo-b` showed Postgres content
persistence as the largest source-local stage:

- `prepare_entities`: `0.117s`
- `upsert_entities`: `158.293s`
- rows: `160,909`
- batches: `537`

Changing `ESHU_CONTENT_ENTITY_BATCH_SIZE` to `600` reduced statements to `269`,
but `upsert_entities` stayed flat at `158.814s`. A direct Postgres microbench
isolated the real cost:

| Shape | Time |
| --- | --- |
| copy `160,909` rows without indexes | `1.661s` |
| with btree lookup indexes | `2.827s` |
| with `content_entities_source_trgm_idx` | `132.174s` |

The cause was not batch size. The repo carried about `1.108 GB` of Variable
`source_cache`, mostly generated/vendor-style assignments. Eshu now bounds
oversized Variable snippets at `4 KiB` and records truncation metadata:

- `source_cache_truncated`
- `source_cache_original_bytes`
- `source_cache_limit_bytes`

The follow-up runtime proof on Eshu `f8322c41` drained the same repo healthy:

- `upsert_entities`: `31.956s`
- total content write: `43.762s`
- total source-local projection: `165.604s`
- canonical graph write: `120.248s`
- persisted content entities: `160,909`
- truncated oversized Variable rows: `37,288`
- total entity `source_cache`: `164 MB`

The durable lesson: source-cache shaping fixed this bottleneck.
`ESHU_CONTENT_ENTITY_BATCH_SIZE` is a diagnostic knob, not the answer to graph
timeouts or trigram-maintenance dominated writes.

When local-authoritative bulk-load proofs still show content trigram index
maintenance as the long pole,
`ESHU_LOCAL_AUTHORITATIVE_DEFER_CONTENT_SEARCH_INDEXES=true` can defer
`content_files.content` and `content_entities.source_cache` trigram indexes
during initial writes and rebuild them after clean drain. Keep that as a local
proof/load knob, not a deployed Postgres schema default.

## Search Index Startup Persistence

Default Compose and Helm set `NORNICDB_PERSIST_SEARCH_INDEXES=true`.

Without persisted search indexes, NornicDB rebuilds search indexes by scanning
the persisted graph on startup. On the 2026-05-15 remote full-corpus recovery,
a reboot left NornicDB reporting HTTP health while it rebuilt `2,279,280`
search-index nodes. `eshu-bootstrap-data-plane` had applied Postgres schema and
then waited behind graph schema work for more than 20 minutes.

No-Regression Evidence: Compose pins
`timothyswt/nornicdb-cpu-bge:v1.1.0@sha256:65855ca2c9649020f7f9e29d2e0fbedf0bf9601457de233d87160ddbe4b473f0`.
That tag resolves to linux/amd64 digest
`sha256:159f988a6987e9ab55ea822520c50bd5ef7a77068eaab80c4696d8905c7754a7`
and linux/arm64 digest
`sha256:be0374a0cc7bfbbf8830d303ee2b51c7e3d629f4539cce6a98a718615f87d1ca`.

Observability Evidence: NornicDB logs expose `BuildIndexes progress` with
phase and processed-node counts. Eshu graph schema bootstrap logs each graph
DDL statement before and after execution and bounds each statement with
`ESHU_GRAPH_SCHEMA_STATEMENT_TIMEOUT`, so recurrence should name the stuck
schema phase and statement.

## Embeddings During Indexing

Eshu Compose and Helm set `NORNICDB_EMBEDDING_ENABLED=false` even if the pinned
NornicDB image carries an embedding-enabled image environment.

Full-corpus indexing writes millions of graph nodes and does not need vector
embeddings for correctness. Leaving the auto-embed worker on during indexing
loads the local embedding model and scans nodes while canonical writes compete
for CPU and storage.

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

EKS startup-loop evidence: readiness probes hit port 7474 while NornicDB was
still scanning `2,279,280` nodes to rebuild search indexes. The scan was
CPU-bound and exceeded 20 minutes, so readiness failures killed the pod before
indexing completed. Persisted indexes plus the startup probe prevent that
restart loop.

Cloud write-timeout evidence: EKS pod-to-pod NornicDB latency under load
exceeded the local 30s write timeout. Full- and medium-corpus checkpoints used
`ESHU_CANONICAL_WRITE_TIMEOUT=120s` and NornicDB `NumCPU` reducer defaults on
an 8-core host. Helm makes timeout and shared-projection worker count explicit;
reducer worker count remains runtime-owned unless set.

Observability Evidence: `graph_write_timeout` failures preserve
`failure_details` naming phase, label, and row count. Worker settings surface
in startup logs and active-worker gauges.
