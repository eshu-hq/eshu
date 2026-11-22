# #11131 The staged winners audit sweeps by identifier range

Refs #11131. Binding rulings:
[11131-audit-sweep-pacing-ruling-2026-11-21.md](../arbiter/11131-audit-sweep-pacing-ruling-2026-11-21.md)
(pacing defaults, M9, the projection wording),
[11131-11119-11120-joint-ruling-2026-11-21.md](../arbiter/11131-11119-11120-joint-ruling-2026-11-21.md)
(decisions 1, 3 and 6-9),
[11117-staged-audit-budget-ruling-2026-11-21.md](../arbiter/11117-staged-audit-budget-ruling-2026-11-21.md)
(the budget deferral and the sweep design) and
[11117-winners-resweep-ruling-2026-11-21.md](../arbiter/11117-winners-resweep-ruling-2026-11-21.md).
The contract gate it keeps is in
[11117-winners-contract.md](11117-winners-contract.md).

## The defect

With `ESHU_POSTGRES_READ_ROUTING=staged-audits` the staged audit read every
active scope generation's impact facts in one tick and deferred on `budget`
past 1,000,000 staged facts. QA holds about 8.37M eligible facts (read-only
replica, 2026-11-21), so every tick deferred and the missed-mark backstop never
ran. A deferring tick still read about 1M facts on the standby before it gave
up, every 10 minutes.

## The sweep

The audit now walks the keyed winners index (migration 291) in its key order
(package_id, repository_id, subject_digest, c0, advisory_id), one range per
`AuditWinners` call:

- **Range read (standby, one statement, one snapshot).** The next B index rows
  after the cursor (B = `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_RANGE_BUDGET`,
  default 10,000), trimmed to whole identifiers. An identifier with at least B
  facts runs alone and whole. The first range has no lower bound; the last
  (fewer than B rows) has no upper bound on the fact side or the stored side.
  Operator-suppression facts load whole on every range. The shipped winner body
  (`supplyChainImpactWinnerBodySQL`, unchanged) recomputes the kept facts, and
  the shipped diff (`winnersAuditDiffSelect`) compares them with the stored
  winners in the same key interval.
- **Guards, fail closed.** The range statement reports whether a range fact has
  `|` in an identifier field (`pipe`: the sweep stops at that range). A NULL
  probe on package_id, repository_id and subject_digest of source facts runs at
  sweep start (`null_key`: nothing runs) and on the last range (`null_key`: the
  range verifies, the sweep restarts and does not count as complete). Operator
  facts are exempt from the probe: they may lack those fields by design.
- **Timeout.** The 25 s statement guard stays. A timed-out range retries at
  half the budget down to one row, which is one whole identifier; a one-row
  timeout defers as `timeout`.
- **Writer verify (one transaction per range).** Seed the candidates, drop
  those already queued in the dirty log or staging without the lock (a keyed
  tick recomputes them), and take the resweep lock only if one remains:
  try-lock, then after two misses on the same range the waiting claim under
  `SET LOCAL lock_timeout = 20s` (`WinnersAuditRangeLockTimeout`, provisional
  until measurement M8; expiry is `lock_busy`). Under the lock the keyed
  statements verify the rest, the stale set is rechecked against the queues,
  repairs are staged, and the cursor row (migration 335) is advanced by
  compare-and-set on `sweep_id` and the five cursor columns. Zero rows rolls
  everything back (`cursor_race`).
- **Keyed yield (coordinator's binding detail of decision 3).** After a
  verify that took the resweep lock, the next range verifies with `NoLock`: a
  range whose candidates are all queued advances without the lock, and one
  that still needs it rolls back (`keyed_yield`) and waits until the winners
  watermark moves (a keyed commit was observed) or one keyed interval (the
  maintainer interval, 30 s by default) has passed, then verifies normally in
  the same pass. lane-11119's M5 run showed why: back-to-back locked verifies
  with no lock-free work between them starved the keyed loop, 1 keyed commit
  to 1,582 skips.
- **Collation.** Range bounds and the cursor are compared only in SQL, by ROW
  comparison over the keyed-index expressions, so the database collation
  decides the order on both sides. Go never orders identifiers. Under the
  `en_US.utf8` collation `@` is ignored at the first level, so
  `@tootallnate/once` sorts among the t's, not first.
- **Pacing.** The maintainer runs ranges back to back for up to
  `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_TICK_BUDGET` (default 5 min) per audit
  tick, renewing the 60 s audit lease (same-owner re-claim) before each further
  range; a lost lease ends the tick. Only the range that completes the sweep
  records the audit commit, so `audit_last_success_age_seconds` keeps meaning a
  completed pass (the A semantics).
- **Unchanged.** The audit gate on the persisted contract (A), the split scan,
  the writer-backed audit, the keyed tick, and the over-cap fallback.

The range statement and the NULL probe read only; they are not in the
winners contract hash set and a change to them needs no version bump. They
only detect: every repair is queued for, and applied by, the pinned keyed
statements. The range statement embeds the pinned winner body byte for byte,
so a body change already bumps the contract, and an audit-only SQL change must
not force a full.

## Proofs (live Postgres 18.6, disposable, one test at a time)

The RED commit ("Pin the staged winners audit range sweep before building
it (RED)") ran every test below against compiling stubs; each failed on its
assertion. GREEN is the commit "Sweep the staged winners audit by identifier
range with a persisted cursor".

| Proof | Test | Result |
| --- | --- | --- |
| (b) T3 differential, union of ranges equals the shipped single statement | `...AuditRangeUnionEqualsSingleStatementLive` | 30 candidates at budgets 1, 7, 50, 200, 1000 and 100,000 (82, 66, 17, 4, 1 and 1 ranges); 0 missing, 0 extra, 0 in two ranges; each of 760 range facts recomputed once; 81 and 61 oversize ranges at budgets 1 and 7 |
| (b) seeded RED: last range closed on the stored side | `...AuditRangeOpenEndedLastRangeLive` | the mutated pair (one textual change derived from the shipped pair) misses the orphan stored after every fact at budgets 50 and 1000; the shipped pair misses nothing |
| (a) a corpus past the old caps completes | `TestStagedAuditSweepCompletesPastTheCorpusCaps` | 1,200,000 facts (old cap 1,000,000): 120 ranges, one completed sweep, 0 budget deferrals |
| (g) seeded stale, missing and orphan winners repaired within one sweep | `...AuditSweepRepairsWithinOneSweepLive` | repairs equal the single-statement candidates; after the keyed ticks the table equals a fresh recompute and the single statement finds 0 |
| (e) roll mid-sweep resumes | `...AuditSweepResumesAfterRollLive` | 3 ranges, then a fresh auditor resumes from the stored cursor; the completed sweep counts every live fact once |
| (f) NULL and `\|` guards | `...AuditSweepGuardsLive`, `TestStagedAuditGuardsFailClosed` | a NULL repository_id defers `null_key` at sweep start; a `\|` package defers `pipe` with no writer contact; a NULL at sweep end verifies but does not complete; the clean fixture (with a no-package operator fact) reads clean |
| (c) two instances | `...AuditSweepCursorRaceLive` | the second verify of a range returns `cursor_race` and its repairs roll back; of two concurrent verifies exactly one advances; two instances sweeping together queue each stale identifier once |
| (d) keyed tick during a verify | `...AuditSweepKeyedTickSkippedDuringVerifyLive` | Skipped while the range holds the lock; the next keyed tick commits the queued mark and the repairs |
| (h) M5, progress at full keyed duty | `...AuditSweepAdvancesUnderFullKeyedDutyLive` | a try-lock loop holding the resweep lock back to back (88.7-95.6 percent duty): 17 of 17 ranges contended, every one advanced within 3 attempts, 0 `lock_busy` (88.1 percent after the yield landed) |
| Keyed yield (live) | `...AuditSweepYieldsToKeyedTicksLive` | a keyed stand-in (150 ms hold, a watermark stamp, a 200 ms retry after a miss) against 54 ranges that each need the lock for 150 ms with no lock-free read between them: RED (the commit "Pin the keyed yield after a locked range verify (RED)") 10 keyed commits for 54 locked verifies; GREEN 186-188 keyed commits for 54-55 locked verifies, 53-54 `keyed_yield`, every range within 3 attempts |
| Keyed yield (unit) | `TestStagedAuditKeyedYieldAfterLockedVerify` | a range after a locked verify asks for `NoLock`; a range that needs the lock yields until the interval passes or the watermark moves; an unlocked verify arms no yield |
| Collation | the differential fixture | packages `@tootallnate/once`, `@scope/pkg`, `pkg:npm/@types/node`, `.dotted`, `-dashed`, `_under`, `Pkg:npm/upper` and an `@` orphan; the union still equals the single statement under `en_US.utf8`, where `@tootallnate/once` sorts after `pkg:npm/p99` |
| Lock policy, timeout halving | `TestStagedAuditLockPolicy`, `TestStagedAuditRangeTimeoutHalvesTheBudget` | the third verify after two misses waits; budgets 10,000, 5,000, 2,500, 1,250, 625 |
| Tick loop | `TestWinnersAuditTickRunsSweepRangesBackToBack`, `...StopsAtItsBudget` | ranges run back to back with a lease renewal each; a lost renewal ends the tick; only the completing range records success |

Two GREEN runs on the shared, loaded host (load1 28-40) failed after their
assertions passed, on the disposable template-clone drop timeout; they are
environmental, and the same tests passed in a quieter run.

### Re-run after the #10880 winner payload landed

The rows above were measured before #10880 slice 1 added `fact.source_confidence`
to the shared winner body. The range CTEs project explicit columns, so on the
rebased branch every range failed `column fact.source_confidence does not
exist (SQLSTATE 42703)` and the sweep would have deferred `batch_abort` on
every tick (found in independent re-review, not by the earlier live run).

- RED: `...AuditRangeUnionEqualsSingleStatementLive` and
  `...AuditRangeOpenEndedLastRangeLive` on the rebased head, PostgreSQL 18.6,
  `-p 1 -parallel 1`: both fail with the 42703 error, exit 1. The hermetic
  guard `TestWinnersAuditRangeProjectsEveryColumnTheWinnerBodyReads` reads the
  `fact.<column>` set from the shipped body constant and fails the same way,
  naming all five arms (`range_raw`, both `range_facts` arms, both
  `active_facts` arms); `TestWinnersAuditRangeColumnGuardSeesAMissingColumn`
  drops the column from one arm at a time and from all of them.
- GREEN after carrying `source_confidence` through those five arms:
  `TestSupplyChainImpactWinnersResweepAudit(Range|Sweep|Repairs)...Live`, one
  test at a time, exit 0: 10 passed, 1 skipped (the opt-in scale measurement).
  The differential reads 30 candidates against the single statement at every
  budget (82, 66, 17, 4, 1 and 1 ranges, 760 facts), M5 reads 17 of 17 ranges
  contended, worst 3 attempts, `lock_busy` 0, and the keyed-yield proof reads
  55 ranges, 55 locked verifies, 219 keyed stand-in commits, worst 3 attempts,
  `keyed_yield` 54. The host was loaded (load1 about 40), so the figures are
  for correctness, not a timing baseline.

### Collation of the CI database

The reducer contention gate runs a Postgres image whose database collation is
C (`database collation sorts '@tootallnate/once' after 'pkg:npm/p99': false`),
while the earlier runs above used en_US.utf8 (`true`). Two proofs assumed the
orphan `a-orphan-first` opens the sweep, which holds only where punctuation is
ignored at the first level; under C the identifiers `-dashed`, `.dotted`,
`@scope/pkg` and the like sort before it, so the first range carried no
candidate and `...AuditSweepKeyedTickSkippedDuringVerifyLive` and
`...AuditSweepCursorRaceLive` failed on the pull request.

- RED: a `postgres:18.6` container initialised with `--locale=C`, both tests,
  `-p 1 -parallel 1`: both fail with the CI messages (`first verify = {Advanced:true
  ... Locked:false Candidates:0 ... Repairs:0}`), exit 1.
- Fix: the two proofs verify the candidate-free ranges in key order (they take
  no lock) until the first range with a candidate, instead of assuming which
  identifier opens the sweep.
- GREEN under C: both tests three times, then the whole `...ResweepAudit*Live`
  family (15 tests, one skipped: the opt-in scale measurement), exit 0; under
  en_US.utf8 the same two tests plus the differential, repairs, resume and
  guard proofs pass, exit 0.

The same gate run also failed on `skipped enrolled test or subtest:
TestSupplyChainImpactWinnersResweepAuditRangeCostAtScaleLive` (the gate rejects
an enrolled test that skips, and this one skipped without its scale knob). The
test now runs a smoke fixture of 400 identifiers when the knob is unset or
`smoke`, with the plan assertions only for an explicit scale of 20,000
identifiers or more; the measured scale readings below are unchanged. Under a
`--locale=C` `postgres:18.6` container the unset run passes (5.6 s), exit 0.

## Performance Evidence

Performance Evidence: theory measured before any code on the QA read-only
replica (lane-11119), then the shipped statement measured locally on a
330,070-fact corpus against the shipped single-statement audit on the same
host and cache state; figures and the 24 h projection below.

Theory (before code, lane-11119 on the QA read-only replica and a disposable
clone, recorded in the joint ruling): the self-sizing range statement reads
the keyed index under a ROW index condition with a Limit and no Sort; a
15,000-row range read cold in 20.4-21.3 s, so the default budget is 10,000
(about 14 s cold); the T3 differential on the clone found 945 candidates with
0 missing, extra or duplicated at budgets 7, 50, 1000 and 100,000; 8.37M
eligible facts make about 840 ranges per sweep, about 2.8-3.3 h of standby
statement time, projected then at about 7 h per sweep (the corrected
projection is below). A one-range-per-tick sweep would take 140 h, which is
why ranges run back to back.

Shipped statement, local measurement
(`TestSupplyChainImpactWinnersResweepAuditRangeCostAtScaleLive`,
`ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_RANGE_SCALE=30000`, disposable
PostgreSQL 18.6, shared host at load1 29-42, warm cache, 330,070 live finding
facts with about 2 KB of payload padding each, 11,568 seeded candidates):

| Reading | Value |
| --- | --- |
| First range plan | `Limit` over `Index Scan using fact_records_supply_chain_impact_winners_keyed_idx` (1 index search, 10,000 rows); no Seq Scan on `fact_records`; the oversize branch's index scan `never executed` (One-Time Filter on an InitPlan); the stored side is a Seq Scan of the winners table |
| First range, EXPLAIN ANALYZE | 2,871.5 ms execution, 461.2 ms planning; buffers 1,057,376 hit, 6,727 read |
| Sweep at budget 10,000 | 34 ranges, 330,070 facts, sum 110.863 s (1 min 51 s), worst range 9.877 s, 0.336 ms per fact |
| Shipped single-statement audit, same data | 115.661 s (1 min 56 s); ranged sum over single 0.96 |
| NULL probe | 1.402 ms execution (skip scan: 277 index searches on the keyed index for subject_digest, 1 each for package and repository); one timed call 4.023 s on the loaded host |
| Built path (read, verify with the lock, repairs, compare-and-set) | 34 ranges in 252.169 s (4 min 12 s), 7.417 s per range with about 340 candidates each |

Cold, on the QA read-only replica (lane-11119, theory measurements M3 and
M7 of the joint ruling): 20 ranges of 10,000 at fact-mass quantiles, shipped
body, mean 1.309 ms per fact cold (0.939-1.747), worst range 17.14 s, 9,750 to
9,999 rows kept per range. The NULL probe, all three arms 0 rows:
subject_digest 4.97 s cold and 0.17 s warm through the PG18 skip scan on the
keyed index (12,673 index searches), package_id 0.025 ms, repository_id
2.9 ms cold. The shipped probe runs the three EXISTS in one statement, so its
cold cost is about 5 s under the 25 s guard, twice per sweep. The local plan
above has the same shape (a skip scan on the keyed index for subject_digest,
one search each for package and repository).

The ranged sweep costs no more than the single statement it replaces on the
same host and cache state (0.96x), and no range approached the 25 s guard.

### Sweep period against the 24 h bar (arbiter ruling 11131c)

The audit loop waits `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_INTERVAL` (10
min) before every tick and then runs ranges for up to the tick budget (5 min),
so ranges run 5 of every 15 minutes (duty 1/3). The yield wait runs inside a
pass and spends tick budget. QA holds 8.37M eligible facts, about 840 ranges
of 10,000. Every figure below is a projection from the lane's cold replica
cost (1.309 ms per fact), not a QA measurement.

| Case | Per range | Tick time per sweep | Wall time per sweep at the defaults |
| --- | --- | --- | --- |
| Expected while candidates are queued: no range needs the lock | 13.1 s cold read + about 1.4 s verify = 14.5 s | about 12,200 s (3.4 h) | about 10.2 h |
| Every range needs the lock, keyed loop idle: read + verify up to 30 s + one keyed interval of yield (30 s) | about 73 s | about 61,300 s (17 h) | about 51 h |
| Every range needs the lock, keyed loop busy: up to three standby reads (two try-lock misses re-read the range), the 20 s waiting claim, the 30 s verify and the 30 s yield | about 119 s | about 100,000 s (28 h) | about 83 h |

Projection, not measured: with a 30 min tick budget (duty 3/4) the 73 s case
is about 23 h, a 5 percent margin under 24 h; the 119 s case is about 37 h.

Break-even at the defaults: a sweep fits 24 h while the mean range costs at
most 28,800 s / 840 = 34.3 s. With L the cost of a locked range and f the
share of ranges that lock, 14.5 + f x (L - 14.5) <= 34.3, so f <= 0.34 at
L = 73 s and f <= 0.19 at L = 119 s. A range locks only when a candidate
survives the pending filter (an identifier nothing queued in the dirty log or
staging), so a saturated keyed tick alone does not produce the worst case.

The defaults stay (ruling 11131c decision 1): raising the default duty would
run 14-21 s standby statements at 75 percent duty on an unmeasured standby
conflict rate. An operator tunes with env only, the tick budget first, then
the interval (interval 2 min with budget 30 min gives duty 0.94), reading the
standby conflict and range timeout signals before each next step.

M9 (QA read-only replica, before the PR): 2026-11-21 20:49-20:55Z, standby replay lag 0.0-0.6 s.
20 ranges of 10,000 rows, cursors at every twentieth stored-winner identifier
in SQL order, each one read-only statement (shipped next-range statement
joined to the shipped restricted pending select over its candidates, 30 s
timeout). 19 of 20 ranges carried candidates (1,543 in all, 5 to 351 per
range); 0 candidates were absent from the dirty log and staging, so f_hole =
0/20 = 0.00, under the 0.15 rule: ship as ruled. The combined statement took
12.7-26.0 s (mean 18.6 s); it includes the pending check's staging scan, so it
is not the range read alone. The rows are in the lane's scratch output, not
committed.

The local 330,070-fact figures above are warm and local; this projection uses
the lane's cold replica cost. The QA sweep duration is an acceptance reading
after deploy. The whole-corpus tick this replaces read about 1M facts and
deferred every 10 minutes on QA without a result.

## Observability Evidence

Observability Evidence: new deferral reasons, sweep counters and gauges, a
range duration histogram, span attributes and log lines, each asserted by a
recorder or registration test and listed in the telemetry coverage row.

The sweep adds, registered over `StagedAuditTelemetry` and the persisted cursor
row (documented in
[Supply-Chain Impact Winners Resweep](../../public/reference/telemetry/supply-chain-impact-winners.md)):

- `staged_audit_deferrals_total{reason}` gains `timeout`, `null_key`, `pipe`,
  `lock_busy`, `cursor_race` and `keyed_yield`; `budget` no longer occurs.
- `audit_sweep_locked_ranges_total`: committed ranges whose verify took the
  lock (each arms the keyed yield).
- `audit_range_duration_seconds{outcome}` (histogram: advanced, completed,
  skipped, lock_busy, cursor_race, deferred), the range cost.
- `audit_sweep_ranges_total`, `..._range_retries_total`, `..._facts_total`,
  `..._candidates_total`, `..._pending_total`, `..._repairs_total`,
  `..._discards_total`.
- `audit_sweep_ranges_done` (the sweep position, from the persisted row, so it
  survives a roll), `audit_sweep_last_completed_age_seconds` (the 24 h bar),
  and the last sweep's seconds, ranges, candidates, repairs and discards.
- Per range: the `outcome` and `partial` span attributes and one
  `staged winners audit range` log line with the cursor; one
  `staged winners audit sweep completed` line per sweep.

At 3 AM: a flat `audit_sweep_ranges_total` while the audit is allowed means the
sweep is stuck; the deferral reason says why. A rising
`audit_sweep_last_completed_age_seconds` past 24 h with ranges advancing means
the sweep is too slow: raise the tick budget before the range budget.

## QA acceptance (decision 8, after deploy)

One completed sweep: `audit_sweep_last_seconds` at most 24 h, with its ranges,
candidates, repairs and discards; zero `budget` deferrals;
`audit_last_success_age_seconds` reset; each standby statement under 25 s
(`audit_range_duration_seconds` and the range log's `read_seconds`) and each
verify under 30 s.

After the first 100 committed ranges (ruling 11131c decision 9): report
`audit_sweep_locked_ranges_total` / `audit_sweep_ranges_total`, the
`keyed_yield` and `lock_busy` deferrals, discards against repairs, the mean
`audit_range_duration_seconds`, and the projected sweep time. Report keyed
commits per 10 minutes and the p95 watermark lag for one hour before and one
hour during the sweep: the sweep must not lower the keyed commit rate by more
than 10 percent. If the projected sweep exceeds 20 h, raise
`ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_TICK_BUDGET` in the QA overlay (env
only) and report it. A locked fraction above 0.19, or a tuning step that still
misses, returns pacing to the arbiter.

## NOT_CHECKED

- QA readings (the acceptance above) and the cold range cost on QA; the scale
  figures above are a local, warm-cache synthetic corpus.
- The pending filter against #11129's bounded staging collapse: a capped
  collapse leaves the remainder in staging, and the filter reads staging fact
  and scope rows directly, so a remainder still counts as queued. The filter
  joins staging on the five identifier columns; migration 330's captured_at
  index does not serve that join and was not meant to.
- `WinnersAuditRangeLockTimeout` against the measured keyed tick maximum (M8).
- The standby recovery-conflict rate for 14-21 s range statements.
- The read-to-filter race (ruling 11131c correction 3): a range is read at a
  standby snapshot and filtered on the writer later, so a key the keyed tick
  applied in between is stale in the snapshot and no longer queued; the range
  locks and the verify discards it. Rough rate per range: applied identifiers
  per second x (read seconds + standby lag) x 88 / 74,134; about 0.1 at
  today's 5.4 identifiers/s and 15 s, about 0.9 at the #11120 target of 51/s.
  A faster keyed path makes locked ranges common. M9 does not measure it.
- The pending filter's staging join is a hash join over the whole staging
  queue (no identifier index on staging; decision 9 forbids a second index);
  its cost on QA's 2.3M staged rows is about 1.35 s per range with candidates,
  from the lane's whole-pending measurement, not re-measured here.
- The retired whole-corpus staged tick (`RunTick`, the compactor, the census
  batch) is no longer wired but is not deleted in this change.
