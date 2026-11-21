# #11145 Scope-remainder conversion holds the watermark at its scope mark

Refs #11145 (epic #10860; related #11120, #10972, #11117). Rulings relied on:
[11145-scope-remainder-horizon-ruling-2026-11-21.md](../arbiter/11145-scope-remainder-horizon-ruling-2026-11-21.md).
This change stacks on the #11117 winners contract (A), whose note is
[11117-winners-contract.md](11117-winners-contract.md).

## The defect

A keyed tick admits the oldest dirty rows and probes at most 200 identifiers.
When the chunk applies only part of a scope row (an activation), the settle
turns the unapplied remainder into fact rows and deletes the scope row in the
same transaction (`convertWinnersKeyedScopeRemainderSQL`). The converted rows
took the column default `clock_timestamp()`, a fresh conversion-time capture.
The horizon is the residual set's earliest `captured_at` minus 1 us. With the
scope row gone and every remainder row captured at conversion time, the horizon
moved past the original scope mark while the remainder's winners were still
stale. The tick's in-transaction `winners_watermark` wake then reopened every
waiter with `enqueued_at <= watermark`, including a waiter enqueued after the
activation it depends on.

Reproduction (the lane-11119 M5b probe, the shape the committed RED test keeps):
8 winners re-ranked by one activation, chunk of 3. Scope mark
`15:00:27.766159Z`, waiter enqueued `.787464Z`, watermark after the first
bounded tick `.823476Z` (the converted rows' capture minus 1 us). The tick was
bounded, stale winners went from 8 to 5, 5 remainder fact rows were queued, and
no scope row was left.

## The fix

- The convert's SELECT arm carries `MIN(c.captured_at)` of the admitted scope
  rows that name the key, and the INSERT names `captured_at`.
- The conflict arm keeps the earlier capture:
  `captured_at = LEAST(queued.captured_at, EXCLUDED.captured_at)`. The arbiter
  of that conflict is the fact-key unique index from migration 305,
  `(package_id, repository_id, subject_digest, c0, advisory_id) WHERE kind = 'fact'`.
  The convert already targets it, so LEAST applies to the queued fact row of
  the same key. It is the only fact-row uniqueness on the log.
- Nothing else changes. Conversion and the scope-row delete stay in one
  transaction. Statement order (convert, delete, residual min, stamp, wake) is
  the same, and so are the lock order and the stamp formula.

## Proofs

RED at `dd3164acf4` (the A code), GREEN after the fix, both on disposable
PostgreSQL 18:
`supply_chain_impact_winners_keyed_scope_remainder_horizon_live_test.go`.

| Case | RED | GREEN |
| --- | --- | --- |
| M5b shape, 8 winners, chunk 3 | converted rows carry a fresh capture; watermark past the scope mark at ticks 1 and 2; waiter woke with 5, then 2, stale winners | watermark under the mark until all 8 apply; waiter parked, then woken once drained; parity with a full recompute |
| two scope rows | watermark past both marks; the waiter keyed between the marks woke early | each mark holds until its own scope's winners apply |
| remainder over more than one tick (9 winners, chunk 2) | watermark past the mark on every tick 1-4 | held on every tick |
| queued fact row for a remainder key captured after the mark | merged row kept its own later capture | merged row carries the scope mark, `dirty_version` 2 |
| staged mark for a remainder key commits after the conversion | watermark past the mark | held; the late mark folds in and drains; staging empty |

The hermetic pin `TestWinnersConvertRemainderKeepsTheScopeCapture` checks the
shipped statement. Its seeded-violation half plants four regressions (fresh
capture, conversion-time arm, later-wins conflict, overwrite), and each one
fails the check.

Edge cases:

- Duplicate marks for one key from a fact row and a scope row: the earlier
  capture wins (live case 4). Two scope rows naming one key: MIN over them.
- Retry and idempotency: the convert runs inside the tick transaction. A
  rolled-back tick leaves the log as it was, and the retry recomputes the same
  rows and captures from the same scope rows.
- Crash between conversion and apply: the conversion and the scope-row delete
  are one transaction. A crash rolls back both, and the scope row re-expands on
  the next tick.
- Concurrency: the convert writes only the dirty log, under the resweep
  advisory lock. The audit verify and a full take the same lock and are Skipped
  or wait as before. Writers only stage. LEAST adds no lock, the key-ordered
  upsert keeps the R2 lock order, and the full winners live suite (143 tests,
  including the deadlock, contention, collapse and audit proofs) passed on the
  fix.
- Rollback to the A binary: the binary writes fresh captures again. No schema
  or data change ships, so the rollback needs no runbook step.
- A late mark that executed before a queued row's capture but committed after
  it keeps the queued row's capture in the staging collapse (keep-existing, not
  LEAST). That is the collapse's documented late-commit tolerance and predates
  this change. It is reported to the coordinator, not changed here.

## Contract decision: re-pin, no version bump

The convert was not in A's hash set. Per arbiter-11120a decision 9 it joins the
set, and version 1 is re-pinned to
`1b5f967bf7ca338917b3137f98b7c4423cec49318440e583b216d629654cc201` (was
`c5731efd...`). This is a text change, not a semantic change to stored winners:
which keys get recomputed is the same, and only the capture time of a queued
row moves. What does change is when `winners_watermark` waiters wake: no
earlier than their activation's winners. A version bump would force one
single-statement full on every instance after the roll (about 6,400 s on QA)
without changing that wake behavior, so the bump rule (a winner output change
for the same facts) does not apply.

What the re-pin does on the roll: nothing different. The maintainer decides
on versions only (`decideWinnersContract` in `winners_keyed_maintainer.go`).
`StoredHash` and `BinaryHash` are read into the contract but nothing branches
on them. Both full paths stamp the hash; no tick compares it. A binary carrying
the new hash, rolled over a table whose last full recorded `c5731efd...` at
version 1, runs keyed ticks and the audit on its first tick: no full, no
stand-down, no new reason on `full_resweeps_total`. The stored hash stays
`c5731efd...` until a later full (a version bump or the force-full runbook
step) records the new one. `TestWinnersMaintainerRePinnedHashAtEqualVersionRunsKeyed`
pins this: equal versions with different hashes give a keyed tick and zero
full calls. A hash-aware decision turns it red. The re-pin rule itself is A's
pin pair: `TestSupplyChainImpactWinnersContractHashPinnedToVersion` failed on
the new statement set until the pin moved, and
`TestSupplyChainImpactWinnersContractPinRejectsAChangedStatement` changes each
covered statement in turn, including the convert, and requires the pin to fail.

Differential (bounded replay, three scopes of 7, 5 and 9 re-ranked winners plus
14 fact marks, drained at chunk 2, 3 and 50): the md5 of every stored winner's
data columns is `a4ef02840ec21e79fa51fcf33ab9c0da` (28 winners) on the shipped
code and on the fix, at all three chunk sizes, in repeated runs. Tick count and
total inserted and updated rows are identical (chunk 2: 14 ticks, 7 inserted,
21 updated). The per-tick order differs: converted rows now keep their age, so
oldest-first admission takes the remainder before newer fact marks.

## Performance

No-Regression Evidence: one `EXPLAIN (ANALYZE, BUFFERS)` pair per round, shipped
convert against the pre-fix text (derived from the shipped constant), alternated
in one rolled-back tick transaction under savepoints. Data: one activation of
41,400 findings, chunk 200, so the convert writes 41,200 rows (the worst case for
the write: QA scopes reach about 10,700 facts). The plan shape is identical. The
GroupAggregate emits 41,200 rows in 116-117 ms on the fix and 120-141 ms
shipped (one shipped outlier at 256 ms). Warm
shared hits are about 425,000 on both arms. The sort row grows from 116 to 124
bytes, and its temp blocks from 379 to 445. Warm paired rounds 3-5 ran
1,844 / 1,589 / 1,566 ms on the fix against 1,860 / 1,536 / 1,655 ms shipped:
within noise. The host was loaded (load1 31-54 on 16 CPUs), so the absolute
times are noisy and the plan and buffer equality is the decision-bearing part.
The existing scale harness (fact churn, 5 bounded ticks; it does not convert)
ran 407 / 651 / 627 ms shipped against 440 / 782 / 502 ms on the fix,
alternated: no measurable difference. The existing slow-window convert plan
proof (`TestSupplyChainImpactWinnersResweepKeyedConvertSlowWindowPlanLive`)
passes on the fix.

## Observability

Observability Evidence: `eshu_dp_supply_chain_impact_winners_scope_remainder_converted_rows_total`
counts the rows a keyed tick's convert wrote (inserted or merged). The keyed
tick result carries `ScopeRemainderConverted`, and the
`supply chain impact winners resweep committed` log line carries
`remainder_rows_converted`. With the fix,
`dirty_log_oldest_age_seconds` keeps reporting the activation's age while its
remainder drains. Before the fix, every conversion reset it to the conversion
time. At 3 AM, a flat watermark with a growing oldest age, a falling
`dirty_log_depth` and a rising converted counter is a large activation
draining over bounded ticks, with its waiters held on purpose. The telemetry
reference documents the counter and the reading:
[supply-chain-impact-winners.md](../../public/reference/telemetry/supply-chain-impact-winners.md).
The NVD trip tally buckets a converted row by its capture minute. That is now
the scope mark's minute, not the conversion minute, so a conversion no longer
shows up as fresh fact inflow. The six trip-sample live proofs pass; one hit a
disposable-database drop timeout after its assertions passed.
