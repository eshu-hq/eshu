# Metrics scrape serves the stored summary with the reader off (#10872)

## Scope

#10872 closes the enablement hole the arbiter ruling names in D2: a
`StoredActiveWorkOnly` scrape (`metricsSnapshotSelection`, the runtime
`/metrics` surface) used to fall back to the live active-work and coordinator
statements whenever the stored-summary reader flag was off, so every
flag-off hosted runtime ran the full queue-counting statement on every
~30 s scrape. After this change the scrape never runs the live statement,
whatever the flag says: it serves the stored row under the scrape fences
(fresh, else last-decodable or zero with `stale` set). Status routes are
unchanged: with the reader off they still run the live statement with
reason `flag_off`.

No new metric, span, log key, or pipeline stage is added. No statement text
changes: the writer still computes the same pinned
`activeWorkSummaryQuery` byte-for-byte, and the reader still selects the
same stored rows (same `SourceSHA256`, same 33 s `StaleAfter` bound).

## Benchmark Evidence: scrape statement inventory before and after

`TestStatusMetricsStatementInventory` (`go/internal/runtime/metrics_summary_test.go`)
counts every statement one scrape issues over an empty store:

- Before (reader off): 22 statements, including 1 live active-work statement
  and 8 live coordinator statements.
- After (reader off): 17 statements: the summary clock and the keyed row read
  per stored model (2 + 2), zero live active-work and zero live coordinator
  statements. Identical to the reader-on inventory.

`TestScrapeWithTheReaderOffAttributesOnlyModelReads`
(`go/internal/storage/postgres/status/summary/reader_read_total_labels_test.go`)
pins the per-read attribution on the same instruments QA uses: a flag-off
scrape records database time only under
`eshu_dp_status_snapshot_read_duration_seconds{read="active_work_summary_model"}`
and `{read="coordinator_model"}`, plus one
`eshu_dp_status_summary_scrape_total` point per model
(`active_work_summary`/`model`/`fresh`, `coordinator_snapshot`/`zero`/`missing`
in the test shape). No sample is recorded under the live labels
`active_work_summary` or `coordinator`. Before the fix the same scrape
recorded its time under the live labels, so the before/after per-reader
split is measurable on unchanged instruments.

QA-shape before/after (`pg_stat_statements` 60 s deltas, per-service rates
from the same duration/read_total series, writer-pass histogram) is not
measured here: it needs QA access and the deployment enablement the ruling
files as proposal P1 (outside this repo). The ruling's QA baseline to
re-measure against is recorded in the arbiter handoff: primary main form
31 calls/min, 53.9 db-s/min in-window. The residual floor is explicit: the
writer still runs the full statement every 10 s (~1.4 s mean per issue
evidence); that one shared computation is the accepted cost.

## No-Regression Evidence (#10872): correctness, staleness, plan

- Correctness: `TestScrapeWithTheReaderOffMatchesTheReaderOnServe` serves a
  stored row carrying nonzero waiting, pending, retrying, overdue, failed,
  and dead-letter counts plus a backlog, a blockage, and the latest failure,
  and asserts the flag-off snapshot is identical to the flag-on one at the
  same `as_of` (counts frozen, ages advanced by the 10 s row age).
  `TestStatusMetricsWithTheReaderOffServeTheStoredSummary` asserts the
  handler renders byte-identical output with the flag off and on (fresh,
  stale-last-row, and zero cases). Existing differentials
  (`TestActiveWorkSummaryMatchesStandaloneReads`,
  `TestActiveWorkSummaryDecodesStoredSummaryRowsUnchanged`, gate/oracle and
  reopened-backlog-age fixtures) still pass.
- Staleness bound: `TestScrapeWithTheReaderOffHonorsTheStalenessBound` pins a
  row exactly 33 s old as fresh and 34 s old as `last_row`/stale; the
  full-coverage test pins counts frozen with ages advanced.
- Query plan: `TestStatusActiveFactWorkItemsCTEUsesGenerationIndex` passes
  against a scratch Postgres 18.7 with a 2,000-scope / 100,000-generation /
  20,000-work-item fixture: the main form runs `EXPLAIN (ANALYZE, BUFFERS)`
  in both gate states (execution 38.3 ms detail, 97.4 ms grouped, plus the
  generic-plan runs at 63.5/62.9 ms), the `fact_work_summary_mode` gate CTE
  evaluates once (`loops=1`), and the seeded nested-loop rescan is rejected
  in both CTEs. The statement shape is unchanged by this fix; a QA-shape
  (268,754 rows / 1.7 GB, grouped branch) plan capture still needs QA access.

## Observability Evidence: what a flag-off scrape emits now

- `eshu_dp_status_snapshot_read_duration_seconds{read="active_work_summary_model"}`
  and `{read="coordinator_model"}` (the two stored-row reads, outcome-labeled);
  the live `active_work_summary` / `coordinator` legs no longer appear on
  scrapes, so a 3 AM reader attributes scrape database time to the stored
  reads directly.
- `eshu_dp_status_summary_scrape_total{model_key,source,reason}` with
  `source` in `model`, `last_row`, `zero` (was: never emitted with the flag
  off); a sustained non-`model` share means the writer is down, slow, or on
  another statement version.
- `eshu_runtime_status_summary_stale{service_name,model_key}` (`0` fresh,
  `1` otherwise) and
  `eshu_runtime_status_summary_age_seconds{service_name,model_key}` (row age,
  `NaN` for the zero summary) per model, plus the existing rate-limited
  `status summary row not served fresh` Warn with
  `failure_class=status_summary_scrape_stale`.
- A store built without a summary reader fails the scrape read
  (`ErrScrapeReaderDisabled`, HTTP 500 `load runtime metrics failed`) instead
  of running the live statement; production stores always attach the
  process-wide reader via `NewInstrumentedStatusStore`.
