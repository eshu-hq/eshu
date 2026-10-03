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

A lateral is planned from the average fan-in per entity, not the busy entity's, so "another index plus a sort" is always a candidate. The result order comes from the statement's `ORDER BY` under any plan. What the plan decides is whether the ranking is served as an ordered walk of the rank index, because that walk is index-only and every alternative needs the heap. The planner prices an Index Only Scan by the share of the table's pages marked all-visible, so the ordered walk is chosen while autovacuum keeps them so: 54,033 of 55,185 pages (97.9%) on the QA replica, read from `pg_class.relallvisible`, and the live guards run on a vacuumed fixture. A page range a fresh snapshot rewrite has not yet had vacuumed needs heap reads, which can tip the plan toward "another index plus a sort"; that state was not measured and is NOT_CHECKED.

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

The rewrite's numbers below were taken after other probes had already read the same pages, so no cold first run of the rewrite was observed. The previous statement's first run above was cold. These ad-hoc `EXPLAIN (ANALYZE, BUFFERS)` runs used a custom plan; the next section measures both statements under a forced generic plan on the replica's statistics.

| Page | Shape | Run 1 | Run 2 | Shared buffers |
| --- | --- | ---: | ---: | ---: |
| 250 IDs | previous | 585.5 ms | 594.9 ms | 6,752 / 6,744 |
| 250 IDs | lateral | 4.28 ms | 2.75 ms | 1,005 / 1,000 |
| 101 IDs | previous | 254.5 ms | 253.0 ms | 6,744 / 6,744 |
| 101 IDs | lateral | 1.09 ms | 1.11 ms | 404 / 404 |

The lateral used the rank Index Only Scan on both pages. For the PR's grant note only: the same 250-ID page with a 3-repository grant took 496.2 ms (5,565 buffers) on the previous statement, which grant-bound reads keep. The lateral with the grant bound took 111.8 ms (706 hit, 294 read), planned as index 101 plus a sort.

#### Plan mode on the replica

The production driver caches prepared statements, so after about five executions per connection PostgreSQL may serve a generic plan. Both statements were prepared and run as `EXPLAIN (ANALYZE, BUFFERS)` under `plan_cache_mode` forced to custom and to generic, `jit = off`, on the 250-ID Function page of the 12,403-file repository (0 rows returned), twice each. The lateral text is the production statement; the previous statement was prepared with 250 placeholders.

| Shape | Plan mode | Run 1 | Run 2 | Shared buffers |
| --- | --- | ---: | ---: | ---: |
| previous | custom | 606.9 ms | 581.4 ms | 6,730 / 6,722 |
| previous | generic | 613.0 ms | 585.0 ms | 6,722 / 6,722 |
| lateral | custom | 7.84 ms | 2.81 ms | 1,005 / 1,000 |
| lateral | generic | 3.37 ms | 2.79 ms | 1,000 / 1,000 |

On the replica's statistics a generic plan is the same plan as the custom one for both shapes: the previous statement stays scope-driven (819 primary-key probes) and the lateral stays on the rank Index Only Scan. The local fixture shows a different generic-plan outcome, with the lateral slower in wall time than the previous statement (23.5 ms against 11.6 ms, table above); the replica does not reproduce that, and it is a limit of this change rather than something the replica numbers rule out. The cold first run of the lateral under a generic plan was not observed.

Row and position differential, lateral against the previous statement, `EXCEPT ALL` in both directions with the row position included:

- zero differences on all 7 pages;
- the pages were the 250 highest fan-in entities, fan-in ranks 251-500, a 250-ID Function page plus 50 high fan-in entities, 1,500 active-consumer entities crossing the 1,001-row cap (1,001 rows), and three consumer-repository grant runs (0, 284 and 250 rows).

This differential is weak evidence. Every entity on the QA replica has at most one active consumer row, so the replica cannot exercise a busy fan-in. The local fixture's differentials above carry that case.

Performance Evidence: Query shape: the unscoped cross-repo consumer-evidence page as a per-entity LATERAL Index Only Scan of `code_reachability_entity_confidence_rank_idx`, with per-row liveness subqueries and a five-column primary-key-plus-depth fetch; grant-bound reads unchanged. Backend: PostgreSQL 18.3 on the QA replica and 18.6 in a local container. Input cardinality: 101 and 250 producer IDs on the replica; 250 IDs with a 1,500-consumer busy entity on the local fixture. Index state: unchanged, no new index, migration 103's rank index present. Before and after on the replica, 250 IDs: 1,305.1 ms first and 588.3 ms warm with 6.7k buffers, against 4.28 and 2.75 ms with 1.0k buffers. On 101 IDs: 254.5 and 253.0 ms against 1.09 and 1.11 ms. Plan mode on the replica, 250 IDs, forced custom and forced generic: 606.9 and 581.4 ms custom and 613.0 and 585.0 ms generic for the previous statement, against 7.84 and 2.81 ms custom and 3.37 and 2.79 ms generic for the lateral; on the local fixture the lateral's generic plan is slower in wall time than the previous statement's (23.5 ms against 11.6 ms). Every figure is a PostgreSQL statement time under the cache state and plan mode described, not an HTTP or MCP endpoint p95.

No-Observability-Change: The existing `postgres.query` span with `db.operation=cross_repo_dead_code_consumer_evidence` still records errors and duration for this statement. The change alters only SQL text and argument shape. It adds no worker, queue stage, metric, or status field.

## Legacy producer-local incoming read: candidate indexes disproven

The legacy producer-local read `ContentReader.DeadCodeIncomingEntityIDs` probes a repository's completed `code_calls` intents in `shared_projection_intents` by `payload->>'callee_entity_id'` (and, for `USES_METACLASS` edges, `payload->>'target_entity_id'`). No index covers either expression, so each branch scans the repository's `code_calls` intents. This section records a candidate fix that was measured and **not shipped**.

### Candidate and acceptance bar

The candidate was two partial expression indexes, `(repository_id, (payload->>'callee_entity_id'))` and `(repository_id, (payload->>'target_entity_id'))`, both `WHERE projection_domain = 'code_calls' AND completed_at IS NOT NULL` (the second also `AND payload->>'relationship_type' = 'USES_METACLASS'`), plus `CREATE STATISTICS` on both expressions. PostgreSQL ignores the expression statistics of a partial index, so the statistics are what keeps the planner on the index. The completed-only predicate keeps an intent out of both indexes at INSERT; the completion UPDATE, already non-HOT because `completed_at` is in the pending indexes, adds the entry.

Before the run, an arbiter fixed the bar: accept the completion-UPDATE cost only if every line below passes, and treat any failure as a disproven theory.

### Measurement

One bounded run on the shared performance host: PostgreSQL 18.6 in one throwaway container (4 CPUs, 8 GiB, `shared_buffers` 2 GiB, which is 262,144 buffers of 8 kB (the run manifest prints the setting and its unit run together, `2621448kB`), autovacuum off). The corpus is synthetic, generated by the harness: 3,023,000 intents across 401 repositories, one of them with 603,000 `code_calls` intents over 27,000 distinct callees and an average payload of 717 bytes, 8,601 intents pending. Two schemas were built from identical rows in identical physical order with the nine production indexes, arm B then gaining the candidate indexes and statistics. Six null rounds (A against A before any candidate exists) set the harness's own noise floor; twelve main rounds, 10,000-row batches, arm order from a seeded balanced shuffle, a CHECKPOINT before each arm, timing inside one procedure with the COMMIT included, WAL bytes from the insert LSN, one warm-up round excluded from the statistics. The harness is a throwaway script and is not committed (the run manifest records the SHA-256 of `run_wc.sh`: `d52c645b4d615c0af9538de0e347f93321497987b23890e7c69c3542fc5849f4`); this is a single attempt. The host had 16 cores; in-window load1 peaked at 4.77 against the rule-PD ceiling of 8, and an idle peer stack on the host was left untouched (its `docker ps` is identical before and after).

| Criterion | Bar | Measured | Result |
| --- | --- | --- | --- |
| INSERT mean pair ratio | within null mean + 2 SD (1.056) | 0.973 (INSERT WAL ratio 1.000; the main-phase insert spread is wider, SD 0.174 against 0.028 in the null phase, so this pass rests on the identical WAL) | pass |
| Added completion-UPDATE time per row, by medians | at most 15 us | 34.4 us (371.6 to 715.7 ms per 10,000 rows) | fail |
| UPDATE mean pair ratio | at most 1.6 | 1.920 (pairs 1.563 to 2.539; null-phase pairs 0.932 to 1.067) | fail |
| UPDATE WAL | at most 1.20x | 1.485 (pairs 1.445 to 1.577) | fail |
| Lifecycle (INSERT plus UPDATE) WAL | at most 1.06x | 1.069 (pairs 1.068 to 1.081) | fail |
| HOT updates | 0 in both arms | 0 and 0 over 120,000 updates | pass |
| Row set, `EXCEPT ALL` both ways | 0 and 0 in all 8 comparisons | 0 and 0 in all 8 | pass |
| Index used in both plan modes, buffers | at most 5% of baseline | index used in all 8 groups; buffers 11.0% to 38.7% of baseline | fail |
| Rule PD (host quiet in the window) | pass | pass | pass |

The candidate indexes grew 496,299 bytes per 10,000-row batch; at the end of the main phase the callee index was 85,090,304 bytes and the metaclass index 221,184 bytes.

The read side did improve, which is why this is recorded rather than dropped. Statement execution medians in milliseconds, baseline arm to candidate arm, for the legacy statement with a 101-id page (`top` and `weighted` are the harness's two candidate-id mixes; the pre-churn and post-churn states are separate measurements):

| State | Mix | Auto | Forced generic |
| --- | --- | ---: | ---: |
| pre-churn | top | 965 to 199 | 2,759 to 230 |
| pre-churn | weighted | 940 to 74 | 2,725 to 101 |
| post-churn | top | 1,407 to 197 | 3,449 to 221 |
| post-churn | weighted | 1,771 to 84 | 3,432 to 109 |

### Verdict

Disproven for this shape: the read gain is real, but the completion UPDATE cost is about 2.3 times the budget, the UPDATE WAL is 1.49 times against a 1.20 bar, and the index still reads 11% to 39% of the baseline's buffers. No migration ships from this work, and the legacy statement keeps its scan. Trace evidence from the deployed QA build later settled whether it matters: among 60 traced requests to the cross-repo dead-code endpoints for the largest repository, the slowest request took 1,368 ms, and this legacy statement's span accounted for 1,173.6 ms of it. That is one request, not a p95 or a mean, but it shows the statement is the material part of the slow endpoint requests. The bound read below addresses it without an index.

Not measured, and not recommended by this evidence: an index that is not partial on `completed_at` (it would move the entry cost from the completion UPDATE to the INSERT), and a narrower payload expression. Any follow-up must prove its theory with the same harness before code.

Performance Evidence: Query shape: the legacy producer-local incoming read, a three-branch `UNION ALL` over `shared_projection_intents` with a 101-id `IN` list per branch, statement unchanged. Backend: PostgreSQL 18.6 in a disposable container on the shared performance host. Input cardinality: a synthetic 3,023,000-intent table, 603,000 `code_calls` intents in the largest repository, 10,000-row batches, a 101-id candidate page. Index state: the nine production indexes in both arms; the candidate arm adds two partial expression indexes and two expression-statistics objects. Before and after: the completion UPDATE costs 371.6 ms against 715.7 ms per 10,000 rows by medians (mean pair ratio 1.920) and 16.5 MB against 24.6 MB of WAL; the read falls from 940 to 3,449 ms to 74 to 230 ms across the eight groups. The change is not shipped. Every figure is a PostgreSQL statement time, not an HTTP or MCP endpoint p95.

No-Observability-Change: This is an evidence record only. It changes no code, migration, worker, queue stage, metric, or status field.

## Legacy producer-local incoming read: bound to a provably complete active run

`ContentReader.DeadCodeIncomingEntityIDs` read every completed `code_calls` and `inheritance_edges` intent the repository ever had. For the largest repository on the QA replica that was 677,963 `code_calls` rows across 11 generations, where the active generation holds 61,633. The reads in the cross-repo dead-code path go through `legacyDeadCodeIncomingEntityIDs` and the `incoming.go` fallback.

### Why the bound is conditional

A plain bound to the active acceptance run would be wrong for two kinds of repository.

- Delta generations carry only changed files. The collector builds a delta snapshot from the changed file targets only (`go/internal/collector/repo/git/source_types.go`, `Delta`; `go/internal/collector/repo/git/selection_native.go`), and code-call and inheritance materialization load facts for the intent's own generation only. No fact carries forward. On the replica 11 of 819 active generations are delta, and every one of those 11 has zero edge intents in its active run: a plain bound returns nothing and calls every symbol dead. One such repository has 891 incoming-live candidates today and 0 under a plain bound, 890 of them with a caller still present.
- A full generation is incomplete while it projects. Acceptance rows commit with the intents (`SharedIntentAcceptanceWriter.UpsertIntents`), but code-call and inheritance materialization are separate reducer work items that share one acceptance key, and written intents can still be pending. The unbound read errs toward live during that window; a plain bound would not.

So the statement binds only when, for every active (scope, generation) of the repository:

1. the generation is full (`scope_generations.is_delta = false`, written from the same `snapshot.Delta` as the `delta_generation` fact);
2. reducer `code_call_materialization` and `inheritance_materialization` work items for the generation both succeeded, and none for either domain is in any other status (`fact_work_items_scope_generation_idx`);
3. no `code_calls` or `inheritance_edges` intent of the generation for the repository is pending (migration 108's partial index).

Reducer work for a generation is enqueued only after its facts are durable and read: `IngestionStore.commitScopeGeneration` (`go/internal/storage/postgres/ingestion.go`) upserts every streamed fact, then enqueues projector work, then commits, in one transaction; `Runtime.Project` (`go/internal/projector/runtime/projection.go`) builds the projection from that committed generation's facts and only then enqueues the reducer intents. A succeeded work item that emitted nothing therefore means the input was empty, not missing, so no separate at-least-one-completed-intent check is needed. A rebuild reset deletes the succeeded work items and reopens the intents, so it falls back.

Otherwise the statement returns one sentinel row and the reader runs the previous statement, byte for byte (`deadCodeIncomingUnboundQuery`). The guard and the bound read are one statement, so they share a snapshot. The bound read uses the same key as the reducer's reachability edge loader: the active acceptance run's `source_run_id` and `generation_id`, matched as a pair. Each branch keeps a row only when `EXISTS` an `active_run` row with that row's `source_run_id` and `generation_id` together; two independent `IN` lists, as first shipped, would also admit a row that pairs one scope's run with another scope's generation.

Intended semantics: when bound, an edge kept only by a superseded generation no longer keeps its target alive. On the replica's largest repository the live candidate set moves from 27,440 to 27,436; each of the 4 dropped entities has a caller that is still present and that the active run re-emitted without that edge, so each is a correct drop. None are live only under the bound.

The reachability loader has the same gap for partial or delta runs; that is tracked in #7547 and not changed here.

### Proof

- `TestDeadCodeIncomingEntityIDsActiveRunBoundLive` on a disposable PostgreSQL 18 database with the bootstrap schema: a complete full active run counts its live call, metaclass target and inheritance parent and not the stale-only callee (this failed on the previous statement), and a complete run with no edge to any candidate answers empty without falling back. Eight fallback fixtures (delta active, pending active-run intent, inheritance work item missing, code-call work item missing, queued replay, rebuild reset, no active acceptance row, and a second active scope whose inheritance work never succeeded) each return exactly what the shipped unbound statement returns. Every case also asserts the span's `dead_code_incoming.read_mode`. A repository with two complete active scopes and one intent stamped with the first scope's run and the second scope's generation counts the paired edges but not that cross-pair (it failed on the two-`IN`-list predicate). Removing any one guard, treating an empty bound answer as a fallback, replacing the gate's `bool_and` with `bool_or`, or reverting the pair predicate to two `IN` lists each fails exactly its fixture.
- Hermetic tests pin that the sentinel runs the shipped unbound statement and that the bound branches equal the unbound branches plus the pair-correlated run predicate, with no independent `IN` list.

### QA replica, the exact built statement

Same 101-id page of the largest repository, read-only replica, `EXPLAIN (ANALYZE, BUFFERS)` of the statement the Go code builds, one run per plan mode. The guard evaluated true and both statements returned the same 78 rows. The bound plan reads `shared_projection_intents_repo_run_idx` with the `source_run_id` condition in each branch; the guard reads `fact_work_items_scope_generation_idx` and `shared_projection_intents_generation_pending_idx`.

| Plan mode | Previous statement | Bound statement |
|---|---|---|
| Forced generic (both all shared hits) | 3,148 ms, 384,604 buffers | 282 ms, 30,055 buffers |
| Forced custom (previous: 190,523 of 373,931 buffers read from disk; bound: 328 of 30,058) | 1,752 ms | 97 ms |

The generic pair ran with every buffer already cached, so it is the same-cache comparison. The custom pair is not: the previous statement ran first and read half its buffers from disk, so part of that gap is cache state. The coordinator's earlier warm custom measurement of the shim with the same bound shape was 1,164 ms against 84 ms.

Pair-correlated predicate: after a review found the two independent `IN` lists admit cross-pair rows, the branches match the (`source_run_id`, `generation_id`) pair with a correlated `EXISTS`. The coordinator measured the two forms side by side on the replica, read-only, as prepared statements on the same page: forced generic 313 ms for the `IN` lists against 270 ms for `EXISTS`, forced custom 84 against 86 ms, 29,842 buffers each, both probing `shared_projection_intents_repo_run_idx` by the run key. One `EXPLAIN (ANALYZE, BUFFERS)` per plan mode of the exact statement the Go code now builds kept that plan shape and the 78 rows: forced custom 95.3 ms with 30,055 buffers, all shared hits; forced generic 359.8 ms with 30,085 buffers, of which 15,055 were read from disk because it ran first, so that one figure is not cache-matched. Shape, from the coordinator's read-only count on the replica: of the 799 repositories with an active acceptance pair, 787 have one active generation with several runs, 1 has two active generations, and 11 have a single (run, generation) pair (787 + 1 + 11 = 799); no repository has more than four runs. Only the one repository with two active generations can form a cross-pair, so the fix is not expected to change an answer there today.

Coverage, a separate count over the same 799 repositories: 787 pass the gate and take the bound read, 11 have a delta active generation, and 1 lacks a succeeded materialization work item (787 + 11 + 1 = 799); those 12 keep the previous statement. This 787 and the 787 in the shape count above measure different things and only happen to be equal.

### Rollback and limits

Rollback is reverting the statement change; no schema, index or data change ships. Limits: one replica data copy; statement times, not endpoint p95. A repository whose active run cannot be proven complete (12 of 799 on the replica) pays the guarded statement and then the previous one. One read-only `EXPLAIN (ANALYZE, BUFFERS)` of the guarded statement on one delta-active repository, forced custom plan, returned only the sentinel row in 4.9 ms execution and 7.6 ms planning with 528 buffers; that, plus a second round trip, is the measured fallback overhead for that one repository, not a distribution. A deployed replay is still required to confirm the endpoint p95.

Performance Evidence: Query shape: the legacy producer-local incoming read as one statement, an active-acceptance-run CTE and a completeness gate (full generation, both reducer materialization work items succeeded and none outstanding, no pending edge intent) followed by the unchanged three-branch `UNION ALL` with each row's (`source_run_id`, `generation_id`) pair matched to an active acceptance pair by a correlated `EXISTS`, or one sentinel row that makes the reader run the previous statement. Backend: PostgreSQL 18 on the QA replica, read-only. Input cardinality: the largest repository, 677,963 completed `code_calls` intents across 11 generations, 61,633 in the active run, a 101-id candidate page, 78 result rows. Index state: unchanged, no migration. Before and after, same cache state (forced generic, all shared hits): 3,148 ms and 384,604 buffers against 282 ms and 30,055. Forced custom: 1,752 ms against 97 ms, but the previous statement read 190,523 buffers from disk and the bound one 328, so that pair is not cache-matched; the coordinator's warm custom shim pair was 1,164 ms against 84 ms. 787 of 799 repositories with an active acceptance row pass the gate. The pair-correlated predicate measured 270 ms against 313 ms forced generic and 86 against 84 ms forced custom with the same buffers. Every figure is a PostgreSQL statement time, not an HTTP or MCP endpoint p95.

Observability Evidence: The existing `postgres.query` span with `db.operation=dead_code_incoming_entity_ids` still records errors and duration, and now carries `dead_code_incoming.read_mode` set to `active_run` when the bound read answered or `all_generations` when the reader fell back to the previous statement. No worker, queue stage, metric or status field is added.
