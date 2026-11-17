# NVD baseline pacing: bounded whole-corpus seed (#10728, criterion 2)

Status: **Draft**. This record approves no code, migration, contract change,
deployment, or QA enable. It answers criterion 2 of
[the #10728 ruling](../arbiter/10728-baseline-ruling-2026-11-16.md): the
pacing design for the bounded one-time baseline. Criterion 1 (whether to hold
the corpus) is settled YES by that ruling. Parent transport design:
[10671-nvd-feed-transport-fallback.md](10671-nvd-feed-transport-fallback.md).
Measurements:
[10671-nvd-feed-measurements.md](../evidence/10671-nvd-feed-measurements.md).

Pace and resume (revised under #10816 to the arbiter's pace ruling of
2026-11-16,
[10728-baseline-pace-ruling-2026-11-16.md](../arbiter/10728-baseline-pace-ruling-2026-11-16.md)):
record-atomic 16 MiB chunks resumed by (stamp, CVE ID), newest year first, a
drain-gated adaptive pace, and a seed rollback refused at commit. The whole
baseline takes about 35 hours at the adaptive floor and 8.3 days at the
unchanged pace, with 2026 and 2025 in about 8 hours (section 2). These rules are
ruled design with owning issues (collector #199, eshu #10809, #10810, #10732 and
the pace item), not shipped code. Baseline completion is still not a pre-enable
condition: enabling does not wait for the whole corpus, and the lane drains
behind the trip line.

Refs #10728. Part of #9209.

## Hard constraints (from the ruling, decisions 2-4, as amended)

- Baseline claims **never arm or advance** the `nvd/_window` cursor CAS.
- Per-chunk caps: window claims keep **10,000 facts / 2 MiB**; a baseline chunk
  is record-atomic, **16 MiB at most**, and paged (section 2).
- Same `nvd_envelope` and stable keys as the window path.
- Deterministic year order: newest first (section 1).
- Resumable checkpoint **distinct** from `nvd/_window`.
- QA trip line: kill switch off on **>15k NVD-proxy dirty rows/h in 2
  consecutive hourly samples** or **oldest dirty >96h**; re-enable <48h, 6
  hourly samples; #10706 p95 <= 15s / watermark <= 60s stay soak bars. No
  pausing or serializing OSV/GHSA.
- Pacing: 30 minutes a chunk by default; the drain-gated adaptive pace of
  section 4 may go faster only while the trip-latch samples stay quiet, never
  below the configured `min_chunk_interval`, which is 30 minutes when blank.

## 1. Year order

Newest first (ruled 2026-11-16, decision 7): the configured `last_year`
(required) down to 2002, one pass, no revisits. The 25 year files span 2002 to
2026 (measurements M3, `10671-nvd-feed-measurements.md`, section "M3. Year-feed
footprint and generation times"). Each year is an independent lane: one chunk in
flight per year, so per-year order is preserved; a year whose last claim did not
advance waits 30 minutes and does not block other years; `max_in_flight_years`
defaults to 1, with a ceiling of 4, and rises above 1 only after the canary
shows cycle time and inflow headroom. Year files share no CVE id (B2 of the
superseded measurements), so concurrent years touch disjoint rows. The resume
position is per year (section 3), so no global monotone stamp is needed across
years. Newest first puts 2026 and 2025 (32.1% of records, 19.6% of facts) in
about 8 hours at the adaptive floor; "most queried years" is a proxy from record
share, because no query data exists.

Within a year, records emit in ascending (`lastModified` floored to the UTC
millisecond, canonical CVE ID by byte order). This mirrors the satellite's
emitted-prefix order, ascending `lastModified` with CVE ID tiebreak
(`nvdWindowSplitOrder` in `nvd_window_split.go`, in
`eshu-collector-vulnerability-intelligence`), so the baseline chunker and the
window-path splitter agree on order. They differ on cut points: the window path
cuts at whole `lastModified` groups, and the baseline cuts between records
(section 2).

Year files are rewritten only when a record changes (M4,
`10671-nvd-feed-measurements.md`, section "M4. Do year files regenerate on
change?"); the baseline streams them and never holds the 2.7 GiB decompressed
set whole (parent design section 5, `10671-nvd-feed-transport-fallback.md`, the
"Year-feed footprint" row of the Limits table).

## 2. Chunk boundaries and the measured corpus

Chunk (ruled 2026-11-16 in
[10728-baseline-pace-ruling-2026-11-16.md](../arbiter/10728-baseline-pace-ruling-2026-11-16.md),
decisions 1 to 3; work items K1 and C1, not shipped). A baseline chunk is the
longest prefix of records, in (`lastModified` floored to the UTC millisecond,
canonical CVE ID by byte order) order from the resume position (section 3), that
fits the chunk budget. The budget is collector instance config
`nvd_baseline_chunk_budget_bytes`: 16 MiB default and **16 MiB ceiling**
(16,777,216 bytes), range 2,097,152 to 16,777,216, and out of range is
`invalid_config`. The byte measure is marshalled size plus one separator byte
per fact (`nvdFactBudgetBytes` in `nvd_output_budget.go`). A cut may fall inside
an instant, a lone record over the budget is emitted alone, and caps cut but
never refuse, so `nvd_feed_group_oversize` leaves the baseline path (this
replaces ADR-0004 item 2 for the baseline only). The window path stays
group-atomic at 10,000 facts and 2 MiB.

The whole chunk is one paged generation: 5 pages at the default page bound, and
the collector clamps the effective budget to 48 x the page bound so a lowered
`result_page_bound_bytes` cannot cross the host's 64-page ceiling. The
10,000-fact and 2 MiB limits are collector constants (`nvd_output_budget.go`),
not host limits: the host enforces a per-call stdout limit
(`stdout_limit_bytes`, default 4 MiB) and at most 64 pages a claim, and neither
the host nor the SDK validator caps fact count.

Why 16 MiB and not 32 MiB (coordinator adjustment, 2026-11-16). The arbiter
ruled a 32 MiB ceiling from retained heap. The measured collector child RSS
against the 256 MiB budget the collector repo tests is 109 MiB at 8 MiB, 184 MiB
at 16 MiB, and 279 MiB (first page) and 319 MiB (continuation) at 32 MiB
([collector issue
#203](https://github.com/eshu-hq/eshu-collector-vulnerability-intelligence/issues/203)).
The pace is set by the interval, so a chunk above 16 MiB buys no speed. Raising
the ceiling waits for the memory work in #203.

Measured corpus. This replaces the earlier extrapolation (about 3.01M facts and
about 151 hours, from the Modified-feed mean of M5) and its first correction
under #10811 (1,256 budget bytes a fact and about 63 days, which differs from
the measured figure only by id lengths). The tables, the byte breakdown and the
limits are in M11 of
[10671-nvd-feed-measurements.md](../evidence/10671-nvd-feed-measurements.md).

| Quantity | Measured |
|---|---|
| Records and facts | 425,782 records, 5,039,862 facts (about 11.8 a record) |
| Budget bytes a fact (`nvdFactBudgetBytes`) | 1,270.9 |
| Chunks at the shipped caps (2 MiB, 10,000 facts, group-atomic) | 3,124, plus 2 groups refused for ever; 3,124 x 30 min = 65 days |
| Chunks at 16 MiB, record-atomic | 397 (mean 12,695 facts, max 14,855 facts, max 2,917 records) |
| The two stalling groups | 2025 file, `2026-10-18T21:10:00.120`: 1,402 records, 17,478 facts; 2026 file, `2026-10-26T21:10:00.170`: 1,581 records, 14,156 facts |

Method: a scratch test ran the shipped `scanNVDFeed`, `nvdFeedRecordStamp`,
`NVDRecordEnvelopes` and `nvdFactBudgetBytes` over the 25 cached year files
(2026-11-15), and chunk counts cut the (stamp, CVE ID) ordered record list at
each budget.

Time to each milestone, by pace (ruling section (a); method in M11):

| Milestone | Unchanged 30 min | Open-loop 15 min | Adaptive, 5 min floor |
|---|---|---|---|
| 2026 + 2025 (79 chunks) | 39.5 h | 19.8 h | 8.4 h |
| 2026 to 2023 (139 chunks) | 69.5 h | 34.8 h | 13.4 h |
| All 25 years (397 chunks) | 8.3 days | 4.1 days | 34.9 h |

The target is the adaptive column: the whole baseline in under 36 hours and 2026
and 2025 in under 9 hours. The 15-minute column is about 50,800 facts an hour,
under the 60,000 an hour the trip line supports (15,000 dirty rows an hour at
the one measured ratio of 0.25 rows a fact, from 80 planted facts in
[10729-baseline-disposable-proof.md](../evidence/10729-baseline-disposable-proof.md)
section 2). The adaptive column assumes each chunk's collect, commit, projection
and reducer pass finishes inside 5 minutes, which is not measured; if it is
longer, the lane is cycle-bound, not unsafe.

## 3. Resumable checkpoint, distinct from `nvd/_window`

The baseline checkpoint is keyed `(baseline_run_id, year)`; its resume position
is `(resume_unix_micro, resume_cve_id)` beside `year_sha256` (ruled: migration
300 adds `resume_cve_id`, `year_generated_unix_micro`, `last_scope_id`,
`last_generation_id`, `records_done` and `records_total`), in its own store (new
table or keyspace; implementation lane chooses, following the
`NVDWindowCursorStore` pattern (read query `nvdWindowCursorReadQuery` in
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

Resume rule: after a crash, the next baseline claim re-reads the checkpoint and
restarts at the recorded `(year, resume_unix_micro, resume_cve_id)`, an inclusive
lower bound, not a pointer to a record. The scope id is
`vuln-intel://nvd/_baseline/<run>/<year>/<start_unix_us>` when the id is empty
and `.../<start_unix_us>/<from_cve_id>` otherwise, so one position has exactly
one scope id (an empty id is never a trailing segment). The canonical id is
trimmed, upper-cased, `[A-Z0-9-]{1,64}`; a record id outside that is
`nvd_feed_decode`. The compare-and-advance is a row comparison on
(`resume_unix_micro`, `resume_cve_id`). Committed prefix records replay
idempotently under stable keys (section 6); uncommitted records were never
checkpointed, so they re-emit.

### 3.1 The `advance_to` contract (`nvd_baseline_advance_to_unix_us`)

The satellite stamps one `nvd_baseline_advance_to_unix_us` per chunk beside
`nvd_baseline_year_sha256`, `nvd_baseline_year_complete`, and
`nvd_baseline_should_advance`. The ruled design adds
`nvd_baseline_advance_to_cve_id` (string, conditional, read by core) and
`nvd_baseline_year_generated_unix_us` (int64, always, read by core). The host
commit policy (`NVDBaselineCommitPolicy`) decides from them; the chunker in the
collector must produce them to this contract (the shared vector lands in
eshu-sdk first, ruling section (c)):

- **Meaning.** `advance_to` names the **first record the chunk did not emit**,
  as (`lastModified` floored to a whole UTC millisecond, as Unix microseconds,
  and canonical CVE ID). It is not "the last committed record". The next plan
  starts at it, and the planner's start is an inclusive lower bound, so a record
  is never skipped and never doubled. A chunk may end inside an instant, so a
  same-stamp group can drain across chunks.
- **Precision.** Whole UTC milliseconds, the same form as the scope id and the
  claim's `ModifiedStart`. The host floors a finer value
  (`NormalizeNVDWindowTime`) before it compares or stores it, so a stamp with
  sub-millisecond digits (collector#123) cannot make the stored resume, the
  planned start, and the scope id disagree. Flooring only re-reads, which
  replays idempotently.
- **Progress.** An incomplete verdict (`year_complete` false) must move
  **strictly forward** in (stamp, CVE ID) order. A verdict that does not is held
  as `not_covered`: the facts commit, the checkpoint stays, and the planner
  re-mints the same scope on every pace interval. There is no single-group
  refusal: a lone record over the budget is emitted alone, so a chunk always has
  a first record to move past.
- **Completion.** A complete verdict (`year_complete` true) may equal its start.
  The last chunk of a year can end exactly at the stream tail with
  `complete=false`; the next chunk resumes there, finds nothing left, and stamps
  `complete=true` with `advance_to` equal to its own start. That verdict must
  land `year_complete` or the lane stalls on the year. `year_complete` is sticky
  in the store. No verdict may move behind the chunk start; (T, "") orders
  before (T, id).

### 3.2 Seed rotation: forward resumes, rollback refuses at commit

The refresher re-pins year files daily and a year takes days to replay, so the
seed rotates mid-year. The plan-time digest-equality hold (the "seed stale"
hold) is removed rather than amended: the coordinator cannot evaluate it,
because it has no path to the seed manifest (the volume is read-write-once
beside the extension host, ADR-0006) and the SDK claim carries only the scope.
The ruled replacement is a refusal at commit (ruling decision 6):

- The collector stamps `nvd_baseline_year_generated_unix_us`, the manifest's
  `lastModified` for the year, beside the sha.
- The checkpoint writer, inside the commit transaction, first runs a guarded
  `UPDATE ... WHERE year_generated_unix_micro > $new OR (= $new AND year_sha256
  <> $sha)`. One row affected returns an error that rolls back the whole
  generation (`generation_state.go`), with class `nvd_baseline_seed_rollback`.
- A forward rotation resumes. A record's stamp only moves later, so no record
  can move from ahead of the position to behind it. A record changed after a
  chunk read it is the window path's job (ruling decision 4 of #10728).
- The design holds no pin, so it agrees with the V_Y rule: every claim reads the
  current pin and `nvd_feed_year_stale` works unchanged. #10746 loses its
  seed-manifest input; years come from config (`last_year`, required; first year
  2002).

## 4. Pace and planner-cadence interaction

The window planner is pure planning math with no I/O and no locks (per the
planner doc comment in
`go/internal/coordinator/vulnerability/nvd_window_planner.go`): one bounded
window per call, windows tile, next start is the committed cursor
(`planNVDWindow` in that file). The baseline lane mirrors this shape with its
own planner over the checkpoint instead of the cursor: one chunk per call for
each year, and the next start is the checkpointed (stamp, CVE ID) position
(section 3).

The pace below is ruled design (2026-11-16,
[10728-baseline-pace-ruling-2026-11-16.md](../arbiter/10728-baseline-pace-ruling-2026-11-16.md),
decision 8; work item C2), not shipped code. It replaces the fixed 30-minute
cadence and the "one chunk per planner run" bound.

- **Compiled default.** 30 minutes is `nvdBaselineDefaultPaceInterval` in the
  pure planner, budgeted as 10,000 facts per 30 minutes against the dirty-row
  trip line (`10671-nvd-feed-transport-fallback.md`, the "Windows per planner
  run" row of the Limits table). It is not a coordinator cadence (reconcile
  defaults to 30 s) and it reads no queue depth.
- **Drain gate.** A year's next chunk is minted when the trip latch is shut, the
  year's previous chunk has drained, and the pace interval has passed. Drained
  means its work item is closed, its generation is active, and its
  `supply_chain_impact` reducer item is done, read by key from the checkpoint's
  `last_scope_id` and `last_generation_id`. This is enforced in the baseline
  loop, not assumed from the tick, because cadence is per-target config
  (`refresh_interval`, field of `vulnerabilityTargetConfiguration` in
  `go/internal/coordinator/vulnerability/intelligence_scheduler.go`; targets
  planned inside their interval skip in `selectDueVulnerabilityTargets` in
  `intelligence_refresh_cadence.go`).
- **Adaptive interval.** The interval is `max(floor, 30 min / 2^streak)`.
  `streak` counts consecutive hourly trip-latch samples with CVE-keyed inflow at
  or under 7,500 an hour and oldest dirty age at or under 96 hours; any other
  sample resets it to 0. Migration 301 adds `soft_quiet_streak` to the latch row
  (section 5, "Durable latch contract"). The rate at most doubles once an hour
  and only after an hour at or under half the trip rate, so if inflow scales
  with rate one step cannot cross the trip rate; a hot hour resets the pace to
  30 minutes before the two-hour latch can open.
- **The floor is `min_chunk_interval`.** Blank means 30 minutes, so the pace
  stays as it is today until an operator lowers it. The compiled minimum for the
  knob drops from 30 to 5 minutes. This supersedes the earlier clamp that paced
  a lower value at 30 minutes (eshu-hq/eshu#10762).
- **Slow start.** The chunks an hour go 2, 4 and 8 over the first three hours,
  then 12 an hour: 3 h + (397 - 14) / 12 = 34.9 h for the corpus.
- **No blanket fast pace.** A fixed 5-minute interval with no feedback is
  rejected: the planted worst case is 152,000 facts an hour, about 38,000 dirty
  rows an hour, over the trip rate. Only measured inflow justifies that speed.
  The downstream cycle (collect, commit, projection, reducer inside 5 minutes)
  is unmeasured, so the default stays 30 minutes.
- **One chunk of a year at a time.** The next start is unknown until the
  previous chunk commits.

Lease fencing covers the overrun case: a feed attempt that outlives its lease is
fenced by the claim-row lock and the `fencing_token` filter (parent design
section 7, `10671-nvd-feed-transport-fallback.md`, "7. Claims, queue, and
cutover back"), and the commit-transaction writer model means a fenced claim's
state write rolls back with its generation (the `CommitExecutor` interface in
`go/internal/collector/generation_state.go`).

The window stream is unaffected: it keeps planning from the cursor
(`deriveNVDWindowTarget` in `nvd_window_derivation.go`, one deterministic scope
per tick), and cutover back after catch-up needs no flag (parent design section
7).

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

The latch row, its guarded write, the fail-closed load, the gap and replay
handling, the clock bound, the loop owner, the contract #10746 must keep and the
rollback story are in
[10728-baseline-trip-latch-contract.md](10728-baseline-trip-latch-contract.md),
moved there unchanged under #10816 to keep this record under the Markdown line
cap. The adaptive pace in section 4 adds `soft_quiet_streak` to that row
(migration 301).


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

- Folded facts per record by year. The corpus totals and byte breakdown are
  measured (section 2 and M11), and the largest record is CVE-2021-3942 at
  6,983,745 bytes.
- Dirty-log rows per committed NVD fact in QA. One measurement exists, 0.25 rows
  a fact on 80 planted facts
  ([10729-baseline-disposable-proof.md](../evidence/10729-baseline-disposable-proof.md)
  section 2); the live ratio is unmeasured (M6,
  `10671-nvd-feed-measurements.md`, section "M6. The dirty log and who feeds
  it", first paragraph).
- Chunk cycle time (collect, commit, projection, reducer) under 5 minutes,
  commit time for a 12,700-fact generation, and the evidence-truncation ratio
  for a 2,900-record scope: settled by the QA canary (eshu#10817).
- Collector child RSS at the 16 MiB ceiling beyond collector issue #203's
  measurement, and at 24 and 32 MiB after that issue's memory work.
- Per-year record counts (M3 measures sizes only).
- Whether the satellite runtime on the QA path carries the intended
  `nvd_feed_transport` value (M10 caveat, `10671-nvd-feed-measurements.md`,
  section "NOT_CHECKED", fifth bullet).

## Implementation lanes (not this record)

Merge order from the ruling: S1, then C1, then K1; C2 after C1; #10746 after C2.

- S1, eshu-sdk: the verdict-key vector with the position cases (ruling section
  (c)).
- C1, core (eshu #10809, #10810, #10732): scope grammar, planner and checkpoint
  with the resume position and the seed-rollback refusal, migration 300.
- K1, collector #199: the record-atomic chunker, paged chunk, chunk budget
  config, verdict stamps.
- C2, core pace: the drain gate, the adaptive interval, migration 301,
  telemetry.
- #10746: the enable lane over the above, without a seed-manifest input.
- Proof gates per the ruling: disposable-DB run, R6-style idempotency, the "how
  much of NVD held" count query inside a 15s statement timeout, and the QA
  canary (eshu#10817).
