# Active Work Source

Status routes that render queue, stage, backlog, blockage, or latest-failure
data carry an `active_work_source` object (#7009). It says whether those
sections came from the stored summary row or from the live statement, and how
old they are. The routes are `GET /api/v0/status/pipeline`,
`GET /api/v0/status/index` (and `GET /api/v0/index-status`),
`GET /api/v0/status/ingesters`, `GET /api/v0/status/ingesters/{ingester}`,
`GET /api/v0/ingesters` and its `{ingester}` alias,
`GET /api/v0/status/hosted-readiness`, and `GET /api/v0/status/operations`,
plus the runtime `/admin/status` JSON. A scoped caller of the index and
operations routes does not read the status snapshot, so the object is absent
there. A reader that reports no source adds no key.

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
| `as_of` | When the active-work counts are true. For `model` it is the stored row's `as_of`; otherwise it is the snapshot clock. |
| `age_seconds` | How old the served data was at the read, from the database clock. `0` for a live read. |
| `stale` | `true` when a stored row existed but was older than the limit, so the live statement answered. |

## Staleness contract

A stored row is never served older than `ESHU_STATUS_SUMMARY_STALE_AFTER`
(default `33s`). Its age is the reader's database clock minus the row's
`as_of`; the caller's clock is not used. The counts in a stored row are true at
`as_of`, so `overdue_claims`, blockage membership, and in-flight counts can lag
the live answer by up to that age. The oldest-outstanding and oldest-blocked
ages are advanced by the row's age at read time, so they match what the live
statement would report to within the read's own clock skew.

A fallback is whole: one report never mixes a stored row with a live
active-work read. The other sections of the report (scopes, generations,
coordinator, collectors, and the rest) are always read live in the same
snapshot transaction. A database error reading the row fails the status read
instead of falling back, because inside a snapshot transaction a failed
statement aborts it.

## Turning the reader on

The reader is off by default. It needs the reducer's writer
(`ESHU_STATUS_SUMMARY_WRITER_ENABLED`) to have stored a row; without one every
read reports `live_fallback` with reason `missing`. Set
`ESHU_STATUS_SUMMARY_READ_ENABLED=true` on the API, the MCP server, and every
runtime that serves status. Watch `eshu_dp_status_summary_read_total` by
`source` and `reason`: a sustained `live_fallback` rate means the writer is
down, slow, or running another version.
