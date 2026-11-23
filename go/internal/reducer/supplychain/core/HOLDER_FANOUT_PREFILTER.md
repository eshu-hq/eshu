# Queued-holder prefilter (#10972)

How the supply-chain impact holder fan-out drops holders that are already
queued before it slices the list. This is a section of
[`HOLDER_FANOUT.md`](HOLDER_FANOUT.md), split out to keep that file under the
500-line Markdown cap.

## The cost it removes

A fan-out claim collects every holder of the changed CVEs, takes the first 256,
and runs one locking `UPDATE` per holder (`ReopenHolderDetailed`). On QA a
holder is almost always already `pending` or `retrying`, so the statement
classifies it `pending_noop` and writes nothing, yet it still costs a statement
and a share of the 256 cap. The overflow is stored as `fanout_remaining`, and
each drain claim re-runs the whole findings pass only to issue more of the
same no-op statements.

## The rule

After the holder list is sorted and before the cap slice, one set read
(`FindQueuedHolderScopes`, `findQueuedSupplyChainImpactHoldersQuery`) returns
the holders whose item at the scope's active generation is `pending` or
`retrying` and that have no item in any other state the reopen reads. Those
holders are dropped. Everything else stays and goes through
`ReopenHolderDetailed` unchanged, in sorted order:

- `claimed` or `running`: the reopen sets the replay bit.
- `succeeded`: the reopen returns it to `pending`.
- `dead_letter`: reopened or skipped by failure class (#11147).
- no item at the active generation: enqueued, or reported as having no active
  generation.

The cap and the continuation list apply after the prefilter, so they count only
holders that still need a statement. When every holder is queued, `rest` is
empty and no continuation entry is written, and the owed marker clears.

The reopen and the prefilter read one status set
(`reopenSupplyChainImpactMatchedStatuses`) and one active-generation join, so
they cannot disagree on which rows exist. With more than one item at the active
generation, `bool_and` over the matched rows returns the holder only when every
matched row is queued, which is the reopen's strongest-outcome rule.

## Why it is sound

The source pass commits its rows before this read. A holder pending or retrying
at the read claims after that commit and loads evidence that includes it, so a
reopen would change nothing: the same argument the per-holder statement already
relies on (it leaves a queued row untouched), taken at one snapshot. A holder
that is not queued at the read is not dropped, so one claimed after the read is
still flagged by the reopen. The in-flight read still runs first and the row
read second; the prefilter reads last.

No lock, lease, claim query, or ordering changes. The read takes no row lock and
writes nothing. A crash before `UpdateFanOutState` leaves the stored list as it
was, and the retry reads and filters the same list again, which is idempotent.
A failed read fails the pass for a queue retry (outcome `read_error` or
`read_timeout`, site `holder_prefilter`). There is no unfiltered fallback.

A drain claim does not need the prefilter to be exact: a holder queued after the
read costs one `pending_noop` statement, as it does today.

## Operator signals

- `eshu_dp_supply_chain_impact_fanout_total{outcome="prefiltered_pending"}`
  counts the holders dropped. `pending_noop` now means a holder that queued
  between the prefilter read and its reopen, or one already flagged.
- Read failures carry `site="holder_prefilter"`.
- The fan-out info log carries `prefiltered_pending`, and the result sub-signal
  `fanout_prefiltered_pending` carries the same count. `fanout_holders` still
  counts every holder before the prefilter.

At 3 AM: `prefiltered_pending` rising with `pending_noop` and `deferred` falling
is the fix working. `deferred` rising with `prefiltered_pending` flat means the
holders are not queued, and the cap is doing real work.

## Proof

- Core, no database: 300 all-queued holders cost no reopen and no continuation
  entry and one prefilter read; a stored list of queued holders is cleared by
  one read; non-queued states reach the reopen in order; the cap applies after
  the prefilter; a failed read fails the pass before any reopen
  (`fanout_prefilter_test.go`).
- Postgres, live (run by the coordinator): the prefilter equals the reopen's own
  classification, derived from the shipped statement; queued holders keep their
  row version (`xmin`); a holder claimed between the read and the loop is still
  flagged; the read has the fan-out timeout; the plan probes the queue by scope
  (`supply_chain_impact_fanout_queued_*_test.go`).

Performance Evidence (#10972 PR-2 holder prefilter): baseline is the QA
replica reading at 16:27 to 16:41Z under the 338 backfill. 1,248 open
`supply_chain_impact` items carried `fanout_remaining`, 1,229 of them also
`fanout_owed`. A sample of 200 items listed 4,298 distinct holders and none had
succeeded (3,577 pending, 713 retrying, 4 claimed, 4 dead_letter). Per hour the
fan-out ran 24,202 `pending_noop` and 13,896 `deferred` per-holder statements
(`eshu_dp_supply_chain_impact_fanout_total`). The change replaces those with one
set read per claim over the stored list; it touches no claim, lease, or queue
path and adds a read-only statement at one snapshot. The statement is keyed by
scope on `fact_work_items_scope_generation_idx`; its QA `EXPLAIN (ANALYZE,
BUFFERS)` over the largest stored list is NOT_CHECKED here and is run by the
coordinator from `.claude/pr2-prefilter-explain.sql`. The success bar is the
arbiter's: deferred holder statements per hour down at least 90% from 13,896
across three consecutive hours, and open `supply_chain_impact` work lower at the
end than the start.

No-Regression Evidence (#10972 PR-2 holder prefilter): a pass with no queued
holders pays one extra read-only statement and drops nothing; every kept holder
runs the same statement, in the same order, as before. The concurrency model is
unchanged: no lock, lease, claim query, worker count, or batch size changes,
so nothing is serialized.

Observability Evidence (#10972 PR-2 holder prefilter): new counter outcome
`prefiltered_pending` and new read site `holder_prefilter` on
`eshu_dp_supply_chain_impact_fanout_total`, the `prefiltered_pending` field on
the fan-out info log, and the `fanout_prefiltered_pending` sub-signal. A
failing prefilter read counts `read_error` or `read_timeout` with its site, and
the fan-out span records the error.
