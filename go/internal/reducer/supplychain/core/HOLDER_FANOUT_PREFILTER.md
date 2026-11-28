# Queued-holder prefilter and stamp widening (#10972)

How the supply-chain impact holder fan-out drops holders whose queued pass
already covers a change before it slices the list, and how the reopen widens
the stamp of a queued holder that is not covered. This is a section of
[`HOLDER_FANOUT.md`](HOLDER_FANOUT.md), split out to keep that file under the
500-line Markdown cap.

## The cost it removes

A fan-out claim collects every holder of the changed CVEs, takes the first 256,
and runs one locking `UPDATE` per holder (`ReopenHolderDetailed`). On QA a
holder is almost always already `pending` or `retrying`, so before #10972 the
statement classified it `pending_noop` and wrote nothing, yet it still cost a
statement and a share of the 256 cap. The overflow is stored as
`fanout_remaining`, and each drain claim re-runs the whole findings pass only to
issue more of the same no-op statements.

## The cover rule

A queued row's next pass covers a change when it runs full, or when it runs
scoped on a set that contains every id of the change. The rule is the Go pass
rule (`fanOutOneHop` and `scopedPassCVEIDs`, exported for the storage proofs as
`ScopedPassCVEIDs`) written in SQL once, in
`supply_chain_impact_fanout_stamp_cover.go`. A pending or retrying row is
covered when any of these holds:

- it carries `fanout_owed: true`, so its next pass is genuine;
- its next claim attempt is above 1, by the claim's own arithmetic: a
  `retrying` row in a non-counting failure class keeps its count
  (`reducerNonCountingFailureClassPredicateSQL`, the predicate the claim uses),
  every other claim adds 1;
- its stamp names no bounded set: no `fanout_origin` object (missing, JSON
  null, or another JSON type), no `cves` array, an empty array, or the sentinel;
- the change is bounded and the stored `cves` array contains every id of it.

The change is the fan-out's `stampOut`, the capped value it stamps on reopened
holders. An empty or sentinel `stampOut` is unbounded, so only a full next pass
covers it. A disagreement between the SQL and Go rules always falls on the side
of keeping a holder: a `cves` array with no usable string runs full in Go, but
the SQL keeps it and widens it.

Every row condition is written so it is never NULL. `bool_and` ignores NULL
rows, so a NULL arm would drop a holder whose other row is not covered. The
owed arm is wrapped in `IS TRUE`, `jsonb_array_length` runs only inside a CASE
branch that has proved an array, and the whole row condition sits in
`COALESCE(..., FALSE)`.

## The prefilter

After the holder list is sorted and before the cap slice, one set read
(`FindQueuedHolderScopes(ctx, holders, stampOut)`,
`findQueuedSupplyChainImpactHoldersQuery`) returns the holders whose every item
at the scope's active generation, in a state the reopen reads, is `pending` or
`retrying` and covered. Those holders are dropped. Everything else stays and
goes through `ReopenHolderDetailed`, in sorted order, and takes a cap slot:

- `pending` or `retrying` but not covered: the reopen widens its stamp.
- `claimed` or `running`: the reopen sets the replay bit, or widens the stamp
  of a row whose bit is already on.
- `succeeded`: the reopen returns it to `pending`.
- `dead_letter`: reopened or skipped by failure class (#11147).
- no item at the active generation: enqueued, or reported as having no active
  generation.

The cap and the continuation list apply after the prefilter, so they count only
holders that still need a statement. When every holder is dropped, `rest` is
empty, no continuation entry is written, and the owed marker clears.

The reopen and the prefilter read one status set
(`reopenSupplyChainImpactMatchedStatuses`), one active-generation join, and one
cover rule, so they cannot disagree on which rows exist or which are covered.
With more than one item at the active generation, `bool_and` returns the holder
only when every matched row is covered.

## Stamp widening

A stamp is never narrowed while a pass that would read it has not run. The
reopen statement (`reopenSupplyChainImpactHolderQuery`) writes, per state:

- `pending` or `retrying`, not covered: `payload.fanout_origin` only, merged.
  It never writes `updated_at`, `visible_at`, status, attempts, lease, or
  failure fields. The claim orders by `updated_at`, so a bump would push a busy
  holder to the back on every fan-out.
- `claimed` or `running` with the replay bit already on: the stamp is merged
  when its set does not name the change, and left untouched when it does. A
  row with no stamp object (missing, JSON null, or not an object) is never
  written: its flag was set by something other than a fan-out, its rerun is a
  genuine full pass, and a stamp would narrow it to a scoped one hop.
- `claimed` or `running` with the bit off: the bit is set as before, and the
  stamp is merged instead of overwritten. A row with no stamp object has
  nothing to merge and takes the incoming stamp, as before, so a genuine pass
  in flight still reruns scoped on the change. An absent stamp is not an owed
  set (arbiter Amendment 1); the rule lives in one SQL fragment,
  `fanOutStampAbsentSQL`.
- `succeeded` and dead-letter rows: unchanged.

The set-less reopen of the commit-skew fence
(`ReopenSupplyChainImpactHolderInTransaction`) follows the same rule, so it now
widens a pending or retrying holder stamped with a bounded set to set-less, and
that holder's next pass runs full where base left the row untouched; an
unstamped, set-less, owed, or attempt-above-1 holder is covered and not written.

The merge (`supply_chain_impact_fanout_reopen_merge.go`) keeps the incoming
origin's identity fields and sets `cves` to the sorted distinct union of the
stored and incoming ids, in byte order (`COLLATE "C"`, the order `sort.Strings`
gives). The result is unbounded when either side is unbounded or the union
exceeds `FanOutOriginCVEIDCap` (taken from the Go constant): the sentinel when
the incoming stamp names a set, or the incoming stamp itself when it names
none. The merge reads the locked current row, never the target CTE snapshot,
so two fan-outs that reach one holder serialize on its row lock, and the
second merges into what the first committed.

This is safe: the merged stamp only grows, so a row that is covered stays
covered until its pass runs. A row is written at most once per new id and never
after it reaches the sentinel. A pass that claimed under the old stamp fails the
stamp-equality flag clear (`ClearReplayFlagIfStampSeen`) and reruns, which is
the existing design. The statement still takes one holder's row locks in
`work_item_id` order and nothing else. No worker count, batch size, or claim
query changes.

## Why the drop is sound

A holder that was not queued, or not covered, at the read reaches the reopen.
A covered holder claimed after the read is dropped, and its pass covers the
change. The read runs after this pass's writer commit, so the claim loads the
committed source facts, and the stamp only grows, so the pass is full or
contains `stampOut`. The in-flight read still runs first and the row read
second; the prefilter reads last.

No lock, lease, claim query, or ordering changes. The read takes no row lock and
writes nothing. A crash before `UpdateFanOutState` leaves the stored list as it
was, and the retry reads and filters the same list again, which is idempotent.
A failed read fails the pass for a queue retry (outcome `read_error` or
`read_timeout`, site `holder_prefilter`). There is no unfiltered fallback.

A drain claim does not need the prefilter to be exact: a holder that queues
after the read costs one statement, as it did before.

Not treated as a widening: a covered `retrying` holder that later dead-letters.
That is the same event as a reopened holder that later dead-letters, which no
fan-out handles today ([`HOLDER_FANOUT_DEAD_LETTERS.md`](HOLDER_FANOUT_DEAD_LETTERS.md)).

The status-only drop that shipped first in this PR dropped every pending or
retrying holder. That lost a change for a holder stamped for another one: base
deferred such a holder past the cap, and its drain claim reopened it after its
pass succeeded, but the status-only drop removed it from the list for good. The
pre-existing limit, a pending holder's stamp never widened, is fixed here by the
stamp merge rather than recorded.

## Operator signals

- `eshu_dp_supply_chain_impact_fanout_total{outcome="prefiltered_pending"}`
  counts the holders dropped. `outcome="stamp_widened"` counts holders whose
  queued or flagged item got the change merged into its stamp; it ranks above
  `pending_noop`, so a widened row is never counted as a no-op. `pending_noop`
  now means a covered holder that queued between the prefilter read and its
  reopen, a flagged row whose stamp already names the change, or an enqueue
  that conflicted with an item that appeared after the reopen's read
  (`enqueueMissingHolder`).
- Read failures carry `site="holder_prefilter"`, and
  `eshu_dp_supply_chain_impact_fanout_read_duration_seconds{kind="holder_prefilter"}`
  times every prefilter read.
- The fan-out info log carries `prefiltered_pending` and `stamp_widened`, the
  span carries the same attributes, and the result sub-signals
  `fanout_prefiltered_pending` and `fanout_stamp_widened` carry the counts.
  `fanout_holders` still counts every holder before the prefilter.

At 3 AM: `prefiltered_pending` rising with `deferred` falling is the fix
working. `stamp_widened` should fall as stamps converge; a flat or rising
`stamp_widened` with `deferred` flat means the holders keep receiving new
changes faster than they run. `deferred` rising with `prefiltered_pending` flat
means the holders are not queued, and the cap is doing real work. A prefilter
read p95 near 2 s on `kind="holder_prefilter"` means the domain's matched-status
rows have grown and the plan needs a look.
