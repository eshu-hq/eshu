# PR-B1 theory proof: a supports table can serve the scoped reads (#10880)

Status: **Theory proof, verdict GO.** No production code, no migration in a
mergeable path. Refs #10880, part of #10860. This record answers the
precondition the arbiter set in the PR-B ruling (`arbiter-10880-prb-ruling.md`,
option B): measure a per-support read against the legacy CTE with
`EXPLAIN (ANALYZE, BUFFERS)` before PR-B1 builds it. It sits beside
[the PR-B record](10880-pr-b-reader-moves-and-scoped-fork.md) and
[the family-move design](10880-first-result-family-move.md).

## Decision, threshold, deadline

- **Decision:** a supports-table read serves the scoped list, the scoped
  aggregates and explain correctly.
- **Threshold:** the supports read returns row-for-row the legacy answer on the
  committed multi-support seed for every grant shape (scope grants SA and SB,
  repository grant, union, derived-class rows absent in both); is within the
  legacy read's buffers and p95 at the same row counts on a representative
  corpus; and does not regress the unscoped winners route.
- **Deadline:** 90 minutes wall clock from 05:21 UTC, 2026-11-22. A failed
  attempt did not restart the clock.

## Verdict

**GO**, with three design corrections the first scratch read exposed (they are
in the design below, and each was measured):

1. Equality holds. 0 mismatches over 154 seed cases and 88 scaled cases
   (292,107 list rows, 337 explain lookups), with derived-class rows in the
   corpus and absent from every answer.
2. Time passes with margin: the final read is 0.03x to 0.36x of the legacy
   median on every shape, and its p95 is below the legacy p95 on every shape
   measured. Legacy itself misses the 1000 ms matrix bar for the unscoped
   `profile=comprehensive` filter (p95 1135 ms); the supports read does it in
   171 ms.
3. **One clause is not fully met and is reported as is:** the *buffer hit*
   count of the final read is above legacy's on three wide shapes (scope grant
   over half the scopes, unscoped `image_ref`, unscoped `comprehensive`). Hits
   are warm-cache probes; the cold distinct-block footprint is 8.0k against
   7.4k blocks (+8%). See "Buffers" below. The unscoped winners route is
   untouched.

## What was built, and where

Everything ran on a private `postgres:18` container (`10880-b1-pg`, port
25881, `shared_buffers=2GB`, `work_mem=16MB`, `jit=off`, `track_io_timing=on`),
bootstrapped with the repository's `ApplyBootstrap` so the 157 `fact_records`
indexes and all triggers are the real ones. The container is removed at the
end. The Go harness (equality, EXPLAIN, latency, concurrency) lives in this
worktree as untracked `zz_b1_*_scratch_test.go` files under
`go/internal/query/supply/chain/impact/` and is not committed; the SQL is
committed under `docs/internal/evidence/10880-pr-b1/` (see its README).

**Corpora.**

- *Seed:* the committed `seedMultiSupportCorpus` (14 static facts over 8 keys
  plus two derived-class facts), plus two same-scope facts that collide on one
  canonical key (`same_scope_collision.sql`).
- *Scaled:* `gen_scale_corpus.sql`. 800 scopes (480 repository, 320 image),
  20,000 canonical keys, 28,000 active supports (70% of keys one support, 20%
  two, 10% three, so 30% multi-support), 10,450 stale-generation supports (459
  scopes carry a superseded generation; this is heavier than production and
  harder on both reads), 200 operator suppression overrides (a third already
  expired), 40 derived-class and 200 static classifications, and 150,000
  unrelated `fact_records` rows as table noise. Payloads are about 2.3 KB.
  `scale_profile_divergence_patch.sql` then makes the even multi-support keys
  `affected_exact` so precise eligibility can diverge (the first generator tied
  multi-support keys to ineligible statuses; the census below is after the
  patch). This is synthetic data with no private content.

## 1. Census: what a support is

Queries are in `census.sql`. Anchors: `canonical_key` is
`CanonicalFindingKeySQL`; the fact id is `supplyChainImpactFactID`
(`supplyChainImpactFactRowIdentity` plus scope and generation).

| Question | Answer | Evidence |
| --- | --- | --- |
| Supports per canonical key (scaled, active, static) | 14,040 keys with 1, 3,960 with 2, 2,000 with 3 | `census.sql` section 1 |
| Can one scope hold two active supports of one key? | **Yes.** The Go fact id uses the logical identity (cve and advisory ids separate); the SQL key collapses them (`COALESCE(NULLIF(cve_id,''), NULLIF(advisory_id,''), '')` first). Two facts with `cve_id='ADV-C1'` and `cve_id=''` plus `advisory_id='ADV-C1'` share one SQL key and one scope. | `same_scope_collision.sql`: both facts get distinct support rows; `census.sql` then reports 1 duplicate (key, scope) pair on the seed |
| Do supports of one key differ? | Yes on every field the filters read. Of 5,963 multi-support keys: priority 5,960; priority bucket 5,560; match reason 3,960; severity bucket 3,600; image ref 1,928; **precise eligibility 1,960**; public finding id 1,000. | `census.sql` section 3 (and `TestDetectionProfileIsAPerSupportOutputOfOneCanonicalKey` for the writer reason) |
| Is `(canonical_key, scope_id)` unique? | **No.** Not by constraint, and shown by the collision above. | same |

**Ruling consequence:** the row identity is the support, `fact_id`. The
equality matrix stays green with the collision rows present (they are in the
seed database), so a `(canonical_key, scope_id)` key would have lost a row.

## 2. Scratch DDL and the candidate read

DDL is `supports_ddl.sql`; indexes are `supports_indexes.sql`; the backfill is
`supports_backfill.sql`, generated from `CanonicalFindingKeySQL` and
`severityBucketFactSQL` so the key and bucket expressions cannot drift from the
legacy read.

One table, `supply_chain_impact_finding_supports`, one row per finding fact
(source supports and operator overlay rows alike, as the legacy CTE treats
them): `fact_id` (primary key), `canonical_key`, `scope_id`, `generation_id`,
`finding_id`, `has_payload_finding_id`, `priority_score`, `source_confidence`,
`suppression_state`, the filter columns (`impact_status`, `ecosystem`
lower-cased with `LOWER(x COLLATE "C")`, `severity_bucket`, `repository_id`,
`cve_id`, `advisory_id`, `package_id`, `subject_digest`, `image_ref`,
`priority_bucket`, `detection_profile`, `observed_version`, `match_reason`) and
`payload JSONB`. It carries `generation_id`, and the read keeps the
active-generation join (`ingestion_scopes.active_generation_id` and
`scope_generations.status = 'active'`), as the ruling requires.

The candidate read is the legacy CTE with three fragments replaced
(`candidate_list_read_delta.sql`; the aggregate and explain reads get the same
treatment): `source_candidates` and `operator_candidates` read the supports
table; the page payload joins supports by `fact_id` instead of `fact_records`;
everything after (`source_winners` DISTINCT ON, `joined_winners`,
`canonical_facts`, `paged_facts`) is the legacy text unchanged, so the
pick-after-filter order that defines the legacy answer is preserved. Three
measured corrections to the first draft are in the final shape:

1. **Static-authority guard stays.** The two classification joins and the
   static predicate (`supplyChainImpactProtectedSourceJoinsSQL` and the static
   arm of `supplyChainImpactProtectedSourcePredicateSQL`) stay in the read. The
   supports backfill takes every fact, including the derived-class ones (the
   classification arrives in `scanner_image_result_fact_classifications`, not
   with the fact). Without the guard the candidate differs from legacy in 126
   seed comparisons and 64 scaled ones. The dead derived grant leg
   (`finding_class.source_class = 'derived'`) is not reproduced; no derived row
   appears in any candidate answer. Drop the guard only if the writer excludes
   derived-class facts at write time and a classification trigger removes the
   support; that is not proven here.
2. **Grant legs.** A granted read must drive from the grant, not scan every
   active scope and filter afterwards. The first shape joined
   `ingestion_scopes` first and applied the grant as a filter, so buffers did
   not fall with a narrow grant. The final shape reads the supports through
   `scope_id = ANY(scopes) UNION ALL repository_id = ANY(repos) AND NOT
   COALESCE(scope_id = ANY(scopes), FALSE)`. The `COALESCE` matters: an empty
   scope array is NULL, and `NOT NULL` is NULL, which dropped every
   repository-grant row (126 seed mismatches before the fix). This is a
   template for granted calls only. The unscoped existence-filter reads (image
   ref, profile) use the template without grant legs.
3. **Operator override by indexed probe.** Both the legacy CTE and the first
   candidate joined `operator_overrides` with a nested loop that re-ran the
   200-row DISTINCT ON subquery once per source winner (the plan shows
   `loops=20000` and 3,979,900 rows removed by join filter; the planner
   estimated one outer row). That cost 700 ms of the legacy 1.5 s on the
   unscoped `comprehensive` filter. The final shape probes the operator rows
   with `LEFT JOIN LATERAL ... ORDER BY priority_score DESC,
   has_payload_finding_id DESC, fact_id ASC LIMIT 1` over a partial index on
   `canonical_key WHERE scope_id = 'operator:vulnerability_suppressions'`.

## 3. Equality (the row-for-row bar)

Method: for each case the legacy query (`ListFindingsQuery`, the aggregate
`AggregateCanonicalFactsCTE` queries and `InventoryQueryTemplate`,
`ExplainFindingByPublicIDQuery`) and the candidate run with identical binds;
list rows compare in order (finding id, confidence, normalized payload JSON);
aggregate rows compare sorted. Aggregates compared: count, priority buckets,
severity buckets and five inventory dimensions (status, priority bucket,
severity, repository, ecosystem).

| Corpus | Grants | Filters | Cases | Mismatches | Rows compared |
| --- | --- | --- | --- | --- | --- |
| Seed (with collision) | SA, SB, SA+SB, SA+derived scope, derived scope only, repositories m1+m2, s2, m5, scope+repository unions, unscoped | none, status, profile precise, profile comprehensive, priority bucket, severity, min priority, image ref, include suppressed, suppression state, ecosystem | 154 | 0 | 533 list rows, 78 explain |
| Scaled (after the divergence patch), final shape | 5, 80, 400 scopes; 20 repositories; 80 scopes + 20 repositories; unscoped | same 11 | 88 | 0 | 292,107 list rows, 337 explain |

Derived-class coverage (the B0 arbiter requirement): the seed has the two
derived-class facts at priority 99 (one sharing m2's key inside an
`image://derived` scope, one with key m5 in scope SA) and the grant shapes
include a scope grant that does not cover the derived scope (SA), one that
does (SA+derived, derived only) and a repository grant naming m5. No candidate
list or aggregate contained a priority-99 row (0 of 533), matching legacy,
which excludes them too. The control (guard removed) differs from legacy in
126 comparisons, so the assertion has teeth.

The profile divergence the B0 test pins (p1 precise on SA, comprehensive on SB;
p2 the reverse) is answered identically by both reads for `profile=precise` and
`profile=comprehensive`, both under SA and SB grants and unscoped.

## 4. EXPLAIN (ANALYZE, BUFFERS): legacy CTE against the supports read

Method: `plan_cache_mode = force_custom_plan`; one warm-up run then three runs;
median of `Execution Time` and of the root node's shared blocks. "A" is the
first scratch read (default join shape); "B" is the final shape (grant legs for
granted rows, lateral override probe everywhere). Scaled corpus, limit 50,
default sort. Every run had `shared read = 0` and no temp spill. Host load1 was
19 to 27 and io PSI some avg10 36 to 71 during all runs (shared machine), so
read these as ratios, not absolute timings; the per-run load columns are in the
raw output.

list (limit 50):

| grant / filter | legacy ms | legacy buffers | A: supports ms | A buffers | B: final ms | B buffers | B / legacy ms |
|---|---|---|---|---|---|---|---|
| scope grant 5 scopes | 106.8 | 59458 | 32.8 | 9957 | 3.2 | 1878 | 0.03 |
| scope grant 80 scopes | 273.2 | 59458 | 137.5 | 44520 | 30.2 | 20721 | 0.11 |
| scope grant 400 scopes | 875.4 | 59458 | 535.8 | 55806 | 134.5 | 89707 | 0.15 |
| repository grant 20 repos | 116.8 | 59458 | 39.0 | 42140 | 5.9 | 3904 | 0.05 |
| union 80 scopes + 20 repos | 278.3 | 59458 | 151.5 | 44902 | 36.7 | 23373 | 0.13 |
| unscoped image_ref filter | 74.2 | 31453 | 30.4 | 41919 | 20.3 | 40987 | 0.27 |
| unscoped profile=comprehensive | 1504.5 | 59458 | 856.7 | 69936 | 183.1 | 109094 | 0.12 |

aggregate count:

| grant / filter | legacy ms | legacy buffers | A: supports ms | A buffers | B: final ms | B buffers | B / legacy ms |
|---|---|---|---|---|---|---|---|
| scope grant 5 scopes | 107.6 | 59258 | 32.1 | 9679 | 3.1 | 1600 | 0.03 |
| scope grant 80 scopes | 280.7 | 59258 | 139.4 | 44230 | 30.7 | 20431 | 0.11 |
| scope grant 400 scopes | 973.2 | 59258 | 539.1 | 55522 | 138.7 | 89423 | 0.14 |
| repository grant 20 repos | 119.0 | 59258 | 34.2 | 41796 | 5.4 | 3560 | 0.05 |
| union 80 scopes + 20 repos | 299.7 | 59258 | 146.4 | 44588 | 36.8 | 23059 | 0.12 |
| unscoped image_ref filter | 70.2 | 31253 | 27.0 | 41635 | 25.6 | 40703 | 0.36 |
| unscoped profile=comprehensive | 1684.2 | 59258 | 872.5 | 69640 | 188.1 | 108798 | 0.11 |

inventory by repository:

| grant / filter | legacy ms | legacy buffers | A: supports ms | A buffers | B: final ms | B buffers | B / legacy ms |
|---|---|---|---|---|---|---|---|
| scope grant 5 scopes | 102.9 | 59258 | 32.1 | 9679 | 2.8 | 1600 | 0.03 |
| scope grant 80 scopes | 261.7 | 59258 | 139.9 | 44230 | 32.6 | 20431 | 0.12 |
| scope grant 400 scopes | 876.8 | 59258 | 543.9 | 55522 | 145.0 | 89423 | 0.17 |
| repository grant 20 repos | 116.2 | 59258 | 36.7 | 41796 | 5.2 | 3560 | 0.04 |
| union 80 scopes + 20 repos | 280.5 | 59258 | 148.7 | 44588 | 38.7 | 23059 | 0.14 |
| unscoped image_ref filter | 69.5 | 31253 | 31.1 | 41635 | 22.8 | 40703 | 0.33 |
| unscoped profile=comprehensive | 1513.6 | 59258 | 865.7 | 69640 | 192.9 | 108798 | 0.13 |

explain by public id (one visible finding):

| grant / filter | legacy ms | legacy buffers | supports ms | supports buffers |
|---|---|---|---|---|
| scope grant 5 scopes | 58.0 | 30522 | 0.2 | 4 |
| scope grant 80 scopes | 56.6 | 30522 | 0.2 | 4 |
| scope grant 400 scopes | 54.1 | 30522 | 0.2 | 4 |
| repository grant 20 repos | 55.4 | 30522 | 0.2 | 4 |
| union 80 scopes + 20 repos | 55.1 | 30522 | 0.2 | 4 |
| unscoped image_ref filter | 61.4 | 31020 | 0.3 | 17 |
| unscoped profile=comprehensive | 57.4 | 30522 | 0.2 | 4 |

Winners baseline, unscoped, no filter, measured in the same session with the
supports table present: list 0.7 ms (281 buffers), aggregate count 16.6 ms
(4,939 buffers). That route does not read the supports table and its SQL is
unchanged, so it cannot regress; the table adds only 87 MB of cache pressure.

**p95 on 30 runs each** (plain execution, no EXPLAIN, final shape; load1 20 to
26):

| query | grant/filter | read | p50 ms | p95 ms | max ms | n | load1 |
|---|---|---|---|---|---|---|---|
| list (limit 50) | scope grant 80 scopes | legacy CTE | 213.5 | 219.3 | 221.8 | 30 | 25.69 |
| aggregate count | scope grant 80 scopes | legacy CTE | 229.3 | 233.1 | 235.7 | 30 | 24.16 |
| list (limit 50) | scope grant 80 scopes | supports+lateral+grantleg | 34.7 | 35.7 | 36.2 | 30 | 24.16 |
| aggregate count | scope grant 80 scopes | supports+lateral+grantleg | 34.6 | 35.1 | 35.2 | 30 | 24.16 |
| list (limit 50) | repository grant 20 repos | legacy CTE | 116.5 | 119.1 | 122.2 | 30 | 23.10 |
| aggregate count | repository grant 20 repos | legacy CTE | 118.4 | 122.3 | 123.7 | 30 | 22.13 |
| list (limit 50) | repository grant 20 repos | supports+lateral+grantleg | 11.1 | 12.0 | 12.3 | 30 | 22.13 |
| aggregate count | repository grant 20 repos | supports+lateral+grantleg | 11.3 | 11.9 | 12.2 | 30 | 22.13 |
| list (limit 50) | scope grant 400 scopes | legacy CTE | 618.4 | 621.8 | 623.3 | 30 | 20.15 |
| aggregate count | scope grant 400 scopes | legacy CTE | 713.5 | 715.4 | 718.8 | 30 | 21.51 |
| list (limit 50) | scope grant 400 scopes | supports+lateral+grantleg | 129.2 | 130.2 | 130.8 | 30 | 21.51 |
| aggregate count | scope grant 400 scopes | supports+lateral+grantleg | 131.8 | 133.5 | 134.3 | 30 | 21.95 |
| list (limit 50) | unscoped profile=comprehensive | legacy CTE | 1087.8 | 1134.8 | 1141.1 | 30 | 24.22 |
| aggregate count | unscoped profile=comprehensive | legacy CTE | 1277.2 | 1350.6 | 1357.2 | 30 | 21.75 |
| list (limit 50) | unscoped profile=comprehensive | supports+lateral+grantleg | 167.3 | 171.4 | 176.2 | 30 | 20.73 |
| aggregate count | unscoped profile=comprehensive | supports+lateral+grantleg | 171.4 | 174.8 | 175.6 | 30 | 19.95 |

**Buffers.** Shared hits count every page touch, including repeated index
probes of the same pages. The final read is below legacy on all narrow grants
(1.9k to 23k against 59k) but above it on three wide shapes: scope grant over
400 scopes (89.7k, legacy 59.5k, 0.15x time), unscoped `image_ref` (41.0k,
legacy 31.5k) and unscoped `comprehensive` (109k, legacy 59.5k, 0.12x time).
The extra hits are two index probes per surviving candidate (the classification
primary key and the lateral operator probe) over 28,000 candidates, all
warm. To see the I/O footprint rather than the touch count, the container was
restarted before each run and one `EXPLAIN` taken un-warmed: distinct blocks
read were 7.4k (legacy) and 8.0k (supports) for the 80-scope grant, 7.4k and
8.0k for the repository grant, 7.4k and 8.0k for `comprehensive` (+8%, about
60 MB for both). Those cold runs used the first read shapes (A, and A with the
lateral probe), taken before the grant legs existed, so they overstate the
narrow-grant footprint of the final shape. Cold wall time on that run is not usable (io PSI above 50 from
other tenants). The tuning room: the per-candidate probes disappear if the
classification is carried on the support row and kept current by a trigger on
`scanner_image_result_fact_classifications` (the table already has statement
triggers feeding the winners dirty log); that is a B2 option, not needed for
the time bar.

## 5. Indexes: only what a measured plan needs

Variant runs on the scaled corpus, final read shape, buffers are shared hits:

| Index set | scope 80 list | repository 20 list | union list | unscoped image_ref list |
| --- | --- | --- | --- | --- |
| primary key, `(scope_id)`, `(repository_id)`, `(finding_id)`, operator partial | 30.8 ms, 20,767 | 6.0 ms, 3,919 | 35.7 ms, 23,448 | 26.7 ms, 41,380 |
| drop `(repository_id)` | 36.2 ms, 28,917 | **82.5 ms**, 13,924 | 40.1 ms, 30,816 | 22.1 ms, 41,380 |
| `(scope_id, generation_id)` instead of `(scope_id)` | 30.2 ms, 20,723 | 6.0 ms, 3,904 | 34.9 ms, 23,375 | 17.2 ms, 30,539 |
| drop the scope index | 36.0 ms, 25,317 | 81.3 ms, 14,370 | 38.9 ms, 27,662 | 9.6 ms, 9,492 (seq scan) |

- **Keep** the primary key (`fact_id`: page join and every override join),
  `(repository_id)` (the repository grant leg is 14x slower without it), the
  `(finding_id)` index (explain by public finding id takes 4 buffers, 0.2 ms,
  against 30,522 buffers and 55 to 61 ms for legacy), and the partial operator
  index on `canonical_key` (the lateral probe and the explain operator CTE).
- **Use `(scope_id, generation_id)`,** not `(scope_id)`: same grant-leg
  performance, and it lets the read skip stale-generation rows (10,450 here)
  without a heap visit. It also cut unscoped `image_ref` buffers from 41k to
  30.5k, below legacy's 31.5k.
- The no-scope-index run shows the unscoped existence-filter read prefers a
  sequential scan (9.5k buffers). Hash or btree: btree throughout; no hash
  index (no equality-only column is hot enough to justify the lost range and
  ordering use, and hash needs its own WAL and vacuum story). GIN on the
  runtime-filter arrays is not needed: the runtime filter resolves repository
  ids from active facts and compares `repository_id`, which the repository
  index serves.
- Unmeasured, so not built: any index for `canonical_key` over all scopes
  (needed by the granted explain-by-canonical-key selector, not run here; see
  NOT_CHECKED), `image_ref`, `detection_profile`.

## 6. Concurrency: the same-transaction maintainer

Design. In the writer transaction of `PostgresSupplyChainImpactWriter`
(`writer_retract.go`): the conflict-domain advisory lock
(`lockSupplyChainImpactConflictDomainQuery`, key
`supplyChainImpactConflictDomainKey(scope, generation)`) is taken first; then
`BatchInsertVersionedFacts` upserts the facts; the supports upsert comes next,
in the same transaction and under the same lock; the retraction
(`retractSupersededSupplyChainImpactFindingsQuery`) tombstones facts and a
companion `DELETE` removes the supports of that (scope, generation) not in the
keep set; dirty-log capture and the rest are unchanged. Operator overlays are
written by `VulnerabilitySuppressionStore.Upsert` in its own transaction;
their supports are written there. The support row is keyed by `fact_id`, which
embeds scope and generation, so two writers for different domains never touch
one support row; the supports upsert is ordered by `fact_id`, and it is guarded
by `WHERE (existing columns) IS DISTINCT FROM (EXCLUDED columns)` so a replay
changes nothing.

Lock order, and why no cycle with the winners upsert: domain advisory lock, then
fact rows, then support rows, then dirty-log rows. The winners resweep
(`rebuildSupplyChainImpactWinnersSQL`, single-flight by
`tryClaimSupplyChainImpactWinnersResweepSQL`) takes its own advisory lock and
writes only the winners and watermark tables; it reads facts and (in PR-C)
supports without row locks. The writer transaction never writes winners rows.
No path holds a supports row while waiting for a winners row or the reverse.

Demonstration (`zz_b1_conc_scratch_test.go`, a SQL replica of the writer
transaction on the scaled database, since the real writer needs its full
fixture; see NOT_CHECKED):

| Race | Result |
| --- | --- |
| 40 pairs of writers (80 transactions), different scopes, the same three canonical keys | 0 errors, 6 support rows, 3 keys, 237 ms |
| 30 pairs (60), one (scope, generation), different finding sets `{f1,f2}` and `{f2,f3}`, **with** the domain lock | 0 errors; fact set `{f2,f3}` equals support set `{f2,f3}` |
| same race **without** the lock (control) | **40 of 60 transactions deadlocked**; the surviving state was still consistent |
| replay the same write five times, write-on-change guard | support row versions unchanged (`xmin` sum 12,652 before and after) |
| replay with a plain `ON CONFLICT DO UPDATE` (control) | row versions changed (12,676 to 12,696) |

The control shows the domain lock is load-bearing for the supports writes, the
same as for facts, and that the guard is what keeps a rerun from writing WAL.

## 7. Write cost per generation save

One rolled-back transaction on the scaled corpus (`write_cost.sql`): 1,411
findings (40 scopes, the corpus average of 35 per scope) written as a new
generation, then retracted. WAL bytes by `pg_current_wal_insert_lsn()`:

| Step | WAL bytes | Per finding |
| --- | --- | --- |
| Fact insert (157 `fact_records` indexes present, 12 of them on this kind) | 26,565,200 | 18.8 KB |
| Supports insert (payload column included) | 5,723,296 | 4.1 KB |
| Fact tombstone update | 3,444,512 | 2.4 KB |
| Supports delete | 100,648 | 0.07 KB |

Rows per generation save: one support row per finding written, so rows written
rise by one per finding (the facts are unchanged). The supports insert is
**21.5% of the fact insert's WAL**; insert plus retraction together add
**19.4%** (5.82 MB on 30.0 MB). The supports table is 87 MB (62 MB heap) for
38,664 rows against the 375 MB `fact_records` of this corpus. The payload column
is the cost driver and the PR-C target: after finding facts stop being written,
the supports payload replaces the fact payload, so the net becomes negative
(ten partial indexes leave `fact_records`). Until then the narrow alternative
(no payload, page payload from `fact_records`) would cut the supports insert
to roughly the index and key columns; it is not chosen because PR-C needs the
payload on the support row. This is a ratio on a synthetic corpus, not a
production estimate; the rate of generation saves in production is unmeasured.

## 8. Design the production PR builds

**DDL** (numbered migration, schema lockstep entries): the table above with
`fact_id` primary key; indexes `(scope_id, generation_id)`, `(repository_id)`,
`(finding_id)`, partial `(canonical_key) WHERE scope_id =
'operator:vulnerability_suppressions'`. Nothing else until a measured plan asks.

**Maintainer (PR-B1):** supports written in the writer transaction under the
conflict-domain lock, derived from the just-written fact rows
(`INSERT ... SELECT ... FROM fact_records WHERE fact_id = ANY($1) ORDER BY
fact_id ON CONFLICT (fact_id) DO UPDATE ... WHERE ... IS DISTINCT FROM ...`) so
the fact payload stays the one source of truth for the key and buckets; same
transaction for the retraction delete and for operator overlay writes. A
backfill walks active scopes in chunks, each under that scope's conflict-domain
lock so it cannot interleave with a writer, using the same SQL. Drift audit:
supports against active facts, reusing the winners-audit pattern; telemetry:
supports written, deleted and unchanged per save, drift count, and a counter
for reads refused for a missing table.

**Reads (PR-B2):** two templates per read. Granted: grant legs, lateral
override probe, static guard, active-generation join. Unscoped existence
filter: same without grant legs. A missing or behind-contract supports table
answers 503; a granted call is never served from winners. Explain by public
finding id reads the supports `(finding_id)` index.

**Equality gate (PR-B1/B2):** the matrix of section 3 as a committed live test
on the seed (every grant shape x every filter, derived-class rows, the
same-scope collision), plus the scaled run as a measured gate.

## NOT_CHECKED

- The real `PostgresSupplyChainImpactWriter` with the supports statements in
  its transaction. Section 6 uses a SQL replica, so dirty-log capture, protected
  publication and the pass-state write are not exercised. The production PR
  needs a live test of two real writers racing one key.
- Production scale. 28,000 active supports on 800 scopes is the stated gate
  shape; 9,000 scopes and QA volumes were not run. The scope index's benefit
  is size dependent (about 20% at this size); re-measure at 10x before PR-B2.
- Runtime filters (`service_id`, `workload_id`, `environment`): the runtime
  filter CTE is carried unchanged with empty binds; those filters were not
  exercised on either read.
- Granted explain by fact id or canonical key
  (`ExplainFindingQuery`, the non-public selectors) and its need for an
  all-scope `canonical_key` index.
- The explain read was compared only for `ExplainFindingByPublicIDQuery`.
- Cold-cache latency (host io PSI above 50 made it unusable) and any p95 above
  30 runs. QA gate state, QA census of multi-support keys, `make pre-push`,
  `make pre-pr` (owner order: none run).
- The derived path: not reproduced (supports are static-only, as the arbiter
  ruled). Opening the derived branch belongs to #9468.
- The classification-on-the-row option for removing the per-candidate probes.
