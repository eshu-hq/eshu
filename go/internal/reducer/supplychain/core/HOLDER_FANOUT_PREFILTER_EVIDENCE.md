# Queued-holder prefilter: proofs and QA evidence (#10972)

The proofs, QA replica reading, and evidence notes for
[`HOLDER_FANOUT_PREFILTER.md`](HOLDER_FANOUT_PREFILTER.md), split out to keep
that file short.

## Proof

- Core, no database: 300 all-queued holders cost no reopen and no continuation
  entry and one prefilter read; a stored list of queued holders is cleared by
  one read; non-queued states reach the reopen in order; the cap applies after
  the prefilter; a failed read fails the pass before any reopen; the handler
  hands the port the capped `stampOut` it stamps (bounded, empty, and sentinel);
  a kept uncovered holder reaches the reopen and takes a cap slot;
  `stamp_widened` is counted and is not `pending_noop`; a holder pending
  `{A}` that a fan-out for `{B}` widens claims a next intent scoped to `[A, B]`
  (`fanout_prefilter_test.go`, `fanout_prefilter_stamp_test.go`).
- Postgres, live (run by the coordinator on PostgreSQL 18.3):
  - the cover matrix, one row per arm, including a holder with two pending
    rows whose second row's arms would evaluate NULL, with a seeded NULL-unsafe
    form that drops it (`supply_chain_impact_fanout_queued_cover_live_test.go`);
  - Go/SQL lockstep: each shape's real claimed intent goes through
    `ScopedPassCVEIDs`, and every dropped row runs full or scoped on a superset
    of `stampOut` (`supply_chain_impact_fanout_queued_lockstep_live_test.go`);
  - the differential: a holder the prefilter returns gets no row version from
    the shipped reopen for the same origin (`xmin`), replacing the status-only
    differential;
  - the late claim through a real `ReducerQueue.Claim`: a succeeded holder kept
    at the read, reopened by a second origin and claimed, is flagged with the
    merged stamp; a covered holder claimed after the read is dropped and keeps
    its row version; an uncovered one is flagged with `[A, B]`
    (`supply_chain_impact_fanout_queued_late_claim_live_test.go`);
  - the widen: a pending holder's stamp widens without moving `updated_at` or
    `visible_at`; a flagged in-flight holder's stamp widens and its claim-time
    clear fails; a second same-origin reopen writes nothing; the cap goes to the
    sentinel and a set-less change removes `cves`
    (`supply_chain_impact_fanout_stamp_widen_live_test.go`);
  - concurrency: 8 concurrent reopens end at the union; a reopen and a real
    claim in both orders, the reopen blocked on the claim's row lock and
    rechecked under EvalPlanQual; the claim order of a widened row is unchanged
    (`supply_chain_impact_fanout_stamp_widen_race_live_test.go`);
  - the read timeout, and the plan guard: the plan never scans
    `fact_work_items` sequentially and reads it through
    `fact_work_items_scope_generation_idx` or
    `fact_work_items_stage_domain_status_idx`
    (`supply_chain_impact_fanout_queued_*_test.go`). The plan guard has a
    database-free seeded pair (`TestQueuedHolderPlanViolationSeededRedGreen`):
    a plan with a sequential scan of `fact_work_items` fails, and both accepted
    plans pass.

## QA replica reading

Theory shim, QA replica, read only, 2026-11-23 22:51 to 22:52Z, over the three
largest stored `fanout_remaining` lists. The `stampOut` came from each list
owner's own stamp, which was empty, so the first pass reads an unbounded change.
A second pass over the largest list used the most-stamped CVE among queued
items (CVE-2026-27606) as a bounded `stampOut`.

| List | Holders | Status-only drop | Covered drop | Of which full (no stamp or owed) | Via the attempt arm | Covered by the set | Kept, uncovered queued | Kept, other states |
|---|---|---|---|---|---|---|---|---|
| 1, unbounded | 3,732 | 3,720 | 2,092 | 2,081 | 11 | 0 | 1,628 | 12 |
| 2, unbounded | 2,724 | 2,717 | 1,474 | 1,466 | 8 | 0 | 1,243 | 7 |
| 3, unbounded | 2,618 | 2,611 | 1,430 | 1,422 | 8 | 0 | 1,181 | 7 |
| 1, bounded | 3,732 | 3,721 | 2,290 | 2,082 | 11 | 197 | 1,431 | 11 |

So the covered-only drop removes 56% of the largest list (61% with the bounded
change), where the status-only drop removed 99.7%. The 1,628 kept uncovered
holders each get one `stamp_widened` statement on this fan-out; once their
stamp names the change, the next fan-out drops them. Queued manifest-marker
holders among the listed holders: 0, so no marker-specific rule is needed.

The plan of the new statement, largest list: `GroupAggregate` over a `Sort` over
a `Hash Join`. `fact_work_items` is read by an Index Scan on
`fact_work_items_stage_domain_status_idx` (3,736 rows kept, 5,576 removed by the
holder filter, 44,413 buffers, 59.9 ms). It is hash-joined to a Seq Scan of
`ingestion_scopes` (15,408 rows, 13,018 in the hash, 2,427 buffers, 6.4 ms).
There is no Seq Scan of `fact_work_items`. Execution took 1,263.3 ms and
1,258.5 ms on two runs (planning 12.1 ms). Lists 2 and 3 took 912.1 ms and
870.9 ms, and the bounded run took 1,304.9 ms. The aggregate's cover expression
(payload reads) is most of that time: the join finishes at 140 ms. Every run
read only shared-buffer hits (486,886 on the largest list), so a cold read was
not measured; the cold bar of 5 s is NOT_CHECKED on QA. Settings were
`work_mem` 64MB, `effective_cache_size` 18GB, `random_page_cost` 1.1. The warm
bar (under 2 s per statement, the bar in `fanout.go`) holds. The guard accepts
this plan and the per-scope probe, and refuses a Seq Scan of `fact_work_items`.

Superseded, status-only predicate: on 2026-11-23 17:30Z the status-only
prefilter over the then-largest list of 4,013 holders returned 4,011 to drop in
514.9 ms. That predicate dropped uncovered holders and is replaced.

## Evidence notes

Performance Evidence (#10972 PR-2 holder prefilter): baseline is the QA
replica reading at 16:27 to 16:41Z under the 338 backfill. 1,248 open
`supply_chain_impact` items carried `fanout_remaining`, 1,229 of them also
`fanout_owed`. A sample of 200 items listed 4,298 distinct holders and none had
succeeded (3,577 pending, 713 retrying, 4 claimed, 4 dead_letter). Per hour the
fan-out ran 24,202 `pending_noop` and 13,896 `deferred` per-holder statements
(`eshu_dp_supply_chain_impact_fanout_total`). The change replaces the covered
share of those with one set read per claim over the stored list (2,092 of
3,732 on the largest list), and turns the uncovered share into one
`stamp_widened` write each that the next fan-out drops. The read touches no
claim, lease, or queue path; the widen writes one payload key on a row the
reopen already locks and never moves its claim order. The read never scans
`fact_work_items` sequentially, at 1.26 s warm on the largest list. The success
bar is the arbiter's: deferred holder statements per hour down at least 90%
from 13,896 across three consecutive hours, open `supply_chain_impact` work
lower at the end than the start, and `stamp_widened` per hour falling as
stamps converge.

No-Regression Evidence (#10972 PR-2 holder prefilter): a pass with no covered
holders pays one extra read-only statement and drops nothing; every kept holder
runs one reopen statement, in the same order, as before. A queued holder that
was a no-op before is now widened once per new id; that write is bounded by
the stamp cap and stops at the sentinel. The concurrency model is unchanged: no
lock, lease, claim query, worker count, or batch size changes, so nothing is
serialized.

Observability Evidence (#10972 PR-2 holder prefilter): counter outcomes
`prefiltered_pending` and `stamp_widened` and read site `holder_prefilter` on
`eshu_dp_supply_chain_impact_fanout_total`; read kind `holder_prefilter` on
`eshu_dp_supply_chain_impact_fanout_read_duration_seconds`; the
`prefiltered_pending` and `stamp_widened` fields on the fan-out info log and
span; and the `fanout_prefiltered_pending` and `fanout_stamp_widened`
sub-signals. A failing prefilter read counts `read_error` or `read_timeout`
with its site, and the fan-out span records the error.
