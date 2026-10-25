# Coordinator snapshot summary codec

## Purpose

This package is the codec for the `coordinator_snapshot` model of
`status_summary_snapshots` (#8828). The model stores the workflow coordinator
section of the status snapshot as one bounded row, so a status read or a
`/metrics` scrape reads one primary-key row instead of aggregating
`workflow_work_items`, `workflow_runs`, `workflow_claims` and
`workflow_run_completeness` on every call.

## Flow

```text
reducer status summary writer (every ESHU_STATUS_SUMMARY_WRITER_INTERVAL)
  -> postgres.ReadCoordinatorSummaryEntries   runs readCoordinatorSnapshot
  -> snapshot.Encode                          [section, ordinal, json] entries
  -> status_summary_snapshots (model_key = coordinator_snapshot)

status read / scrape (ESHU_STATUS_SUMMARY_READ_ENABLED)
  -> summary.Select                           fences: version, row count, stale, decode
  -> snapshot.Decode, snapshot.AdvanceAges    same shape the live read returns
```

## Sections

| section | rows | content |
| --- | --- | --- |
| `instance` | one per collector instance | identity, mode, flags, observed and updated times, scan interval |
| `run_count`, `work_item_count`, `completeness_count` | one per status | name and count |
| `backpressure` | one per (kind, instance, source system) | counts, ages, retry delay, failure-class counts |
| `terminal_failure` | at most two per eligible instance | bounded class and operation tokens, scope, work item, times |
| `claims` | exactly one | active and overdue claims, oldest pending age |
| `recent_failures` | zero or one | window and the three recent counts |

An empty entry list is the live read's nil snapshot (no coordinator state).
The row's size is bounded by the number of collector instances and statuses, not
by the number of work items.

## Freshness

The counts are true at the row's `as_of`. Durations are advanced to the read by
`AdvanceAges`: the oldest-pending, oldest-retry and oldest-claim ages grow by the
row's age, and the next-retry delay shrinks by it, stopping at zero. Counts and
terminal failures are not adjusted, so the staleness bound
(`ESHU_STATUS_SUMMARY_STALE_AFTER`, 33 s by default) is the freshness bound of
the whole section. Counts are counts of retained rows, not cumulative history.
