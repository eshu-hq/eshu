# #11131 audit sweep pacing ruling (2026-11-21)

Arbiter ruling 11131c (arbiter-eshu, binding), recorded by the #11131 lane from
the coordinator's copy. The arbiter read the branch at `1578c6b43a`; file and
line citations refer to that commit. The evidence note
[11131-audit-range-sweep.md](../evidence/11131-audit-range-sweep.md) carries
the projection, break-even, M9 reading and acceptance additions it requires.

Branch drive/11131-audit-range-sweep, HEAD 1578c6b43a, base a744af3924. Issue #11131 (epic #10860). Earlier rulings: [the joint ruling 11120a](11131-11119-11120-joint-ruling-2026-11-21.md) (decisions 3 and 8), [11117-winners-resweep-ruling-2026-11-21.md](11117-winners-resweep-ruling-2026-11-21.md), [11117-staged-audit-budget-ruling-2026-11-21.md](11117-staged-audit-budget-ruling-2026-11-21.md). Skills loaded: concurrency-deadlock-rigor, performance-rigor.

RULING: Keep the defaults (tick budget 5 min, audit interval 10 min) and the yield design as built; ship the worst case as a stated, operator-tunable limit, not as a met bar; one replica measurement and five wording fixes are owed before the PR.

## Verified by the arbiter (read-only, worktree at 1578c6b43a)
- The loop waits `AuditInterval` before every tick (core/winners_keyed_maintainer.go:310) and checks the tick deadline only after a whole pass (:369-375). The yield wait runs inside the pass (staged/audit/tick.go:151-165, 374-389), so it spends tick budget. Duty at defaults is 5/15.
- Arithmetic of the note (the evidence note's projection table) reproduces: 840 x 14.5 s = 12,180 s, x3 = 10.2 h; 840 x 73 s = 61,320 s, x3 = 51.1 h; at 30 min budget duty 30/40 gives 22.7 h.
- Both knobs are env-only: `..._AUDIT_TICK_BUDGET` (cmd/reducer/winners_wiring.go:120-126) and `..._AUDIT_INTERVAL` (:86-97; any positive duration, 0 turns the audit off). No code change is needed to tune either.
- A range takes the lock only when a candidate survives the pending filter (storage/postgres/supply_chain_impact_winners_audit_sweep.go:349-364). A key queued in the dirty log or staging never locks.
- A try-lock miss returns Skipped and the next pass reads the range again on the standby (tick.go:118-131 runs before every verify; :183-185). The waiting claim is bounded at 20 s (audit_sweep.go:26).

## Corrections to the brief
1. 73 s per range is not the upper bound. Under a busy keyed loop a locked range can cost up to three standby reads, a 20 s lock wait, the verify and the yield: about 13+13+13+20+30+30 = 119 s. On that figure the worst case is about 83 h at defaults and about 37 h at a 30 min budget.
2. A saturated keyed maintainer does not by itself produce the worst case. Its backlog is queued, and queued candidates are dropped without the lock. The worst case needs an unqueued candidate in nearly every range.
3. "Ranges rarely lock" is a theory, not a measurement. One unmeasured source: the range is read at a standby snapshot and filtered on the writer later. A key the keyed tick applied in between is stale in the snapshot and no longer queued, so the range locks and the verify discards it. Rough rate per range: applied identifiers per second x (read seconds + standby lag) x 88 / 74,134. At 5.4/s and 15 s that is about 0.1; at the #11120 target of 51/s it is about 0.9. A faster keyed path makes locked ranges common.
4. The #10643 ruling record has no winners, watermark or dirty-log content (`rg -i 'winner|watermark|maintainer'` returns nothing). The "63k remaining, keyed saturated" claim is not verified here; the joint ruling 11120a read 48,434 dirty rows at 14:41Z.
5. The note says "These figures are warm and local"  directly under a table built from the cold replica cost. One of the two is wrong.

## Decisions
1. Defaults stay: tick budget 5 min, interval 10 min. Raising the default to 30 min would run 14-21 s standby statements at 75 percent duty for good, 2.25 times today's load, to cover a state nobody has observed. The standby recovery-conflict rate at that duty is NOT_CHECKED in the note and unverified in the joint ruling 11120a. That is an unmeasured theory and does not ship as a default.
2. No design change in this PR. Excluding the yield wait from the tick budget does not meet the bar: 7 ranges per tick, cycle 600 + 300 + 210 s, 120 ticks, about 37 h. It also stops the budget from bounding a tick's wall time. Rejected.
3. The yield stays. It is not serialization as a fix (the joint ruling 11120a decision 3: the single-flight lock already exists, no worker count or batch size changes). Removing or shortening it to buy sweep time is rejected: the RED run shows 10 keyed commits for 54 locked verifies. A longer interval or a smaller budget as an answer to keyed contention is rejected too.
4. The 24 h bar stays a QA acceptance reading (the joint ruling 11120a decision 8). The PR does not close #11131; it uses `Refs #11131.` and the issue stays open until the reading lands.
5. Break-even, to be stated in the note: at defaults a sweep fits 24 h while the mean range costs at most 28,800 s / 840 = 34.3 s. With L the cost of a locked range and f the locked fraction: 14.5 + f x (L - 14.5) <= 34.3. That gives f <= 0.34 at L = 73 s and f <= 0.19 at L = 119 s.
6. Tuning order for an operator, env only: raise the tick budget first, then lower the interval (interval 2 min with budget 30 min gives duty 0.94). Each step needs the standby conflict and range-timeout readings before the next.
7. No new issue is filed now. Re-trigger for a pacing redesign (pace ranges to a target sweep period): decision 9 below fails after one tuning step, or the QA locked fraction exceeds 0.19. That question returns to the arbiter with the readings.

## Proof owed before the PR
8. M9 (QA read-only replica, about 5 minutes, one snapshot per range): 20 ranges of 10,000 at uniform cursor positions. For each, count candidates and candidates not present in the dirty log or staging. Report f_hole = share of ranges with at least one unqueued candidate. Rule: f_hole <= 0.15, ship as ruled. f_hole > 0.15, stop and return to the arbiter with the table before opening the PR. Record the standby replay lag at the time of the run.
   M9 does not measure the read-to-filter race of correction 3. State the race and its formula in the note as NOT_CHECKED.
   If the replica is not reachable from the lane, request the output from the coordinator. Do not infer it.
9. QA acceptance after deploy, added to the note's acceptance section: after the first 100 committed ranges report locked_ranges_total / ranges_total, `keyed_yield` and `lock_busy` deferrals, discards against repairs, mean `audit_range_duration_seconds`, and the projected sweep time. Report keyed commits per 10 minutes and p95 watermark lag for one hour before and one hour during the sweep; the sweep must not lower the keyed commit rate by more than 10 percent. If the projection exceeds 20 h, raise the tick budget in the QA overlay (env only) and report it.
10. Note fixes: the 119 s case beside the 73 s case; the break-even of decision 5; correction 5 resolved; the 30 min figure labelled a projection with 5 percent margin; the race under NOT_CHECKED.

## PR body wording
- Do not write "meets the 24 h bar" or "inside the 24 h bar" without the condition. Required form: "Projected about 10 h per QA sweep at the defaults while ranges rarely need the resweep lock. Projection from the replica cold cost; not measured on QA."
- Required sentence: "If most ranges need the lock, a sweep at the defaults is projected at 51 h or more and misses the 24 h bar. `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_TICK_BUDGET` and `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_INTERVAL` tune this without a code change."
- State the break-even locked fraction and the M9 result with its date and standby lag.
- Do not call 73 s a bound. Do not call the 30 min setting a fix or say it meets the bar: "projected about 23 h at 73 s per range, not measured".
- Do not write "typical" as an observed fact. Write "expected while candidates are queued".
- Name the QA acceptance reading as open, and name `audit_sweep_last_completed_age_seconds` and `audit_sweep_locked_ranges_total` as the signals.
- The default tick budget and interval are listed as unchanged.

## Will not do
- Raise the default duty on a projection: unmeasured standby load, against Prove-The-Theory-First.
- Drop or weaken the keyed yield, or widen the lock timeout, to shorten the sweep: it trades keyed freshness, which is accuracy, for audit period.
- Accept the worst case as closed by documentation alone: decision 9 keeps it a measured reading with a fixed rule.
- Add adaptive pacing to this PR: no reading shows it is needed.

## Unverified (arbiter)
The live test results (RED 10 / 54, GREEN 186-188 / 54-55) were read from the note, not re-run. The cold cost of 1.309 ms per fact, the 840 range count on today's corpus, and the 1.4 s unlocked verify are the lane's. The dirty-log depth and keyed duty today. Standby replay lag and recovery conflicts. Whether a repeated range read after a lock miss is warm. The rate of the read-to-filter race. Whether the first QA sweep finds unqueued holes at all.
