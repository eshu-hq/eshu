# Supply-Chain Impact Supports

The per-support table `supply_chain_impact_finding_supports` holds one row per
live finding (#10880 PR-B1). The impact writer is its only writer: it upserts the
support rows from the rows it holds in memory and retracts superseded ones with a
`DELETE ... RETURNING` of the table, in one transaction, and writes no finding fact
row (#10880 PR-C2 h, i and j). A reducer-owned backfill fills it from the frozen
finding facts of installs that wrote them before. An old-writer fence marks any
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
| `eshu_dp_supply_chain_impact_supports_reads_total` | counter | `source`, `outcome` | Reducer reads of the supports table, one series per reader. `source` is one of `security_alert_reconciliation`, `service_vulnerability_advisory`, `fanout_holders`, `fanout_holders_key_range`, `fanout_holder_mass`, `repository_holders`, `scoped_packages`, `out_of_stamp` or `go_modules_fence`. `outcome` is `served` (the read ran), `not_ready` (the reader refused because the table was not ready) or `failed` (the readiness probe failed, or, for `security_alert_reconciliation` and `service_vulnerability_advisory`, the supports read failed; a probe failure keeps its own classification and never takes the not-ready class; for `go_modules_fence` the predicate runs inside the fence's holder statement, so a holder-statement error, a lock or statement timeout included, also counts `failed`). For the impact fan-out sources (`fanout_holders`, `fanout_holders_key_range`, `fanout_holder_mass`, `repository_holders`) a statement error or timeout after a ready probe records nothing here. For `fanout_holders`, `fanout_holders_key_range` and `fanout_holder_mass` it shows on the fan-out read histogram (`eshu_dp_supply_chain_impact_fanout_read_duration_seconds`, outcome `read_timeout` or `read_error`); for `repository_holders` it shows on `eshu_dp_supply_chain_impact_fanout_total` (site `repository`, outcome `read_timeout` or `read_error`), because that read is not a histogram kind. Both end as the dead-letter class `supply_chain_impact_fanout_failed`. A lookup with an empty id list runs no read and records nothing, and the holder, key-range and mass reads drop blank ids first, so an all-blank mass lookup also records nothing. A rising `not_ready` count means reducer work is waiting on the backfill; it stops when `eshu_dp_supply_chain_impact_supports_backfill_ready` reads 1. |
| `eshu_dp_supply_chain_impact_stored_key_missing_total` | counter | none | Stored findings the impact writer read with no `stable_fact_key` on the support and none on its fact row. The writer reads its stored baseline from the supports table and links a finding across generations by that key; a row with neither key is treated as a changed finding for that pass. Counted when the writer transaction commits. It should be 0 once the supports written before migration 350 have been rewritten or their facts still exist; a steady non-zero rate means a writer that does not fill the key, or supports whose fact rows are gone. |
| `eshu_dp_supply_chain_impact_fact_write_stop_armed` | gauge | none | 1 while the finding-fact reject trigger on `fact_records` reads enabled, 0 while it is disabled or missing, as the reducer last read it from `pg_trigger` (at start and every 30 seconds). The supports-only impact writer defers, non-counting, while it reads 0 (counted as `not_ready` under reader `fact_write_stop`). A 0 after the roll means the trigger was disabled (the rollback switch) or never applied. |
| `eshu_dp_supply_chain_impact_supports_drain_raw_rows` | histogram | `source` | Raw supports rows one drain read (one whole call for the service-catalog reader; every row the ordered key scans returned, active or not, and the rows a failed drain read before it stopped), by reader (`source`, the same closed set as the reads counter). Raw rows far above the active rows mean the key sits mostly in superseded generations. A drain in the hundred thousands is a hot key. |
| `eshu_dp_supply_chain_impact_supports_drain_duration_seconds` | histogram | `source` | Wall time of one drain of the supports table (one whole call for the service-catalog reader), by reader. A drain near half the domain's handler timeout means a hot key is paging too slowly; the raw-rows histogram above says how large the key was. |
| `eshu_dp_supply_chain_impact_fact_write_rejected_total` | counter | none | Impact-finding fact writes the PR-C reject trigger refused (SQLSTATE 55000, constraint `fact_records_reject_supply_chain_impact_finding`), one per refused statement. It carries no label. It stays 0 until the trigger exists; after that, a rising count means a reducer pod runs an image older than the schema. The intent retries without spending an attempt (class `supply_chain_impact_fact_write_stopped`). |
| `eshu_dp_supply_chain_impact_supports_backfill_steps_total` | counter | `outcome` | Backfill steps: `chunk` (supports written or orphans removed for one scope and generation), `drain` (a scope the old-writer fence marked dirty was re-read), `verify_gap` (the verification found live findings without a support and restarted the sweep), `complete` (the step that set the readiness marker), `idle` (a step after completion with nothing to drain), `contended` (another reducer held the lock), `verify` (a verification slice that neither restarted the sweep nor set the marker: it moved the cursor, removed an orphan pair, or was a last slice the fence held back) and `failed`. A rising `verify_gap` means supports went missing after the sweep passed them. |
| `eshu_dp_supply_chain_impact_supports_backfill_step_duration_seconds` | histogram | `outcome` | Wall time of one backfill step. A step holds one scope-and-generation advisory lock for at most one chunk of 500 supports, so the tail is the longest a writer can wait on the backfill. |
| `eshu_dp_supply_chain_impact_supports_backfill_verify_slices_total` | counter | `outcome` | Verification slices. Each slice checks one bounded key interval (at most 20,000 supports and 20,000 live findings) in one snapshot. `advance` (nothing found, the cursor moved to the end of the interval), `orphan` (a support of a tombstoned fact was found and that scope and generation's orphans removed; the slice runs again), `gap` (a live finding without a support restarted the sweep), `last` (the clean slice at the end of the key space ran the completion statement) and `failed` (the slice or the statement after it returned an error, a statement timeout among them; the step fails and the slice runs again). The `advance` count since the last `gap` is how far the verification got. |
| `eshu_dp_supply_chain_impact_supports_backfill_verify_slice_duration_seconds` | histogram | `outcome` | Wall time of one verification slice statement. The tail is the longest snapshot the verification holds open (QA, 11.6M live findings: 4.6 to 6.0 s cold, 20.3 s for a slice that starts inside the largest scope and generation). A `failed` sample is a slice that hit an error, often a statement timeout; a slice cut short by shutdown records nothing. |
| `eshu_dp_supply_chain_impact_supports_dirty_scopes` | gauge | none | Scopes the old-writer fence marked dirty that the backfill has not drained, capped at 1000. Non-zero holds the table not ready. A count that never falls means a writer without the maintainer is still running. |
| `eshu_dp_supply_chain_impact_supports_backfill_last_success_timestamp_seconds` | gauge | none | Unix time of the last backfill step that finished without error. A value that stops moving while the gauge below reads 0 means the loop is stuck. |
| `eshu_dp_supply_chain_impact_stable_key_fill_steps_total` | counter | `outcome` | Stable-key fill steps (#10880 PR-C1k): `chunk`, `verify`, `gap` (a verification slice found a keyless support whose fact twin has a key and restarted the sweep), `complete`, `failed`. A chunk updates at most 500 supports of one scope generation under its conflict-domain lock. |
| `eshu_dp_supply_chain_impact_stable_key_fill_rows_total` | counter | `outcome` | Supports the fill gave a key (`filled`) or left keyless because their fact twin has no key (`skipped`). |
| `eshu_dp_supply_chain_impact_stable_key_fill_pauses_total` | counter | `reason` | Passes held back by a pause condition: `replay_lag` (a replica over the limit), `free_disk` (under the limit), `disk_unreadable` (a limit is set and no reading is wired), `index_build` (`pg_stat_progress_create_index` not empty), `probe_error` (a condition could not be read, never taken as clear). |
| `eshu_dp_supply_chain_impact_stable_key_fill_complete` | gauge | none | 1 once the fill marker is set: a clean verification found no keyless support whose twin has a key. 0 until then, and while the fill is off (the default). |
| `eshu_dp_supply_chain_impact_supports_backfill_ready` | gauge | none | 1 once the backfill finished and verified at the current contract version and the fence is empty, else 0. A reader of the table refuses a grant-scoped call while it is 0. |
| `eshu_dp_supply_chain_impact_go_evidence_recheck_aborts_total` | counter | `reason` | Impact passes aborted at the end of their write transaction (their writes roll back) because the Go evidence they loaded no longer names its go scope's active generation (`generation_changed`: a Go-modules fence retracted it or a successor activated) or the shared go-evidence lock timed out behind a fence (`lock_timeout`). Each retries without spending an attempt (class `supply_chain_impact_go_evidence_changed`) and the retry reloads the evidence. A sustained rate means fences and passes keep colliding on one repository. |
| `eshu_dp_go_fence_advisory_lock_timeouts_total` | counter | none | Go-modules fence attempts that hit the lock timeout (SQLSTATE 55P03) waiting for the exclusive go-evidence advisory lock behind an in-flight impact pass. Nothing was detached; the coordinator retries the page next tick. Three consecutive ticks timing out on the same scope returns to the owner of #11327. |
| `eshu_dp_go_fence_advisory_lock_wait_seconds` | histogram | none | How long the fence waited for the exclusive go-evidence lock, granted or timed out. A pass holds the shared lock only for its recheck, its owed marker and its commit, so the expected wait is milliseconds; a tail means a pass stalled between its recheck and its commit. The lock timeout is 2 s. |
| `eshu_dp_supply_chain_impact_write_tx_duration_seconds` | histogram | `outcome` (`committed`, `rolled_back`, `recheck_aborted`) | Wall time of one impact write transaction, begin to commit or rollback. `recheck_aborted` is a pass whose Go evidence went stale during its writes: the whole transaction was spent for nothing, so the abort cost is this histogram's `recheck_aborted` series. Buckets reach 300 s. Use the `committed` p99 to size how long a fence can wait behind a pass. |
| `eshu_dp_supply_chain_impact_write_tx_statement_duration_seconds` | histogram | `statement` (closed: `conflict_lock`, `suppression_diff`, `admission`, `stored_read`, `fact_insert`, `supports_upsert`, `winners_capture`, `retract`, `supports_delete`, `recovery_read`, `pass_state`, `go_evidence_close`, `commit`), `outcome` (`committed`, `rolled_back`) | Wall time of one statement group inside one impact write transaction. `fact_insert` is one pass-level aggregate no matter how many bulk-insert chunks the pass took; `commit` is timed on every pass. A `rolled_back` pass records the groups that ran before the failure. Buckets run 0.5 ms to 300 s. Use the `committed` `fact_insert` and `commit` series to split the `write_findings` tail. |
| `eshu_dp_supply_chain_impact_go_evidence_lock_wait_seconds` | histogram | none | How long a pass spent in the closing statements after its first shared go-evidence lock: taking the locks, granted or refused, plus its owed marker, which can wait on its work-item row. A tail means passes queue behind a fence or a stalled marker, and bounds how long a fence can wait behind the pass. Passes with no resolved Go scope record nothing. |

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

## Statement-timing overhead (#11430)

No-Regression Evidence: the per-statement timing adds one clock read per
statement group plus struct appends per pass, with no new I/O, locks,
statement text, ordering, or round trips. The baseline is the pre-PR writer
with pass-level timing only; after the change, callers without timings take
the byte-identical delegate path, and
`TestCommittedPassAccumulatesOneSamplePerStatement` proves 2500 rows over 3
chunks keep identical call counts with one `fact_insert` sample. Proof is
`go test -count=1` on `internal/telemetry` and
`internal/reducer/supplychain/core` (ok, 9.6 s and 16.0 s) and the floor's
`go test -race` on the changed packages (green), all on go1.27.4 darwin/arm64
with no live backend. No live-backend measurement was taken; the live QA
readout is pending a deployed build with this signal.

Observability Evidence: the new
`eshu_dp_supply_chain_impact_write_tx_statement_duration_seconds{statement,outcome}`
histogram is the operator signal for this change, capped at 13 statements
times 2 outcomes (26 series) with no scope, CVE, or fact-id labels. It is
recorded after the write transaction returns, so metric emission never holds
the transaction open.

## Refused fact writes

After the finding-fact write stop (#10880 PR-C), the database refuses every INSERT and UPDATE of a
`reducer_supply_chain_impact_finding` row in `fact_records` with SQLSTATE 55000 and the constraint name
`fact_records_reject_supply_chain_impact_finding`. The reducer's impact transaction turns that refusal
into a retryable error of failure class `supply_chain_impact_fact_write_stopped` and counts it on
`eshu_dp_supply_chain_impact_fact_write_rejected_total`. The queue treats the class as non-counting, like
the not-ready class below: the intent retries about every 60 seconds with its attempt count unchanged and
never dead-letters because of it, and it is not parked under a wait kind. The service reports it as a
deferral (execution status `deferred`, a WARN log), like every non-counting class, so the rejected
counter is the signal to watch: a pod that hits it runs an image older than the schema. A pod that
runs an image older than this class still dead-letters the intent, so roll old pods to zero before the
trigger is armed. A different
55000, or the trigger's constraint under another code, keeps its ordinary terminal classification.
Until the trigger exists nothing raises this error and the counter stays at 0.

## The reject trigger and its roll (#10880 PR-C2 k)

Migration 355 installs `BEFORE INSERT OR UPDATE ON fact_records FOR EACH ROW WHEN (NEW.fact_kind =
'reducer_supply_chain_impact_finding')`, which raises with SQLSTATE 55000, constraint
`fact_records_reject_supply_chain_impact_finding` and table `fact_records`. It is unconditional (no
setting bypasses it) and does not cover DELETE, so retention still removes frozen rows with their
generation. The migration takes the next free number above the C1 migrations. The runner applies any
migration whose path is missing from its ledger, so the order of numbers never blocks an apply.

`CREATE TRIGGER` takes `SHARE ROW EXCLUSIVE` on `fact_records` for the catalog write only: it waits
behind open writer transactions, blocks new ones while it is granted and held, and is bounded per attempt
by the runner's lock timeout. `SELECT` is not blocked. A re-apply takes no lock and keeps a disabled trigger
disabled (the `pg_trigger` guard). Check `pg_locks` and `pg_stat_progress_vacuum` for a running vacuum on
`fact_records` before the roll, as for any `SHARE ROW EXCLUSIVE` migration. Migration 338 took the same
lock for its fence triggers; the duration at the QA scale is not measured in this change.

Roll order: every reducer pod on the PR-C0 image or newer (it classifies the refusal), then the image that
carries this migration and the supports-only writer, with the migration applied in PreSync before the first
supports-only write. The writer defers, non-counting, while the trigger reads disabled, so a pod that
starts before the migration applies waits instead of writing. Rollback: `ALTER TABLE fact_records DISABLE
TRIGGER fact_records_reject_supply_chain_impact_finding` (the same lock level), only after every reducer
pod is on the old image; the armed gauge then reads 0 on a new pod and its passes defer.

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

The supported upgrade path is to reach supports-ready
(`eshu_dp_supply_chain_impact_supports_backfill_ready` reads 1) on a #10880 PR-B image before
rolling to the PR-C1 image, whose winners maintainer reads the supports table. On a direct
jump from an image older than migration 338, an old reducer's winners maintainer does not
wait for the marker and can stamp the winners watermark after the marker completes; the
winners audit on facts re-queues the keys it left stale, so the result converges, but only
on a later audit pass.

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
  Roll from a PR-B image that already reads ready, not straight from an image
  older than migration 338 (see the upgrade path above).
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

## What the stored `intent_id` and `cause` mean

The stored payload of a support is the impact finding's payload, which every reducer pass stamps with
its own `intent_id` and `cause`. The write-on-change guard compares the payload without those two
keys, so a pass that confirms a finding without changing it leaves the support untouched. After that,
the stored `intent_id` and `cause` of a support name the pass that last changed the row, not the pass
that last confirmed it. The pass that last confirmed it is the work item: the reducer uses the
intent's id as the work item id, and its time is `last_pass_at` on the scope's pass-state row. The
API and MCP never serve either key. A replay that changes nothing shows up as the `unchanged` action
of the supports rows counter instead of `updated`.

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
- `SELECT` and `INSERT` on `supply_chain_impact_retracted_finding_keys`
  (migration 351), which the writer's two retraction statements read for the keys
  they already recorded and insert into, and the recovery reads select from. Rows
  leave it through the generation foreign key cascade, which the owner runs, so the
  login needs neither `UPDATE` nor `DELETE`.

A pass that loaded Go evidence also rechecks, at the end of its write transaction,
that each go scope it read is still active (#11327). That recheck is a plain `SELECT`
on `ingestion_scopes` and `scope_generations`, which the reducer login already needs
for every claim; a login that lacks either fails the recheck with `permission denied`
(SQLSTATE 42501), and the pass rolls back and retries. The advisory lock functions
need no grant (PUBLIC execute).

None of the four tables has a sequence. Since PR-C2 the impact writer inserts and
updates no `fact_records` row of the finding kind, so its login needs no write
privilege on `fact_records` for its own statements; it still reads the table for the
recovery transition leg, and other writers of other kinds keep their grants. Until the first grant exists, the new
reducer's impact writes fail with `permission denied for table
supply_chain_impact_finding_supports` (SQLSTATE 42501), and each pass rolls back
and retries.

The scope-delete and scanner capture functions (migrations 352 and 353) run as their
owner (`SECURITY DEFINER`, a pinned search_path, PUBLIC execute revoked), like the
fence trigger below: they read the supports table as the owner and the login needs no
grant for them. Without the new table's grant the writer's retraction fails with
`permission denied for table supply_chain_impact_retracted_finding_keys` (SQLSTATE
42501).

The old-writer fence trigger needs no grant for any login. Its function runs as
its owner (`SECURITY DEFINER`, with a search_path pinned to `pg_catalog`, its own
schema and `pg_temp`, and PUBLIC execute revoked), the same form as the winners
capture functions. A reducer of the previous release that writes finding facts
through a least-privilege login still marks its scope dirty during a rolling
upgrade, with no privilege on the dirty table. This is proven by live tests
against a login that holds `INSERT` and `UPDATE` on `fact_records` and nothing on
the dirty table. It is not proven on a deployed least-privilege database.

## Stable-key fill

The fill (#10880 PR-C1k) copies the fact twin's `stable_fact_key` into the supports no
rewrite has keyed, so the writer's stored read can leave `fact_records`. It is off unless
`ESHU_SUPPLY_CHAIN_IMPACT_STABLE_KEY_FILL_ENABLED=true`, because it must not run inside a
counted window or before the lab run. One elected reducer runs it, in chunks of at most 500
supports, paced to `ESHU_SUPPLY_CHAIN_IMPACT_STABLE_KEY_FILL_WAL_BYTES_PER_HOUR` (default
8 GB) using an estimate of `..._WAL_BYTES_PER_ROW` per filled row. It pauses on replica
replay lag, free disk and a running index build (see `pauses_total`). Free disk is not
readable through SQL, so with the default limit and no reading wired it pauses with
`disk_unreadable`; set `..._MIN_FREE_DISK_BYTES=0` only when disk is watched elsewhere.
The replay-lag read needs the `pg_monitor` role. Read the fill with the `complete` gauge, the
`rows_total{outcome="filled"}` rate against the WAL budget, and `pauses_total` by reason.
