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
and of visible parked rows (the `waitingCensusQuery` classes), the consult p50
and p95 with parked rows present at TTL 0 and at the candidate TTL, and the
wait-table row count during the run. If the peak count of visible retrying rows
on any source exceeds 1,000, or the consult p50 exceeds 100 ms, the thresholds
stay unset and the question returns to the arbiter. No new gauge is added:
`eshu_dp_reducer_waiting_items` already reports the wait-table size by failure
class (context, not a cost alarm), and no consult-duration instrument exists, so
add one before enablement if the replay cannot price the consult otherwise.

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
retrying rows and the cost does not depend on the wait table's size. The test is
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

G1 meets the ruled bars in every cell: K=0 within 10% and under 100 ms with the
wait-table scan never executed, K=1,000 within 10%, K=all within 50% with one
probe per scanned row. G2 (a CASE-guarded anti-join in the four FILTERs) fails
K=1,000 at 50,000 and 200,000 wait rows and K=all at every size, so it is the
unused fallback. With stale statistics (the wait table analyzed empty, then
filled, never re-analyzed) G1 kept the same bounds: K=0 +1.9% to +2.2%, K=1,000
+3.8% to +5.5%, K=all +32% to +42%. Plan shape: G1 matches the baseline's own
data-driven variation in every cell except one, the empty wait table (no rows,
analyzed empty), where the planner chose a sequential scan of the empty table
for the probe; the bounded probe never executes it (K=0), and the grouped probe
runs it against an empty relation. The grouped query keeps its baseline shape
(a sort aggregate that spills in the baseline too) with the probe added.

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
- The probe's plan on an empty wait table is a sequential scan; if a cached
  generic plan from that moment survived the table's growth, the probe would scan
  the table once per retrying row until the table's statistics change forces a
  re-plan (autoanalyze triggers that after a small number of inserts). Not
  measured.

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
same 100 ms; the worst cell logs its wall time and asserts its plan). Seeded
mutations: dropping `parked_wait.parked IS NULL` fails the live and shape tests,
and dropping the `work.status = 'retrying'` guard fails the shape pin and the
guard plan test.

**Status of #11187 after this decision.** Met for the claim-gate class and the
parked-wait class; the timer-path class and the built-binary replay arm are
owed.
