# NVD baseline pacing: bounded whole-corpus seed (#10728, criterion 2)

Status: **Draft**. This record approves no code, migration, contract change,
deployment, or QA enable. It answers criterion 2 of
[the #10728 ruling](../arbiter/10728-baseline-ruling-2026-11-16.md): the
pacing design for the bounded one-time baseline. Criterion 1 (whether to hold
the corpus) is settled YES by that ruling. Parent transport design:
[10671-nvd-feed-transport-fallback.md](10671-nvd-feed-transport-fallback.md).
Measurements:
[10671-nvd-feed-measurements.md](../evidence/10671-nvd-feed-measurements.md).

Completion time (corrected under #10811): about 63 days at one chunk per 30
minutes, not the 151 hours first stated (section 2, "Scale check"). Baseline
completion takes weeks at the current cadence and is not a pre-enable condition:
enabling does not wait for the whole corpus, and the lane drains behind the trip
line. Pacing (per-chunk cap and cadence) is the lever, not correctness.

Refs #10728. Part of #9209.

## Hard constraints (from the ruling, decisions 2-4)

- Baseline claims **never arm or advance** the `nvd/_window` cursor CAS.
- Per-chunk caps: **10,000 facts / 2 MiB**, same as window claims.
- Same `nvd_envelope` and stable keys as the window path.
- Deterministic year order.
- Resumable checkpoint **distinct** from `nvd/_window`.
- QA trip line: kill switch off on **>15k NVD-proxy dirty rows/h in 2
  consecutive hourly samples** or **oldest dirty >96h**; re-enable <48h, 6
  hourly samples; #10706 p95 <= 15s / watermark <= 60s stay soak bars. No
  pausing or serializing OSV/GHSA.
- Pacing: no faster than **one chunk per planner run** until disposable-DB
  proof justifies otherwise.

## 1. Year order

Ascending, 2002 through 2026, one pass, no revisits. The 25 year files span
2002 to 2026 (measurements M3,
`10671-nvd-feed-measurements.md`, section "M3. Year-feed footprint and
generation times"). Ascending order is the only order
that keeps the resume stamp monotone (section 3): the checkpoint
`(year, lastModified)` advances lexicographically, so a crash replays from a
prefix that is always older than uncommitted work.

Within a year, records emit in ascending `(lastModified, cveId)` order. This
mirrors the satellite's emitted-prefix order, ascending `lastModified` with
CVE ID tiebreak (`nvdWindowSplitOrder` in `nvd_window_split.go`, in
`eshu-collector-vulnerability-intelligence`), so the baseline chunker and the
window-path splitter agree on what a "group" is.

Year files are rewritten only when a record changes (M4,
`10671-nvd-feed-measurements.md`, section "M4. Do year files regenerate on
change?"); the baseline streams them and never holds the 2.7 GiB decompressed
set whole (parent design section 5, `10671-nvd-feed-transport-fallback.md`,
the "Year-feed footprint" row of the Limits table).

## 2. Chunk boundaries within caps

Each baseline chunk is a prefix of the remaining ordered stream cut at whole
`lastModified`-group boundaries such that the chunk holds at most 10,000
facts and 2 MiB. The caps are the existing satellite constants
`nvdMaxOutputFacts = 10000` and `nvdMaxOutputBytes = 2 << 20`
(the `nvdMaxOutputFacts` and `nvdMaxOutputBytes` constants in
`nvd_output_budget.go`); the byte measure is marshalled size plus one
separator byte per fact (`nvdFactBudgetBytes` in `nvd_output_budget.go`), so the baseline
chunker and the window path agree on what fits.

Group atomicity follows the window path: groups are maximal runs of equal
stamps in emitted-prefix order (`nvdWindowGroupEnd` in
`nvd_window_split.go`), the prefix accumulates whole groups ascending
(`selectNVDWindowPrefix` in `nvd_window_split.go`), and a window whose first
group alone exceeds the caps is detected, not split
(`nvdFeedWindowSlice.firstGroupOversize` in `nvd_feed_slice.go`,
`errNVDFeedGroupOversize` in `nvd_feed_errors.go`).

Dispositions at a boundary:

- Prefix fits, remainder exists: commit the prefix; the remainder replans
  from the next group stamp (same rule as the window path, parent design
  section 3, `10671-nvd-feed-transport-fallback.md`, "3. Overflow", first
  paragraph).
- First group exceeds the caps: the chunk routes to the API walk with class
  `nvd_feed_group_oversize` (`nvdFeedClassGroupOversize` in
  `nvd_feed_selector.go`, handled in `collectNVDFeed` in
  `nvd_feed_collect.go`), exactly as the window path does (parent
  design, `10671-nvd-feed-transport-fallback.md`, "3. Overflow", second
  paragraph). It never loops
  silently and never sheds. The Modified-feed sample suggested this route was
  unreachable for fact count (densest single record 2,048 facts, M5,
  `10671-nvd-feed-measurements.md`). The year files disprove it for groups:
  1,402 records at one stamp fold to 17,478 facts in the 2025 file and 1,581 to
  14,156 in the 2026 file, so the lane cannot leave 2025 until a group drains
  across chunks (collector #199, eshu #10809; defect 1 of
  [the 2026-11-16 ruling](../arbiter/10671-collector-118-107-ruling-2026-11-16.md)).

Scale check (corrected under #10811). This section first extrapolated 425,770
records at 7.07 facts per record (M5) to about 3.01M facts, about 301 claims at
the 10,000-fact cap, and about 151 hours at one chunk per 30-minute planner run.
Two inputs were wrong: the mean came from a Modified-feed sample, and the byte
cap binds before the fact cap. The arbiter measured the real figures (defect 4 of
[the 2026-11-16 ruling](../arbiter/10671-collector-118-107-ruling-2026-11-16.md)):

| Quantity | Old figure | Measured figure |
|---|---|---|
| Facts in the corpus | about 3.01M | 5,039,862 |
| Budget bytes per fact (`nvdFactBudgetBytes`) | not measured | 1,256 |
| Facts per chunk at the binding cap | 10,000 (fact cap) | about 1,670 (2 MiB byte cap) |
| Chunks (claims) | about 301 | about 3,000 |
| Wall time at one chunk per 30 minutes | about 151 hours | about 63 days |

Method: fold the cached year files with the satellite's folding, count the
facts, then divide by the byte-cap chunk size. The byte cap is 2 MiB, so a chunk
holds about 2,097,152 / 1,256 = 1,669 facts, below the 10,000-fact cap. Then
5,039,862 / 1,670 is about 3,018 chunks, and 3,018 x 30 minutes is about
1,509 hours, or about 63 days. The 5,039,862 count over the 425,782 year-file
records is about 11.8 facts per record, which is why the Modified-feed mean of
7.07 understated the load.

The figure moves with its inputs. A larger per-chunk byte cap or a shorter
cadence shortens it in proportion, and a different folding changes the fact count
and the bytes per fact. The cadence floor is the 30-minute planner run (section
4), so shortening the lane takes a higher per-chunk cap or a faster cadence under
its own proof. Neither is part of this record.

## 3. Resumable checkpoint, distinct from `nvd/_window`

The baseline checkpoint is `(baseline_run_id, year, resume_last_modified,
year_sha256)` in its own store (new table or keyspace; implementation lane
chooses, following the `NVDWindowCursorStore` pattern (read query
`nvdWindowCursorReadQuery` in
`go/internal/storage/postgres/vulnerability/nvd_window_cursor.go`).
It is **not** a row in `vulnerability_nvd_window_cursors` (DDL
`vulnerabilityNVDWindowCursorSchemaSQL` in that file, migration
`275_vulnerability_nvd_window_cursor.sql`, its `CREATE TABLE`), and
baseline claims **never** carry a `GenerationStateWriter` for that table.

The mechanism that enforces this is already in the commit path: the host
arms the cursor writer only for window scopes
(`Source.armNVDWindowCursorWriter` with the `IsNVDWindowScope` check in
`go/internal/collector/extensionhost/nvd_window_cursor.go`) and only when
the generation's snapshot verdict says should-advance
(the `nvdWindowShouldAdvancePayloadKey` check in `armNVDWindowCursorWriter`
in the same file). Baseline claims use a
scope family **outside** the `nvd/_window/` prefix (prefix constant
`nvdWindowScopePrefix` in `go/internal/vulnsource/nvd_window_scope.go`),
so the arming function returns nil for them by construction, and the CAS
query (`NVDWindowCursorAdvanceQuery`) never sees a baseline candidate. A wrong
scope prefix is therefore a fail-closed shape error (the scope parser
rejects non-window ids in `ParseNVDWindowScope` in
`go/internal/vulnsource/nvd_window_scope.go`), not a cursor
corruption.

Resume rule: after a crash, the next baseline claim re-reads the checkpoint
and restarts at the recorded `(year, resume_last_modified)` group stamp.
Committed prefix groups replay idempotently under stable keys (section 6);
uncommitted groups were never checkpointed, so they re-emit whole.

### 3.1 The `advance_to` contract (`nvd_baseline_advance_to_unix_us`)

The satellite stamps one `nvd_baseline_advance_to_unix_us` per chunk beside
`nvd_baseline_year_sha256`, `nvd_baseline_year_complete`, and
`nvd_baseline_should_advance`. The host commit policy
(`NVDBaselineCommitPolicy`) decides from it; the chunker in the collector
(collector#100) must produce it to this contract:

- **Meaning.** `advance_to` is the **exclusive next-group stamp**: the
  `lastModified` of the first group the chunk did **not** emit, as Unix
  microseconds of a whole UTC millisecond. It is not "the last committed
  stamp". The next plan starts at it, and the planner's start is inclusive,
  so a group is never skipped and never split. A group is every record that
  shares one displayed-millisecond `lastModified`; the chunker closes a
  chunk only on a group boundary.
- **Precision.** Whole UTC milliseconds, the same form as the scope id and
  the claim's `ModifiedStart`. The host floors a finer value
  (`NormalizeNVDWindowTime`) before it compares or stores it, so a stamp with
  sub-millisecond digits (collector#123 reads one millisecond back and may
  surface them) cannot make the stored resume, the planned start, and the
  scope id disagree. Flooring only re-reads, which replays idempotently.
- **Progress.** An incomplete verdict (`year_complete` false) must move
  **strictly past** the chunk start. A verdict that does not is held as
  `not_covered`: the facts commit, the checkpoint stays, and the planner
  re-mints the same scope on every pace interval. A chunker that cannot
  move past its start (a single group larger than the per-claim caps) must
  fail loudly instead of stamping a non-advance.
- **Completion.** A complete verdict (`year_complete` true) is accepted at
  `advance_to >= chunk start`. The last chunk of a year can end exactly at
  the stream tail with `complete=false`; the next chunk resumes there, finds
  nothing left, and has no later stamp to name, so it stamps
  `complete=true` with `advance_to` equal to its own start. That verdict
  must land `year_complete` or the lane stalls on the year. For a complete
  verdict any `advance_to >= chunk start` is accepted, and the resume is
  unused once the year is complete. `year_complete` is sticky in the store. No verdict, complete or not, may move behind the
  chunk start.

## 4. Planner-cadence interaction

The window planner is pure planning math with no I/O and no locks
(per the planner doc comment in
`go/internal/coordinator/vulnerability/nvd_window_planner.go`): one
bounded window per call, windows tile, next start is the committed cursor
(`planNVDWindow` in that file). The baseline lane mirrors this shape with
its own planner over the checkpoint instead of the cursor: one chunk per
call, next start is the checkpointed group stamp, at most one chunk per
planner run (ruling decision 2), which the parent design budgets as at most
10,000 facts per 30 minutes (`10671-nvd-feed-transport-fallback.md`, the
"Windows per planner run" row of the Limits table). The 2 MiB byte cap binds
first on the measured corpus, at about 1,670 facts per chunk (section 2). The pace knob clamps at
that floor: a configured `min_chunk_interval` below 30 minutes paces at 30
minutes instead of speeding the lane, so the knob can only slow the lane,
never remove the bound (eshu-hq/eshu#10762).

Cadence is per-target config (`refresh_interval`, field of
`vulnerabilityTargetConfiguration` in
`go/internal/coordinator/vulnerability/intelligence_scheduler.go`;
targets planned inside their interval skip in
`selectDueVulnerabilityTargets` in `intelligence_refresh_cadence.go`, called from
`PlanVulnerabilityIntelligenceWork` in `intelligence_scheduler.go`), so the one-chunk-per-run bound must be
enforced **in the baseline loop** (skip while a chunk for the current stamp
is already claimed or in flight), not assumed from the tick. Lease fencing
covers the overrun case: a feed attempt that outlives its lease is fenced by
the claim-row lock and the `fencing_token` filter (parent design section 7,
`10671-nvd-feed-transport-fallback.md`, "7. Claims, queue, and cutover back"),
and the commit-transaction
writer model means a fenced claim's state write rolls back with its
generation (the `CommitExecutor` interface in `go/internal/collector/generation_state.go`).

The window stream is unaffected: it keeps planning from the cursor
(`deriveNVDWindowTarget` in `nvd_window_derivation.go`, one deterministic scope per tick), and
cutover back after catch-up needs no flag (parent design section 7).

No-Regression Evidence: the #10762 commit-policy change removes the
baseline pre-check from `applyCommitPolicy`, so the wired-or-builtin
baseline policy owns the armed-verdict refusal (with its refused outcome
and WARN) instead of a silent early return; non-baseline generations do
one fewer scope-prefix comparison. Measured on linux/arm64 with
`BenchmarkApplyCommitPolicyNonBaselineCost` and
`BenchmarkCollectedGenerationArmingCost` (1M ops x3, base c01c2e3cec vs
this change): non-window arming 389-392 ns/op base vs 382-389 ns/op after,
commit policy 400-403 ns/op base vs 395-398 ns/op after, identical 736
B/op and 4 allocs/op (944 B/op, 6 allocs/op on the advancing-window arm)
throughout — the removed check is inside run-to-run noise with no
allocation change.

Observability Evidence: the refused path now emits
`eshu_dp_nvd_baseline_advance_total{outcome="refused"}` with the WARN log
`nvd baseline generation refused by commit policy, nothing committed`
through the production commit path, as the telemetry-coverage collectors
row for #10728 promises; the config-error hold rides the
`vulnerability_intelligence` plan span (exception event plus closed
`baseline_config_error` hold reason), never a label.

## 5. Trip-line wiring and operator flip

The QA baseline run obeys the same governed trip line as the window-path
enable (parent design section 8,
`10671-nvd-feed-transport-fallback.md`, "8. Rollout and the QA-enable gate"):

- Off (>15k NVD-proxy dirty rows/h in 2 consecutive hourly samples, or
  oldest dirty row >96h): the operator sets `nvd_feed_transport` to off.
  The key is the satellite instance-config switch
  (`nvdFeedTransportConfigKey` in `nvd_feed_config.go`); absent or false keeps
  the API-only transport and is the kill switch (`parseNVDFeedTransport` in
  `nvd_feed_config.go`, `SourceConfig.NVDFeedTransport` in `runtime.go`). "NVD-proxy rows" means dirty-log rows whose
  `advisory_id` starts with `CVE-` (parent design,
  `10671-nvd-feed-transport-fallback.md`, section 8, "NVD-proxy dirty rows"
  paragraph); OSV also carries CVE ids,
  so the proxy over-counts and trips early, which is the safe direction.
- The 96h age trip is a **page, not an automatic flip**: the OSV/GHSA
  backlog is already ~58h old at measurement (M6,
  `10671-nvd-feed-measurements.md`, section "M6. The dirty log and who feeds
  it"), so the operator compares against the pre-enable baseline before
  acting (parent design, `10671-nvd-feed-transport-fallback.md`, section 8,
  same paragraph).
- Re-enable under 48h: the trip latch refuses a close until 6 consecutive
  hourly samples with oldest dirty <48h have followed the trip opening
  (opening the trip resets the count, and an hour at or past 48h withdraws
  eligibility; `NVDBaselineTripState.Close`, #10760). `Close` returns
  cleared, refused, or nothing-to-close, so the #10746 caller records
  `closed` only for a cleared trip and `close_refused` only for a refused
  one. The `close_refused` recorder is registered today but has no
  production caller until #10746 wires it, so an early attempt will be
  counted as `close_refused` from that lane on. A quiet hour is an hourly
  sample with oldest-dirty age under 48h, as the ruling words it; the rate
  is not part of the test. After re-enabling, keep recording hourly
  samples against the soak bars. The #10706 bars (p95 <= 15s /
  watermark <= 60s) stay soak bars.

No pausing or serializing OSV/GHSA to make room (ruling decision 3).

Known limits to close before the baseline enable lane (#10746) turns on.
These come from the #10760 review findings F8 and F10. The follow-ups are
tracked as #10767 (inflow measure, F8, closed below) and #10768 (durable
latch, F10), and both must land before #10746 turns the lane on. F10 is
closed in the store and fold by #10768 (below). #10746 wires the recorders
and the plan-time latch load into the service.

- Inflow, not backlog (F8), closed by #10767. The sampler reads hourly
  inflow: the CVE-% rows still queued plus the CVE-% rows the winners
  maintainer already drained, which its two DELETE statements add to
  `supply_chain_impact_winners_dirty_tally` by capture minute in the same
  statement (migration 299). The ruled definition holds unchanged (CVE-%
  fact rows, over-counting on purpose). NVD `kind = 'scope'` rows, which
  carry a NULL `advisory_id` by migration 272's shape constraint, are
  counted one per row by scope family in a separate
  `NVDScopeRowsPerHour` field and are not folded into the 15k comparison
  (decided in #10767, with the reasoning and what would reopen it in
  `docs/internal/evidence/10767-trip-inflow.md`; weighting by fan-out stays a
  separate ruling). A raw sequence high-water
  delta was rejected: it counts every source, and the measured all-source
  inflow (M6) is 60k to 109k rows an hour against the 15k threshold.
- Durable latch (F10, #10768). Done in the store and fold; the service
  wiring is still #10746. See "Durable latch contract" below. `Observe`
  itself counts samples, not elapsed time (the sample timestamp is not
  read), so a missed or duplicate hourly sample would count as a
  consecutive hour; the fold closes that with the gap and replay handling
  described below.
- Recorders. The lane wires the trip, plan, latch, and dirty-probe recorders,
  including `close_refused` from a refused `Close`, each with a test.

### Durable latch contract (#10768)

The trip latch is a row per baseline run in
`vulnerability_nvd_baseline_trip_latches` (migration 298): every field of
`NVDBaselineTripState`, `sampled_at` (the last sample folded in), and a
`revision` that counts applied writes. Code: `NVDBaselineTripLatchStore` in
`go/internal/storage/postgres/vulnerability/nvd_baseline_trip_latch.go` and
the fold in `go/internal/coordinator/vulnerability/nvd_baseline_trip_latch.go`
(`LoadNVDBaselineTripLatch`, `ObserveNVDBaselineTrip`,
`CloseNVDBaselineTrip`).

- Guarded write. One statement per write, never read-then-write. The update
  applies only when `revision` still equals the value the writer loaded and
  `sampled_at` is not older than the stored one; zero rows affected is a
  skipped stale write, not an error. The first write is an
  `INSERT ... ON CONFLICT DO NOTHING` for a writer that saw no row. A
  `sampled_at` compare alone is not enough: the latch is a read-modify-write
  fold, so a writer that loaded state at T and folds the T+2h sample would
  overwrite another writer's T+1h fold, and an hourly sample could resurrect
  a trip an operator `Close` just cleared (Close records no sample). The
  revision closes both; the time predicate refuses a replayed older sample
  even when the revision matches.
- Fail closed. `LoadNVDBaselineTripLatch` returns `Open` for an unreadable
  store, a nil store, a blank run id, and a run with no row yet. A missing row
  holds planning until the first hourly sample writes it, so a restart that
  lost the row never reads as "never tripped". A run that already committed
  baseline chunks (non-empty `ReadNVDBaselineCheckpoints`) whose row is
  missing folds its first sample onto an open trip that needs six fresh quiet
  hours, never onto a clean slate. The error return and the open verdict are
  both set, so a caller that drops the error still holds.
- Gaps and replays. Samples stand for hours. A sample more than 90 minutes
  after the stored one (`NVDBaselineTripMaxSampleGap`: one hour plus 30
  minutes of tick jitter) is a missed hour: the hot streak and the quiet
  streak restart and re-enable eligibility is withdrawn before the sample is
  folded, and an open trip stays open. A sample less than 45 minutes after the
  stored one (`NVDBaselineTripMinSampleSpacing`) or at or before it is
  refused without a write, so a re-sample after a restart cannot score one
  hour twice toward re-enable. The thresholds are this change's decision, not
  the ruling's; they are constants.
- Clock bound. A sample time more than `NVDBaselineTripMaxSampleSkew` (2
  minutes) past the `now` passed to `ObserveNVDBaselineTrip` is refused with
  the `future_sample` outcome, an error, an open verdict, and no store read or
  write. Without the bound, one future-dated sample is persisted as
  `sampled_at` and every later real sample reads as stale until real time
  catches up, so the detector goes deaf. The bound is only as good as
  the independence of `now`: it is normally the database clock, and stamping
  the sample from that same clock is also safe, but a replica that stamps the
  sample and passes its own local clock as `now` would pass its own check
  after a clock step. A zero `now` is an error. A row already poisoned (stored sample
  time past `now` plus the allowance) is reported as `stored_ahead` with an
  error and no write instead of answering stale forever; the recovery is to
  delete the row, which re-folds onto an open trip through `runStarted` and so
  needs six fresh quiet hours.
- Who owns the hourly loop. The coordinator process that plans baseline
  chunks (the vulnerability-intelligence scheduler lane, wired by #10746)
  owns the hourly sample-and-save loop: it already holds the database and the
  planner, so the load and the fold sit in one process. The window collector
  and the commit path never write the latch. Interleaving does not need a lease:
  with several coordinator replicas each running the loop, the revision guard
  lets exactly one write per revision apply, and a second replica's sample
  inside 45 minutes is refused as too soon. Clock steps are a separate
  hazard the revision guard does not cover; the clock bound above covers them
  only when `now` is the database clock. A lease or single-flight remains an
  optimization against duplicate probe work (one aggregate query per hour),
  and #10746 may add one.
- Contract #10746 must keep. (1) Load the latch at the start of every plan
  tick, not once per process, and set the whole `NVDBaselineTripLoad` on
  `IntelligencePlanRequest.NVDBaselineTrip`; any error holds. A request that
  never carries a load has the zero value, which the planner holds with the
  closed reason `baseline_trip_unloaded` (an open, absent, or unreadable
  latch holds as `baseline_trip_open`), so skipping the load or dropping its
  error fails closed instead of planning into the load that tripped. (2) Start the hourly loop
  independently of planning: a new run has no row, so planning holds until the
  first sample lands, and a loop gated on planning would never start. (3) Pass
  `runStarted` from a non-empty checkpoint read. (4) Pass the database clock (one `SELECT now()` per tick) as
  `now` (stamping the sample from that same database clock is also safe;
  a replica's local clock for both is not); record `future_sample` and
  `stored_ahead` so a skewed replica or a poisoned row is visible to an
  operator. (5) Route the operator re-enable through
  `CloseNVDBaselineTrip`; on `ErrNVDBaselineTripLatchConflict` retry. (6)
  Record `RecordNVDBaselineTripLatch` with each returned outcome and the trip
  transitions from the `Before` and `After` states of each observation. (7)
  Hold planning when `stored_ahead` is recorded. `LoadNVDBaselineTripLatch`
  takes no clock, so the planner reads a poisoned row as an ordinary loaded
  latch and `ObserveNVDBaselineTrip` is the only place the condition is seen;
  the loop must stop planning for that run until the row is deleted, or the
  lane would keep planning off a latch that can no longer count. Load's
  signature does not change.
- Conservative semantics, by design. (a) A hot sample that loses its write to
  an operator `Close` is dropped, not retried, so the next sample lands about
  two hours after the stored time, reads as a missed hour, and restarts the
  hot and quiet streaks; a hot hour can be forgotten, never invented. (b) With several replicas each
  sampling, the 45 minute minimum spacing admits samples closer than an hour
  when ticks jitter, so quiet hours toward re-enable can accrue slightly faster
  than wall-clock hours (at worst one sample per 45 minutes). The re-enable
  count is still a count of distinct sample times at least 45 minutes apart,
  and the operator `Close` stays the final decision.
- #10746 observability notes. The recorders in this change count outcomes but
  log nothing. #10746 must (a) emit a WARN log on `read_failed` and
  `write_failed` (the counter alone does not say which run or which error),
  and (b) carry the latch outcome on the plan span next to the closed hold
  reason, so an operator reading one plan span can tell an open trip from an
  unreadable latch. The hold reasons `baseline_trip_open` and
  `baseline_trip_unloaded` already ride the plan span through
  `appendNVDBaselineTarget` (`nvd_baseline_hold_reason`); only the latch
  outcome attribute is left for #10746.
- Rollback. The migration only creates a new table nothing references, so
  the schema rollback is `DROP TABLE vulnerability_nvd_baseline_trip_latches`.
  A binary rollback is safe on its own: the older binary never reads the
  table and runs with the in-memory latch as before. A newer binary that loses
  the table fails closed (every load errors, planning holds), so a premature
  drop stops the lane and corrupts nothing. The migration number was
  re-checked at merge time. Main topped out at 297
  (`297_vulnerability_nvd_baseline_checkpoints.sql`) when this branch took 298.
  Sibling lane #10767 added 299 and edits the same golden files; main reached
  298 when this lane merged, and #10767 was rebased over it, taking 299. The
  second lane's rebase recomputed, rather than hand-resolved,
  `goldenBootstrapDefinitionsDigest` and `goldenBootstrapDefinitionsCount` in
  `go/internal/storage/postgres/migrations/embed_invariant_test.go` (the count
  is 325 with both 298 and 299), appended to
  `migration_checksum_manifest_test.go`, and extended
  `orderedBootstrapDefinitionNamesLatest` in
  `go/internal/storage/postgres/schema_order_latest_test.go` keeping 298 before
  299.

## 6. Idempotency argument

1. Same input, same facts. Every NVD field is an identity translation with
   no contract change (parent design section 4,
   `10671-nvd-feed-transport-fallback.md`, "4. Field dispositions" table):
   feed records carry all `cpeMatch` ids, all seven `vulnStatus` states, and
   all CVSS families (M2), and 20/20 sampled records are deep-equal to the API
   (M8).
2. Same record, same keys. One record emits one `vulnerability.cve`, one
   `vulnerability.affected_product` per distinct
   `(criteria, matchCriteriaId)` identity with repeats folded, and one
   `vulnerability.reference` per distinct sanitized URL with tags unioned,
   so one stable key never carries two payload shapes inside a claim
   (`NVDRecordEnvelopes` in `nvd_envelope.go` in the collector).
3. Same chunk, same scope. Same window bounds mint the same scope id on
   every run (prefix doc and `nvdWindowScopePrefix` in
   `go/internal/vulnsource/nvd_window_scope.go`, mint in `NVDWindowScopeID`); a
   refusal, skip, or crash re-plans the same scope and the commit path
   fences it as a same-scope successor (`deriveNVDWindowTarget` in
   `go/internal/coordinator/vulnerability/nvd_window_derivation.go`).
   The baseline lane must mint its chunk scopes with the same determinism
   (bounds in, same id out) so replays converge instead of superseding.
4. Same scope, one row set. State writes run inside the generation's own
   commit transaction (the `CommitExecutor` interface in
   `go/internal/collector/generation_state.go`),
   so cursor/checkpoint state advances with its facts or not at all; the
   cursor CAS keeps the max on strictly-greater candidates only
   (`NVDWindowCursorAdvanceQuery`).
5. Proof required before QA: the R6-style convergence test (two baseline
   passes = one row set) from the ruling's "Required proof" section, plus
   facts/record under satellite folding and dirty-log rows per committed
   fact on a disposable DB (same section).

## NOT_CHECKED (must be measured before any QA baseline run)

- Per-fact byte size spread and folded facts per record by year. The corpus
  totals are measured (section 2, "Scale check": 5,039,862 facts at 1,256
  budget bytes each); the per-record maximum in bytes is not.
- Dirty-log rows per committed NVD fact (unmeasured; M6,
  `10671-nvd-feed-measurements.md`, section "M6. The dirty log and who feeds
  it", first paragraph).
- Per-year record counts (M3 measures sizes only).
- Whether the satellite runtime on the QA path carries the intended
  `nvd_feed_transport` value (M10 caveat,
  `10671-nvd-feed-measurements.md`, section "NOT_CHECKED", fifth bullet).

## Implementation lanes (not this record)

- Satellite: baseline chunker over the year-set seed store (collector#100
  seed + manifest + guard), chunk-scope minting, checkpoint write.
- Core: baseline checkpoint store (section 3 shape), baseline commit policy
  (refuse any baseline generation that arms a window-cursor writer),
  trip-line counters and telemetry.
- Proof gates per the ruling: disposable-DB run, R6-style idempotency, the
  "how much of NVD held" count query inside a 15s statement timeout.
