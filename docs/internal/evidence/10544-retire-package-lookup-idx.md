# #10544: retire fact_records_supply_chain_impact_package_lookup_idx

Issue #10544 (parent #9975) found the keyed winners probe still planning on
the triple-only package-lookup index after migration 291 built
`fact_records_supply_chain_impact_winners_keyed_idx`. The arbiter ruling on
#10544 (2026-11-11) retires the old index: root cause is costing, not
matching — the planner estimates rows=1 for a correlated triple through
either index, so the heap-fetch waste is invisible to it. This note records
the two gates the ruling required before code, and what shipped.

Investigation budget: decision recorded at start — ship the drop only if
Gate A (QA census) and Gate B (disposable fixture) both pass; stop for a new
ruling on any Gate A disproof. Started 2026-11-10T23:25Z, one approach, no
clock resets.

## Gate A: read-only QA census

The QA environment (Postgres primary and read replica, 2026-11-10
23:25Z, all read-only; one faithful-lateral EXPLAIN ran inside a
rolled-back transaction with a temp identifier table, as in the #10544
evidence). `pg_stat_database.stats_reset` is NULL on both (never reset
since postmaster start 2026-10-16), so `idx_scan` is lifetime traffic, not
post-291 attribution:

| Index | Primary idx_scan / fetch | Replica idx_scan / fetch | Size |
|---|---|---|---|
| package_lookup (old) | 1,577,763 / 99,079,875 | 87,487 / 44,375 | 618 MB |
| winners_keyed (291) | 0 / 0 | 0 / 0 | 519 MB |

Migration 291 applied 2026-11-10T22:46:36Z, 292 at 22:48:01Z
(`eshu_schema_migrations` tail); both indexes `indisvalid`/`indisready`.
`fact_records` last manual ANALYZE 2026-11-10T23:04Z (after 291), so stale
statistics alone no longer explain the choice.

Plans as shipped (primary `EXPLAIN`, never `ANALYZE`; literals from a live
winners row and dirty-log row):

- Verbatim single-identifier probe leg: Index Scan on the old index,
  cost 0.55..2.79 rows=1, c0/advisory as heap Filter. Replica
  `EXPLAIN (ANALYZE, BUFFERS)`: same plan, actual 26 rows kept, 74 rows
  removed by filter (100 actual triple rows against the rows=1 estimate),
  834 buffers.
- Faithful shipped lateral (`buildWinnersKeyedActiveFactsSQL` over a
  3-identifier analyzed temp table): Nested Loop over old-index scans,
  same Filter, cost 2.79 per identifier. Finish-lane re-run
  (2026-11-10T23:55Z, primary, `VALUES` CTE in place of the temp table)
  reproduces the same plan and costs, with lifetime counters since
  grown (package_lookup 1,623,641 scans / 126,539,265 fetches / 623 MB;
  winners_keyed still 0 / 0, 558 MB). Scope-join check on the same 3
  dirty-log identifiers: 360 triple candidates each, 29 five-equality
  kept, 29 after the active-generation scope join — mismatch 0.
- `findRepositoryFindingHoldersQuery`
  (`supply_chain_impact_repository_holders.go:28`) verbatim: Index Scan on
  the old index with only `repository_id = ANY(...)` as the condition (a
  skip over the leading package column), cost to 7312. This is the one
  non-winners statement planning on the old index today; its intended
  index, `fact_records_supply_chain_impact_repository_lookup_idx`
  (leading repository expression, 542 MB, 2.97M lifetime scans), serves
  the same predicate and the Gate B fixture shows identical buffers.
- `listSupplyChainAdvisoriesForReposQuery`
  (`service_vulnerability_advisory_loader.go:24`) verbatim: nested loop
  over `fact_records_supply_chain_impact_active_scan_idx`, repository as
  filter — not the old index.
- `listActiveSecurityAlertReconciliationFactsQuery`
  (`facts_active_security_alert_reconciliation.go:14`) verbatim:
  `fact_records_scope_generation_idx`; its kind set is an `IN` list, which
  cannot prove either partial index's predicate.
- `FindSupplyChainImpactHoldersQuery` (`supply_chain_impact_fanout.go:50`)
  and `listScopedSupplyChainImpactFindingPackagesQuery`
  (`supply_chain_impact_scoped_packages.go:20`) verbatim: active-scan
  index, payload filters residual.

Code census (every shipped statement filtering the finding kind by a
triple-expression subset, `generation_id`, or `fact_id` ordering):

- Triple equality, whole triple: only the keyed probe
  (`supply_chain_impact_winners_keyed_sql.go:121`).
- Triple subset as optional filters: query impact candidates
  (`aggregates_queries.go:43`, `findings_split_queries.go:35`,
  `explain_postgres.go:333`, all `($n = '' OR payload->>... = $n)` with
  `fact_kind` a bound parameter, so no partial index is provable under a
  generic plan; under a custom plan the package prefix is identical in
  both indexes). Package-only leg on QA: old index today, cost to
  7218.63 for an estimated 6,563 rows, 144 buffers for LIMIT 100 on the
  replica; the 291 index leads with the same expression.
- Repository-only: the two holder reads and the advisory loader above
  (repository-lookup index leads; old/new both carry it second).
- Package/repository in projections or canonical-key concatenations
  (`supply_chain_impact_canonical_winners_store.go:198`,
  `findings_queries.go:18`, writer retract `RETURNING` in
  `writer_retract.go:333`): not predicates; the `COALESCE(...,'')` form
  does not match the index expression in either index.
- `generation_id`: every finding-kind use pairs it with `scope_id`
  equality or the active-generation join (full rebuild
  `supply_chain_impact_canonical_winners_store.go:48`, staged scan
  `supply_chain_impact_winners_staged_scan.go:66`, scoped and out-of-stamp
  reads, `writer_changed.go:120`, scope expansion
  `supply_chain_impact_winners_keyed_sql.go:68`) — the
  `(scope_id, generation_id, fact_id)` active-scan index. In the old
  index `generation_id` is fifth, behind unconstrained `fact_id`, so it
  was never a search bound there anyway.
- `fact_id` ordering/cursor on the finding kind: the advisory loader
  (repository-only, above), the reconciliation read (kind `IN`, above),
  and the staged batch (scope/generation pair, active-scan order). None
  binds all three triple columns, so none can use the one ordering only
  the old index provides: triple equality then `fact_id`. The fixture
  measures that unshipped shape to bound the risk.

Verdict: PASS. No shipped statement needs the old index's unique
property, and the two traffic classes that touch it today (probe,
holders) have an equal or better index after the drop.

## Gate B: disposable PostgreSQL 18 fixture

Throwaway container (PostgreSQL 18.6), 203,000 findings with ~1.2 KB
payloads: 4,000 correlated triples (package and repository 1:1, digest
constant) at 50 rows per triple, one hot triple of 3,000, plus 20,000
non-finding rows. At/above the contract-test scale (43,000).

RED, both indexes present, single-identifier verbatim probe on a 50-row
triple, each index forced by dropping the other:

| Forced | Plan cost | Est. rows | Actual | Buffers |
|---|---|---|---|---|
| old | 0.42..8.46 | 1 | 1 kept, 49 filter-removed | 13 |
| 291 | 0.42..8.45 | 1 | 1 kept, 0 removed | 4 |

Forced total costs differ by 0.01 at rows=1 while the triple really holds
50 rows: the planner is indifferent, which is the QA mechanism (QA breaks
the tie toward the old index; the fixture happens to break it toward
291). The durable regression tests encode the retirement, not the tie:
`TestSupplyChainImpactPackageLookupIndexRetired` and the index-parity arm
of `TestSupplyChainImpactWinnersResweepKeyedProbePlanLive` both failed on
the pre-change tree (old index present in the Go fragment and after a
full bootstrap).

GREEN, old index dropped, shipped `buildWinnersKeyedActiveFactsSQL` over
an analyzed temp identifier table (30 of the identifiers on the hot
triple at n≥50): at 1 / 50 / 200 / 2000 identifiers the 291 index is
searched exactly n times, returns n kept rows, removes 0 rows by filter,
no sequential scan of `fact_records`, buffers 6 / 202 / 802 / 8002 —
byte-identical to the both-indexes run, and the result-set md5 at every
scale is identical before and after the drop (0-diff). Verbatim literal
post-drop: 291 index scan, 4 buffers, all five equalities as index
conditions.

Non-winners shapes, buffers (`EXPLAIN ANALYZE`) both-indexes vs post-drop,
row sets identical (md5) in every row:

| Shape | Both | Post-drop | Plan after drop |
|---|---|---|---|
| holders (repository ANY + scope join) | 31 | 31 | bitmap on repository-lookup |
| advisory loader (repository ANY + cursor + LIMIT) | 34 | 34 | bitmap on repository-lookup |
| package-only LIMIT 100 | 13 | 13 | index scan on 291 |
| scope+generation+CVE | 40,676 | 40,676 | sequential in both (one scope in fixture; QA uses active-scan) |
| triple + ORDER BY fact_id LIMIT 10 (hot, unshipped) | 9 | 1,150 | 291 scan + sort |

The last row is the only regression class and no shipped statement has
that shape (census above). Nothing sequential-scans `fact_records` after
the drop that did not already, and every shipped shape is within the
ruling's 10% buffer bar (equal, in fact).

## Fix

- Migration 293
  (`293_drop_fact_records_supply_chain_impact_package_lookup_idx.sql`):
  one `DROP INDEX CONCURRENTLY IF EXISTS`, so existing deployments lose
  the index without blocking finding writers, fresh databases converge
  through 003 → 291 → 293, and the coordinator runs it outside a
  transaction as a sole concurrent statement.
- `schema_fact_records.go`: the Go bootstrap fragment creating the index
  is removed, so isolated/Go DDL parity matches the post-293 state.
  Migration 003 itself is byte-identical (#7002); the one cost is that a
  full untracked replay rebuilds the index in 003 and drops it in 293,
  recorded as the single entry in `replayRebuiltIndexNames`. Tracked
  installs skip 003 entirely and converge once.
- Probe SQL (`buildWinnersKeyedActiveFactsSQL`) and
  `analyzeWinnersKeyedIdentsSQL` are unchanged, per the ruling. Winner
  selection, the single-flight lock, and worker concurrency are
  untouched; finding writes shed one partial-index maintenance per row.
- Rollback, if ever needed, is recreating the old index definition from
  003 — but that restores the cost tie this change removes.

## Tests

- `TestSupplyChainImpactPackageLookupIndexRetired` (new): the Go fragment
  lacks the retired index and keeps its neighbors plus 291; migration 293
  exists, is one sole concurrent `DROP INDEX CONCURRENTLY IF EXISTS`.
  RED on the pre-change tree.
- `TestSupplyChainImpactWinnersResweepKeyedProbePlanLive` (extended):
  after a full bootstrap the retired index is absent and 291 present; a
  planted copy of the retired index is detected by the same census
  (seeded-violation arm) and dropped before the existing plan assertions
  (291 searched once per identifier, 50 kept of a 3,000-candidate hot
  triple, MATERIALIZED fence, no sequential scan). RED on the pre-change
  tree at the absence assertion.
- Bootstrap parity pins move with the new file: checksum manifest,
  embed golden digest (count 318 → 319), ordered definition names, and
  the replay guard entry above.

## Performance Evidence:

QA before (2026-11-10, migrations 291/292 applied 22:46-22:48Z): probe
plans on the old index at cost 2.79 rows=1 (replica actual: 100 triple
rows, 834 buffers for one identifier); #9975 acceptance (keyed p95,
watermark) stays NOT_CHECKED for 291 until the post-293 re-roll re-read,
per the ruling. Fixture: forced-old vs forced-new single-identifier
buffers 13 vs 4 at 50 rows/triple, scaling with candidate count (the
hot-triple candidate fetches are what the shipped lateral no longer does
at 1/50/200/2000 identifiers). Write side: one fewer partial index
(622 MB on QA) maintained per finding insert. No shipped read shape
regresses (Gate B table; equal buffers everywhere a statement ships).

## No-Observability-Change:

No new metric, span, log, route, worker, or knob. The existing
`tick_phase_duration_seconds{phase="build_active_facts"}` histogram and
the winners telemetry page's probe cost model (one 291 index descent plus
kept heap rows per identifier) remain the operator readout; operators
verify the retirement through `pg_indexes` / `pg_stat_user_indexes` and
the plan-shape live test.
