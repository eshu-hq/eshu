# Active Work Source

The status routes below carry an `active_work_source` object (#7009). It says
whether the queue, stage, backlog, blockage, and latest-failure data on that
route came from the stored summary row or from the live statement, and how old
that data is. The routes are `GET /api/v0/status/pipeline`,
`GET /api/v0/status/index` (and `GET /api/v0/index-status`),
`GET /api/v0/status/ingesters`, `GET /api/v0/status/ingesters/{ingester}`,
`GET /api/v0/ingesters` and its `{ingester}` alias,
`GET /api/v0/status/hosted-readiness`, and `GET /api/v0/status/operations`,
plus the runtime `/admin/status` JSON. A scoped caller of the index and
operations routes does not read the status snapshot, so the object is absent
there. A reader that reports no source adds no key.

Not every route that renders this data carries the object yet. The live evidence
bundle and the freshness-causality route read the same report and have no
`active_work_source`, so with the reader on they serve stored counts without the
marker; a follow-up covers them before the reader's default changes (see the
evidence note for #7009).

```json
{
  "active_work_source": {
    "source": "model",
    "reason": "fresh",
    "as_of": "2026-10-06T12:00:20Z",
    "age_seconds": 9.4,
    "stale": false
  }
}
```

| Field | Meaning |
| --- | --- |
| `source` | `model`: a stored row that passed every check. `live`: the stored-summary reader is off, so the live statement ran. `live_fallback`: the stored row could not be served, so the live statement answered the whole active-work part of the report. |
| `reason` | `fresh`, `flag_off`, `missing` (no row yet), `not_installed` (migration 161 not applied), `version` (the row came from another schema or statement version), `row_count`, `stale`, or `decode`. |
| `as_of` | When the active-work counts are true. For `model` it is the stored row's `as_of`; otherwise it is the snapshot clock, or, for a read that shared another read's live statement, the clock that statement ran at. |
| `age_seconds` | How old the served data was at the read, from the database clock. `0` for a live read. |
| `stale` | `true` when the served data is older than the limit. A stored row that is too old is never served, so this is `false` on every route above; `reason: stale` says the live statement answered because the row was too old. It is reserved for the runtime `/metrics` scrape. |

## Staleness contract

A stored row is never served older than `ESHU_STATUS_SUMMARY_STALE_AFTER`
(default `33s`). Its age is the reader's database clock minus the row's
`as_of`; the caller's clock is not used. The two database clocks (the writer's primary and the
reader's replica) differ by the clock skew between those hosts, and that term is
part of the age. It is small next to the limit: the 25-sample replay-lag probe on
the ops-qa read replica measured a 0.026 s median and 2.09 s p95 for the lag
itself, which the 33 s default already budgets; a reader clock behind the
writer's makes the age negative, and it is clamped to zero. The counts in a
stored row are true at `as_of`, so `overdue_claims`, blockage membership, and in-flight counts can lag
the live answer by up to that age. The oldest-outstanding and oldest-blocked
ages are advanced by the row's age at read time, so they match what the live
statement would report to within the read's own clock skew.

A fallback is whole: one report never mixes a stored row with a live
active-work read. When concurrent reads in one process fall back together, they
share one live statement; each reports the clock that statement ran at as its
`as_of`. The other sections of the report (scopes, generations,
coordinator, collectors, and the rest) are always read live in the same
snapshot transaction. A database error reading the row fails the status read
instead of falling back, because inside a snapshot transaction a failed
statement aborts it.

## Compatibility

The `active_work_source` key is present on these routes from the release that
adds it, whatever the reader flag says: with the reader off it reads
`source: live`, `reason: flag_off`. It is an additive field.

## Turning the reader on

The reader is off by default. It needs the reducer's writer
(`ESHU_STATUS_SUMMARY_WRITER_ENABLED`) to have stored a row; without one every
read reports `live_fallback` with reason `missing`. Set
`ESHU_STATUS_SUMMARY_READ_ENABLED=true` on the API, the MCP server, and every
runtime that serves status. The settings are read once when the process starts,
and an invalid `ESHU_STATUS_SUMMARY_STALE_AFTER` stops the process from
starting instead of failing each status read. Watch `eshu_dp_status_summary_read_total` by
`source` and `reason`: a sustained `live_fallback` rate means the writer is
down, slow, or running another version.
