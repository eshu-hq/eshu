# #6527 The Cross-Repo Consumer-Evidence Page's Own Bound

`POST /api/v0/code/dead-code/cross-repo` answers with two bounded reads. The
hidden-consumer probe is measured in
[#5167 cross-repo hidden-consumer walk](5167-cross-repo-hidden-consumer-walk.md).
This note is the other one: the evidence page, whose `LIMIT` bounded what came
back and not what was read.

`buildCrossRepoDeadCodeConsumerEvidenceQuery` orders a page of producer
entities' consumers `(entity_id, confidence DESC, depth, repository_id,
root_entity_id)` and stops at `maxCrossRepoDeadCodeConsumerEvidenceRows + 1`
rows. Nothing on `code_reachability_rows` carried that order, so Postgres had to
rank a producer entity's whole fan-in group before it could emit the group's
first row. One busy symbol therefore cost the page its entire consumer set.

The fix is an index and nothing else. Migration 103 builds `(entity_id,
confidence DESC, depth, repository_id, root_entity_id)`, which IS that `ORDER
BY` with `entity_id` pinned by the statement's `IN` list, so the scan is already
in output order and the `LIMIT` stops it. The statement, the page contract, the
`consumer_evidence_truncated` marker and the OpenAPI shape are untouched.

Root-Cause Evidence: the shipped plan is
`Limit -> Incremental Sort (Presorted Key: entity_id) -> Nested Loop -> Index
Scan using code_reachability_entity_repository_scope_generation_idx`, and that
scan reports `rows=1000497` for a page whose answer is 1,001 rows. The sort is
what forces it: the presorted key is `entity_id` alone, so every group has to be
read in full before its first row can be ordered by confidence.

## How This Was Measured

Throwaway PostgreSQL 16.15 container, `shared_buffers=1GB`, `work_mem=64MB`,
`SET jit = off` throughout, `VACUUM ANALYZE` after seeding, four
`EXPLAIN (ANALYZE, BUFFERS)` samples per cell with the first discarded as cold
and the last three reported. Host: MacBook Pro, arm64, macOS. Schema applied
from the branch's own migration files: `001_ingestion_scopes.sql`,
`002_scope_generations.sql`, `027_code_reachability.sql`, then 101 and 102.

The seed is the one #6527's figures were filed from: 2,207,196
`code_reachability_rows`, one ingestion scope, one generation, a producer page
whose middle entity `ent-m-hot` carries 1,000,000 active-generation consumer
rows across five consumer repositories, 249 ordinary entities with four consumer
rows each, `ent-wide` with 300 consumer repositories, and 1.2M rows on entity
ids off the page. Table 1,175 MB total, of which 367 MB is heap.

Two pages and two grants. The `hot` page is all 251 entities; the `ordinary`
page is the same 250 without `ent-m-hot`. `g5` is the five consumer
repositories `ent-m-hot` is spread across; `g4` is the same minus one.

Both plan modes, every time: once as `EXPLAIN` on a literal statement, which is
a custom plan, and once through `PREPARE`/`EXECUTE` under
`plan_cache_mode = force_generic_plan`. Which of the two production actually
gets is measured below rather than assumed.

## What The Page Cost, And What It Costs Now

Performance Evidence: with migration 103 the page read stops at its own `LIMIT`.
The rows under the driving scan are the claim; the times follow them.

| page and grant | rows under the driving scan | shared buffers | custom plan |
| --- | ---: | ---: | --- |
| hot, g5, before | 1,000,497 | hit=113,832 | 815.3 / 869.2 / 825.5 ms |
| hot, g5, after | 1,124 | hit=929 | 1.68 / 4.56 / 1.66 ms |
| hot, g4, before | 800,497 | hit=113,832 | 853.3 / 719.6 / 724.0 ms |
| hot, g4, after | 1,124 | hit=929 | 1.59 / 1.59 / 1.62 ms |
| ordinary, g5, before | 996 | hit=7,026 | 3.18 / 3.16 / 3.13 ms |
| ordinary, g5, after | 996 | hit=7,333 | 3.00 / 3.07 / 3.20 ms |

The before figures reproduce the ones #6527 was filed with (885 ms over
1,000,497 rows, and 752 ms over 800,373) on this machine.

The plan changes shape rather than degree. Before:

    Limit
      -> Incremental Sort   Presorted Key: entity_id
           -> Nested Loop
                -> Index Scan using code_reachability_entity_repository_scope_generation_idx
                     rows=1000497

After:

    Limit
      -> Nested Loop
           -> Index Scan using code_reachability_entity_confidence_rank_idx
                rows=1001   Rows Removed by Filter: 123

The whole sort node is gone: the index supplies the order, so the `Limit` is the
only thing that decides how far the scan goes.

The ordinary page is where the cost of a wider index shows: same 996 rows, same
time within noise, `hit=7,026` against `hit=7,333`. A page with no busy entity
pays about 4% more buffers for a page with one costing 123 times fewer.

Confirmed a second time after a `VACUUM FULL` rebuilt the relation's physical
layout, which is a different heap ordering and therefore an independent sample:
746.6 / 747.7 / 754.9 ms at `hit=113,902` without the index against
3.45 / 1.94 / 3.51 ms at `hit=928` with it.

## Which Plan Production Actually Gets

Under `plan_cache_mode = force_generic_plan` the planner keeps the shape it used
before and does not read this index at all: 947.1 / 830.9 / 860.3 ms on the hot
page at g5, against 783.9 / 825.4 / 795.2 ms before it existed. No better there,
and no worse.

That mode is a hostile setting, not the production one, and the difference is
measurable rather than arguable. `pgx` runs these reads as server-side prepared
statements, so what decides the plan is Postgres's plan cache. `PREPARE`
followed by twelve `EXECUTE`s under the default `plan_cache_mode = auto` leaves
`pg_prepared_statements` reading `generic_plans = 0, custom_plans = 12`. Every
execution used the custom plan and the new index; the first eight ran in
1.85 / 1.43 / 1.40 / 1.42 / 1.39 / 1.42 / 1.53 / 1.40 ms.

The cache never promotes the generic plan here because `choose_custom_plan`
compares the generic plan's estimated cost against the average custom one, and
those are 11,452 and 194. A shape whose generic estimate is sixty times its
custom average does not get locked onto a generic plan.

`docs/internal/evidence/5167-freshness-family-allowlist.md` records the same
reading for a different statement, so this is the family's second measurement of
it rather than a one-off.

## The Answer Does Not Move

The statement is unchanged, so the only way the answer could move is a tie
broken differently. Every returned row was captured with `row_number() OVER ()`
preserving output order, with the index and without it:

| page and grant | rows | with minus without | without minus with | position mismatches |
| --- | ---: | ---: | ---: | ---: |
| hot, g5 | 1,001 | 0 | 0 | 0 |
| hot, g4 | 1,001 | 0 | 0 | 0 |
| ordinary, g5 | 996 | 0 | 0 | 0 |
| hot, disjoint grant | 0 | 0 | 0 | 0 |

Not merely the same set: the same row in the same position.

## The Per-Entity Top-K #6527 Suggested, Measured And Rejected

#6527 proposed `CROSS JOIN LATERAL (... ORDER BY confidence DESC ... LIMIT k)`
over the page's entity ids. It was built and measured with `k` equal to the
statement's own cap, which makes it answer identically: the global limit can
never take more than 1,001 rows from one entity, so restricting each entity to
its own top 1,001 cannot lose a row the page would have returned.

| page, grant, plan mode | shipped statement + index | lateral top-k + index |
| --- | --- | --- |
| hot, g5, custom | 1.68 / 4.56 / 1.66 ms, hit=929 | 10.85 / 10.84 / 11.05 ms, hit=7,789 |
| hot, g4, custom | 1.59 / 1.59 / 1.62 ms, hit=929 | 456.0 / 454.5 / 454.3 ms, hit=89,523 |
| ordinary, g5, custom | 3.00 / 3.07 / 3.20 ms, hit=7,333 | 3.27 / 3.35 / 3.17 ms, hit=7,455 |
| hot, g5, forced generic | 947 / 831 / 860 ms | 604.7 / 588.9 / 585.5 ms |

Without the index the lateral does not help at all: 619.0 / 619.5 / 625.9 ms at
`hit=114,836`, which is the unfixed cost.

Two reasons it loses, one measured and one structural.

Measured: at a four-of-five grant the planner puts a `Sort` back INSIDE the
lateral -- `Limit (cost=776.78..777.54) -> Sort` -- because the grant filter
drops the per-entity row estimate below the point where an ordered index scan
looks cheaper. The lateral does not remove the planner's freedom; it moves it
inside, where it is harder to see.

Structural: this route's cap is GLOBAL, 1,001 rows across the page, not per
entity. A per-entity top-k reads up to `k` rows for every entity on the page and
throws the surplus away -- 1,997 rows read for a 1,001-row answer on this seed,
and up to 251 x 1,001 on a page where every entity is busy. The index lets the
statement the route already has stop at exactly 1,001, which is a strictly
tighter bound than the per-entity one and needs no change to
`consumer_evidence_truncated`.

## No Regression On The Other Reads

No-Regression Evidence: three reads share this table, and the two this change
does not target are measured with the index present and absent on the same
physical layout.

The hidden-consumer probe (`crossRepoDeadCodeUngrantedConsumerProbeQuery`,
251-entity page, five-repository grant) takes an identical plan either way --
five `Index Scan`s, every one of them on
`code_reachability_entity_repository_scope_generation_idx` -- and identical
`hit=4,894` buffers, at 4.87 / 4.70 / 4.67 ms without the index against
4.63 / 4.60 / 4.58 ms with it. It cannot use the new index and does not try:
its pair walk needs `(entity_id, repository_id, scope_id)` order and its
liveness seek needs `generation_id`.

The incoming-edge read (`deadCodeIncomingEntityIDs`, a `DISTINCT` over seven
entity ids including `ent-m-hot`) also takes an identical plan either way --
`HashAggregate` over a `Hash Join` over a `Parallel Seq Scan` -- and reads
46,999 buffers in both. The new index is not in its plan. Wall time on that read
moved with page-cache state rather than with the index (228.9 ms at `hit=746
read=46,253` against 404.5 ms at `hit=22,641 read=24,358`), which is why the
buffer total is the claim and the seconds are not.

The reducer's write path maintains one more btree, and that is the real cost.
A 200,000-row reachability insert, alternated so both arms saw the table grow:

| arm | rows in the table | wall |
| --- | ---: | --- |
| without the index | 2.2M, 2.2M | 6.02 s, 6.06 s |
| with the index | 2.4M, 2.4M | 7.24 s, 7.47 s |
| with the index | 2.6M, 2.6M | 7.63 s, 7.39 s |
| without the index | 2.8M, 3.0M | 6.46 s, 6.72 s |

About +15-20%, roughly +6 us per row, and the second without-arm ran on the
LARGER table, so the gap is if anything understated. On disk the index is 163 MB
built in 6.4 s `CONCURRENTLY`, against a 367 MB heap and 808 MB of existing
indexes: the relation grows about 14%.

Nothing was dropped to pay for it. `code_reachability_entity_lookup_idx` is
`(entity_id, state, confidence DESC)` and no read in the tree constrains
`state`, so it looks superseded on paper -- but proving that needs before/after
numbers on the reads that use it, which this change does not have, and dropping
an index on an unmeasured argument is exactly what this repository does not do.

## Observability

No-Observability-Change: the page read keeps the span
`postgres.query` with `db.operation = cross_repo_dead_code_consumer_evidence`
and `db.sql.table = code_reachability_rows` that `CrossRepoDeadCodeConsumerEvidence`
already emits, and the `db.rows.consumer_signal_entities` attribute beside it.
An index is not a new statement, a new stage, or a new failure mode: the same
span, on the same statement, over the same rows.

## Guards

Four of the five bite on the fix itself; the fifth is the answer.

| guard | where | what it fails to |
| --- | --- | --- |
| `TestCodeReachabilityPageRankIndexIsCreatedOnceAndNeverDropped` | `go/internal/storage/postgres/schema_index_replay_test.go` | a migration that stops creating the index, or a later file that drops it -- a create/drop pair rebuilds the index on every bootstrap, forever |
| `TestCodeReachabilityIndexMigrationsReapplyWithoutRebuildLive` | `go/internal/storage/postgres/code_reachability_index_replay_live_test.go` | the same, on a populated store, per definition rather than per pass |
| `TestCrossRepoDeadCodeConsumerEvidencePageBoundLive` index and work guards | `go/internal/query/code_dead_code_cross_repo_page_bound_live_guards_test.go` | an index whose key columns are in another order, and a page read that scans the entity's group instead of its `LIMIT` |
| the same test's answer guard | the same file | an ordering change that moves which entity the truncation marker lands on |
| the same test's work guard, sort assertion | the same file | a plan carrying any sort node at all. The row count says the `LIMIT` held; this says why, and it survives a planner finding some other way to over-read |
| `TestCrossRepoDeadCodeConsumerPageOrderMatchesItsIndexKey` | `go/internal/query/code_dead_code_cross_repo_bound_test.go` | the statement's `ORDER BY` and migration 103's key drifting apart, from either side. It reads both — the statement from its builder, the key from the shipped migration file — and runs in the unit lane rather than waiting for a Postgres |

The live proof applies the shipped migrations rather than a hand-written fixture
schema. That is not tidiness: on a hand-written schema with the same rows the
planner chose a `Hash Join` and a `top-N heapsort` and never considered the
index at all, so the guard would have failed for a reason that has nothing to do
with the code under test.

What the fixture can and cannot show is recorded in the test's own comment. At
fixture scale both plan modes take the index; on the 2.2M-row corpus a
251-entity page keeps the pre-index plan under a forced generic one, which is
what the `pg_prepared_statements` reading above exists to put in proportion.

The red/green captures for all five guards are in
[#5167 code family batch 1 proofs](5167-code-family-batch-1-proofs.md), rows 55-60.
