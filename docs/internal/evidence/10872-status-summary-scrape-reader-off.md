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

- Before (reader off): 26 statements, including 1 live active-work statement
  and 8 live coordinator statements.
- After, reader on: 17 statements: the summary clock and the keyed row read
  per stored model (2 + 2), zero live active-work and zero live coordinator
  statements.
- After, reader off with no servable row: 26 statements: the two probe reads
  plus the full live path each model falls back to (`reason: flag_off`).
- After, reader off with fresh rows for both models: 21 statements, zero live
  active-work and zero live coordinator statements — the 17 stored-serve
  statements plus the four probe reads (one clock and one keyed row read per
  model), live-measured in
  `TestScrapeStatementInventoryOnAnEmptyStoreLive`
  (`before=26 (active-work 1, coordinator 8), reader off=21 (0, 0), reader
  on=17 (0, 0)`).

`TestScrapeWithTheReaderOffAttributesOnlyModelReads`
(`go/internal/storage/postgres/status/summary/reader_read_total_labels_test.go`)
seeds both models, so a flag-off scrape records database time only under
`eshu_dp_status_snapshot_read_duration_seconds{read="active_work_summary_model"}`
and `{read="coordinator_model"}`, plus one
`eshu_dp_status_summary_scrape_total` point per model
(`active_work_summary`/`model`/`fresh`, `coordinator_snapshot`/`model`/`fresh`
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
  in both CTEs. The statement shape is unchanged by this fix.
- QA-shape plan: `TestStatusActiveWorkSummaryQAShapePlan`
  (`active_work_qa_shape_proof_test.go`) runs the unchanged main form
  under `EXPLAIN (ANALYZE, BUFFERS)` at 268,750 `scope_generations` /
  268,750 `fact_work_items` rows with `fact_work_items` at 1,665 MB total
  (database 2,610 MB on scratch Postgres 18.7; two further runs agree within
  1%: detail ~1.16 s, grouped ~0.43 s, generic ~0.40-0.42 s), the QA
  268,754-row / 1.7 GB
  band with incompressible random-hex payload padding so buffer counts
  reflect physical pages: detail 1,159.7 ms (shared hit=17,375 read=30,813,
  temp read=29,642 written=14,874), grouped 425.7 ms (shared hit=21,606
  read=38,385 written=5,352, temp read=2,167 written=2,167), forced generic
  plan 403.9 ms (shared hit=29,614 read=30,387 written=71, temp read=2,167
  written=2,167). The gate CTE reports `loops=1` and the single-pass
  generation-scan rules hold in all three states. The grouped branch — the
  one QA runs — never executes the parallel-seq-scan `InitPlan` over
  `fact_work_items`; its full scans are one hashed pass per CTE (`loops=1`).
  The proof carries its own red demonstration: with index, hash, merge,
  materialize, and memoize paths forced off, the guard rejects the seeded
  nested-loop rescan in both CTEs (plain `EXPLAIN`, never executed).

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

## Follow-up: flag-off scrape falls back to live with no servable row (#10872)

Review of the change above found an availability gap it introduced: with the
reader flag off and no servable stored row (a writer-off deployment, where no
row can ever exist), the scrape served the zero summary — a zero queue that
would hide a real backlog. The fix keeps the stored path untouched and adds a
per-model probe in the postgres layer: with the reader off, each model runs
only its cheap keyed row select first, and serves the stored row only when
`Select` returns `Stored != nil`; otherwise that model falls through to the
live statement and reports `source: live`, `reason: flag_off`. A database
error on the probe fails the scrape like any other failed status read — it
never falls back silently.

The Scope claim above is therefore narrowed: the scrape never runs the live
statement with the reader **on**; with the reader **off** it runs zero live
statements while a servable row exists, and the live statement for exactly
the models with no servable row. The per-model decision is independent: one
model can serve stored while the other runs live. Evidence:

- `TestScrapeWithTheReaderOffServesTheStoredRow` and
  `TestCoordinatorScrapeWithTheReaderOffServesTheStoredRow`: the missing-row
  case now pins `live`/`flag_off` with the live statement executed (1
  active-work statement, 8 coordinator statements) and the live counts
  served instead of zeros.
- `TestStatusMetricsWithTheReaderOffServeTheStoredSummary`: with both rows
  present the flag-off scrape is byte-identical to the flag-on scrape with
  zero live statements; with both rows gone the flag-off scrape runs both
  live fallbacks while the flag-on scrape serves the remembered stale rows.
- `TestScrapeWithTheReaderOffAttributesOnlyModelReads` seeds both models so
  it keeps pinning the stored-only attribution
  (`active_work_summary`/`model`/`fresh`,
  `coordinator_snapshot`/`model`/`fresh`).
- Public contract:
  [active-work-source.md](../../public/reference/http-api/active-work-source.md#runtime-metrics-scrape).
