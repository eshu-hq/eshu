# #6527 The Cross-Repo Consumer-Evidence Page's Own Bound

This is the follow-up to #6535, and it answers #6527. #6535 bounded the
hidden-consumer probe and filed the page read's own bound as an issue off its
own measurements; that issue is what this note answers, so nothing here was part
of #6535.

`POST /api/v0/code/dead-code/cross-repo` answers with two bounded reads. The
hidden-consumer probe is measured in
[#5167 cross-repo hidden-consumer walk](5167-cross-repo-hidden-consumer-walk.md).
This note is the other one: the evidence page, which ranked a producer entity's
whole fan-in before it could return the strongest few.

`buildCrossRepoDeadCodeConsumerEvidenceQuery` orders a page of producer
entities' consumers `(entity_id, confidence DESC, depth, repository_id,
root_entity_id, scope_id, generation_id)` and stops at
`maxCrossRepoDeadCodeConsumerEvidenceRows + 1` rows. Nothing on
`code_reachability_rows` carried that order, so Postgres had to rank a producer
entity's whole fan-in group before it could emit the group's first row. One busy
symbol therefore cost the page its entire consumer set.

The fix is an index plus a tiebreak on that ordering. Migration 103 builds
`(entity_id, confidence DESC, depth, repository_id, root_entity_id, scope_id,
generation_id)`, which IS that `ORDER BY` with `entity_id` pinned by the
statement's `IN` list, so the scan is answered in output order rather than by
ranking the group first. What that does NOT mean is that the `LIMIT` bounds the
read: it bounds rows RETURNED, and
[What The LIMIT Bounds](#what-the-limit-bounds-and-what-it-does-not) is the
measurement. The page contract, the `consumer_evidence_truncated` marker and the
OpenAPI shape are untouched.

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

Performance Evidence: with migration 103 the page read stops at the cap where
nothing is retained, and at the cap times the retained generations per position
where something is. This seed carries one generation per position, so the after
rows below are the first case and the entries walked equal the rows returned;
[What The LIMIT Bounds](#what-the-limit-bounds-and-what-it-does-not) measures the
second, where 1,001 becomes 11,081. The rows under the driving scan are the
claim; the times follow them.

| page and grant | rows under the driving scan | shared buffers | custom plan |
| --- | ---: | ---: | --- |
| hot, g5, before | 1,000,497 | hit=113,832 | 1819 / 2557 / 1571 ms |
| hot, g5, after (0 retained) | 1,001 | hit=930 | 2.03 / 1.77 / 1.49 ms |
| hot, g4, before | 800,497 | hit=113,832 | 751.7 / 730.1 / 831.2 ms |
| hot, g4, after (0 retained) | 1,001 | hit=930 | 1.54 / 1.47 / 1.48 ms |
| ordinary, g5, before | 996 | hit=7,026 | 3.93 / 3.82 / 3.57 ms |
| ordinary, g5, after (0 retained) | 996 | hit=7,347 | 6.97 / 3.58 / 6.30 ms |

Rows and buffers are the claim. The wall times were taken under concurrent load
on a shared machine, and an earlier run of BOTH cells in the same session reads
730.6 / 705.5 / 798.4 ms before and 1.75 / 1.72 / 1.78 ms after -- different
absolute numbers, the same three orders of magnitude. That earlier pair is not
the headline because it was taken before the proof schema had been analyzed. The
ratio is what survives a loaded machine; the seconds are not.

The before figures match the ones #6527 was filed with: 1,000,497 rows there and
here at the five-repository grant, and 800,373 there against 800,497 here at
four of five. Those 124 were not chased. The count is rows the scan read before
the `Limit` stopped it, not a total the seed determines, and the seed was
rebuilt for this note.

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
                rows=1001

The sort node is gone, and with it the need to read a producer entity's whole
group before its first row can be ordered. What that does NOT mean is that the
`LIMIT` now bounds the read; see the next section.

The ordinary page is where the cost of a wider index shows: same 996 rows,
`hit=7,026` against `hit=7,347`. A page with no busy entity pays about 5% more
buffers so that a page with one costs 122 times fewer.

Confirmed a second time after a `VACUUM FULL` rebuilt the relation's physical
layout, which is a different heap ordering and therefore an independent sample:
746.6 / 747.7 / 754.9 ms at `hit=113,902` without the index against
3.45 / 1.94 / 3.51 ms at `hit=928` with it.

## What The LIMIT Bounds, And What It Does Not

It bounds rows RETURNED. The read is bounded by the cap times the retained
generations per position, and on a page whose positions carry no retained
generation those are the same number, which is why the table above shows 1,001.

The active-generation test is a join ABOVE the driving scan, and this index's
key carries neither the scope's active generation nor a way to reach it. So the
scan emits one entry per RETAINED generation per position and the join throws
the superseded ones away. The reducer's reachability delete is keyed
`(scope_id, generation_id, repository_id)`, so a new generation adds a row set
and leaves the previous one, and `DefaultGenerationRetentionPolicy` keeps at
least 24 superseded generations per scope: positions with retained generations
are the normal case, not the exception.

Performance Evidence: measured on a retention-representative seed -- one
ingestion scope per consumer repository, every superseded generation carrying
the same population as the active one, so a position holds 1 + R rows and only
the newest is live. Active population identical across arms, 200,996 active
rows, 250-entity page, ordered path taken.

| retained generations per position | entries walked | buffers | ms |
| ---: | ---: | ---: | --- |
| 0 | 1,001 | hit=929 | 1.43 / 1.51 / 1.48 |
| 20 | 11,081 | hit=2,402 | 9.18 / 9.73 / 9.16 |

11.1x the entries for the same 1,001-row answer. Not 21x, because the answer is
drawn partly from ordinary entities that carry no retained generations.

Both rows are three independent runs that agree exactly, and they are measured
with `max_parallel_workers_per_gather = 0`. That is not tidying: with the
default two workers the planner takes a `Parallel Index Scan` under a
`Gather Merge`, and the entries walked then depend on how the workers race to
fill the cap -- the same arm read 11,780 in one run and 17,835 in another. The
multiplication is real either way, but only the serial number is a figure that
reproduces, so it is the one quoted here and in migration 103's header.

An earlier version of this note said the `LIMIT` was the only thing deciding how
far the scan goes. That was true of the one-generation seed it was measured on
and false in general, and the table above is what replaced it.

## When The Planner Takes This Index, And When It Does Not

Past some retention depth it stops taking it. At twenty retained generations the
active-generation join is estimated at 1,905 rows where it yields 200,996, so a
top-N sort of the whole active group looks cheap and is chosen: 200,995 rows
scanned, `hit=23,200`, 120-170 ms. That is the behaviour this index exists to
remove, and there it does not remove it.

It is not a regression there either, which is the part that decides whether the
index still earns its place. Interleaved on that corpus, dropping and rebuilding
the index between arms, two rounds: identical plan and identical `hit=23,200` in
all four, at 120.5 / 119.8 / 119.8 and 122.5 / 122.4 / 121.6 without it against
125.1 / 130.2 / 125.3 and 121.4 / 129.6 / 126.4 with it. Same plan, same
buffers, times inside the machine's noise.

Corpus size is not the axis and neither is scope count, both checked rather than
assumed: the ordered path is taken on the 2.2M-row corpus with a million-row
entity, on 200,996 active rows under five scopes, and on the same 200,996 under
one scope. Only the retention arm loses it.

Root-Cause Evidence: the estimate is what decides it. Three remedies were
measured against an actual of 200,996 and all three failed:

| remedy | estimate | plan | buffers |
| --- | ---: | --- | ---: |
| none | 1,905 | sort | hit=23,200 |
| `CREATE STATISTICS (dependencies, ndistinct, mcv) ON scope_id, generation_id` | 1,905 | sort | hit=23,200 |
| `SET STATISTICS 1000` on `generation_id` and `scope_id` | 1,281 | sort | hit=23,200 |
| both, plus a deeper target on `scope_generations.generation_id` | 1,905 | sort | hit=23,200 |

None moves the plan, and the raised target moves the estimate away from the
truth. Why extended statistics do not reach this join estimate is not
established here and is not claimed. Nothing is forced: a planner hint would be
a different kind of change and is not one this repository makes. What would
change the picture is an accurate row estimate for the active-generation join,
or an index the liveness test can reach -- the correlated-subquery form measured
in the rejected-alternatives section below is the closest thing tried, and it
costs more than it saves.

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

## The Answer, And The Tie The Old Order Could Not Break

Two things had to be shown: that the index does not reorder distinct rows, and
that rows the ranking cannot distinguish get one order rather than whichever the
plan happens to produce.

The first is the capture. Every returned row was recorded with
`row_number() OVER ()` preserving output order, comparing what this change ships
-- seven-column ORDER BY, seven-column index -- against what main ships --
five-column ORDER BY, no such index:

| page and grant | rows | new minus old | old minus new | position mismatches |
| --- | ---: | ---: | ---: | ---: |
| hot, g5 | 1,001 | 0 | 0 | 0 |
| hot, g4 | 1,001 | 0 | 0 | 0 |
| ordinary, g5 | 996 | 0 | 0 | 0 |

Not merely the same set: the same row in the same position.

The second needed a fixture that seed could not produce. Two rows tie on all
five ranking columns exactly when one `(entity_id, repository_id,
root_entity_id)` triple has rows under two ingestion scopes whose generations
are both active, with equal confidence and depth -- the two-scopes-per-repository
case #6535's walk rewrite is built on, and impossible on a single-scope seed.
The fixture builds that pair and puts it at the 1,001 boundary, which is the
only place a tie can change what comes back.

| state | ordered scan | sort plan |
| --- | --- | --- |
| as inserted | `scope-a` | `scope-a` |
| after the scope-a row is moved in the heap | `scope-a` | **`scope-b`** |
| after `REINDEX` | `scope-a` | **`scope-b`** |
| after `VACUUM FULL` | `scope-a` | **`scope-b`** |

They agree until physical order stops matching insertion order, and then they
disagree and stay disagreeing. A top-N heapsort is not stable, so among rows
equal on every ordering column the one it keeps is arbitrary; an index scan
returns them in index order. Two different arbitrary choices.

With `scope_id, generation_id` appended to the ORDER BY and to the index key,
both plans return `scope-a` in all four states above and in a fifth where the
other row of the pair is moved instead. The order is total, so there is nothing
left for a plan to decide.

What that was worth, stated honestly: both rows describe the same consumer edge
-- same consumer repository, same root entity, same producer entity, same
confidence and depth -- so the bucket the route reaches was never in question.
What moved was which generation and scope the returned row cites, and therefore
the `code_reachability_rows:<generation>/...` citation string.

Not a documented-contract change, checked with exit codes rather than by
reading: `rg -i 'order|sort|rank'` over the route's OpenAPI fragment
(`openAPIPathsCodeCrossRepoDeadCode`) exits 1 with zero matches, and over
`docs/public/reference/dead-code-reachability-spec.md` exits 1 with zero
matches. `docs/public/reference/http-api/code.md` has nine ordering mentions and
every one belongs to another route; the nearest sits under
`## Search And Discovery`.

## The Per-Entity Top-K #6527 Suggested, Measured And Rejected

#6527 proposed `CROSS JOIN LATERAL (... ORDER BY confidence DESC ... LIMIT k)`
over the page's entity ids. It was built and measured with `k` equal to the
statement's own cap, which makes it answer identically: the global limit can
never take more than 1,001 rows from one entity, so restricting each entity to
its own top 1,001 cannot lose a row the page would have returned.

Both columns below were measured against the FIVE-column form of the index,
before the tiebreak was added, and are left as they were taken. The rejection
does not turn on the difference: the lateral loses by two and three orders of
magnitude, and by a structural argument the tiebreak does not touch.

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
Measured against the SEVEN-column index on the same corpus, `EXPLAIN (ANALYZE,
BUFFERS, WAL)` over a 200,000-row reachability insert, arms alternated:

| arm | WAL records | buffers dirtied |
| --- | ---: | ---: |
| without the index | 1,221,760 / 1,221,774 / 1,221,777 | 16,001 / 15,934 / 16,004 |
| with the index | 1,424,877 / 1,424,879 / 1,424,883 | 19,158 / 19,099 / 19,167 |

**+16.6% WAL records and +19.8% buffers dirtied**, or about one extra WAL record
per row inserted. WAL records rather than seconds because seconds do not survive
this machine: the same eight inserts, four per arm, timed 11.36 / 16.98 / 7.58 / 7.44 s without
the index against 9.66 / 11.56 / 18.85 / 8.19 s with it -- fully overlapping, no
signal. The three samples per arm above vary by three records out of 1.2 million,
which is what makes them a measurement. On disk the index is 201 MB
built in 10-20 s `CONCURRENTLY`, against a 367 MB heap and 808 MB of existing
indexes: the relation grows about 17%. The five-column form without the tiebreak
was 163 MB in 6.6 s, so the tiebreak costs 38 MB and buys the page one order.

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

Six of the seven bite on the fix itself; the seventh is the answer.

| guard | where | what it fails to |
| --- | --- | --- |
| `TestCodeReachabilityPageRankIndexIsCreatedOnceAndNeverDropped` | `go/internal/storage/postgres/schema_index_replay_test.go` | a migration that stops creating the index, or a later file that drops it -- a create/drop pair rebuilds the index on every bootstrap, forever |
| `TestCodeReachabilityIndexMigrationsReapplyWithoutRebuildLive` | `go/internal/storage/postgres/code_reachability_index_replay_live_test.go` | the same, on a populated store, per definition rather than per pass |
| `TestCrossRepoDeadCodeConsumerEvidencePageBoundLive` index and work guards | `go/internal/query/code_dead_code_cross_repo_page_bound_live_guards_test.go` | an index whose key columns are in another order, and a page read that scans the entity's group instead of its `LIMIT` |
| the same test's answer guard | the same file | an ordering change that moves which entity the truncation marker lands on |
| the same test's work guard, sort assertion | the same file | a plan carrying any sort node at all. The row count says the `LIMIT` held; this says why, and it survives a planner finding some other way to over-read |
| the same test's retention arm | the same file | a scan reading more than the retention window explains, and -- through a floor under the same budget -- a fixture that has quietly stopped retaining generations, which would leave the ceiling bounding nothing |
| `TestCrossRepoDeadCodeConsumerPageOrderMatchesItsIndexKey` | `go/internal/query/code_dead_code_cross_repo_bound_test.go` | the statement's `ORDER BY` and migration 103's key drifting apart, from either side, including the tiebreak columns specifically. It reads both — the statement from its builder, the key from the shipped migration file — and runs in the unit lane rather than waiting for a Postgres |

The live proof applies the shipped migrations rather than a hand-written fixture
schema. That is not tidiness: on a hand-written schema with the same rows the
planner chose a `Hash Join` and a `top-N heapsort` and never considered the
index at all, so the guard would have failed for a reason that has nothing to do
with the code under test.

What the fixture can and cannot show is recorded in the test's own comment. At
fixture scale both plan modes take the index; on the 2.2M-row corpus a
251-entity page keeps the pre-index plan under a forced generic one, which is
what the `pg_prepared_statements` reading above exists to put in proportion.

The red/green captures for all seven guards are in
[#5167 code family batch 1 proofs](5167-code-family-batch-1-proofs.md), rows 55-63.
