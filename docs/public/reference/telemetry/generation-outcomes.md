# Generation Outcome Telemetry

A collector poll that reaches the ingestion commit ends in one of two counted
outcomes: the store **mints** a new scope generation, or it **skips** the
refresh because nothing stored needs to change. Two other endings count
neither: a commit that fails (the generation rolls back and the error
surfaces), and a refresh the store refuses because the scope is retiring
(rolled back, logged as a warning, no outcome recorded). A collector that mints on every poll without upstream
change keeps one full snapshot per poll until retention removes it (#10020: the
KEV catalog minted 614 generations for 8 distinct catalog versions). These
signals let an operator count mints against skips per collector kind without
reading logs.

## Counter

`eshu_dp_ingestion_generation_outcomes_total` counts one increment per commit
outcome. Every label is a closed set:

| Label | Values |
| --- | --- |
| `collector_kind` | A collector kind from `scope.AllCollectorKinds` (for example `git`, `vulnerability_intelligence`), or `other` for any value outside that list. A skip recorded without scope identity (the reconcile re-projection path) lands in `other`. |
| `outcome` | `minted` (a new generation was stored) or `skipped` (nothing was stored). |
| `reason` | `none` when `outcome` is `minted`. When `outcome` is `skipped`, one value of the closed refresh-skip set: `unchanged`, `pending_same_identity`, `reconcile_reprojected`, `reconcile_reprojection_pending`, `reconcile_superseded`, `finalized_generation_replay`, `destructive_successor_refused`, or `other`. |

It never carries a scope id. A collector such as OSV owns more than 1,600
scopes, so a scope label would explode the series count. The per-scope source of
truth is the logs below.

The counter is per process. With several collector replicas, sum across
replicas, and expect a restart to reset a series. A new series also starts at
its first event: Prometheus `increase()` has no earlier sample to subtract from,
so the first mint after a restart is not counted by the queries below. For a
rare event such as a KEV mint, read the raw series or the
`ingestion.generation_minted` span event when a count of one matters.

The reconcile re-projection path knows only the scope id, not its collector
kind, so its skips land in `collector_kind="other"` even when the scope belongs
to `git`. While a reconcile sweep runs, the `git` skipped count is a lower bound
and the mint-to-skip ratio for `git` reads high; the `other` series carries the
rest.

## Operator queries

Mint-to-skip ratio per collector kind over the last day (a ratio near 1 for a
polling collector means every poll stores a generation):

```promql
sum by (collector_kind) (increase(eshu_dp_ingestion_generation_outcomes_total{outcome="minted"}[1d]))
/
sum by (collector_kind) (increase(eshu_dp_ingestion_generation_outcomes_total[1d]))
```

Generations minted per hour for one collector kind:

```promql
sum(increase(eshu_dp_ingestion_generation_outcomes_total{collector_kind="vulnerability_intelligence",outcome="minted"}[1h]))
```

Skips by reason (a collector whose skips are all `unchanged` and whose mints
track upstream publishes is healthy):

```promql
sum by (reason) (increase(eshu_dp_ingestion_generation_outcomes_total{collector_kind="vulnerability_intelligence",outcome="skipped"}[1d]))
```

A rate of mints that tracks the poll cadence (48 per day for a 30-minute poll)
while upstream publishes a few times a week means the collector's freshness
hint is not matching, or the hint is empty. Find the scope in the
`commit_transaction` stage logs below.

## Per-scope evidence

| Signal | Where | Carries |
| --- | --- | --- |
| `ingestion refresh skipped` INFO log | skip | `scope_id`, `collector_kind`, `refresh_skip_reason`, `generation_id`, `matched_generation_id` |
| `ingestion.refresh_skipped` span event | skip, on the caller's span | `refresh_skip_reason`, `scope_id`, `matched_generation_id` |
| `ingestion commit stage completed` INFO log with `stage=commit_transaction` | mint | `scope_id`, `generation_id`, `collector_kind`, `refresh_outcome=minted` |
| `ingestion.generation_minted` span event | mint, on the caller's span | `scope_id`, `collector_kind`, `generation_id` |

The older `eshu_runtime_refresh_skipped_by_reason_total{reason}` series on the
runtime `/metrics` endpoint still counts skips by reason without a collector
kind and has no minted side; use the counter above for the ratio.
