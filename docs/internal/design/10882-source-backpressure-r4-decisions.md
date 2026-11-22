# #10882 round 4 decisions and rollout notes

Status: Decided on arbiter ruling `arbiter-10882-r4` (2026-11-22). Companion to
[10882-source-backpressure.md](10882-source-backpressure.md) and
[10882-source-backpressure-fork-rulings.md](10882-source-backpressure-fork-rulings.md).
Those records hold the trigger, the actuators, the fork rulings, and the
earlier review decisions. This one holds the round 4 decisions that would push
them past the 500-line cap, and the rollout notes an operator needs before
enabling the leg.

## Enablement gate (binding)

The per-source leg ships disabled and stays disabled until measured. No
deployment sets the `ESHU_SOURCE_BACKPRESSURE_*` thresholds until the
built-binary two-source replay reports the per-claim consult cost at
`ESHU_SOURCE_BACKPRESSURE_SIGNAL_CACHE_TTL=0` and at a candidate non-zero TTL,
and this record names a recommended non-zero TTL with its staleness bound.

Why there is no TTL default in the code: a default would be an unmeasured
theory, and the proofs rely on a TTL of 0 so every consult reads the signal.
The measured cost that motivates the gate: with the TTL at 0 and the leg on,
every projector claim runs one bounded per-source probe (same-session p50 about
92 ms over about 13k claimable rows of one source at 10x replay scale, against
a harness budget of 100 ms) plus a latch read. A TTL trades that cost for
signal staleness, and the replay is what prices both.

The replay also reports, per source, the peak count of visible retrying rows
and of visible parked rows (the `waitingCensusQuery` classes), the count of
visible woken rows that still hold a wait row, the consult p50 and p95 with
parked rows present at TTL 0 and at the candidate TTL, and the wait-table row
count during the run, each separately.

Ceiling. For each source, the peak count of visible retrying rows (K, parked or
not) times 5 microseconds (the R4-3 per-probe bound) must not exceed 10% of that
source's measured consult p50 at TTL 0, that is K <= 20 x the consult p50 in ms
(about 1,840 at 92 ms). If any source exceeds it, or the consult p50 exceeds 100
ms, the thresholds stay unset and the question returns to the arbiter. K here is
the operational count: every visible retrying row the consult scans pays one
wait-table lookup whether or not it is parked, so the parked count alone would
hide the cost driver. This is not the "K = all" of the R4-3 cost proof below,
which means every visible row of the probed source retrying and holding a wait
row (the worst case). The replay source (1,238 rows, about 6.2 ms, about 6.7%
of 92 ms) is inside the ceiling; the 10x seed (1,857 rows, about 9.3 ms, about
10.1%) sits at the line. This is a replay-time check, not a fixture check.

Because K can grow after a one-time replay, a consult-duration instrument is a
precondition of enablement (none exists today). The enablement record must name
a consult p95 alert at 100 ms. A post-enablement p95 above 100 ms, or a replay K
above 1,000, returns the question to the arbiter; the candidate redesign is a
writer-side parked marker on the queue row (a #10876 change). No new gauge is
added in this PR: `eshu_dp_reducer_waiting_items` already reports the wait-table
size by failure class (context, not a cost alarm), and the leg is off by default.

## Rollout and rollback

Inert while the leg is off. With the thresholds unset no consult runs and no
paced release is issued, so none of the following can occur.

- **Roll every projector first.** Set the thresholds only after every projector
  and ingester is on the version that carries migration 333 and the source-paced
  retry kind. An older binary computes an item's projection claims as
  `attempt_count - admission_wait_attempts` and ignores `source_paced_waits`.
  An item paced N times and then handled by an older binary (a rollback, or a
  mixed roll with the leg on) sees N spent projection claims and dead-letters on
  its next genuine retryable failure.
- **Unset before rollback.** To roll back, unset the thresholds first and let
  the paced items drain (a released latch re-admits them within
  `max(GrantDelay, ReleaseAge)` plus jitter, see the Accounting paragraph in the
  fork rulings), then roll the binary. Rolling back with items still carrying a
  large `source_paced_waits` is the hazard above.
- **The hazard arrives sooner than it first looked.** The resume bound
  (`ReleaseAge`, Proposed 15 minutes) means a paced item is re-claimed about 4
  times an hour while its source stays slowed, and each re-claim raises
  `attempt_count` and `source_paced_waits` together. A long engagement reaches a
  large counter in a day, not a week.
- **Migration 333 takes a short lock.** It is `ALTER TABLE fact_work_items ADD
  COLUMN ... NOT NULL DEFAULT 0`: metadata-only on PostgreSQL 11 and later, but
  it takes `ACCESS EXCLUSIVE` and queues behind long `fact_work_items`
  transactions. It carries no `lock_timeout`, like the 325 precedent. Apply it
  in a quiet window.

## R4-3 decision: parked-wait rows are not takeable (#11187)

Status: Decided on the arbiter rulings `arbiter-10882-r4` and
`arbiter-10882-r43`. All timings are same-session medians on a private
`postgres:18` at 10x replay scale (87,130 claimable reducer rows, the bounded
probe scans 13,069 rows of one source), `work_mem` 4 MB, host load 20 to 46, so
read the ratios against the in-run no-predicate baseline (VB), not the
milliseconds.

**Finding.** The takeable-signal test excluded rows the claim's readiness gate
refuses. A second class waits and was still counted: an item parked in
`reducer_waiting_items`. A parked item stays `retrying` and turns visible when
its 10 minute backstop expires; if the wake has not run, it is claimable by
status and old, so a source whose aged rows are all parked would engage the
latch with nothing the paced intake does able to clear it.

**Decision.** The signal skips a row that has a wait row, through one
status-guarded lateral probe in the shared FROM of the grouped query (the
bounded probe derives from the same text) and one `parked_wait.parked IS NULL`
term in each of the four aggregate FILTERs, nothing in the WHERE:

    LEFT JOIN LATERAL (SELECT TRUE AS parked FROM reducer_waiting_items AS wait
                       WHERE work.status = 'retrying' AND wait.work_item_id = work.work_item_id
                       LIMIT 1) AS parked_wait ON TRUE

A parked row is always `retrying` (the retry statement sets it, every wake
reopens only `retrying` rows, and the audit treats a wait row whose queue row is
not `retrying` as dangling), so the wait table is read only for visible
retrying rows: with none, the consult never touches it, and with K of them it
adds one primary-key lookup each, at a cost that grows with the index depth. The test is
the wait row itself: not a `failure_class` equality and not a `next_attempt_at`
or `visible_at - updated_at` shortcut, each of which can count a parked row, the
direction that holds a latch.

**Census (theory first).** The replay fixture and the 10x seed hold no parked,
woken, or dangling rows (0 for every class on every source), so they say
nothing about real wait-table size. The probed source has 1,857 natural
`retrying` rows among its 13,069. From code, the wait table is bounded only by
deferred items times keys: one row per parked item for single-key classes, one
per producer domain for the cross-scope producer class; rows go away on a wake
for that key, the audit (500 oldest rows per 5 minutes, which can reconcile
zero while live waits stay at the head), or the retired-scope purge; the 10
minute backstop does not delete them, and a row whose item was claimed and
succeeded stays until the audit reconciles it.

**Rejected forms, measured.** Bounded p50 in ms, VB about 92 in every cell
(waits rows on other sources, no probed row parked):

| form | 0 | 1,000 | 50,000 | 200,000 |
|---|---|---|---|---|
| V0 correlated NOT EXISTS in each of the four FILTERs | 109 | 124 | 208 | 238 |
| V1 hashed NOT IN | 93 | 96 | 128 | 238 |
| V2a unguarded lateral, one probe per row | 99 | 102 | 123 | 132 |
| V2b LEFT JOIN of a DISTINCT set | 93 | 758 | 111 | 144 |

V0 fails the bar at every size (four index probes per scanned row, 120,872 to
225,380 buffers on the empty table). V1 builds the hashed set once per FILTER
copy, four full reads of the wait table per consult (`Seq Scan on
reducer_waiting_items`, `loops=1`, once per copy), so its cost follows the whole
table. V2a probes every scanned row, including pending rows that cannot be
parked. V2b flipped to a nested loop with a join filter at 1,000 rows.

**Shipped form measured (G1) and its fallback (G2).** Bounded p50 in ms against
VB in the same cell; K is the number of visible retrying rows of the probed
source that each hold a wait row; the first column is wait rows on other
sources. Wait-table scan loops for G1 were 0 at K=0, 1,000 at K=1,000, and
13,069 at K=all.

| wait rows | K | VB | G1 | G2 |
|---|---|---|---|---|
| 0 | 0 | 91.8 | 93.7 (+2.0%) | 93.0 (+1.3%) |
| 0 | 1,000 | 93.9 | 96.8 (+3.1%) | 101.2 (+7.8%) |
| 0 | all | 91.4 | 117.6 (+28.8%) | 191.3 (+109%) |
| 1,000 | 0 | 93.1 | 94.9 (+1.9%) | 93.8 (+0.8%) |
| 1,000 | 1,000 | 92.4 | 95.9 (+3.8%) | 100.4 (+8.7%) |
| 1,000 | all | 92.4 | 122.1 (+32.2%) | 202.2 (+119%) |
| 50,000 | 0 | 92.3 | 94.6 (+2.5%) | 93.2 (+1.0%) |
| 50,000 | 1,000 | 90.7 | 95.3 (+5.1%) | 101.3 (+11.7%) |
| 50,000 | all | 94.5 | 131.1 (+38.8%) | 226.5 (+140%) |
| 200,000 | 0 | 93.0 | 95.5 (+2.7%) | 94.4 (+1.5%) |
| 200,000 | 1,000 | 91.8 | 97.1 (+5.8%) | 104.0 (+13.3%) |
| 200,000 | all | 99.7 | 141.0 (+41.4%) | 250.4 (+151%) |

G1 meets the ruled claim-path bars in every cell but one. K=0: +1.9% to +2.7%
and under 100 ms, with the wait-table node never executed. K=1,000: +3.1% to
+5.8%. K=all: +28.8% to +38.8% at 0, 1,000 and 50,000 wait rows. K=all at
200,000 wait rows is the exception: +41.4% in the matrix above, but +48.8% to
+55.6% in ten later same-session repeats (median +52.6%) against the +50% bar,
in both the unprepared and the default prepared mode with identical plans and
buffers. The repeats are the better evidence: the matrix cell's baseline ran
99.7 ms against about 93 to 96 ms in the repeats. The cost is the probe's own
worst case, 13,069 index lookups into a 200,000-row index at about 3.8
microseconds each, and only when every visible row of the probed source is
retrying and the wait table holds 200,000 rows. The arbiter did not move the ratio line to the measured number. The K=all bar is
restated as a plan pin plus a cost per probe: exactly one wait-table node, an
Index Only Scan on the primary key under Limit and Result with a One-Time Filter
on `work.status = 'retrying'`, run once per visible retrying row; and an added
cost per probe, (probe p50 minus baseline p50) divided by K, of at most 5.0
microseconds at up to 200,000 other wait rows, as the median of seven
interleaved runs. The ratio is reported, not barred. The reason: the probe adds
at most one primary-key descent per scanned row to the three the baseline
already does, so the ratio has a structural cap, and the per-probe cost is what a
later change can regress. The cost is not flat in the wait table's size (B-tree
depth and cache growth, about log N), and the record does not claim it is. G2 (a CASE-guarded
anti-join in the four FILTERs) fails K=1,000 at 50,000 and 200,000 wait rows
and K=all at every size, so it is the unused fallback. With stale statistics
(the wait table analyzed empty, then filled, never re-analyzed) G1 kept the
same bounds: K=0 +1.9% to +2.2%, K=1,000 +3.8% to +5.5%, K=all +32% to +42%.
Plan shape: G1 matches the baseline's own data-driven variation in every cell
except one, the empty wait table (analyzed empty), where the planner chose a
sequential scan of the empty table for the probe. The bounded probe never
executes it at K=0; the residual that a cached plan could keep that scan after
the table fills is measured under "Cached plans" below.

Per-probe cost at K=all (13,069 visible retrying rows of the probed source, each
holding a wait row; unprepared mode, 7 interleaved runs of 15 samples, medians;
plans saved, all with the one-node shape above):

| other wait rows | index rows | B-tree level | index size | baseline / probe (ms) | per probe |
|---|---|---|---|---|---|
| 0 | 13,069 | 1 | 1 MB | 94.5 / 127.8 (+35.3%) | 2.53 us (2.47 to 2.62) |
| 1,000 | 14,069 | 1 | 1 MB | 93.7 / 123.6 (+31.9%) | 2.33 us (2.28 to 2.58) |
| 50,000 | 63,069 | 2 | 8 MB | 100.7 / 143.5 (+42.5%) | 3.30 us (3.12 to 3.37) |
| 200,000 | 213,069 | 2 | 22 MB | 96.2 / 145.9 (+51.7%) | 3.89 us (3.60 to 4.17) |
| 1,000,000 | 1,013,069 | 2 | 110 MB | 96.7 / 153.6 (+58.8%) | 4.36 us (4.26 to 4.52) |

The level is the `bt_metap` root level. Earlier repeats at 200,000 wait rows
measured 3.70 to 4.07 microseconds over 11 runs; K=all at 0 wait rows measured
+35.2%, +31.0% and +33.7% in the first unprepared runs. Two definitions of K must
not be confused: "K = all" here is the worst case, every visible row of the
probed source retrying and holding a wait row; the enablement gate's K is the
operational count of visible retrying rows, parked or not.

Further cells, unprepared probe against the same-cell baseline unless stated:
K=1,000 at 200,000 other wait rows, 94.4 to 99.9 ms (+5.8%, bar 10%); K=0 at
200,000 against the prepared baseline (main's mode, so the planning cost shows),
92.6 to 96.0 ms (+3.7%; +2.2% against the unprepared baseline); the natural mix
(1,857 visible retrying rows of the probed source, none parked) at 200,000 other
wait rows, 94.2 to 102.0 ms (+8.3%, bar 10%) at 4.14 microseconds per probe
(3.63 to 4.46, bar 5.0), and at 0 other wait rows 93.8 to 96.6 ms (+3.1%). At
that last operating point the consult p50 is 102.0 ms, above the gate's 100 ms
line, so the enablement gate would refuse it on the p50 line.

**Grouped query (status publication).** The grouped query misses the ruled
grouped bar: interleaved 7 by 9 medians against the same-cell baseline at 10x,
true K=0 (no retrying row anywhere) 262.0 ms against 295.3 ms (+12.7%), natural
mix (10,523 retrying rows across sources, each probed) 265.6 ms against 324.1
ms (+22.0%). The cause is per-scanned-row executor work, not wait-table size:
the lateral adds a nested loop over all 87,130 scanned rows and its Limit and
Result nodes run once per row even when the status guard returns nothing. In
the parallel plan the probe also sits above the gather (GroupAggregate, Nested
Loop Left Join, Gather Merge), so it runs serially in the leader. The bar is
restated for this query only, because it is not on the claim path: its only
production caller is `readSourcePacingStatus` inside the coordinator snapshot,
reached through the status-summary writer companion (10 s default, 5 s floor,
pass budget two intervals) and the live status fallback, and only when the leg
is configured or a latch is engaged; `SourcePacingStates` has no production
caller. The extra cost is +33 to +59 ms per pass, 0.33% to 0.59% of one backend
at 10 s. Restated bars (interleaved 7 by 9 medians against the same-cell
baseline, unprepared mode, wait rows on other sources 0 and 200,000):

Baseline / probe medians in ms, with the difference in parentheses:

| state | bar | 0 wait rows | 200,000 wait rows |
|---|---|---|---|
| true K=0 | +20% | 634.6 / 649.7 (+2.4%) | 644.9 / 659.5 (+2.3%) |
| natural mix | +30% | 639.2 / 664.0 (+3.9%) | 739.8 / 725.9 (-1.9%) |
| natural mix, repeat 1 | +30% | 637.8 / 659.1 (+3.3%) | 639.3 / 687.7 (+7.6%) |
| natural mix, repeat 2 | +30% | 659.1 / 695.1 (+5.5%) | 635.6 / 685.2 (+7.8%) |
| natural mix, every probed-source row parked | +50% | 635.0 / 704.9 (+11.0%) | 637.0 / 729.2 (+14.5%) |

Flatness (the natural-mix ratio at 200,000 wait rows minus the ratio at 0, bar 5
points): the first run measured -5.8 points, the favorable direction: its
200,000-row baseline had a 739.8 ms outlier (about 100 ms above every other
baseline), which made the ratio look better, not worse. The two repeats measured
+4.2 and +2.3 points, inside the bar.

**Intended delta from the rejected form.** A pending row that still has a
dangling wait row is now counted (the unguarded anti-join hid real takeable work
until the audit ran). The live test proves it as a delta: the census reads the
30 pending rows as parked, the signal counts 30 and the source engages.

**Residuals.**
- A missed wake under-counts a ready row for at most the 10 minute backstop
  (unchanged).
- Timer-path classes that never park stay counted (unchanged), including a
  parking class whose key could not be built: the queue takes the 10 minute delay
  before the wait row is written and gives up on the key.
- New: a row woken on one key that still holds wait rows for other keys is not
  counted until it is re-claimed (the wake deletes only the woken key's rows, and
  a producer-domain wait parks one row per domain). The error direction is
  under-count, so it cannot hold a latch. The replay census must count visible
  rows with `reopened_at` set and a wait row (`woken_with_wait`); a material count
  returns to the arbiter.
- If the replay shows visible retrying rows above 1,000 on a paced source, the
  candidate redesign is a writer-side parked marker on the queue row (a #10876
  change), not a larger budget.

**Cached plans (measured).** pgx's default exec mode caches prepared statements,
and under `plan_cache_mode=auto` a generic plan made while the wait table is
analyzed empty keeps its sequential scan after the table fills, until a
statistics update re-plans it. On one dedicated connection at 10x (1,857 natural
retrying rows on the probed source; `autovacuum_analyze_threshold` 50,
scale factor 0.1, `autovacuum_naptime` 1 min): after eight executes the cached
plan was generic with `Seq Scan on reducer_waiting_items` (bounded 95 ms,
grouped 306 ms). With 1,000 wait rows and no ANALYZE the bounded probe took
160 ms (+68%) and the grouped query 748 ms; with 50,000 rows 3.87 s and 18.7 s;
`force_generic_plan` matched. `ANALYZE` healed it (bounded 98.8 ms, index form).
Autoanalyze fires after the wait set drains as well as on first deploy, so the
empty state recurs. All three signal reads therefore pass `pgx.QueryExecModeExec`
(`unpreparedSignalArgs`), as the entity search page does for the same reason:
each run plans against the current table, at 1.1 to 1.3 ms of planning time. The
unprepared mode adds no execution cost: at K=all and 200,000 wait rows the plan
and buffers were identical in both modes (+51.9% and +52.9% unprepared, +53.1%
and +52.4% prepared).

**Proof.** `TestSourceBackpressureTakeableSignal` (live; parked rows with expired
backstops never engage, an engaged latch on them releases on the next consult, a
woken row counts and ages from its wake, a pending row with a dangling wait row
counts, the bounded probe equals the grouped query for every shape),
`TestSourceBackpressureParkedProbeIsGuardedByRetrying` (live plan: a pending-only
source never executes the wait-table node, a retrying one runs it once per row as
an index lookup), `TestSignalReadinessGateSitsInTheAggregateFilters` (the probe
text and its FILTER term in exactly the four aggregates of both queries, the
wait table named once between FROM and WHERE, the retrying guard present), and
the S0 populated cells in `TestSourceBackpressureS0SignalCost` (asserted at the
same 100 ms; the worst cell logs its wall time and asserts its plan),
`TestSourceSignalReadsRunUnprepared` (every signal read passes the exec mode),
and `TestSourceBackpressureSignalSurvivesAnEmptyThenFilledWaitTable` (analyze
empty, eight consults, fill 50,000 wait rows with no ANALYZE: the consult stays
under 2 s and the unprepared plan is the index form). Seeded
mutations: dropping `parked_wait.parked IS NULL` fails the live and shape tests,
dropping the `work.status = 'retrying'` guard fails the shape pin, the bounded
and grouped guard plan assertions and the dangling-row test, and dropping the
exec mode fails the live empty-then-filled test (the consult took 3.7 s) and
the hermetic pin.

**Status of #11187 after this decision.** Met for the claim-gate class and the
parked-wait class; the timer-path class and the built-binary replay arm are
owed.
