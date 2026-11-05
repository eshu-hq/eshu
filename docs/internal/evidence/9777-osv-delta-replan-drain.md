# #9777 Step 2 — OSV Delta Replan Drain: Cost Note and Decisions

Step 2 of the arbiter-ruled #9777 sequence builds the change-driven re-query
drain flag-off: the `owned_target_replan` queue and commit watermark
(migration 278), the changed_name fact reader, the coordinator
intersection/drain behind the per-instance `osv_delta_replan` flag (default
off), and the five step-6 signals (registered; nothing emits at runtime
until enablement).

## Measured cost (built code, disposable PG 18.6)

Synthetic 7,352-id burst (`TestOSVDeltaReplanBurstDrainsWithoutLossLive`,
seeded fixture inventory, one resolve call per pass):

- intersect 7,352 tuples: 31 ms (pure, no store).
- admit 7,352 tuples, one statement: 72 ms.
- drain to quiescence: 8 passes, 290 ms total resolve time across all passes
  (each pass resolves the whole pending hit list once; pending shrinks as
  trivial rows and the chosen slot commit).
- Quiescence: zero pending rows, watermark on the newest drained feed time.

The lead's theory proof measured 112 ms for one 7,352-lookup burst resolve
on scratch PG; the built code resolves the shrinking pending set per pass,
so per-pass resolve stays at or under that bound. The folding differential
(`TestOSVDeltaReplanFoldingDifferentialLive`) drains 8 advisories across the
hit ecosystems in a handful of passes with the same per-pass shape.

## Watermark semantics (decision, recorded for the enablement review)

"Watermark never passes an unqueued record" is enforced operationally, not
as a numeric feed-timestamp prefix:

- The watermark advances only inside the drain-commit transaction, in the
  same statement batch that marks the covered rows enqueued
  (`CommitReplanDrain` on the caller's executor). It can never advance
  without the marks, and the compare-and-set keeps it monotonic under rival
  coordinators (overlapping-commit test pins the serialization; the loser
  marks zero and holds the watermark).
- Consumption is table-driven: pending rows re-drive every pass until
  marked, and the watermark never filters what the drain reads. A numeric
  prefix would contradict the one-slot bound under interleaved feed
  timestamps (a slot-bounded pass covering a prefix would drain ~1 row and
  take thousands of passes for a day's burst), so the proof asserts
  per-pass marked == covered, monotonicity across passes, exactly-once under
  concurrency, and quiescence (zero pending, watermark on the newest drained
  timestamp, rewind all-duplicates).
- Re-reading the watermark for the gauges happens after the commit, so the
  ages report stored truth rather than the pass's candidate.

## Commit scope restriction (decision)

The commit covers only drained rows with a scope that survived tenant-grant
authorization. Duplicate-dropped scopes (already open under the same
deterministic id) still count as queued. Denied scopes leave their rows
pending for the next pass. Rows riding a scope the plan already held, and
trivial rows with no owned targets, commit unconditionally.

## Provisional bounds (re-set from measured hit rate before enablement)

- One slot per pass (`OSVDeltaReplanSlotsPerPass`), most-queries slot wins,
  ties break to the lowest slot. Overflow waits in the queue.
- Stale alarm on last-successful-poll age, 2 h (`OSVDeltaReplanStaleAfter`)
  on the assumed 15-minute poll (`OSVDeltaReplanPollInterval`). Feed gaps of
  hours to weeks are normal per ecosystem, so feed age never alarms. A
  never-polled instance is not stale.
- Replan scope ids live under `_replan`, never `_slot`, so a replan subset
  cannot share an id with a rotation scope carrying a different query set.

## What emits now vs at enablement

Now (flag-off, proven by tests): all store statements, the intersection,
the drain plan, the full service pass against fakes, the five registered
signals (counter + four gauges) via the record method, the two contract
spans, and the stale WARN path. Nothing emits at runtime with the flag off:
preparation returns before any store touch.

At enablement: flip `osv_delta_replan` per instance; wire the three
`cmd/workflow-coordinator` adapters (changed-name reader, stored-name
reader, owned-name resolver) and the replan store; the satellite poller owns
fetch bytes (in-tree records nothing for it). Open follow-ups: fold the
mark + watermark advance into the `WorkflowControlStore` admission
transaction (today a separate drain-commit transaction after admission);
bound or page the changed-name read as feed history grows; revisit the
most-queries slot choice against the measured hit rate.

No-Regression Evidence: the drain is flag-gated — with `osv_delta_replan`
off (the default) the tick returns before any replan reader or store touch
(proven by `TestServiceOSVDeltaReplanFlagOffTouchesNoReplanStore` with
call-failing fakes), so the rotation, NVD, refresh, and admission paths
keep their current behavior byte-identical; the rotation slot-scope minter
change is a pure namespace parameter (same `_slot` ids, proven by the
unchanged `TestDeriveVulnerabilityTargets*` suite). Baseline: the pre-change
`go test ./internal/coordinator/... ./internal/workflow/...` green on the
same scratch PG 18.6 backend; after: same suites green plus the new
flag-on/flag-off tests. The new-path cost is measured, not inferred:
7,352-tuple burst intersects in 31 ms and admits in 72 ms in one statement;
per-pass resolve stays at or under the 112 ms theory bound (290 ms total
over 8 passes); terminal state zero pending with the watermark on the
newest drained feed time. The commit is a single UPDATE plus one CAS
upsert on the open transaction — no second transaction, no worker or
batch reduction.

Observability Evidence: the pass records the five step-6 signals
(`eshu_dp_osv_delta_records_total` with the closed outcome set,
`eshu_dp_osv_delta_watermark_age_seconds`,
`eshu_dp_osv_delta_poll_age_seconds`,
`eshu_dp_owned_target_replan_depth`,
`eshu_dp_owned_target_replan_oldest_age_seconds`, all per
`collector_instance_id`) proven by
`TestNewMetricsPublishesOSVDeltaReplanSignals` against a real meter, plus
the INFO commit line and the WARN `osv_delta_watermark_stale` line. Quiet
passes stay silent in the logs; the gauges carry them.
