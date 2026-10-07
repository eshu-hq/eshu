# Status summary read model: runtime /metrics scrape (#7009, PR-D)

## Scope

This note covers the fourth slice of the #7009 status read model: the runtime
`/metrics` scrape of every hosted runtime (ingester, every collector, reducer,
projector). The table and store are PR-A, the periodic writer is PR-B, and the
status reader with its live fallback is PR-C
(`7009-status-summary-reader.md`). The terraform recent-warnings fold (PR-E) and
the deployed A-B-A sweep (PR-F) are not in this slice.

The design ruling's rule for this path: it never falls back. A fresh row serves
the model; a stale or missing row serves the newest row the process can decode
(arbiter ruling: a stale row included), else the zero summary, and in both cases the scrape exports
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
| stale row that passes version, row_count, decode and age-key checks, newer than any held row | that stale row (`Select` with `DecodeStale`), ages advanced by its age at this read | `last_row` | `1` |
| any other outcome, process holds a row | the held row, ages advanced by the database clock minus its `as_of` at this read | `last_row` | `1` |
| any other outcome, process can decode no row | the zero summary | `zero` | `1` |
| database error | the scrape fails (HTTP 500, `eshu_runtime_status_snapshot_available 0`) | none | none |

The held row is one row per `ModelReader`: the process-wide reader the hosted
runtime builds once through `NewInstrumentedStatusStore`, so it is the same
object for every scrape. It is guarded by a mutex, replaced only by a row with a
newer `as_of`, kept as the writer stored it (before the age advance), and given
its age again at every read from the database clock, so a held row is never
served as fresh, and its age keeps growing while the writer is down. Hosted
runtimes read in autocommit (PR-C decision 13), so the clock read, the row read,
and every other statement are separate statements; a writer commit between the
first two can only make an age negative, which clamps to zero.

The gauges are rendered only when the reader answered, so a scrape with the
reader off is byte-identical to one before this change
(`TestStatusMetricsWithTheReaderOffAreUnchanged`).

## What the scrape reads

`renderStatusMetrics` (`go/internal/runtime/metrics.go`) reads only these
`Report` fields: `ScopeActivity` (lines 115-117), `RetryPolicies` (123),
`Health.State` (137), `Queue` (146), `CollectorGenerationDeadLetters` (162),
`GenerationTotals` (184-191), `StageSummaries` (195), `DomainBacklogs` (209),
`Coordinator` (224), and `ActiveWorkSource` for the summary marker (225).
`Health` is computed by `evaluateHealth` (`go/internal/status/health.go:14-22`),
called at `go/internal/status/status.go:162` with exactly the queue snapshot,
generation totals, domain backlogs, producer activity, coordinator, and
collector generation dead letters (plus options). None of the four sections the
selection omits (`TerraformStateLastSerials`, `TerraformStateRecentWarnings`,
`CollectorFactEvidence`, `RegistryCollectors`) is an argument or a rendered
field.

That is also measured, not only read: `TestScrapeReadSetExcludesTheSectionsTheSelectionOmits`
fills every `RawSnapshot` field, blanks one at a time, and records which blanking
changes the rendered bytes or the computed health. The set is
CollectorGenerationDeadLetters, Coordinator, DomainBacklogs, GenerationCounts,
Queue, RetryPolicies, ScopeActivity, StageCounts; the four omitted sections are
outside it, and the test requires every section the render reads to be found
(its non-vacuity list). Health reads producer activity only behind earlier
decisive checks that the all-populated baseline always trips, so
`TestScrapeBytesIgnorePopulatedOmittedSections` measures health with only the
omitted sections populated instead.

## Last row: concurrency model

- One `lastRow` per `ModelReader`, and a hosted runtime has one `ModelReader`
  for the process (`NewInstrumentedStatusStore`, copied by pointer into every
  store wrapper). It holds at most one row: the entries as the writer stored
  them and the row's `as_of`.
- Writers: any scrape that serves a fresh row, or finds a newer stale row that
  decodes, calls `remember`. Readers: any
  scrape that cannot serve a fresh row calls `recall`. Both take one
  `sync.Mutex` for a few field reads or writes; nothing is held across a
  database call or a decode.
- `remember` replaces the held row only for a strictly newer `as_of`, so a slow
  scrape that finishes late cannot move the row back; a row with the same
  `as_of` is not copied. `recall` returns the held slice without copying: its
  `Entry` values are strings and `AddAge` never modifies its input, so a
  concurrent replacement swaps the slice and cannot change what a reader holds.
- Allocation: the holder allocates nothing per scrape in steady state
  (`TestLastRowHolderAddsNoAllocationToAScrape`: 0 allocations for re-remembering
  the held row and for recalling it); it copies the entries once, when a newer
  row replaces the held one. A scrape that serves the held row still pays for
  `AddAge` and the decode of the row's few tuples, as a fresh scrape does for
  its own `Select` and decode; that cost is proportional to the row (about 35
  tuples at production shape), not to the number of scrapes or the queue size.
  No claim is made of zero allocation beyond the holder.
- `TestScrapeLastRowIsSafeAndWholeUnderParallelScrapes` runs under `-race`: a
  writer goroutine moves the database clock and replaces the stored row with
  fresh rows (each carrying its id in the stage count), stale rows, and no row,
  while 16 scrapers run 300 scrapes each through the production store and one
  shared reader. Every answer is one whole row, a fresh serve is never stale and
  a stale one never fresh, the live statement runs zero times, a scraper that has
  seen a row never gets the zero summary again, and the held `as_of` a scraper
  sees never goes back. Removing the lock makes the race detector fail it.

## Proof

Hermetic, `go test -race -count=1` per package:

- `summary`: fresh row served with no live statement; every reason a status read
  would fall back on (table missing, row missing, schema version, digest, row
  count, stale-and-undecodable, payload) serves the zero summary on a new process
  with zero live statements; a valid stale row on a new process serves that row
  (`last_row/stale`, its counts, ages advanced, real age) and the API path on it
  is unchanged; an older stale row never replaces a newer held row and a newer
  one does; a held row is served with an age that advances with the database
  clock and is never the rejected row's data; a row that goes missing keeps the
  held row; a clock read error fails the read with no live statement; the
  reader off runs the live statement once; 16 goroutines scraping together under
  `-race`; the holder never moves back in time and copies what it holds; a fresh
  row that does not decode serves the held row with reason `decode`; the scrape
  counter, span attributes, and rate-limited Warn.
- `internal/runtime`: the statement inventory over the production
  `StatusStore`; the end-to-end scrape over the production store (zero summary,
  fresh row, last row at +100 s and +200 s, row deleted, a restart against a 100 s
  old row); the gauge text per
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
  a new process serves the stale row itself (age past 10 minutes, its own counts) and, with the table dropped, the zero summary; the live active-work statement runs zero
  times.
- `TestScrapeStatementInventoryOnAnEmptyStoreLive`: the statement inventory on a
  real empty database, with the selection captured from the production handler
  instead of copied.
- `TestScrapeBytesEqualWithTheOmittedSectionsPopulatedLive`: a real database
  whose omitted sections are populated (collector evidence summary, registry
  collector instances, registry work items with retryable and terminal failures
  that the coordinator section also counts) renders identical `/metrics` bytes
  through the production handler with the metrics selection and with the broader
  full-minus-Terraform selection, at one pinned instant with the reader off. The
  test asserts the broader read returns non-empty evidence, registry failures,
  and coordinator instances, and plants a violation: finishing three work items
  must change the bytes.

## Performance Evidence

Performance Evidence: one scrape through the production StatusStore on an empty store sends 24 statements before this change, 20 with the reader off, and 21 with the reader on and no live active-work statement; the table below gives the split and the figures behind it.

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

Observability Evidence: the per-scrape gauges eshu_runtime_status_summary_stale and eshu_runtime_status_summary_age_seconds (service_name, model_key), the counter eshu_dp_status_summary_scrape_total (model_key, source, reason), span attributes on the status snapshot span, and a Warn at most once a minute per reason let an operator see a stale or empty scrape summary at 3 AM; the list below gives each signal.

- `eshu_runtime_status_summary_stale{service_name,model_key}` and
  `eshu_runtime_status_summary_age_seconds{service_name,model_key}` per-scrape
  gauges, only while the reader is on. Age is `NaN` for the zero summary.
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

## Arbiter rulings applied (B/7009-pr-d-decisions-ruling-20261007.md)

- A database error fails the scrape (500, `eshu_runtime_status_snapshot_available 0`),
  not a stale serve and not a live run. The other statements of the scrape run
  on the same pool and would fail with it.
- The age gauge for the zero summary is `NaN`, not a sentinel number: the
  exposition format's value for "no number", dropped by every PromQL comparison.
- A valid stale row this process never served is served, not zeroed: `Select`
  with `DecodeStale` (set by the scrape hook alone) decodes a stale row that
  passed the version and row_count fences and whose age keys can be advanced, and
  `ReadScrape` keeps it as the last row when it is newer than the one held and the
  production decoder accepts it. A pod that restarts during a writer outage
  therefore exports the stored counts with their true age, so a queue-age alert
  keeps seeing the stall, and the health gauge is evaluated on real counts. A
  stale row that is foreign, miscounted, or undecodable still serves the zero
  summary, with its own reason. Status routes leave `DecodeStale` off: the API
  path on a stale row is statement- and byte-identical (`live_fallback/stale`).
- The scrape drops the fact-evidence and registry collector reads with the reader
  on or off, because it renders neither. Hosted runtimes therefore stop
  emitting `eshu_dp_status_snapshot_read_duration_seconds{read="registry_collectors"}`
  and `{read="collector_fact_evidence"}`; the API and MCP routes still do.
- The scrape counter carries `reason` as well as `source`, so a stale serve says
  why (writer down versus a rolling upgrade).
- The two gauges carry `service_name` as well as `model_key`, like every other
  gauge the renderer writes, because the shipped alert rules join on
  `service_name`.
- The held row is updated only by a fresh serve that decodes or by a newer stale
  row that decodes; a fresh row that fails decode serves the held row with reason
  `decode`.

## Safety

- With the reader off the scrape is byte-identical and the live statement runs as
  before.
- With it on, the live active-work statement never runs on a scrape, so a stopped
  writer cannot turn the fleet's scrapes into a herd.
- A held row is never served as fresh: stale is `1`, and its age is derived from
  its `as_of` and the database clock at each read.
