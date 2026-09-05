# #5167 — The Cross-Repo Hidden-Consumer Walk

`POST /api/v0/code/dead-code/cross-repo` reports whether a symbol defined in one
repository is still called from another. A scoped caller holds an API token
granted a named set of repositories and may not read consumers outside that set.
So beside the page of consumer evidence it returns, which binds that grant, the
route runs a second and deliberately narrow read: does this producer symbol have
a consumer in a repository the caller was not granted? Only yes or no leaves it.
A symbol whose only consumers are hidden stays `unknown_needs_evidence` with
reason `permission_hidden_consumer` rather than being reported dead.

This note is the measurement record for that second read — the three shapes
withdrawn on measurements, the four cost axes each was measured against, and the
bootstrap-replay proof for the two index migrations it rests on. The route
behaviour it serves is in
[#5167 code family batch 1](5167-code-family-batch-1.md); the red/green runs and
the mutation ledger are in
[#5167 code family batch 1 proofs](5167-code-family-batch-1-proofs.md).

Terms, glossed once and then used freely. A **grant** is the set of repository
ids a scoped token may read. A **producer** repository defines a symbol and a
**consumer** repository calls it; `code_reachability_rows` holds one row per
producer entity, consumer repository and resolution. An **ingestion scope** is
what the collector writes under — a git repository scope owns one repository,
and a repository can be covered by more than one scope. A **generation** is one
write pass of a scope: a new one adds rows and leaves the previous set on disk
until the retention runner prunes it, so a row is live only under its scope's
active generation. A **loose index scan** walks an index key by key, seeking to
the smallest value greater than the last rather than scanning a whole group;
Postgres has no operator for it, so the walk below is written as a recursive CTE.

No-Observability-Change: the probe adds no metric instrument, span, log event or
runtime knob. The cross-repo consumer read's existing `postgres.query` span
gains one attribute, `db.rows.consumer_signal_entities`, counting the producer
entities the probe flagged, and both reads are timed by the
`eshu_dp_postgres_query_duration_seconds` histogram that already covers this
route.

## Two Bounded Reads, Not An Unbounded Complement

The signal half went through four shapes, and the three that were withdrawn
were withdrawn on measurements rather than taste.

The first counted the *complement* of the page with one `LATERAL` arm per
producer entity, each capped at 100 rows. That cap bounds rows returned, not
rows scanned, and it misses the common case: when the grant covers most
consumers, every arm inspects all of its entity's reachability rows to prove
none are outside the grant.

The second was the shipped page statement re-run with no grant bound. Its plan
is genuinely capped at 1,001 rows, which is what made it acceptable — but the
cap is on rows *returned*, and its `ORDER BY entity_id, confidence DESC, ...`
makes the `Incremental Sort` above it consume one producer entity's whole
fan-in group before it can emit that group's first row. A producer entity with
a million consumer rows therefore costs a million-row scan on every scoped
request, and spends the shared 1,001-row budget where nothing later on the page
can be proven.

That property belongs to the grant-bound evidence page too, and this change does
not fix it; the closing section carries its numbers. The withdrawal below is
about the signal half only.

The third stopped asking for rows at all and asked only the question the count
needs. Per producer entity: is there one active-generation consumer row in a
repository outside the grant? It expressed "outside the grant" as
`repository_id` ranges around the sorted grant — below the smallest granted id,
between two consecutive ones, above the largest — so
`code_reachability_entity_repository_idx` (migration
the two-column `code_reachability_entity_repository_idx`) could seek to each range and
stop at its first row. That is a seek per range instead of a scan of the group,
and against a five-repository grant it measured two orders of magnitude better
than the read it replaced. It is also where the grant became the cost: one seek
per range means one seek per granted repository, and the section below is about
the size of grant that makes visible.

The fourth walks the other side of the question — the producer entity's own
distinct consumers, in index order, stopping at the first one that is both
outside the grant and live — so its cost follows the answer rather than the
entity's row fan-in or the caller's grant. What ships now,
`crossRepoDeadCodeUngrantedConsumerProbeQuery`, is that walk with one thing
corrected: it steps over `(repository_id, scope_id)` pairs on migration 101's
four-column index and seeks each pair's active row by full key equality, rather
than stopping at a pair's first row and hoping nothing older is still on disk.
[Retention Is An Axis Too](#retention-is-an-axis-too) is why.

Performance Evidence: `EXPLAIN (ANALYZE, BUFFERS)` in a throwaway PostgreSQL
16.15 container, data-plane schema applied from `schema/data-plane/postgres`
(`001_ingestion_scopes.sql`, `002_scope_generations.sql`,
`027_code_reachability.sql`) in filename order plus the new index migration,
synthetic rows only, `VACUUM ANALYZE` after seeding, `SET jit = off`
throughout, warm runs reported, three samples each. Host: MacBook Pro, arm64,
macOS. 2,201,196 `code_reachability_rows`; one active scope and generation; a
250-entity producer page whose middle entity carries 1,000,000
active-generation consumer rows across five consumer repositories, plus 1.2M
rows on entity ids off the page. Both statements were run twice: once as
`EXPLAIN` on a literal statement (a custom plan) and once through
`PREPARE`/`EXECUTE` under `plan_cache_mode = force_generic_plan`, because pgx's
statement cache puts these reads on a generic plan and a literal `EXPLAIN`
hides that difference.

The measured envelope on that seed, beside the machine profile: the table is
1,154 MB and `code_reachability_entity_repository_idx` 16 MB against a 139 MB
`code_reachability_entity_lookup_idx` — both of this index's key columns repeat
heavily within an entity, so btree deduplication compresses it far better than
the existing ones. Per-statement shared buffers are in the tables below.

| Metric | Withdrawn unrestricted signal read | Shipped ungranted-consumer probe |
| --- | ---: | ---: |
| Execution time, custom plan | 757.6 / 756.9 / 779.5 ms | 4.95 / 4.62 / 4.65 ms |
| Execution time, generic plan | 950.2 / 951.3 / 1021.2 ms | 4.72 / 4.58 / 4.78 ms |
| Rows read under the driving scan | 1,000,497 | 0 |
| Rows returned | 1,001 | 0 |
| Shared buffers | hit=646 read=26929 | hit=4886 |
| Driving access | `Index Scan` on `code_reachability_entity_lookup_idx` under an `Incremental Sort` presorted on `entity_id`, under `Limit` | `Index Scan`s on `code_reachability_entity_repository_idx`, each `Index Cond` carrying `entity_id` plus the walk's seek |

That is the no-hidden-consumer case, which is the one that has to prove a
negative. With one consumer repository taken out of the grant the probe answers
in 3.13 / 2.99 / 2.89 ms, `hit=3006` buffers.

The index alone fixes nothing: with `code_reachability_entity_repository_idx` in
place but the predicate left as `NOT (repository_id = ANY($grant))`, the planner
takes a `Parallel Seq Scan` and removes 733,732 rows by filter, 105.1 ms. What
the index buys is a seek, and the shape has to ask for one.

### The Grant Is An Axis Too

The first shape to use that index expressed "outside the grant" as
`repository_id` ranges around the sorted grant, one range per gap. It was
correct, and its cost was one index probe per granted repository per producer
entity — so it scaled with the caller's grant rather than with the answer, and
the five-repository measurement above could not see it. Codex raised it as a P1
on the shipped statement. It reproduces:

| Grant size | Ranges, every consumer granted | Walk, every consumer granted | Ranges, a hidden consumer | Walk, a hidden consumer |
| ---: | ---: | ---: | ---: | ---: |
| 5 | 7.26 / 6.95 / 6.82 ms | 4.95 / 4.62 / 4.65 ms | 6.70 / 6.05 / 6.40 ms | 3.13 / 2.99 / 2.89 ms |
| 50 | 60.81 / 60.38 / 59.85 ms | 4.92 / 4.72 / 4.57 ms | 20.46 / 20.23 / 20.21 ms | 3.19 / 3.01 / 2.96 ms |
| 200 | 247.54 / 248.82 / 247.64 ms | 4.93 / 4.73 / 4.66 ms | 79.67 / 79.14 / 80.08 ms | 3.24 / 3.15 / 3.06 ms |
| 500 | 641.86 / 633.64 / 635.63 ms | 5.16 / 5.01 / 4.96 ms | 209.80 / 209.66 / 210.11 ms | 3.57 / 3.43 / 3.52 ms |

Shared buffers say it more plainly than the timings do. The ranges read
`hit=7622`, `hit=63877`, `hit=251377`, `hit=626377` as the grant grows; the walk
reads `hit=4886` at every one of those four sizes — the same number, because it
does the same work. Generic-plan runs are within noise of the custom-plan ones
throughout (walk: 4.72 / 4.75 / 4.79 / 5.09 ms at 5 / 50 / 200 / 500).

The shape that replaces the ranges is a loose index scan, one walk per producer
entity: seed at that entity's smallest consumer pair, step to the smallest one
strictly greater, and stop at the first pair that is both outside the grant and
live. A GRANTED consumer repository costs one step however many scopes cover
it, so the walk passes at most `min(d, N)` of them for `d` distinct consumer
repositories and a grant of `N`, where the ranges cost `N + 1` regardless of
`d`. What an UNGRANTED one costs is a separate question, and
[Stale Consumers Are An Axis Too](#stale-consumers-are-an-axis-too) is it. What a step costs is a second question, and this seed
answered it wrongly: it holds one generation, so a step's first row is always
the active one. The section below is that correction.

The walk has its own axis, `d`, and it was measured rather than assumed. A
producer entity consumed by 300 distinct repositories, all granted, with a
500-id grant: the walk takes 8.05 / 7.79 / 7.76 ms (`hit=6081`), the ranges
631.99 / 629.74 / 628.31 ms (`hit=626377`). There is no crossover in the
measured space, and the reason is structural rather than lucky: the walk passes
a granted repository in one step, so it can pass at most `min(d, N)` of them,
which is never worse than the ranges' `N`.

Grant order stops mattering as a side effect. The ranges were only correct with
the grant sorted in the database's collation — Go's byte order is not that
collation, and a mis-sorted bound list puts a granted repository inside a range
the probe treats as ungranted. The walk tests membership with an equality
against the `granted` CTE, which Postgres hashes, so no bound is rendered per
granted repository and nothing depends on sort order.

Exactness: the walk returns the same producer entities as the
`NOT (repository_id = ANY($grant))` it replaces, symmetric difference `0/0`, for
sixteen grant shapes on that data — the eight the ranges were accepted on (every
consumer granted, a hidden consumer below the smallest granted id, between two
of them, above the largest, a single-element grant, a grant wider than the
corpus, a grant disjoint from every consumer, and a grant naming only the
producer repository), the 50-, 200- and 500-id grants in both the all-granted
and hidden forms, and the 300-repository fan-out page in both.
Sixteen is the count measured in the container. The differential that ships is
narrower and its own count is ten named grant shapes:
`TestCrossRepoDeadCodeUngrantedConsumerProbeLive` runs the eight accepted
shapes plus both 500-id grants against a disposable Postgres, and adds three
plan assertions: that the walk's per-step seek reaches an index
condition rather than a filter, that the liveness lookup reaches one carrying
all four key columns, and that the recursive term's measured row count stays
inside a budget. The last exists because the walk's stop condition is a
bound on work and not on the answer — remove it and every verdict is identical
while each walk enumerates every consumer repository its entity has.

Both plan assertions run twice, once with the values in hand and once under
`plan_cache_mode = force_generic_plan` through `PREPARE`/`EXECUTE`. That is not
belt and braces: pgx caches server-side prepared statements, so these reads run
on a generic plan in production, and the range shape withdrawn above planned
identically to the walk under a custom plan and then lost its bounds from the
`Index Cond` under a generic one. A guard that only asks the planner with the
values in hand cannot see that class of regression at all. Each pass also
checks it got the plan it asked for — a generic plan leaves the producer
repository a parameter marker where a custom plan inlines it — so a refactor
that stopped forcing the mode fails instead of quietly asking the same question
twice.

### Retention Is An Axis Too

The grant-size seed above holds one ingestion scope and one generation, so
every row in an `(entity_id, repository_id)` group belongs to the active
generation and a step's `LIMIT 1` stops on its first row. Installs do not look
like that, and Codex raised it as a P1 on the shipped statement.

`ReplaceCodeReachabilityRepositoryRows` deletes by
`(scope_id, generation_id, repository_id)` before it writes
(`deleteCodeReachabilityRepositoryRowsSQL`), so a new generation *adds* a row
set and leaves the previous one in place. The only pruner is the
generation-retention runner: `generationRetentionCandidateQuery` selects
`status = 'superseded'` generations ranked per scope, and
`deleteScopeGenerationsForRetentionQuery` deletes them, taking the reachability
rows with them through `ON DELETE CASCADE`. `DefaultGenerationRetentionPolicy`
keeps the 24 most recent superseded generations per scope *and* everything
superseded inside the last seven days, so a scope resynced every five minutes
holds roughly two thousand. A generation in any other non-active status is
never a candidate at all.

So a group holds one row per retained generation per root, the active row is
the newest of them, and a step ordered by `repository_id` reaches it last. The
per-repository bound was not a bound.

Performance Evidence: same container, schema, `SET jit = off`, `VACUUM ANALYZE`
and warm three-sample method as above, on a second seed built to be
retention-representative — one ingestion scope per consumer repository (which is
who writes its rows), three groups of five consumer repositories differing only
in retained superseded generations (0, 20, 200), every generation carrying the
same population, superseded rows written before the active one, and a 201-row
producer-repository run on every page so the only axis is consumer-side
retention. 883,750 rows, 1,316 generations, 516 scopes, table 161 MB.
125-entity producer page, five consumer repositories, grants
`{a,c,e,g,i}` and `{a,c,g,i}`.

| Retained generations | 0 | 20 | 200 |
| --- | ---: | ---: | ---: |
| The two-column index's walk, every consumer granted | 22.3 / 20.0 / 20.3 ms | 89.2 / 86.9 / 87.7 ms | 630.4 / 630.9 / 629.0 ms |
| — shared buffers | hit=39,403 | hit=154,603 | hit=1,150,489 |
| Shipped walk, every consumer granted | 5.13 / 4.72 / 4.70 ms | 8.14 / 7.90 / 7.34 ms | 9.78 / 8.98 / 8.87 ms |
| — shared buffers | hit=3,270 | hit=3,268 | hit=3,263 |
| Shipped walk, a hidden consumer | 4.67 / 4.18 / 4.19 ms | 6.51 / 6.48 / 6.08 ms | 7.68 / 6.38 / 6.44 ms |
| — shared buffers | hit=3,142 | hit=3,140 | hit=3,138 |

The buffer counts are the claim. They do not move with retention, because a
step no longer scans a group for its active row; the residual rise in time at
constant buffers is traversal inside pages already read. Forced-generic runs
agree (5.08 / 5.48 ms at 0 retained, 8.88 / 8.74 ms at 200) and take the same
index.

Two changes make that hold, and the second is not optional. The walk steps over
distinct `(repository_id, scope_id)` PAIRS rather than repositories, because
"is this consumer live" means "does a row exist under the active generation of
the scope that wrote it" and only the scope says which generation that is — a
repository ingested by two scopes has two, and a walk keyed on the repository
alone would test one and miss the other. And the liveness test is written as a
correlated seek on the pair the walk just found, `(entity_id, repository_id,
scope_id, generation_id)` all equalities, with the generation coming from
`ingestion_scopes` by primary key. Left as joins on the outer row, the planner
is free to reorder them, and on this seed it did: it drove the whole walk from
`ingestion_scopes`/`scope_generations` and probed
`code_reachability_rows_pkey` once per scope — 64,500 probes for the seed term
alone, 266.5 / 263.9 / 264.9 ms and `hit=292,615` at 0 retained generations,
where the two-column index's own seed reported 4.9 ms. The documented loose index scan
was not the plan Postgres chose as soon as the seed had more than one scope.

The liveness seek sits behind the grant test rather than beside it
(`NOT granted AND EXISTS (...)`), so it runs only for a repository outside the
grant. A granted repository continues the walk whether it is live or not, so
its answer is never needed. Evaluating it on every step instead measured
8.14 / 7.87 / 8.76 ms against 4.18 / 4.01 / 4.13 on the grant-size seed.

Nothing the earlier shape bought is given back. On that seed — 2,201,196 rows,
one scope, one generation, a producer entity with 1,000,000 consumer rows —
the shipped walk reads 4.18 / 4.01 / 4.13 ms `hit=3,764` at a five-repository
grant and 4.51 / 4.28 / 4.28 ms `hit=3,764` at 500, against 5.12 / 4.49 / 4.49
`hit=4,863` and 5.38 / 4.88 / 4.85 `hit=4,888` for the walk it replaces. Flat
across grant size, flat across row fan-in, and slightly cheaper than what it
replaces on the seed that shape was tuned for.

Exactness: symmetric difference `0/0` against the
`NOT (repository_id = ANY($grant))` reference across 33 grant shapes measured
in the container — the eight accepted shapes on each of the three retention
levels of the new seed, and nine on the grant-size seed including the 500-id
grant. The shipped differential still runs its ten named shapes, now with
`ent-retained` on the page for every one of them.

The cost is index size and it is stated rather than waved at. Migration 101's
`code_reachability_entity_repository_scope_generation_idx` is 79 MB against
the two-column index's 7,520 kB and a 161 MB table on the retention-representative
seed, and 16 MB against 17 MB on the single-generation one, because btree
deduplication collapses a suffix that never varies. Migration 102 drops
the two-column index: its key is a strict prefix of 101's, so no read needs it
and keeping it would make every reachability write maintain a second btree. The
reducer therefore maintains one index either way.

### Scopes Per Granted Repository Are An Axis Too

The pair walk fixed the retention axis and opened a new one, which Codex found
by reading the statement rather than a plan. It steps over
`(repository_id, scope_id)` PAIRS but tests membership with
`granted.repository_id = pair.repository_id`, so a granted repository covered by
many ingestion scopes costs one step per scope. The walk then passes more
granted PAIRS than the grant has repositories, so the granted half of the bound
in its own contract did not hold. The seeds above could not show it: every
one of them gives a repository exactly one scope.

Every skipped pair is granted, and `hidden` requires ungranted, so no answer
changes either way. Only a work measurement can see this, which is why the guard
that ships counts rows rather than checking verdicts.

The step now depends on what it just found. The recursive term carries
`is_granted`, and picks between two `UNION ALL` seek branches gated on it:
`repository_id >` the current one from a granted pair, the
`(repository_id, scope_id)` row comparison from an ungranted one, because an
ungranted repository's remaining scopes are exactly where a hidden consumer
could still be. Two branches rather than one seek with a `CASE` bound, because a
`CASE` cannot become an index condition and a bound built that way would leave
every step scanning. Postgres gates each branch with
`Result (One-Time Filter: walk.is_granted)` and reports the other `never
executed`, so a step still performs ONE seek, and both branches keep their
`Index Cond` on `code_reachability_entity_repository_scope_generation_idx` with
`Heap Fetches: 0`.

Performance Evidence: throwaway PostgreSQL 16.15 container, data-plane schema
from migrations 001/002/027 plus 101, `SET jit = off`, `VACUUM ANALYZE`,
`PREPARE`/`EXECUTE`, warm, three samples. The seed is the retention seed above
extended with a scopes axis: two 125-entity pages differing ONLY in how many
ingestion scopes cover their single granted consumer repository — one scope for
`ent-sfo1-*`, fifty for `ent-sfo50-*` — each with the same ungranted live
consumer sorting after it, so both pages return the same answer and only the
step count differs. 815,625 rows, 1,168 generations, 68 scopes, heap 148 MB,
walk index 110 MB.

| Scopes on the granted repository | 1 | 50 |
| --- | ---: | ---: |
| Pair-stepping walk, recursive rows | 250 | 6,375 |
| — shared buffers | hit=2,255 | hit=26,788 |
| — custom plan | 2.61 / 3.31 / 4.10 ms | 60.98 / 184.60 / 59.69 ms |
| — forced generic | 29.53 / 1.92 / 1.96 ms | 60.70 / 332.75 / 430.40 ms |
| Shipped walk, recursive rows | 250 | 250 |
| — shared buffers | hit=2,255 | hit=2,255 |
| — custom plan | 2.66 / 2.18 / 2.17 ms | 2.46 / 2.21 / 2.18 ms |
| — forced generic | 2.19 / 3.00 / 2.04 ms | 6.07 / 2.16 / 2.37 ms |

The row counts are the claim and they are exact rather than approximate: 6,375
is 125 entities times 51 steps — the granted repository's fifty scopes plus the
hidden consumer — and 250 is 125 times 2. Buffers follow them, 26,788 against
2,255. At one scope the two shapes are indistinguishable, which is the point:
this removes a cost that only appears when a repository has more than one scope,
and adds none where it does not. The 29.53 ms and the 184.60 / 332.75 / 430.40
ms samples are first-run and cold; the later samples in each row are the warm
ones, and both are reported rather than the flattering one.

No-Regression Evidence: on the retention seed, 125-entity page, custom plan,
grants `{a,c,e,g,i}` and `{a,c,g,i}` — the pair-stepping shape and this one
produce identical recursive-row counts (625 all-granted, 375 with a hidden
consumer) and identical buffers at every point measured: `hit=3,002` and
`hit=2,752` at 0 retained generations, `hit=3,064` and `hit=2,783` at 200. Warm
times agree within noise (200 retained, all granted: 8.75 / 8.96 ms against
8.59 / 8.92 ms). One earlier sample read `hit=2,487` for the pair-stepping shape
on that page and did not reproduce on a repeat run of the same pair; the
repeated measurement is the one reported.

Exactness: symmetric difference `0/0` against the
`NOT (repository_id = ANY($grant))` reference across thirteen shapes on this
container — both scope-axis pages against five grants each (both granted, all
three granted, either one alone, and a grant naming none of them), and the three
retention pages against their own grant. The shipped differential adds three
fixtures of its own: `ent-scopes-granted`, whose granted consumer repository
carries fifty scopes and whose hidden consumer sits past it, under a walk-row
budget; `ent-scopes-ungranted`, whose ungranted repository has fifty scopes with
only superseded rows and a fifty-first that is live, so the walk must reach the
last scope of a repository it cannot see; and `ent-scopes-ungranted-stale`, with
nothing live in any scope. A plan assertion requires the granted skip to reach
an index condition rather than a filter, in both plan modes.

### Stale Consumers Are An Axis Too

The walk's continue-condition is `NOT walk.hidden`, and `hidden` is
`NOT is_granted AND EXISTS (a live row)`. Three cases follow and only the third
stops it: a granted pair continues, an ungranted pair with a live row stops, and
an ungranted pair WITHOUT one continues. That third case is a consumer
repository that used to call the symbol and no longer does.
`ReplaceCodeReachabilityRepositoryRows` leaves the previous generation's rows on
disk, and `DefaultGenerationRetentionPolicy` keeps the 24 most recent superseded
generations per scope plus everything superseded inside the last seven days, so
they are still there to be walked — one pair at a time, because the ungranted
step seeks the next PAIR and an exhausted repository simply hands it the next
repository's first one.

So the bound is not `min(d, N) + 1`. It is the granted repositories passed, at
most `min(d, N)` of them, plus the ungranted `(repository, scope)` pairs passed
that hold no live consumer row, plus one — and the third term is bounded by the
retention window rather than by `d` or `N`.
`TestCrossRepoDeadCodeUngrantedConsumerProbeLive` asserts that count rather than
describing it: `ent-stale-repos` carries 300 stale ungranted consumer
repositories and one live hidden consumer after them under a one-repository
grant, and the recursive term must produce exactly 301 rows in both plan modes.
Set to the 2 the old contract claimed, both modes fail with `the recursive walk
produced 301 rows, want exactly 2`.

Performance Evidence: throwaway PostgreSQL 16.15 container, migrations
001/002/027 plus 101/102, `SET jit = off`, `VACUUM ANALYZE`, warm, three
samples, both plan modes. The grant-size seed above extended with one ingestion
scope per stale consumer repository — which is how a git repository scope owns a
repository — and 20 retained superseded generations each, inside the retention
policy's 24. 2,447,218 rows, 302 scopes, 6,302 generations. One producer entity,
one-repository grant.

| One entity, one-repository grant | Walk rows | Buffers | Custom plan | Generic plan |
| --- | ---: | ---: | ---: | ---: |
| 300 GRANTED consumer repositories (the control) | 300 | hit=913 | 2.99 / 3.09 / 3.01 ms | 3.27 / 3.32 / 3.54 ms |
| 300 stale ungranted pairs, 20 rows each | 301 | hit=25,825 | 19.07 / 19.41 / 19.26 ms | 19.28 / 19.46 / 18.92 ms |
| 300 stale ungranted pairs, 400 rows each | 301 | hit=365,181 | 297.0 / 290.7 / 295.5 ms | 293.7 / 294.8 / 298.0 ms |

The step count is the same 301 in all three stale rows; the buffers are not, and
that is a second defect rather than this axis being expensive by nature. On this
corpus the liveness `EXISTS` loses its four-column `Index Cond`: the planner
takes `(entity_id, repository_id, scope_id)` and then probes
`scope_generations` once per retained row — 119,601 probes for one entity — so
every passed stale pair scans the pair's rows instead of seeking past them. The
axis and the plan are separable: at ONE retained generation per pair the
`Index Cond` is already three columns, so retention depth multiplies a cost the
corpus statistics chose.

Restricting the ungranted step's own seek to live pairs was measured and
rejected. Joined to `ingestion_scopes` on the active generation it walks 2 rows
rather than 301, and reads `hit=4,304` in 26.6 / 27.5 / 27.3 ms, because the
restriction never becomes an index condition: one step reads 119,620 index rows
under a hash join against a 302-row `Seq Scan on ingestion_scopes`. It trades a
per-pair cost for a cost in the entity's TOTAL retained rows — the fan-in
sensitivity this walk exists to remove — and would lose on a high-fan-in entity
such as the seed's 1,000,000-row one.

### The Migrations Replay On Every Bootstrap

`migrations/` has no applied-migration ledger, so every bootstrap replays the
whole directory and a create paired with a later drop would rebuild an index on
every start, forever. Migration 101 builds the four-column index and migration
102 drops the two-column one it supersedes; there is no migration 100 any more.
The reasoning, the two tests that prove it, and the pre-existing `fact_records`
offender the second test found are in
[#5167 code-reachability index migration replay](5167-code-reachability-index-migration-replay.md).

The evidence page is unchanged and still reads that group. It carries the same
`ORDER BY entity_id, confidence DESC, ...` ahead of its `LIMIT 1001`, so its
scan is bounded by a producer entity's fan-in rather than by the limit — it has
to rank a producer entity's consumers by confidence to return the strongest, so
that cost is what the page returns rather than an artefact. On the same seed it
takes 885 ms reading 1,000,497 rows with the whole five-repository grant bound,
and 752 ms reading 800,373 with four of the five. Removing the second traversal
of that group is what this change buys; the page's own bound is tracked in
#6527, filed with these measurements, and is the next thing to look at here.

Two consequences are declared rather than incidental. The probe returns
producer entity ids and nothing else, so no ungranted repository id, consumer
entity id, or citation crosses the reader boundary at all — the count is derived
from the caller's own producer entities. And the count it contributes is one per
producer entity that has a hidden consumer, not one per hidden consumer:
`hidden_consumer_evidence_count` reports `1` where it used to report the number
of ungranted rows the capped read happened to see. Classification never used
more than "is it above zero", the number was never a total (the read it came
from stopped at 1,001 rows across the whole page and marked the rest truncated),
and `hidden_consumer_evidence_count` is in no OpenAPI schema or public reference.

On an existing deployment both index migrations run `CONCURRENTLY`, on the dedicated
bootstrap connection the schema apply path runs each definition on, so it does
not block the reducer's reachability writes while it builds. The usual
objection to `CONCURRENTLY` -- that a failed build leaves an `INVALID` index
which `IF NOT EXISTS` then skips forever -- does not apply here, because that
path drops invalid concurrent indexes by name before executing each definition
(`SQLDB.dropInvalidConcurrentIndexes`). That is also why the index cannot join
`027_code_reachability.sql`: that definition is multi-statement, and a
multi-statement `Exec` is sent as an implicit transaction, which
`CONCURRENTLY` refuses, and it is why migrations 101 and 102 are two files
rather than one. Both are registered in the ordered bootstrap list
(`schema_order_test.go`) like every other definition, and the live proof applies
them in that order before it runs, then asserts the state they leave: 101's
index built, 100's gone.

The full rationale lives in the migration file itself, which is where this
repository puts it -- migrations 082, 084 and 099 do the same. It is not in
`go/internal/storage/postgres/README.md` or that package's `AGENTS.md` because
both are pinned by the Markdown line-cap grandfather ledger
(`scripts/lib/markdown-line-cap-grandfather.tsv`, 3,766 and 1,172 lines), which
lets a pinned file shrink but never grow, and refuses a raised pin. There is no
document that lists migrations; `docs/public/reference/postgres-tuning.md` is
operator knob guidance, and this index is not a knob. The reader-side invariant
is in `go/internal/query/AGENTS.md`.
