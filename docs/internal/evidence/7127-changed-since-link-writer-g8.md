# 7127 PR-3a: G7 and G8 detail for the changed-since link writer

Companion to [the evidence note](7127-changed-since-link-writer.md). It holds
the per-round timing tables of G7 and G8, the diagnosis of the G8 stall, and
the fix with its proofs (arbiter ruling arb-7127-g8). Every figure below was
measured on the fixed statement (commit `b4fcc22b75`) unless it says
otherwise; container `postgres:18-alpine` 18.6, 1.0x fixture
(`7127-link-writer-fixture.sql`), driver `7127-link-writer-scale.py`.

## G7 rounds (rerun on the fixed statement, P7)

`--g7-only --valid-rounds 10 --deadline 3h`, fresh container: 10 valid rounds
(load at start 4.95-13.94 on 18 CPUs), bare_b, L1b and root interleaved with
the first mover rotated, all rolled back. Raw: `7127-link-writer-g7-results.json`.

| Round | Load at start | First | bare_b (s) | L1b (s) | root (s) | L1b/bare_b | root/bare_b |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 0 | 5.06 | bare_b | 5.51 | 5.33 | 8.30 | 0.967 | 1.508 |
| 1 | 5.08 | l1b | 6.28 | 5.63 | 9.38 | 0.895 | 1.493 |
| 2 | 4.95 | root | 7.37 | 8.45 | 13.82 | 1.146 | 1.874 |
| 3 | 10.15 | bare_b | 4.89 | 5.26 | 9.98 | 1.077 | 2.042 |
| 4 | 10.00 | l1b | 6.11 | 7.95 | 27.05 | 1.301 | 4.425 (max, above 3x) |
| 5 | 10.47 | root | 4.60 | 4.94 | 8.32 | 1.073 | 1.808 |
| 6 | 8.32 | bare_b | 5.00 | 5.23 | 9.36 | 1.046 | 1.874 |
| 7 | 7.59 | l1b | 5.33 | 6.59 | 10.53 | 1.237 | 1.977 |
| 8 | 7.96 | root | 8.50 | 9.31 | 15.25 | 1.095 | 1.794 |
| 9 | 13.94 | bare_b | 5.04 | 5.75 | 9.10 | 1.141 | 1.806 |

Paired medians: L1b/bare_b **1.086** (range 0.895-1.301; gate 1.3, PASS);
root/bare_b **1.841** (gate 3x, PASS on the median). One round (round 4,
root 27.05 s against 8.3-15.3 s elsewhere) reached 4.425; the root basis is
not stated in ruling 8.6, so both are reported.

## G8 rounds (cap proof on the fixed statement, P8 a)

`--g8-only --valid-rounds 10 --deadline 4h`, fresh container, 4 slots so all
n links run at once, each window waiting for the 1-minute load to fall below
18: 30 valid windows, 10 per n. Raw: `7127-link-writer-g8-results.json`.

| Round | n | Load at start | Link walls (s) | Peak backend RssAnon (kB) | Summed RssAnon (kB) | Probe p95 (ms) | Temp files |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 0 | 1 | 10.0 | 6.0 | 483,960 | 483,960 | 1.7 | 0 |
| 1 | 2 | 8.0 | 7.3, 6.9 | 483,920 | 768,972 | 1.8 | 0 |
| 2 | 4 | 7.4 | 7.4, 6.9, 6.9, 6.7 | 492,096 | 1,383,092 | 1.9 | 0 |
| 3 | 1 | 6.1 | 5.4 | 493,836 | 519,376 | 0.8 | 0 |
| 4 | 2 | 6.7 | 6.6, 6.5 | 492,128 | 868,480 | 1.4 | 0 |
| 5 | 4 | 5.4 | 7.6, 7.3, 7.2, 7.2 | 494,280 | 1,510,124 | 1.3 | 0 |
| 6 | 1 | 7.3 | 15.7 | 490,928 | 490,928 | 29.9 | 0 |
| 7 | 2 | 13.3 | 8.1, 7.0 | 491,192 | 725,144 | 9.6 | 0 |
| 8 | 4 | 15.4 | 12.5, 12.6, 12.4, 12.6 | 350,000 | 1,388,296 | 56.5 | 0 |
| 9 | 1 | 15.3 | 6.6 | 348,416 | 373,748 | 3.0 | 0 |
| 10 | 2 | 13.8 | 5.7, 6.2 | 349,912 | 723,172 | 2.5 | 0 |
| 11 | 4 | 9.6 | 6.7, 6.8, 6.7, 6.6 | 350,064 | 1,397,176 | 7.5 | 0 |
| 12 | 1 | 8.6 | 6.5 | 348,324 | 374,200 | 0.7 | 0 |
| 13 | 2 | 6.4 | 6.3, 6.3 | 350,216 | 724,060 | 3.9 | 0 |
| 14 | 4 | 4.9 | 7.9, 7.0, 7.0, 6.9 | 482,932 | 1,477,332 | 1.5 | 0 |
| 15 | 1 | 5.3 | 7.8 | 484,916 | 511,200 | 1.5 | 0 |
| 16 | 2 | 4.5 | 7.5, 6.6 | 482,892 | 732,440 | 4.2 | 0 |
| 17 | 4 | 4.3 | 7.5, 6.7, 6.6, 6.6 | 482,976 | 1,365,992 | 1.6 | 0 |
| 18 | 1 | 3.9 | 5.9 | 473,908 | 473,908 | 1.4 | 0 |
| 19 | 2 | 4.0 | 5.7, 6.1 | 485,128 | 828,000 | 1.2 | 0 |
| 20 | 4 | 10.2 | 8.1, 8.1, 8.1, 8.0 | 484,948 | 1,805,032 | 1.2 | 0 |
| 21 | 1 | 9.5 | 7.1 | 486,408 | 486,408 | 1.6 | 0 |
| 22 | 2 | 12.0 | 7.0, 7.0 | 484,956 | 851,060 | 4.7 | 0 |
| 23 | 4 | 13.1 | 8.3, 7.5, 8.0, 8.0 | 469,088 | 1,727,712 | 1.6 | 0 |
| 24 | 1 | 11.8 | 7.4 | 467,812 | 467,812 | 1.7 | 0 |
| 25 | 2 | 11.7 | 7.5, 7.0 | 451,708 | 810,480 | 1.4 | 0 |
| 26 | 4 | 10.1 | 8.3, 8.3, 8.3, 8.3 | 350,396 | 1,419,484 | 2.1 | 0 |
| 27 | 1 | 10.9 | 7.7 | 336,032 | 336,032 | 1.2 | 0 |
| 28 | 2 | 12.0 | 8.5, 8.1 | 348,484 | 692,712 | 1.9 | 0 |
| 29 | 4 | 15.2 | 7.9, 6.7, 6.8, 6.9 | 482,868 | 1,381,872 | 3.4 | 0 |

Per n = 1 / 2 / 4 over the valid windows: median slowest wall 6.85 / 7.18 /
7.90 s; median summed RssAnon 468 / 733 / 1375 MiB (max 507 / 848 / 1763
MiB); median read-probe p95 1.5 / 2.2 / 1.7 ms; 0 temp files; no timeout.
The single-backend peak RssAnon never exceeded 494,280 kB (482.7 MiB).

Superseded runs, on the pre-fix statement: the first G7 run (10 valid
rounds, L1b/bare_b median 1.110, root median 1.667, max 3.356 in a round
the reviewer's tests overlapped) and the first load-gated G8 run (9 / 9 / 8
valid windows, stopped by the round-26 stall). Their raw files are in the
history of the two results files (commits `f6028cb21b`, `17e07a0de9`).

## G8 stall diagnosis (round 26)

Cause, observed: a plan that goes quadratic when the state-table
statistics say the scope has almost no state rows. It is CPU-bound, with no
lock wait and no I/O wait. The plans of round 26 and of the reproduction's
round 5 were never captured, so for those rounds the cause is inferred; P8(c)
then observed it directly. The churned n=4 harness, with each link's plan
taken by `EXPLAIN` in the link's own transaction before the statement ran,
stalled in its first window on the pre-fix statement: three of four links
were cancelled at 120 s, and each of those three had a recorded plan with the
state side estimated at 1 row and `del` as a Nested Loop with an inner
`CTE Scan` on `diff`; the fourth link, estimated at 1,235,834 rows, finished
in 6.4 s (`7127-link-writer-proof-p8c-prefix.json`).

Evidence, in order:

1. **Reproduced on the churned database** (same container, `log_lock_waits`,
   `deadlock_timeout=1s`, `log_checkpoints`, `log_autovacuum_min_duration=0`
   on; n=4 windows only; a 1 s sampler over `pg_stat_activity` with
   `pg_blocking_pids`, `pg_stat_progress_vacuum` and `pg_stat_checkpointer`).
   Rounds 0-4 took 6.1-7.5 s. Round 5 (05:47:49-05:49:49 UTC) hit the 120 s
   timeout on one link: pid 301230 was sampled 110 times over 119.6 s; 109
   samples had no wait event (on CPU), one `IO/AioIoCompletion`, and
   `pg_blocking_pids` was empty in every sample. Its three peers finished in
   6.3 s. `log_lock_waits` logged nothing. The server log shows
   `automatic analyze of table ... changed_since_key_state` at 05:47:41,
   8 s before the window, while the harness was deleting and re-rooting the
   scopes.
2. **Plan under stale statistics.** In a rolled-back transaction: delete one
   scope's state rows, `ANALYZE changed_since_key_state`, restore the rows,
   then `EXPLAIN` the shipped `IncrementalLinkSQL` (custom plan). The state
   side is estimated at `rows=1` (actual 771,201). The `del` CTE plans as a
   **Nested Loop** with the scope's state index scan outside and a
   **CTE Scan on diff** inside, rescanned per state row: about 771k x the
   deleted keys (1,120 here) of CPU work.
3. **Timed replay, same transaction shape, scope `r_t4` (state at F0),
   rolled back:** shipped statement with stale statistics: **cancelled at the
   60 s cap**; shipped statement after a fresh `ANALYZE`: 24.3 s (the plan
   drives the delete from `diff` into the primary key, loops=1,120); a
   candidate that deletes by the state rows' `ctid` carried through `diff`:
   7.9 s with stale statistics, 14.9 s with fresh ones (Tid Scan). These
   timings ran while the control's fixture load shared the host, so they
   separate a bounded plan from a runaway one and nothing finer.
4. **Control, fresh container, no re-root churn** (roots once, each window's
   incremental statements rolled back; n=4, 12 windows): no stall, walls
   6.1-11.8 s.

Rejected: (a) a lock conflict between link transactions (no blocker in any
sample, nothing from `log_lock_waits`); (b) temp-table or extension locks
(L1b uses none); (c) checkpoint or WAL stall as the cause (checkpoints ran
through rounds 0-4 without effect, and the stalled backend was on CPU);
(d) autovacuum blocking (no blocker; its effect is indirect, through the
statistics it writes). The slot cap was not involved: this harness sets 4
slots, so its n=4 windows never contend for a slot.

Production relevance (arbiter ruling arb-7127-g8): the inversion needs the
scope's state rows estimated below about two, not merely underestimated. The
inner `CTE Scan on diff` costs about 3,485 per outer row and the good plan
about 6,500 in total, so two estimated outer rows already make the inverted
plan the dearer one. That estimate arises when the scope is absent from
`pg_statistic` while every scope the statistics know fits in the
most-common-values list: a fleet of at most about 100 scopes with a scope
below the autoanalyze threshold, any scope deleted and re-created between
two `ANALYZE` runs, or a `digest_version` change that re-roots every scope.
With 421 scopes the arbiter measured a 319-row estimate for an absent scope
and a linear Hash Join, so a fleet of ops-qa's size (814 scopes) is probably
not exposed in steady state; small fleets are exposed deterministically.

## G8 fix (arbiter ruling arb-7127-g8)

`IncrementalLinkSQL` carries each state row's `ctid` through `diff`, and
`del` deletes by that array with no indexable predicate on the target:
`DELETE FROM changed_since_key_state AS t WHERE t.ctid = ANY (ARRAY(SELECT
d.state_ctid FROM diff AS d WHERE d.current_state IS NULL AND d.state_ctid IS
NOT NULL)) RETURNING t.scope_id`. The planner's only paths are then a Tid
Scan or a sequential scan of the table, and the Tid Scan's cost does not
depend on the statistics. The executor's first proposal kept
`t.scope_id = $1` and was rejected: under stale statistics it planned an
Index Scan of the scope with the `ctid` array as a per-row filter
(`Rows Removed by Filter: 770081`), which grows with state rows times
deleted keys. The statement now also returns the expected deletes and
upserts and the deleted rows of another scope, and `incremental()` fails
the link as a counting `internal` failure unless they match. Under a broken
fence, EvalPlanQual makes the `ctid` form skip a changed row (the arbiter
measured 999 of 1,000 where the key-join form deleted 1,000), so the check is
part of the fix. `changed_since_key_state` must stay an ordinary table
(`relkind = 'r'`, now in G13), because `ctid` is unique per partition only.

| Proof | Result | RED |
| --- | --- | --- |
| P1 plan class, `TestIncrementalLinkPlanClassUnderPlantedStatistics` | PASS: 20 filler scopes of 20,000 rows; scopes of 600 and 45,000 keys; states absent (precondition met: state side estimated at 1 row), fresh, never analyzed (`pg_clear_relation_stats`, `pg_clear_attribute_stats`), over-estimated. Every state: `del` by Tid Scan, no Nested Loop with a rescanning inner child, `diff`'s state side by the primary key (a sequential scan is accepted only in the over-estimated state, as bounded) | The frozen pre-fix statement fails (b) (`Nested Loop` with inner `CTE Scan diff`); form (i) fails (a) (`del` by Index Scan) |
| P2 re-key worst case, `TestRekeyLinkIsBoundedUnderAbsentStatistics` | PASS: 45,000-key scope, every key dropped and as many added, five interleaved rounds: paired median absent/fresh 0.96, rerun 0.88 (gate 2.0) | Pre-fix statement: cancelled at 60 s. Form (i): ratio 2.96, rerun 3.33 |
| P4 invariant, `TestIncrementalLinkRowCountInvariant` (unit) and `TestLinkFailsWhenAFenceBreachSkipsADelete` (live) | PASS: deleted one short, upserted off, or one foreign delete each give an `internal` counting failure and no commit; the live breach (a second session updates a to-be-deleted row until the delete waits on it) fails the link with nothing persisted, the cursor unmoved, one counted attempt, and the rerun equals an undisturbed reference | Before the fix the breach committed; the statement without the check commits a state that differs from the aggregate |
| Stale-statistics regression, `TestIncrementalLinkSurvivesStaleScopeStatistics` | PASS: 194 ms for a 60,000-key link with 2,000 deletions (bound 5 s, a secondary guard; the plan shape is the primary signal) | 12.7 s and the nested loop on the pre-fix statement |
| P5 concurrency (G9, G10) and G1, G15, G16c, G3, G4 | PASS on the new statement (full store and runner live suites, with and without `-race`); G4's state assertion now expects the delete's Tid Scan | as before |
| P3 accuracy at 1.0x, `TestLinkScaleProof` p3 | PASS: the pre-fix and the fixed statement on the 771,201-key scope (fresh statistics, rolled back) give equal SHA-256 digests of state, link deltas, bucket counts and link rows (`7127-link-writer-proof-p3-current.json`), about 4.9 s each | G1's planted writers |
| P8(b) 1.0x absent against fresh | PASS: five interleaved rounds, every absent run planned with the state side at 1 row and the plan class held; paired median absent/fresh **1.00** (ratios 0.94-1.33; gate 2.0), 5.1-6.6 s per link (`7127-link-writer-proof-p8b-current.json`) | the pre-fix statement times out under the same statistics (P2, P8 c) |
| P8(c) churned harness, fixed statement | PASS: 30 windows of n=4 (120 links) with every plan captured before it ran; 0 timeouts; 39 links ran with the state side estimated at 1 row, all within the plan class, median 7.3 s (max 8.3 s) against 7.8 s for the rest (`7127-link-writer-proof-p8c-current.json`) | pre-fix statement: 3 of 4 links cancelled at 120 s in the first window, plans recorded (above) |
| P7 G7 rerun, P8(a) G8 cap proof | PASS, tables above | none |
