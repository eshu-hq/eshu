# Status summary read model: runtime /metrics scrape (#7009, PR-D)

## Scope

This note covers the fourth slice of the #7009 status read model: the runtime
`/metrics` scrape of every hosted runtime (ingester, every collector, reducer,
projector). The table and store are PR-A, the periodic writer is PR-B, and the
status reader with its live fallback is PR-C
(`7009-status-summary-reader.md`). The terraform recent-warnings fold (PR-E) and
the deployed A-B-A sweep (PR-F) are not in this slice.

The design ruling's rule for this path: it never falls back. A fresh row serves
the model; a stale or missing row serves the last decoded row if the process has
one, else the zero summary, and in both cases the scrape exports
`eshu_runtime_status_summary_stale 1` and `eshu_runtime_status_summary_age_seconds`.

## Design as built

`serveStatusMetrics` reads the snapshot with `metricsSnapshotSelection`:

- No Terraform-state serial or warning read (PR-3, #7643).
- No collector fact-evidence read and no registry collector reads. The scrape
  renders neither (`renderStatusMetrics` writes scope, retry, health, queue,
  collector generation, generation, stage, domain, and coordinator gauges), and
  `evaluateHealth` takes the queue, generation totals, domain backlogs,
  producer activity, coordinator, and collector generation dead letters, so
  neither section can change a rendered byte. `TestStatusMetricsSkipsTerraformEvidence`
  proves byte-identical output with the sections omitted from a snapshot that
  carries them.
- `StoredActiveWorkOnly`: with `ESHU_STATUS_SUMMARY_READ_ENABLED` on, the store
  answers the active-work part through `ModelReader.ReadScrape` instead of
  `ModelReader.Read`. With it off the live statement runs as before.

`ReadScrape` runs PR-C's `Select` (the same fences, in the same order) and then:

| `Select` outcome | Served | `source` | `stale` |
| --- | --- | --- | --- |
| fresh row, decodes | the row, ages advanced by its age | `model` | `0` |
| any other outcome, process holds a row | the held row, ages advanced by the database clock minus its `as_of` at this read | `last_row` | `1` |
| any other outcome, process holds none | the zero summary | `zero` | `1` |
| database error | the scrape fails (HTTP 500, `eshu_runtime_status_snapshot_available 0`) | none | none |

The held row is one row per `ModelReader`: the process-wide reader the hosted
runtime builds once through `NewInstrumentedStatusStore`, so it is the same
object for every scrape. It is guarded by a mutex, replaced only by a row with a
newer `as_of`, kept as the writer stored it (before the age advance), and given
its age again at every read from the database clock, so a held row is never
served as fresh and its age keeps growing while the writer is down. Hosted
runtimes read in autocommit (PR-C decision 13), so the clock read, the row read,
and every other statement are separate statements; a writer commit between the
first two can only make an age negative, which clamps to zero.

The gauges are rendered only when the reader answered, so a scrape with the
reader off is byte-identical to one before this change
(`TestStatusMetricsWithTheReaderOffAreUnchanged`).

## Proof

Hermetic, `go test -race -count=1` per package:

- `summary`: fresh row served with no live statement; every reason a status read
  would fall back on (table missing, row missing, schema version, digest, row
  count, stale, payload) serves the zero summary on a new process with zero live
  statements; a held row is served with an age that advances with the database
  clock and is never the rejected row's data; a row that goes missing keeps the
  held row; a clock read error fails the read with no live statement; the
  reader off runs the live statement once; 16 goroutines scraping together under
  `-race`; the holder never moves back in time and copies what it holds; a fresh
  row that does not decode serves the held row with reason `decode`; the scrape
  counter, span attributes, and rate-limited Warn.
- `internal/runtime`: the statement inventory over the production
  `StatusStore`; the end-to-end scrape over the production store (zero summary,
  fresh row, last row at +100 s and +200 s, row deleted); the gauge text per
  source; a database error is a 500 with no gauge and no live statement; the
  reader-off scrape is byte-identical to the scrape from the selection before
  this change; the selection pin.
- `status`: the new selection field changes no other request and a semantic-only
  selection rejects it.

Live, on PostgreSQL 18.6 (native, private cluster, loopback), enrolled in the
`live-postgres-readiness` runner and the reducer contention gate:

- `TestScrapeServesTheStoredRowAndNeverTheLiveStatementLive`: in autocommit, a
  row written by the production writer is served equal to the live statement at
  the same data and clock; after the row ages past the limit, is deleted, or its
  table is dropped, the process serves its last row stale with a growing age; a
  new process serves the zero summary; the live active-work statement runs zero
  times.
- `TestScrapeStatementInventoryOnAnEmptyStoreLive`: the statement inventory on a
  real empty database.

## Performance Evidence

Statements one scrape sends through the production `StatusStore` to an empty
store (count of physical statements; `TestStatusMetricsStatementInventory` over
a recording fake and `TestScrapeStatementInventoryOnAnEmptyStoreLive` on
PostgreSQL 18.6 report the same numbers):

| scrape | statements | live active-work | fact_records aggregate + registry collectors |
| --- | --- | --- | --- |
| before this change (full minus Terraform, reader off) | 24 | 1 | 4 |
| this change, reader off | 20 | 1 | 0 |
| this change, reader on | 21 | 0 | 0 |

With the reader on, the live active-work statement (measured by the design
ruling at 126-449 ms and 45k-153k buffers on the fixture) is replaced by the
clock read (PR-C measured 0.006 ms execution, no buffers) and the keyed row read
(0.021 ms, 2 buffers). The four removed reads are the collector fact-evidence
aggregate over `fact_records` and the three registry collector statements. These
are structural counts and server-side figures on a fixture. No deployed scrape
latency or rate is claimed here: that is PR-F's sweep.

NOT_CHECKED: the cost of the four removed statements on ops-qa, how often
Prometheus scrapes each pod, and the effect of the replica replay lag on the
scrape's age gauge.

## Observability Evidence

- `eshu_runtime_status_summary_stale{model_key}` and
  `eshu_runtime_status_summary_age_seconds{model_key}` per-scrape gauges, only
  while the reader is on. Age is `-1` for the zero summary.
- `eshu_dp_status_summary_scrape_total{model_key, source, reason}` counter;
  `source` is `model`, `last_row`, or `zero`.
- The current span carries `status.active_work.source`, `as_of_age_seconds`, and
  `fallback_reason` for a scrape that did not serve a fresh row.
- A scrape that serves a row other than a fresh one logs a Warn at most once a
  minute per reason per process (`model_key`, `source`, `reason`, `age_seconds`,
  `failure_class=status_summary_scrape_stale`).

At 3 AM, `eshu_runtime_status_summary_stale == 1` on a pod says that pod served a
stale or empty summary; `sum by (source, reason)
(rate(eshu_dp_status_summary_scrape_total[5m]))` says why, and the age gauge
says for how long. The reducer's `eshu_dp_status_summary_writer_passes_total`
says whether the writer is the cause.

## Decisions where the ruling was silent

- A database error fails the scrape (500), not a stale serve. The other 20
  statements run on the same connection pool and would fail with it; the
  composite handler already reports a failed snapshot as
  `eshu_runtime_status_snapshot_available 0`.
- The age gauge is `-1` for the zero summary: there is no row and so no age.
  Alert on the stale gauge, not on this value alone.
- A stale row that this process never decoded is not served: the process serves
  the zero summary. The ruling says "the last decoded row"; a valid stale row at
  process start is therefore reported as zero, not as its own old data.
- The scrape drops the fact-evidence and registry collector reads with the reader
  on or off. The ruling asks the executor to verify from `renderStatusMetrics`
  before narrowing; it is output-neutral, so it is not gated on the flag.
- The scrape counter carries `reason` as well as `source`, so a stale serve says
  why (writer down versus a rolling upgrade).

## Safety

- With the reader off the scrape is byte-identical and the live statement runs as
  before.
- With it on, the live active-work statement never runs on a scrape, so a stopped
  writer cannot turn the fleet's scrapes into a herd.
- A held row is never served as fresh: stale is `1`, and its age is derived from
  its `as_of` and the database clock at each read.
