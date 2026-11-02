# Supply-chain impact canonical winners materialization (#3389)

Tracks the maintained cross-scope dedup materialization that removes the
read-time `ROW_NUMBER` dedup spill from `GET /api/v0/supply-chain/impact/findings`.
Design + measured PoC:
`docs/internal/supply-chain-impact-canonical-dedup-materialization-design.md`.

## Scope of this change (Phase 1a + 1b write-core)

- Phase 1a: `supply_chain_impact_canonical_winners` denormalized read-model table
  + bootstrap registration.
- Phase 1b (write core): `supplyChainImpactWinnerSelectSQL` (winner per
  canonical_key, same tiebreak as the read) + `rebuildSupplyChainImpactWinnersSQL`
  (atomic upsert-all + delete-stale) + `SupplyChainImpactWinnersStore.RebuildAllWinners`
  (backfill / full resweep).

No runtime consumer is wired yet: the shared-projection partition worker
(incremental per-canonical_key recompute + dirty-key/generation-flip wiring) and
the read switch are later phases. This change only adds a table and a
recompute/backfill writer that nothing on the request path calls yet.

## Measurement environment

Isolated throwaway Postgres 18 (`postgres:18-alpine`, `work_mem=16MB`), seeded
with 500,000 active `reducer_supply_chain_impact_finding` facts across 901 active
scopes (the same seed used for the #3389 read measurements). Schema applied from
the shipped `033_supply_chain_impact_canonical_winners.sql`; the shipped
`rebuildSupplyChainImpactWinnersSQL` (dumped from the Go const) was executed
directly.

No-Regression Evidence: the recompute is byte-identical to the legacy read-time
dedup. After `RebuildAllWinners` populated 500,000 winner rows, a winners-derived
read (`SELECT … FROM supply_chain_impact_canonical_winners w JOIN fact_records
refetch ON refetch.fact_id = w.winner_fact_id WHERE <filter on w> ORDER BY
finding_id`) was compared via `COPY (...) TO STDOUT WITH (FORMAT csv)` against the
legacy `listSupplyChainImpactFindingsQuery` output for the same filters. `diff`
is empty for: no filter (500,000 rows), `impact_status=affected_exact` (125,000),
`severity_bucket=high` (100,000), and `ecosystem=npm` (166,666). The winner
selection reuses the read's exact canonical_key, public finding_id fallback,
`has_payload_finding_id` tiebreak, severity-bucket thresholds, and
suppression-state default; the parity is pinned in Go by
`TestSupplyChainImpactWinnerSelectMirrorsReadDedup` and proven against the legacy
read by the CSV diff above.

Performance Evidence: the maintained shape moves the dedup off the read path. The
backfill `RebuildAllWinners` materialized 500,000 winners in ~4.6 s (one-time /
resweep). The downstream read against the winners table (validated in the design
PoC, same seed) is `O(page)`: broad-filter first page **0.18–0.65 ms**, no-filter
first page **1.0 ms**, vs the legacy read-time-dedup **1873 ms** (~2000×), with no
external-merge spill. The read switch lands in a later phase; this note records
that the recompute produces the rows that read will serve.

Observability Evidence: No-Observability-Change for the storage-layer table +
recompute writer themselves (they add no metric/span/log/status/graph/queue and
nothing on the request path calls the writer). Runtime visibility for the
maintainer that drives it is covered below.

## Phase 1b maintainer (this change) — reducer side-runner

`SupplyChainImpactWinnersMaintainer`
(`go/internal/reducer/supplychain/core/winners_maintainer.go`) runs as a
reducer service side-runner: one resweep at startup (backfill/reconcile) and then
on a fixed cadence (default 30s), each resweep calling `RebuildAllWinners` (the
atomic upsert-all + delete-stale statement).

Design choice — correctness over incrementalism: a full atomic resweep is used
instead of per-canonical_key dirty tracking. The atomic reconcile recomputes the
winner set from the current active facts, so it cannot miss a change class
(generation-activation flips, tombstones, new sources are all captured). This
removes the "missed dirty signal / stale winner served" correctness risk the
incremental design carried (ADR §10). Incremental per-key recompute remains a
future performance optimization.

Concurrency (conflict domain): the conflict domain is the whole winners table
during a resweep. A single-owner partition lease (`SupplyChainImpactWinnersDomain`,
partitionID 0, partitionCount 1, reusing the shared-intent
`ClaimPartitionLease`/`ReleasePartitionLease`) normally keeps one reducer instance
resweeping at a time. It is not renewed during a resweep, so a statement that
outlasts the TTL can overlap the next owner; #9774 below closes that case with a
single-flight lock.
Transaction scope = one `RebuildAllWinners` transaction (atomic per resweep). Retry
scope = the maintainer loop (next cadence, exponential backoff capped at 5m on
error). The lease is released after every cycle (even on error), bounding takeover
to the lease TTL; the idempotent rebuild is the backstop if the lease is lost
mid-run — the next owner reconciles to the same state.

No-Regression Evidence: maintainer logic is covered by
`winners_maintainer_test.go` across the replay/retry matrix:
lease-acquired resweep, lease-not-acquired skip (no rebuild), rebuild-error with
the lease still released (no held lease after error), idempotent repeated cycles
(claim/release balanced), missing-dependency validation, and context-cancel loop
exit. `go test ./internal/reducer ./cmd/reducer -count=1` passes; the winners the
resweep writes are byte-identical to the legacy read (proven above).

Observability Evidence: the maintainer emits structured logs on resweep commit
(debug) and failure (error, with the wrapped cause carrying the
lease-claim/rebuild/release failure class), so an operator can see whether
resweeps are progressing or failing. Resweep recency is independently queryable as
`materialized_at` on the singleton `supply_chain_impact_winners_materialization`
watermark, which the read surfaces as `truth.freshness` in Phase 2. (Before #9774
the winners table's own `MAX(materialized_at)` also tracked recency; it now means
the last content change, see below.) No graph write or queue consumer is
added. Richer per-resweep duration/row-count metrics are a follow-up; the logs +
materialized_at timestamp give progress/failure visibility now.

## Write only changed rows (#9774)

The resweep still recomputes every winner each tick, but it now writes only the
rows that are new or changed. `rebuildSupplyChainImpactWinnersSQL` adds a
`changed` CTE (a winner with no stored row, or whose data columns are
`IS DISTINCT FROM` the stored row) and the `INSERT` reads from it, so an
unchanged row is never locked, versioned, or written to WAL. A plain `DO UPDATE
... WHERE` was measured and rejected: it versions no rows but still locks and
WAL-logs every conflicting row. The final watermark upsert and the delete stay
in the same statement, so every committed tick still stamps the watermark.
`materialized_at` is left out of the comparison and is written only with a row,
so on a winner row it means "last content change"; readers use the watermark. The
statement returns inserted, updated, deleted and unchanged counts and the
watermark it replaced; the maintainer turns them into telemetry.

Performance Evidence: scratch PostgreSQL 18.6, real winners DDL (migrations 033
and 083), the statement rendered from the shipped constant, 29,870 winners and
93,795 findings (synthetic ~1 KB payloads, sized to QA's row counts; its read cost
does not match QA, see the scale section), 6 interleaved rounds per phase with an
alternating first mover, autovacuum off and an explicit `VACUUM` every second
round. Counters, per 6 ticks, A = statement on main, C = shipped:

| Phase | Variant | Row versions | WAL, MB | Winners vs fresh recompute |
|---|---|---:|---:|---|
| nothing changed | A | 179,220 | 310.48 | 0 rows differ |
| nothing changed | C | 0 | 1.08 | 0 rows differ |
| 30 changed findings per tick | A | 179,060 | 285.03 | 0 rows differ |
| 30 changed findings per tick | C | 233 | 6.58 | 0 rows differ |

That is 29,870 to 0 row versions and about 51.7 MB to 0.18 MB of WAL per
unchanged tick, and 47.5 MB to 1.1 MB per tick under small churn. QA change rate
(read replica, two hashed dumps 5 minutes apart, aggregates only): 35,466 and
35,429 rows, 142 inserted, 179 deleted, 81 changed, 402 touched, 1.13% of rows,
under the 5% bar. Six further bucketed samples over 15 minutes gave 1.62% and a
worst per-tick rate near 0.3%. A CVE wave cannot cost more than main: at 10x, 100%
churn cost 4,010 MB and 16.7 s against main's 4,494 MB and 17.5 s.

### What this does not fix: the read half (design question)

QA's real tick is far slower than the scratch run, so the scratch wall times say
nothing about QA headroom and this note no longer quotes them as a QA figure.
Measured on QA from a `pg_stat_statements` delta (15.5 minutes, 13 calls,
aggregates only, read-only), on the deployed statement that writes every row:
**39.8 s per statement, 71.5 s per tick, 242 MB of WAL per tick**. That WAL is
about 4.7 times the scratch run's 51.7 MB, so the real saving is larger than the
table above. The 30 seconds is the pause between resweeps, not the period.

The trigger this work set for per-key incremental recompute, a p95 above half the
cadence (15 s) for an hour, is **already exceeded at 1x**: 39.8 s is 2.65 times
it. That changes what this PR means. Write-on-change removes the WAL and row
versions, and EXPLAIN ANALYZE of the compare half shows the anti-join and delete
probe at about 3% of a 42.7 s statement, so it cannot remove the time. The read
half is the whole cost and it stays O(findings). The full-recompute loop has no
headroom left at today's scale; moving to incremental recompute, or otherwise
cutting the read half, is due now rather than a future option. A pause, cap, or
worker-count change does not address it, and none is proposed here.

### Scale: 1x, 5x, 10x

Peak watermark age is about 2 x statement + 30 s, because the stamp is taken
before the statement. Measured means measured; projected scales QA's read cost
linearly with findings, and no QA-scale run backs it.

| Scale | Statement | Tick | Peak watermark age | Against the 120 s API window | WAL per unchanged tick, main to this change | Status |
|---|---:|---:|---:|---|---|---|
| 1x (QA) | 39.8 s | 71.5 s | about 110 s | inside, by 10 s; one 100 s stamp gap reached about 140 s | 242 MB to not yet measured on QA | statement, tick, main WAL measured; new WAL NOT_RUN until deploy |
| 5x | about 199 s | about 229 s | about 428 s | always stale | 302 to 363 MB to 245 to 420 bytes (scratch, 149,350 winners) | times projected; WAL measured on scratch |
| 10x | about 398 s | about 428 s | about 826 s | always stale | 983 MB to 0.00-0.15 MB (scratch, 298,700 winners) | times projected; WAL measured on scratch |

The window is crossed routinely near 1.15x, and a statement passes the 60 s lease
TTL near 1.5x. Past that point every overlapping instance skips, which is safe but
adds no capacity: replicas neither help nor hurt a single-flight resweep. Scratch
wall time at 298,700 winners on a shared host was 6.6 to 7.4 s against 12.05 s for
main, which again shows the scratch read does not model QA's. The 5x WAL figure
is a review rerun: 149,350 winners and 448,050 findings, `full_page_writes` on,
per-statement `EXPLAIN (ANALYZE, WAL)`, unchanged ticks. Main wrote 301.5 and
362.6 MB of WAL; this change wrote 245, 288, and 420 bytes. An earlier shim gave
"561 MB to 0.01 MB" and "38 MB and 1,133 MB", and none of those figures has a
retained output, so they are not used here. The 10x row also uses an independent
review run.

### Overlap: why a resweep is single-flight

The lease TTL is 60 s with no renewal during the statement, and the deployed
statement averaged 101.7 s over 793 calls with a 2,180 s maximum. QA runs one
reducer, so it does not overlap today, but with two or more replicas any tick past
60 s does. The change test reads the stored table from the statement's snapshot,
and overlapping resweeps meet only at the singleton watermark row, so the second
resweep (B) compares against a table the first (A) has not committed.

| Interleaving: winner K retired at A's snapshot, its fact restored before B's | K after both | Watermark | Reader told |
|---|---|---|---|
| Statement on main | present, B's blanket upsert restores it | B | the truth |
| Write-on-change without the lock (RED, test below) | absent: B saw K stored and unchanged, A then deleted it | B, newer than K's restore | a missing winner is fresh |
| Shipped | absent until the next tick | A | the truth: A's snapshot predates K's restore |

So the change kept main's row set in no overlap and lost it in this one, and
`ON CONFLICT` could not help: it only covers rows the statement chose to write.
The fix is `tryClaimSupplyChainImpactWinnersResweepSQL`, a
`pg_try_advisory_xact_lock` run as its own statement, then the rebuild, in one
transaction. Under READ COMMITTED the rebuild's snapshot is taken after the lock,
so it always sees the last committed resweep; a lock inside the rebuild statement
would run after that statement's snapshot, which is the same defect. The rule is
that a resweep that cannot see the last commit must not stamp. K then behaves
like any fact change made after a snapshot: the next tick picks it up.

This is the design's existing single-owner rule enforced where the lease cannot
enforce it, not added serialization. Overlapping resweeps already serialize on
the singleton row (`FOR UPDATE NOWAIT` on it fails while a statement waits on a
winner row) after both have paid for the read, and both recompute the same set,
so a second one adds nothing the next tick does not. A try-lock rather than a
wait means N replicas cannot stack N full resweeps. The lock ends with the
transaction, so a dead backend frees it at once, with no TTL. The cost is one
`BEGIN`, one lock call and one `COMMIT`. A row lock on the watermark would not
work: the row does not exist before the first resweep.

Conflict domain: the whole winners table, guarded by one advisory key
(`hashtext('supply_chain_impact_winners')`, `hashtext('resweep')`). Workers and
lease: one maintainer per reducer, lease TTL 60 s, pause 30 s. Transaction scope:
the claim and the rebuild statement. Retry scope: the next tick, with backoff
only on error; a skip is not an error. Lock order: the claim, then the watermark
row, then winner rows, on every path, so a resweep cannot deadlock another.

No-Regression Evidence: `TestSupplyChainImpactWinnersResweepWritesOnlyChangedRowsLive`
and `TestSupplyChainImpactWinnersResweepSeesEveryColumnChangeLive` run on
PostgreSQL 18.6. An unchanged tick leaves every row's `xmin` untouched and still
advances the watermark. A mixed tick (priority change, winner moving to another
fact, retired key, changed JSONB column, NULL expiry becoming a time, new key)
writes exactly those rows, and an expiry time becoming NULL is seen too. Each
stored column, and a key field, changes one at a time and is seen. The result
equals a wipe-and-rebuild. Mutants that insert every winner, use `<>` for
`IS DISTINCT FROM`, drop a column from the comparison, or ignore a time becoming
NULL fail these tests. The parity tests against the read-time dedup are unchanged.

Concurrency proofs, all on PostgreSQL 18.6 in
`supply_chain_impact_winners_concurrency_live_test.go`:
`...OverlapNeverStampsAMissingWinnerLive` pins the interleaving above (RED before
the claim: watermark 12:02:00 over a missing winner; a row lock pins A after its
snapshot). `...KilledHolderFreesTheResweepLive` terminates A's backend: A gets the
error, its delete does not survive, and the next resweep runs at once.
`...ReportsACommitFailureLive` uses a deferred trigger: the error reaches the
caller (RED before: `err=nil` and `Deleted:1` for a rolled-back tick), the
watermark holds, and the next tick runs. `...ConvergesUnderContentionLive` runs 4
looping resweeps against a churn writer: 15 committed, 45 skipped, 0 errors, 0
deadlocks, and the settled table equals a fresh recompute, in 150 of 150 runs
under `-race`. One earlier run of that test failed by one row. The writer then
stopped by cancelling its context, and cancelling an `UPDATE` can still commit it:
358 of 1,800 cancelled statements did, while reporting `context canceled`. Only a
fact change can make a settled table differ from a recompute, so the cause is the
test's writer by elimination; no commit landing after the writer stopped was
observed directly. It now stops between statements.

Built-binary proof: two `go build ./cmd/reducer` processes, a bootstrapped
disposable PostgreSQL 18.6 and Neo4j, 5,000 winners, so the real `InstrumentedDB`
transaction, leases and `/metrics` ran. A row lock pinned A's resweep past the
real 60 s TTL with K retired, then K's fact returned. B logged one skip,
`resweep_skipped_total` read 1, the watermark did not move, and the gauge climbed
on both processes (91 s, then 101 s 10 seconds later). After release K returned on
the next tick: 5,001 of 5,001 winners, 6 commits, 1 skip, 0 failures, 0 deadlocks.
The same run read `n_tup_ins`, `n_tup_upd`, `n_tup_del` of 5001, 1, 0 after a tick
of 1 inserted and 1 updated, so `n_tup_upd` follows `updated` alone. Claim cost,
16 interleaved rounds with an alternating first mover on 5,001 winners and a host
at load 50: tick p50 507.91 ms with the claim, 567.84 ms without. That is noise:
no measurable cost, not a speedup. A built-binary run at QA scale is NOT_RUN and
falls to the post-deploy check below.

Post-deploy check on QA: compare `pg_stat_user_tables` for
`supply_chain_impact_canonical_winners` against `rows_total{action}` over a
window. `n_tup_ins` should follow `inserted`, `n_tup_upd` should follow `updated`,
and `n_tup_del` should follow `deleted`. Together they should be about 1% of rows
per tick, not the winner count, and WAL per tick should fall from 242 MB to a few
MB. `resweep_skipped_total` should stay at zero with one reducer.
`eshu_dp_postgres_query_duration` includes statement execution, and this
statement runs through the transaction path with the `read` operation label. A
dashboard that splits on that label will show the 40 s statement in the read
series.

### What the PR and an operator must know

- Replicas add no capacity to the resweep. It is single-flight, so a replica that
  finds a resweep in flight skips its tick.
- `resweep_skipped_total` above zero is a headroom signal, not an error. It means
  a statement outlasted the 60 s lease TTL. The skip wrote and stamped nothing.
- The read half is untouched. It stays about 39.8 s per statement at 1x on QA,
  2.65 times the 15 s trigger for incremental recompute. This change removes the
  write half's cost only.
- The read half is #9805. That issue measures the read with a QA EXPLAIN
  attribution and a keyed-recompute shim before anything is built.

Observability Evidence: `eshu_dp_supply_chain_impact_winners_resweep_duration_seconds`,
`eshu_dp_supply_chain_impact_winners_rows_total{action}`,
`eshu_dp_supply_chain_impact_winners_resweep_skipped_total`,
`eshu_dp_supply_chain_impact_winners_watermark_age_seconds`, the span
`supply_chain_impact.winners_resweep`, and the info logs `supply chain impact
winners resweep committed` (previously a debug line with no counts) and
`supply chain impact winners resweep skipped: another resweep is in flight`. The
age gauge is observable and computed at scrape time from a watermark read at the
top of every tick, on every instance, so it keeps climbing through a stall and a
scrape never queries Postgres. See
`docs/public/reference/telemetry/supply-chain-impact-winners.md`.
