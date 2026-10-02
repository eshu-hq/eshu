# #7249 dead-code reachability read proof

## Scope

Read-only PostgreSQL 18 probes used a 12,403-file repository and the `find_dead_code` argument shape (`repo_id`, `limit=12`, no language). The deployed MCP trace below used a different 7,097-file repository, so its endpoint time is not a paired measurement for these SQL probes. The first Function candidate page had 121 unique entity IDs. Those IDs are before application policy, duplicate filtering, and exclusions, so the SQL probe is representative of the first page rather than a claim about the exact dispatched ID set. No production DDL, data write, `ANALYZE`, or setting change was made. Private repository and entity names were withheld.

## Observation and theory

The shipped `CodeReachabilityIncomingEntityIDs` query joined active scopes before finding candidate reachability rows. Its first observed `EXPLAIN (ANALYZE, BUFFERS)` drove 814 active scopes into 914 primary-key index searches, returned zero active rows for this page, and took 1,738.022 ms with 4,744 shared reads. For the separate 7,097-file deployed MCP request, Tempo reported a 2,095 ms reachability span near a 2.536-second first-observed call with the same argument shape. The spans have separate roots, so their request-level attribution is an inference; the trace and SQL times are not subtracted as endpoint proof.

A read-only row-first materialized CTE fetched reachability rows by the 121 candidate entity IDs before joining active scopes and generations. The first observed plan used 121 entity-leading index searches, 189 shared reads, and 114.162 ms. This avoids the scope-first index search fanout. It retains the cross-repository consumer read; a scoped caller still projects grant membership rather than filtering away hidden consumers.

## Query shapes

The shipped query binds each candidate ID, then joins active scopes and generations before applying `row.entity_id IN ($1, ..., $K)` and `row.depth > 0`. The tested rewrite materializes the entity-ID and depth-filtered reachability rows first, then applies the same active-scope and active-generation joins and outer `DISTINCT`. A scoped request continues to project `(candidate_rows.repository_id = ANY($K+1))` as a result column; it does not put the grant in `WHERE`. The unscoped form projects only the two original result columns.

## Row truth and repeat measurement

A second probe combined the 121 page IDs with 20 distinct IDs known to have active incoming rows. Both SQL shapes returned the same 20 distinct `(entity_id, min_resolution_method)` rows. Bidirectional `EXCEPT ALL` returned zero rows in both directions. With those 141 unique IDs, two observed baseline plans took 257.590 and 259.800 ms; two row-first plans took 4.403 and 3.079 ms. The observed plans were:

| IDs | Shape | Result rows | Index searches | Shared reads | Execution |
| ---: | --- | ---: | ---: | ---: | ---: |
| 121 | shipped, first observation | 0 | 914 | 4,744 | 1,738.022 ms |
| 121 | row-first, first observation | 0 | 121 | 189 | 114.162 ms |
| 141 | shipped, repeat 1 | 20 | 914 | 0 | 257.590 ms |
| 141 | row-first, repeat 1 | 20 | 127 entity searches plus 20 generation lookups | 203 | 4.403 ms |
| 141 | row-first, repeat 2 | 20 | 127 entity searches plus 20 generation lookups | 0 | 3.079 ms |
| 141 | shipped, repeat 2 | 20 | 914 | 0 | 259.800 ms |

The same 141-ID set with a one-repository consumer grant returned 20 rows with 20 hidden-consumer markers from both shapes; bidirectional `EXCEPT ALL` was zero in both directions. The scoped plans took 529.817 ms (shipped) and 15.746 ms (row-first). A second 7,097-file repository supplied another 121-ID Function page: both shapes returned zero active rows with zero bidirectional differences, and the observed plans took 600.785 ms (shipped) versus 3.501 ms (row-first).

Performance Evidence: These are PostgreSQL statement times under the observed cache state, not cold or warm HTTP/MCP p95. The first observations are sequential and do not share identical cache state. The row-first shape changes no table or index and adds no writer cost. The existing entity-leading index served the measured row-first plans, so an additional index is not justified by this evidence.

No-Observability-Change: The existing `postgres.query` span retains `db.operation=code_reachability_incoming_entity_ids`, errors, and duration for this statement. The rewrite changes only SQL text and does not add a worker, queue stage, metric, or status field.

## Remaining proof

A Go regression failed on the shipped scope-first SQL and passes on the row-first SQL. The full `go test ./internal/query -count=1` package suite passed after the change; deliberate removal of the active-generation join and addition of a consumer-repository filter each failed the provenance test. Final diff review, local promotion gates, hosted CI, and a deployed same-argument cold/warm p95 sweep are separate gates. Keep #7249 open until the owner deploys the merged change and replays the original eight real argument sets across all seven dead-code endpoints, including the 7,097-file `find_dead_code(repo_id, limit=12, no language)` timeout case and the largest-repository cold case. Cold and warm p95 must each be below 1 second on the same arguments; the SQL probes here do not establish that endpoint result.

## Cross-repo consumer-evidence page (`POST /api/v0/code/dead-code/cross-repo`)

### Observation

The evidence page `buildCrossRepoDeadCodeConsumerEvidenceQuery` sends (`go/internal/query/content_reader_dead_code_cross_repo.go`) bound one placeholder per producer entity and joined `ingestion_scopes` and `scope_generations` above one ordered scan of `code_reachability_rows`. On the QA replica (PostgreSQL 18.3, 699,779 reachability rows, 819 active ingestion scopes, about 78k distinct entities), the 250-ID Function page of the 12,403-file repository planned as a hash join of every active scope with its generation, then probed `code_reachability_rows_pkey` once per scope. That took 1,305.1 ms first (shared hit 4,447, read 2,288) and 588.3 ms warm (hit 6,727). The 101-ID page took the same shape, at 254.5 and 253.0 ms with 6,744 buffers.

### Shape

An unscoped read -- one with no consumer-repository list, the #7249 acceptance path -- now takes a per-entity `LATERAL`:

- `$2` binds the page as one `text[]`, so the statement text is fixed for every page.
- Each page entity's consumers are ranked by an Index Only Scan of migration 103's `code_reachability_entity_confidence_rank_idx`, ordered by that index's key after the pinned `entity_id`, under a per-entity `LIMIT 1001`. The page keeps the same global `ORDER BY` and the same 1,001-row sentinel, so the per-entity cap is lossless.
- The active-scope and active-generation tests are per-row scalar subqueries, not joins, so they cannot drive the plan.
- The columns the index lacks are fetched per returned row, joined on all five primary-key columns plus `depth`.

A grant-bound read keeps the previous statement byte for byte. `TestCrossRepoDeadCodeGrantBoundPageKeepsTheShippedStatement` pins it.

No index, migration, or schema change.

### Why the lateral stays page-bound

A lateral is planned from the average fan-in per entity, not the busy entity's, so "another index plus a sort" is always a candidate. The ranking stays ordered because the walk is index-only and every alternative needs the heap. The planner prices an Index Only Scan by the share of the table's pages marked all-visible, so this holds while autovacuum keeps them so: 54,033 of 55,185 pages (97.9%) on the QA replica, read from `pg_class.relallvisible`, and the live guards run on a vacuumed fixture. A page range a fresh snapshot rewrite has not yet had vacuumed needs heap reads, which can tip the plan toward "another index plus a sort"; that state was not measured and is NOT_CHECKED.

The ranking walk is bounded per entity by the cap times (1 + retained generations), because the liveness test discards superseded entries after reading them. An entity that crosses the cap is read in full to its boundary, which is why the measured 26,250 entries on the 250-ID busy-entity page exceed 1,001 times 25.

The full-row fetch identifies one row by its five primary-key columns. A writer invariant bounds the index entries it reads when the planner serves it from an index whose key lacks `root_entity_id`. The only production writer, `CodeReachabilityStore.ReplaceRepositoryRows`, replaces one (scope, generation, repository) snapshot. `codeintel.BuildCodeReachabilityRowsWithStats` keeps one row per entity per snapshot (`codeReachabilityKeepBest`). `TestBuildCodeReachabilityRowsEmitsOneRowPerEntityPerSnapshot` pins that invariant, and keying `codeReachabilityKeepBest` by root and entity fails it.

### Grant-bound reads

Neither shape is bounded under a grant. These numbers are from the local fixture below, on the 250-ID page with the busy entity:

- With no grant, the lateral ranks on the rank index.
- With a 3-repository grant it plans index 101 plus a sort. That reads up to |grant| × scopes × (1 + R) entries per entity.
- From 50 granted repositories up (50, 300, and 1,500 measured) it plans `code_reachability_entity_lookup_idx` plus a sort, which reads the entity's whole fan-in.
- The previous statement also read the whole page's 38,742 rows under a 1,500-repository grant (bitmap heap scan in the custom plan, `code_reachability_root_idx` in the generic plan).
- The lateral cost more buffers than the previous statement in every grant cell, through its per-row `ingestion_scopes` probe:
  - 3 repositories: 1,072 against 91 (custom) and 1,072 against 313 (generic).
  - 1,500 repositories: 133,931 against 10,764 (custom) and 166,351 against 160,884 (generic).

That is why grant-bound reads keep the previous statement. The grant-bound read is a pre-existing, documented limit, not something this change bounds. The live proof's wide arm shows the lateral, with the grant bound ahead of its per-entity LIMIT, returns the same rows in the same order as the grant-bound statement for a 3-repository grant and a 1,500-repository grant (the latter crossing the 1,001-row cap).

### Local fixture

A disposable PostgreSQL 18.6 container, with the shipped migrations 001, 002, 027, 101, 102 and 103. The data is writer-conformant: one row per entity per (scope, generation, repository).

- 1,500 ingestion scopes, one consumer repository each, with 1 active and 24 retained generations (37,500 generations).
- 40,000 producer entities, entity n under 1 + n%9 generations.
- One busy entity under every scope and generation.
- 237,494 rows in all, inserted generation by generation in writer order, then `VACUUM ANALYZE`, `jit = off`.

Inserting in entity order instead (`entity_id` correlation 1.0, where the QA replica reads 0.02) moved the lateral to index 101 plus a sort.

On the 250-ID unscoped page with the busy entity (1,001 rows returned):

| Shape | Plan | Time | Shared buffers | Driving work |
| --- | --- | ---: | ---: | --- |
| previous | custom | 38.2 ms | 6,208 | hash join of 1,500 scopes, 1,500 primary-key probes |
| previous | generic | 11.6 ms | 10,934 | index 101 by entity array, hash join of scopes |
| lateral | custom | 29.6 ms | 88,575 | rank Index Only Scan, 26,243 entries walked |
| lateral | generic | 23.5 ms | 88,570 | rank Index Only Scan, 26,243 entries walked |

On this fixture the lateral reads about 88.6k shared buffers against the previous statement's 6.2k. Each entry walked costs an `ingestion_scopes` primary-key probe, and the busy entity's 1,001 active rows sit among 25 generations each. The lateral is bounded by the page, and the previous statement is bounded by the active-scope count, so the two scale differently. On the QA replica's statistics, the scope-driven plan is the slow one.

`TestCrossRepoDeadCodeConsumerEvidencePageBoundLive` checks every work arm under a custom plan, a forced generic plan, and -- on the wide arm -- the plan Postgres's plan cache serves after a PREPARE plus 12 EXECUTEs (generic_plans=7, custom_plans=5). Across those modes the ranking walked:

| Arm | Entries walked | Ceiling | Floor |
| --- | ---: | ---: | ---: |
| one generation, 1,500 consumers of the busy entity | 1,016 | 1,200 | |
| three retained generations | 4,016 | 4,204 | 1,200 |
| wide 250-ID page | 26,250 | 27,466 | 2,002 |

The previous statement fails the shape assertions in every work arm:

- the liveness tables are read outside a SubPlan;
- no rank-index index-only walk ranks the consumers;
- one arm fetches 1,515 entries per loop.

The wide arm compares rows and positions against the frozen retired statement, both with the busy entity past the cap (1,001 rows) and for 249 ordinary entities (249 rows). Both match.

### QA replica, unscoped page, read-only

The rewrite's numbers below were taken after other probes had already read the same pages, so no cold first run of the rewrite was observed. The previous statement's first run above was cold.

| Page | Shape | Run 1 | Run 2 | Shared buffers |
| --- | --- | ---: | ---: | ---: |
| 250 IDs | previous | 585.5 ms | 594.9 ms | 6,752 / 6,744 |
| 250 IDs | lateral | 4.28 ms | 2.75 ms | 1,005 / 1,000 |
| 101 IDs | previous | 254.5 ms | 253.0 ms | 6,744 / 6,744 |
| 101 IDs | lateral | 1.09 ms | 1.11 ms | 404 / 404 |

The lateral used the rank Index Only Scan on both pages. For the PR's grant note only: the same 250-ID page with a 3-repository grant took 496.2 ms (5,565 buffers) on the previous statement, which grant-bound reads keep. The lateral with the grant bound took 111.8 ms (706 hit, 294 read), planned as index 101 plus a sort.

Row and position differential, lateral against the previous statement, `EXCEPT ALL` in both directions with the row position included:

- zero differences on all 7 pages;
- the pages were the 250 highest fan-in entities, fan-in ranks 251-500, a 250-ID Function page plus 50 high fan-in entities, 1,500 active-consumer entities crossing the 1,001-row cap (1,001 rows), and three consumer-repository grant runs (0, 284 and 250 rows).

This differential is weak evidence. Every entity on the QA replica has at most one active consumer row, so the replica cannot exercise a busy fan-in. The local fixture's differentials above carry that case.

Performance Evidence: Query shape: the unscoped cross-repo consumer-evidence page as a per-entity LATERAL Index Only Scan of `code_reachability_entity_confidence_rank_idx`, with per-row liveness subqueries and a five-column primary-key-plus-depth fetch; grant-bound reads unchanged. Backend: PostgreSQL 18.3 on the QA replica and 18.6 in a local container. Input cardinality: 101 and 250 producer IDs on the replica; 250 IDs with a 1,500-consumer busy entity on the local fixture. Index state: unchanged, no new index, migration 103's rank index present. Before and after on the replica, 250 IDs: 1,305.1 ms first and 588.3 ms warm with 6.7k buffers, against 4.28 and 2.75 ms with 1.0k buffers. On 101 IDs: 254.5 and 253.0 ms against 1.09 and 1.11 ms. Every figure is a PostgreSQL statement time under the cache state described, not an HTTP or MCP endpoint p95.

No-Observability-Change: The existing `postgres.query` span with `db.operation=cross_repo_dead_code_consumer_evidence` still records errors and duration for this statement. The change alters only SQL text and argument shape. It adds no worker, queue stage, metric, or status field.
