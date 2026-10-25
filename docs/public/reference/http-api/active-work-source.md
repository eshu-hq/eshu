# Active Work Source

The status routes below carry an `active_work_source` object (#7009). It says
whether the queue, stage, backlog, blockage, and latest-failure data on that
route came from the stored summary row or from the live statement, and how old
that data is. The routes are `GET /api/v0/status/pipeline`,
`GET /api/v0/status/index` (and `GET /api/v0/index-status`),
`GET /api/v0/status/ingesters`, `GET /api/v0/status/ingesters/{ingester}`,
`GET /api/v0/ingesters` and its `{ingester}` alias,
`GET /api/v0/status/hosted-readiness`, `GET /api/v0/status/operations`,
`GET /api/v0/status/freshness-causality`,
`GET /api/v0/status/operator-control-plane`, and
`GET /api/v0/evidence/bundle`, plus the runtime `/admin/status` JSON. A scoped
caller of the index and operations routes does not read the status snapshot,
so the object is absent there. A reader that reports no source adds no key.

Routes that read the status report but render none of the stored sections
carry no object (#7660 triage): `GET /api/v0/status/collectors` (and
`GET /api/v0/collectors`), `GET /api/v0/status/collector-readiness` (and
`GET /api/v0/collector-readiness`), `GET /api/v0/status/governance`,
`GET /api/v0/status/semantic-extraction`, and
`GET /api/v0/status/answer-narration`. The collectors routes render collector
runtimes and promotion readiness only; governance renders environment config
plus semantic-extraction state; semantic-extraction reads through the
semantic-only selection, which never touches the stored summary; and
answer-narration renders the static narration posture only.

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
two statements run on two snapshots, so the stored row and a live read can
differ by the rows committed in that gap, bounded by one writer pass. On the API
and the MCP server the live read runs both statements in one REPEATABLE READ
snapshot; a runtime that reads status on a plain connection runs each statement
on its own snapshot. `observed_at` values are the collector's clock, not the
database's, so an `observed_at` can be later than `as_of`. No value in the
section is an age, so nothing is advanced at read. The row shares the `ESHU_STATUS_SUMMARY_STALE_AFTER` limit and the other settings below,
and `stale` is `false` for the same reason as above.

## Coordinator source

`GET /api/v0/status/pipeline`, `GET /api/v0/status/index` (and
`GET /api/v0/index-status`), the ingester routes (`GET /api/v0/status/ingesters`,
`GET /api/v0/ingesters`, and their `{ingester}` forms), `GET
/api/v0/status/hosted-readiness`, and the runtime `/admin/status` JSON carry a
`coordinator_source` object (#8828). It has the same five keys and the same
`source` and `reason` values as `active_work_source`, and says where the
coordinator section came from: collector instances, the workflow run,
work-item and completeness status counts, claims, collector backpressure, the
recent-failure counts, and the collector terminal failures that feed collector
health. The reducer stores that section as its own row, `coordinator_snapshot`,
because reading it live costs time linear in `workflow_work_items`, on every
status read and every `/metrics` scrape of every hosted runtime. The row is a
few KB and bounded by the number of collector instances and statuses, not by the
number of work items.

```json
{
  "coordinator_source": {
    "source": "model",
    "reason": "fresh",
    "as_of": "2026-10-25T09:29:48Z",
    "age_seconds": 12,
    "stale": false
  }
}
```

The row has its own `as_of`, digest, and writer transaction, so the three
markers are independent. `age_seconds` is the freshness bound of the whole
section: the counts, claims, backpressure counts, and terminal failures are true
at `as_of`, and the section can lag the live answer by up to that age. The
oldest-pending, oldest-retry, and oldest-claim ages are advanced by the row's age
at read time, and the next-retry delay is reduced by it and stops at zero. A row
older than `ESHU_STATUS_SUMMARY_STALE_AFTER` is not served on a status route; the
live workflow statements answer the whole section and `reason` says `stale`
(`/metrics` scrapes never run them, see below).

The counts are counts of the rows `workflow_work_items` retains, not cumulative
history. With work-item retention on (the default,
`ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE`), the `completed` and
terminal counts fall as old rows are pruned; with it off
(`ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE=0`) they keep growing. The
stored model reads one row either way, so its read cost does not depend on
retention; only the writer's pass duration does
(`eshu_dp_status_summary_writer_pass_duration_seconds{model_key="coordinator_snapshot"}`).

With the reader off it reads `source: live`, `reason: flag_off`.

## Staleness contract

A stored row is never served older than `ESHU_STATUS_SUMMARY_STALE_AFTER`
(default `33s`). Its age is the reader's database clock minus the row's
`as_of`; the caller's clock is not used. The two database clocks (the writer's primary and the
reader's replica) differ by the clock skew between those hosts, and that term is
part of the age. It is small next to the limit: the 25-sample replay-lag probe on
the QA read replica measured a 0.026 s median and 2.09 s p95 for the lag
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
collectors, and the rest) are always read live in the same
snapshot transaction. A database error reading the row fails the status read
instead of falling back, because inside a snapshot transaction a failed
statement aborts it.

## Compatibility

The `active_work_source`, `terraform_state_source`, and `coordinator_source` keys are present on their routes from the release that
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
row's age (`NaN` for the empty summary). The scrape reads two rows, so each gauge
appears twice: `model_key="active_work_summary"` for the stage, queue, and domain
gauges, and `model_key="coordinator_snapshot"` for the `eshu_runtime_coordinator_*`
gauges. A scrape that can decode no coordinator row renders no coordinator gauge
and reports the marker stale with a `NaN` age. A database error fails the
scrape like any other failed status read
(`eshu_runtime_status_snapshot_available 0`). With the reader off neither gauge
is rendered and the scrape is unchanged.
