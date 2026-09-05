# #5167 — Stale Consumers, The Walk's Fourth Axis

The cross-repo hidden-consumer walk does not stop at the first UNGRANTED
consumer pair. It stops at the first HIDDEN one, and hidden means ungranted AND
live, so a consumer repository that used to call the symbol and no longer does
is a pair the walk steps past rather than stops at. This note is the record for
that axis: what the walk's real bound is, what passing those pairs cost before
the liveness lookup was pinned to a seek, and the alternative that was measured
and rejected. The other three axes — grant size, retention depth within one
pair, and ingestion scopes per granted repository — and the shapes this walk
replaced are in
[#5167 cross-repo hidden-consumer walk](5167-cross-repo-hidden-consumer-walk.md).

Root-Cause Evidence: the observation is the recursive term's own row count.
`EXPLAIN (ANALYZE)` of the shipped statement for one producer entity with 300
ungranted consumer repositories whose rows are all superseded, under a
one-repository grant, reports `Recursive Union … rows=301` in both plan modes,
where the bound the statement's contract claimed — `min(d, N) + 1` — predicts 2.
`TestCrossRepoDeadCodeUngrantedConsumerProbeLive` carries that as an assertion.

No-Observability-Change: the probe adds no metric instrument, span, log event or
runtime knob; the cross-repo consumer read's existing `postgres.query` span and
the `eshu_dp_postgres_query_duration_seconds` histogram already cover it.

## The Axis

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

The step count is the same 301 in all three stale rows; the buffers were not,
and that was a second defect rather than this axis being expensive by nature. On
this corpus the liveness `EXISTS` lost its four-column `Index Cond`: the planner
took `(entity_id, repository_id, scope_id)` and then probed `scope_generations`
once per retained row — 119,601 probes for one entity — so every passed stale
pair scanned the pair's rows instead of seeking past them. The axis and the plan
are separable: at ONE retained generation per pair the `Index Cond` was already
three columns, so retention depth multiplied a cost the corpus statistics chose.

The fix is to stop giving the planner the choice. The generation now arrives as
a SCALAR SUBQUERY inside the `generation_id` equality rather than as a join to
`ingestion_scopes` on the outer row, so it is a parameter the index condition
can carry: `(entity_id = …) AND (repository_id = …) AND (scope_id = …) AND
(generation_id = $18)`, on every corpus measured. The right column above is that
shape; the joined column is what shipped before it. The control row is
unchanged, which is the point — this removes a cost that only appears when the
walk passes ungranted pairs.

| One entity, one-repository grant | Joined liveness | Seeked liveness |
| --- | ---: | ---: |
| 300 stale ungranted pairs, 400 rows each | hit=365,181, 297.0 / 290.7 / 295.5 ms | hit=3,897, 5.64 / 5.66 / 5.56 ms |
| 300 stale ungranted pairs, 20 rows each | hit=25,825, 19.07 / 19.41 / 19.26 ms | hit=3,625, 5.53 / 5.23 / 5.25 ms |
| 300 stale ungranted pairs, nothing live | hit=365,179, 295.3 / 293.0 / 288.1 ms | hit=3,895, 5.04 / 4.55 / 5.16 ms |
| 300 GRANTED consumer repositories | hit=913, 2.99 / 3.09 / 3.01 ms | hit=913, 3.01 / 3.06 / 3.08 ms |

No-Regression Evidence: the two shapes were run interleaved on one machine under
one load, because the axes they must agree on are the ones already measured
above. 251-entity page, 500-id grant, 1,301 walk rows: `hit=4,681` both, joined
7.946 / 8.057 / 8.077 ms and 8.074 / 7.941 / 8.018 ms against seeked
8.161 / 8.223 / 7.814 ms and 7.623 / 8.141 / 8.038 ms; forced generic
8.110 / 8.132 / 8.120 against 7.913 / 8.329 / 7.981. `ent-m-hot`, 1,000,000
consumer rows, five-id grant: `hit=25` and 5 walk rows both, 0.132 / 0.142 /
0.135 ms against 0.122 / 0.135 / 0.128 ms.

Exactness: symmetric difference `0/0` against the
`NOT (repository_id = ANY($grant))` reference on ten shapes — the three stale
pages under a one-repository grant, the fan-out page all-granted and
none-granted, `ent-m-hot` all-granted and with one consumer hidden, and the
251-entity page under the 500-id grant, a five-id grant, and a grant disjoint
from every consumer, where all 251 come back hidden. The subquery cannot return
more than one row: `ingestion_scopes` by primary key, `scope_generations` by
primary key.

`code_dead_code_cross_repo_bound_test.go` pins the subquery form and the absence
of the join, rather than a plan assertion, because a small fixture cannot
reproduce a planner decision: the same statement's liveness lookup took three
different plans across the corpora here — the four-column seek at 20, 100 and
300 stale scopes in an isolated schema, a `code_reachability_rows_pkey` seek
driven from `ingestion_scopes` at 50, and the three-column scan on the
2,447,218-row corpus. A guard sized for a unit-test corpus would pass for
whichever plan it happened to get.

Restricting the ungranted step's own seek to live pairs was measured and
rejected. Joined to `ingestion_scopes` on the active generation it walks 2 rows
rather than 301, and reads `hit=4,304` in 26.6 / 27.5 / 27.3 ms, because the
restriction never becomes an index condition: one step reads 119,620 index rows
under a hash join against a 302-row `Seq Scan on ingestion_scopes`. It trades a
per-pair cost for a cost in the entity's TOTAL retained rows — the fan-in
sensitivity this walk exists to remove — and would lose on a high-fan-in entity
such as the seed's 1,000,000-row one.
