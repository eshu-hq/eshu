# Supply-Chain Impact Supports

The per-support table `supply_chain_impact_finding_supports` holds one row per
live finding fact (#10880 PR-B1). The impact writer keeps it current in the same
transaction that writes the finding facts, and a reducer-owned backfill fills it
for facts written before the maintainer shipped. An old-writer fence marks any
scope a writer without the maintainer touches, and the backfill drains those
marks. Two reducer loaders read it as input (the security-alert reconciliation
evidence loader and the service-catalog vulnerability loader, #10880 PR-B2c), and
the impact holder reads (fan-out, repository holders, scoped seed, out-of-stamp
probe and the Go-modules fence, #11197). With `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ` on, the
scoped-token list, count, inventory, grouped inventory, explain, and the repository,
service, and workload anchored advisory evidence reads serve from it (#10880 PR-B2a) and
answer `503 backend_unavailable` with `Retry-After: 30` while it is not ready. These
signals show whether it is complete and current, which is the same predicate those
reads probe before each call, and how the reducer-side readers used it.

| Signal | Type | Labels | What it tells you |
| --- | --- | --- | --- |
| `eshu_dp_supply_chain_impact_supports_rows_total` | counter | `action`, `source` | Support rows written. `action` is `inserted`, `updated`, `unchanged` (the write-on-change guard left a replayed finding alone) or `deleted` (the row followed a retracted fact). `source` is `writer` (counted after the writer's transaction commits, so a rollback adds nothing) or `backfill`. The backfill reports one count for supports it inserted or changed, so `source=backfill` counts both under `inserted` and never emits `updated`. A `writer` series with `inserted` and `updated` flat while findings are written is a maintainer that stopped. |
| `eshu_dp_supply_chain_impact_supports_reads_total` | counter | `source`, `outcome` | Reducer reads of the supports table, one series per reader. `source` is one of `security_alert_reconciliation`, `service_vulnerability_advisory`, `fanout_holders`, `fanout_holders_key_range`, `fanout_holder_mass`, `repository_holders`, `scoped_packages`, `out_of_stamp` or `go_modules_fence`. `outcome` is `served` (the read ran), `not_ready` (the reader refused because the table was not ready) or `failed` (the readiness probe failed, or, for `security_alert_reconciliation` and `service_vulnerability_advisory`, the supports read failed; a probe failure keeps its own classification and never takes the not-ready class). For the impact fan-out sources (`fanout_holders`, `fanout_holders_key_range`, `fanout_holder_mass`, `repository_holders`) a statement error or timeout after a ready probe records nothing here. For `fanout_holders`, `fanout_holders_key_range` and `fanout_holder_mass` it shows on the fan-out read histogram (`eshu_dp_supply_chain_impact_fanout_read_duration_seconds`, outcome `read_timeout` or `read_error`); for `repository_holders` it shows on `eshu_dp_supply_chain_impact_fanout_total` (site `repository`, outcome `read_timeout` or `read_error`), because that read is not a histogram kind. Both end as the dead-letter class `supply_chain_impact_fanout_failed`. A lookup with an empty id list runs no read and records nothing, and the holder, key-range and mass reads drop blank ids first, so an all-blank mass lookup also records nothing. A rising `not_ready` count means reducer work is waiting on the backfill; it stops when `eshu_dp_supply_chain_impact_supports_backfill_ready` reads 1. |
| `eshu_dp_supply_chain_impact_supports_drain_raw_rows` | histogram | `source` | Raw supports rows one drain read (one whole call for the service-catalog reader; every row the ordered key scans returned, active or not, and the rows a failed drain read before it stopped), by reader (`source`, the same closed set as the reads counter). Raw rows far above the active rows mean the key sits mostly in superseded generations. A drain in the hundred thousands is a hot key. |
| `eshu_dp_supply_chain_impact_supports_drain_duration_seconds` | histogram | `source` | Wall time of one drain of the supports table (one whole call for the service-catalog reader), by reader. A drain near half the domain's handler timeout means a hot key is paging too slowly; the raw-rows histogram above says how large the key was. |
| `eshu_dp_supply_chain_impact_supports_backfill_steps_total` | counter | `outcome` | Backfill steps: `chunk` (supports written or orphans removed for one scope and generation), `drain` (a scope the old-writer fence marked dirty was re-read), `verify_gap` (the verification found live findings without a support and restarted the sweep), `complete` (the step that set the readiness marker), `idle` (a step after completion with nothing to drain), `contended` (another reducer held the lock), `verify` (a verification slice that neither restarted the sweep nor set the marker: it moved the cursor, removed an orphan pair, or was a last slice the fence held back) and `failed`. A rising `verify_gap` means supports went missing after the sweep passed them. |
| `eshu_dp_supply_chain_impact_supports_backfill_step_duration_seconds` | histogram | `outcome` | Wall time of one backfill step. A step holds one scope-and-generation advisory lock for at most one chunk of 500 supports, so the tail is the longest a writer can wait on the backfill. |
| `eshu_dp_supply_chain_impact_supports_backfill_verify_slices_total` | counter | `outcome` | Verification slices. Each slice checks one bounded key interval (at most 20,000 supports and 20,000 live findings) in one snapshot. `advance` (nothing found, the cursor moved to the end of the interval), `orphan` (a support of a tombstoned fact was found and that scope and generation's orphans removed; the slice runs again), `gap` (a live finding without a support restarted the sweep), `last` (the clean slice at the end of the key space ran the completion statement) and `failed` (the slice or the statement after it returned an error, a statement timeout among them; the step fails and the slice runs again). The `advance` count since the last `gap` is how far the verification got. |
| `eshu_dp_supply_chain_impact_supports_backfill_verify_slice_duration_seconds` | histogram | `outcome` | Wall time of one verification slice statement. The tail is the longest snapshot the verification holds open (QA, 11.6M live findings: 4.6 to 6.0 s cold, 20.3 s for a slice that starts inside the largest scope and generation). A `failed` sample is a slice that hit an error, often a statement timeout; a slice cut short by shutdown records nothing. |
| `eshu_dp_supply_chain_impact_supports_dirty_scopes` | gauge | none | Scopes the old-writer fence marked dirty that the backfill has not drained, capped at 1000. Non-zero holds the table not ready. A count that never falls means a writer without the maintainer is still running. |
| `eshu_dp_supply_chain_impact_supports_backfill_last_success_timestamp_seconds` | gauge | none | Unix time of the last backfill step that finished without error. A value that stops moving while the gauge below reads 0 means the loop is stuck. |
| `eshu_dp_supply_chain_impact_supports_backfill_ready` | gauge | none | 1 once the backfill finished and verified at the current contract version and the fence is empty, else 0. A reader of the table refuses a grant-scoped call while it is 0. |

No label carries a scope, repository, canonical key or CVE.

One sample of the two drain histograms is one drain for the security-alert reconciliation
loader and one whole `GetSupplyChainAdvisoriesForRepos` call for the service-catalog loader,
which drains each repository id on its own cursor and records the total of the call (raw rows,
pages, keys, wall time) once. So a sample is what one reducer intent read, and the return
triggers of the hot-key ruling read straight off it: a call past 1,000,000 raw rows, or a
duration approaching half the domain's handler timeout.

A drain that reads 100,000 raw rows or more, or that fails after reading any (the handler timeout case), also logs one structured line,
`event_name=supply_chain_impact_supports.large_drain`, with `reader`, `sample` (`drain`, or `call` for a whole service-catalog call), `outcome` (`served` or `failed`), `keys`, `pages`,
`raw_rows`, `active_rows` and `duration_ms`, so a hot key is findable from logs
without a dashboard.

## Not-ready refusals

A reducer reader that finds the table not ready returns one typed refusal, failure class
`supply_chain_impact_supports_not_ready`. The queue treats the class as non-counting: the
intent is deferred about every 60 seconds, its attempt count stays where it was, and it never
dead-letters because of it. It is not parked under a wait kind, so the retry timer is the only
wake. Every reader checks readiness before it writes anything, and the impact handler asks at entry,
before its writer runs. A refusal in a later read of the same pass, after the writer committed,
defers the pass; the writer is idempotent, so the retry rewrites the same rows.

An install that upgrades across migration 338 and these readers therefore pauses impact
passes, security-alert reconciliation, service-catalog correlation and the Go-modules fence
until the backfill completes. On QA the window is zero because the deploy waits for the
supports marker.

Alert on `eshu_dp_supply_chain_impact_supports_backfill_ready == 0` while
`eshu_dp_supply_chain_impact_supports_backfill_last_success_timestamp_seconds` is more than 30
minutes old: the backfill stopped, and every deferred intent is waiting on it. There is no
elapsed bound and no dead letter on this class, so the alert is the only signal that the wait
has become a fault.

## Reading them

- Right after a deploy the gauge reads 0 and `chunk` steps run about twice a
  second. The gauge reaches 1 with one `complete` step. `idle` steps then
  appear every 30 seconds, one marker read and one fence check each.
- During a rolling upgrade, reducers of the previous release still write finding
  facts without the maintainer. Each such write marks its scope dirty (the dirty
  gauge rises) and the table reads not ready; the backfill drains the marks
  (`drain` steps) and the gauge returns to 1 once the old reducers are gone.
- The marker is set only after a verification found no live finding fact
  without a support and no support of a tombstoned fact. The verification is
  resumable: each step checks one bounded slice, and the end of the last clean
  slice is stored in the marker row, so a restarted reducer continues from it.
  Setting the marker drops that cursor, so a later re-verification starts over.
  A `verify_gap` step moves both cursors back to the start; the next sweep fills
  the gap before the marker is set. At QA scale a whole verification is about
  582 slices of 4.6 to 6.0 s each (45 to 58 minutes in all), where the two
  unbounded statements it replaced took up to 22.9 and 45.2 minutes each in one
  snapshot.
- The readiness predicate is `SupplyChainImpactSupportsReady`: the marker row
  `supply_chain_impact_finding_supports_v<version>` has `completed_at` set, no
  marker for a higher version exists (a newer binary ran the sweep), and the
  dirty-scope table is empty. A missing table reads as an error, and a reader
  treats it as not ready.
- The backfill takes each scope and generation's conflict-domain advisory lock
  (the impact writer's lock) for one chunk of at most 500 supports, so a writer
  waits for at most one chunk.

## Least-privilege logins

Migration 338 creates the tables and grants nothing. A database that uses
least-privilege writer logins must grant the reducer login, after the schema
migration that creates the tables and before the new reducer writes impact
findings:

- `SELECT`, `INSERT`, `UPDATE` and `DELETE` on
  `supply_chain_impact_finding_supports`, which the impact writer upserts and
  retracts and the backfill fills.
- `SELECT`, `INSERT` and `UPDATE` on
  `supply_chain_impact_finding_supports_backfill`, the marker and cursor table.
- `SELECT` and `DELETE` on `supply_chain_impact_finding_supports_dirty`, which
  the backfill drain reads and clears.

None of the three tables has a sequence. Until the first grant exists, the new
reducer's impact writes fail with `permission denied for table
supply_chain_impact_finding_supports` (SQLSTATE 42501), and each pass rolls back
and retries.

The old-writer fence trigger needs no grant for any login. Its function runs as
its owner (`SECURITY DEFINER`, with a search_path pinned to `pg_catalog`, its own
schema and `pg_temp`, and PUBLIC execute revoked), the same form as the winners
capture functions. A reducer of the previous release that writes finding facts
through a least-privilege login still marks its scope dirty during a rolling
upgrade, with no privilege on the dirty table. This is proven by live tests
against a login that holds `INSERT` and `UPDATE` on `fact_records` and nothing on
the dirty table. It is not proven on a deployed least-privilege database.
