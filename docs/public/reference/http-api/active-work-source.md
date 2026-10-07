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
| `stale` | `true` when the served data is older than the limit. A stored row that is too old is never served, so this is `false` on every route above; `reason: stale` says the live statement answered because the row was too old. It is `true` only on the runtime `/metrics` scrape, which never reaches this payload (see below). |

## Terraform state source

`GET /api/v0/status/pipeline`, `GET /api/v0/status/index` (and
`GET /api/v0/index-status`, which the MCP `get_index_status` tool calls), and the
runtime `/admin/status` JSON also carry a `terraform_state_source` object. It has
the same five keys and the same `source` and `reason` values as
`active_work_source`, and says where the `terraform_state` section (the last
observed serial per state locator and the recent warnings per locator) came
from. It is present on these routes even when the section is empty. It is
absent on every route that skips Terraform-state evidence: the ingester, operations,
hosted-readiness, collector, collector-readiness, control-plane,
freshness-causality, governance, semantic-extraction and answer-narration
routes, the live evidence bundle, the runtime `/metrics` scrape, and a scoped
caller of the index route. With the reader off it reads `source: live`,
`reason: flag_off`.

```json
{
  "terraform_state_source": {
    "source": "model",
    "reason": "fresh",
    "as_of": "2026-10-07T09:30:48Z",
    "age_seconds": 12,
    "stale": false
  }
}
```

The reducer writes this row apart from the active-work row, so the two markers
are independent: one report can serve active work from `live` and Terraform state
from `model`. `as_of` is the database clock the writer read after taking its
lock, just before it ran the two Terraform-state statements. Each statement sees
the rows committed when it started, so the stored row can include rows committed
between `as_of` and that statement, and it omits anything committed later. The
two statements run on two snapshots, while the live read runs both in one
REPEATABLE READ snapshot, so the stored row and a live read can differ by the
rows committed in that gap, bounded by one writer pass. `observed_at` values are
the collector's clock, not the database's, so an `observed_at` can be later than
`as_of`, and the two sections are read at their own statement starts. No value in the section is an age, so
nothing is advanced at read. The row
shares the `ESHU_STATUS_SUMMARY_STALE_AFTER` limit and the other settings below,
and `stale` is `false` for the same reason as above.

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

The `active_work_source` and `terraform_state_source` keys are present on their routes from the release that
adds them, whatever the reader flag says: with the reader off it reads
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

## Runtime `/metrics` scrape

A runtime's `/metrics` scrape reads the stored row with the same fences and
never runs the live statement, because a scrape from every pod would turn a
stopped writer into a herd of expensive statements. A fresh row is served; a
stale or missing row serves the newest row that process can decode, stale
included, with its ages advanced to the read: a pod that restarts while the
writer is down serves the stored counts with their true age, not zeros. A
foreign, miscounted, or undecodable row serves an empty summary when the process
has no other decodable row. `eshu_runtime_status_summary_stale{service_name,model_key}`
is `0` for a fresh row and `1` otherwise, and
`eshu_runtime_status_summary_age_seconds{service_name,model_key}` is the served
row's age (`NaN` for the empty summary). A database error fails the
scrape like any other failed status read
(`eshu_runtime_status_snapshot_available 0`). With the reader off neither gauge
is rendered and the scrape is unchanged.
