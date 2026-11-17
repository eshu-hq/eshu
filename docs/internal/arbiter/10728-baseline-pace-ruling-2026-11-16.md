# Arbiter ruling: NVD baseline pace, sub-instant resume, and seed rotation (2026-11-16)

Recorded by the coordinator; the arbiter does not edit files. Arbiter:
`arbiter-eshu`. Goal: #10671. Origin: defects 1, 2 and 4 of [the #118 and #107
ruling](10671-collector-118-107-ruling-2026-11-16.md). Earlier baseline ruling:
[10728-baseline-ruling-2026-11-16.md](10728-baseline-ruling-2026-11-16.md).
Design: [10728-baseline-pacing.md](../design/10728-baseline-pacing.md).
Measurement tables:
[10671-nvd-feed-measurements.md](../evidence/10671-nvd-feed-measurements.md)
(M11). The arbiter verified collector origin/main `a4f7b31` and Eshu origin/main
`bd7fc98` and edited nothing in either repository. Measurements ran on a `git
archive` copy of the collector in a local scratch directory that is not
committed. Collector paths below name the file only; they live in
`eshu-hq/eshu-collector-vulnerability-intelligence`. Environment identifiers,
including local scratch paths, are redacted per #7803, and source line numbers
the arbiter cited are dropped because they drift.

Ruling (the arbiter's headline): make baseline chunks record-atomic
and 16 MiB (paged), resume by (stamp, CVE ID), order years newest first, and
replace the fixed 30-minute pace with a slow-start pace driven by the existing
trip-latch samples. That takes the baseline from about 65 days to 8.3 days at
the unchanged pace and to about 35 hours at the adaptive floor, with 2026 and
2025 covered in about 8 hours.

## Coordinator adjustment (2026-11-16): the chunk ceiling is 16 MiB, not 32 MiB

The ruling's decision 3 named a 32 MiB ceiling and a config range up to
33,554,432. The coordinator lowers it. **The baseline chunk ceiling is 16 MiB
(16,777,216 bytes). The config range for `nvd_baseline_chunk_budget_bytes` is
2,097,152 to 16,777,216, and the default stays 16 MiB.** Out of range is
`invalid_config`, as ruled.

Why. The ruling measured retained Go heap, not process RSS, and listed RSS as
unverified (section (e)). The RSS was then measured by the coordinator and is
recorded in collector issue
[eshu-hq/eshu-collector-vulnerability-intelligence#203](https://github.com/eshu-hq/eshu-collector-vulnerability-intelligence/issues/203):
the child process reads its own peak RSS on a 553 MB synthetic year file with
the real handler, one process per call (the host's model).

| Chunk budget | Collector child peak RSS | Against the 256 MiB budget |
|---|---|---|
| 8 MiB | 109 MiB | under |
| 16 MiB | 184 MiB | under |
| 32 MiB, first page | 279 MiB | over by 23 MiB |
| 32 MiB, continuation | 319 MiB | over by 63 MiB |

The 256 MiB budget is the one the collector repo tests against (item 7 of
section (a) cites it). Live heap peaks near 122 MiB at 32 MiB; most of the RSS
is GC headroom and churn. Issue #203 names two contributors: `freshnessHint`
holds a string copy of the whole chunk plus one marshaled buffer, and the
`nvdFeedRecordStamp` decoder allocates about 1.6 GB while scanning.

What does not change. The 397-chunk, 34.9 hour and 8.3 day figures in section
(a) were computed at 16 MiB (item 3 and item 9 of section (a)), so the ceiling
costs no speed. The rate, not the chunk size, sets the duration at any pace
above the floor (decision 3), so a chunk above 16 MiB buys nothing. The
203-chunk row at 32 MiB in item 3 is no longer reachable. The 64 MiB rejection
stands.

Where this section governs. It replaces the 32 MiB ceiling in decision 3, in the
K1 test list (the child-process RSS test runs at the 16 MiB ceiling), and in the
RSS row of section (e). The follow-up memory work is collector issue #203:
stream `freshnessHint` with byte-identical output, cut the stamp-scan decoder
churn, re-measure at 24 and 32 MiB, and raise the ceiling only then, after
reading the deployed memory limit. A `GOMEMLIMIT` in the collector is the
alternative that issue names.

The adjustment rests on the figures in #203, which this repository cannot
reproduce; the RSS row of section (e) records that.

## (a) Target and measured basis

| Milestone | Unchanged 30 min pace | Open-loop 15 min | Adaptive, 5 min floor |
|---|---|---|---|
| 2026 + 2025 (79 chunks; 32.1% of records, 19.6% of facts) | 39.5 h | 19.8 h | 8.4 h |
| 2026 to 2023 (139 chunks; 48.8% of records, 34.4% of facts) | 69.5 h | 34.8 h | 13.4 h |
| All 25 years (397 chunks) | 8.3 days | 4.1 days | 34.9 h |

The target is the right-hand column: **full baseline in under 36 hours, 2026 and
2025 in under 9 hours**. The left column needs no new ingest claim beyond the
bigger chunk. The middle column is `min_chunk_interval: 15m`: about 50,800 facts
an hour, under the 60,000 that the trip line supports even in the planted worst
case (item 8). The right column assumes each chunk's collect, commit, projection
and reducer pass finishes inside 5 minutes; that cycle time is not measured (see
(e)), and if it is longer the lane is cycle-bound, not unsafe.

Every figure and its method:

1. **Corpus.** 425,782 records, 5,039,862 facts (425,782 cve, 3,247,201
   affected_product, 1,366,879 reference), 6,404,997,673 budget bytes, 1,270.9
   bytes a fact. Method: a scratch test in the archive copy ran the shipped
   `scanNVDFeed`, `nvdFeedRecordStamp`, `NVDRecordEnvelopes` and
   `nvdFactBudgetBytes` over the 25 cached year files (2026-11-15), with scope
   `vuln-intel://nvd/_baseline/qa-baseline-2026-11/<year>/<us>`, a 64-character
   generation id, and the public year-feed source URI. The earlier 1,256 bytes a
   fact differs only by id lengths.
2. **Today's pace reproduced.** Shipped group-atomic caps (2 MiB, 10,000 facts)
   give 3,124 chunks plus 2 groups refused forever; 3,124 x 30 min = 65 days.
   The byte cap binds at 2,097,152 / 1,270.9 = 1,650 facts.
3. **Chunks by budget, record-atomic, cut in (stamp, CVE ID) order** (a records
   table and a chunking script in the scratch directory): 2 MiB 3,180; 8 MiB
   787; 16 MiB 397 (mean 12,695 facts, max 14,855 facts, max 2,917 records); 32
   MiB 203 (max 29,550 facts, 5,213 records); 64 MiB 108.
4. **The two stall groups exist in the cached files**: 2025 file,
   `2026-10-18T21:10:00.120`, 1,402 records, 17,478 facts, 22,945,411 bytes;
   2026 file, `2026-10-26T21:10:00.170`, 1,581 records, 14,156 facts, 18,691,477
   bytes. They are the only groups over 10,000 facts. 73 groups exceed 2 MiB, 2
   exceed 16 MiB, none exceeds 32 MiB. Largest single record: CVE-2021-3942,
   5,485 facts, 6,983,745 bytes.
5. **What dominates 1,271 bytes**: payload 614.2 (48%), `source_ref` 444.5 (35%:
   generation_id 83, scope_id 82, fact_key 78, uri 72, record_id 69.5,
   source_system 45), stable_key 80, kind 37, observed_at 37, source_confidence
   31, schema_version 25. Sampled every 25th fact. By kind: cve 2,166 bytes a
   fact, affected_product 1,248, reference 1,047.
6. **Collection is not the bottleneck.** Folding all 25 files takes 163 s of
   CPU, streaming them (gunzip, sha256, scan) 28.3 s; 2026 alone is 27.4 s fold,
   4.4 s stream (darwin/arm64 laptop, go1.26.11). A 32 MiB chunk of 24,922 facts
   folds in 0.40 s and splits into 9 pages in 0.14 s.
7. **Collector memory.** Retained Go heap is 1.50 x budget bytes: 24.1 MiB at 16
   MiB, 48.0 at 32, 96.0 at 64 (a scratch memory test). The existing child RSS
   budget is 256 MiB (the collector's RSS test, `nvd_feed_rss_test.go`). This is
   heap, not RSS; the RSS figures are in the adjustment above.
8. **Trip-line arithmetic.** The trip opens above 15,000 CVE-keyed dirty rows an
   hour for two hours (`nvd_baseline_tripline.go`). The only measured dirty-row
   ratio is 0.25 rows a fact with every CVE planted to yield a finding
   ([10729-baseline-disposable-proof.md](../evidence/10729-baseline-disposable-proof.md)
   section 2, n = 80 facts). 15,000 / 0.25 = 60,000 facts an hour, or 84 hours
   for the corpus, at any chunk size. Today's lane runs at about 3,300 facts an
   hour, 18 times under that.
9. **Pace arithmetic.** Slow-start runs 2, 4, then 8 chunks in the first three
   hours, then 12 an hour: 3 h + (397 - 14) / 12 = 34.9 h.

## (b) Chosen design and rejected alternatives

### Decisions

1. **The 10,000-fact and 2 MiB limits are collector constants, not host
   limits.** The host enforces a per-call stdout limit (`stdout_limit_bytes`,
   default 4 MiB, in `extensionhost/process.go`, operator config in
   `cmd/collector-component-extension/config.go`) and at most 64 pages a claim
   (`pagination.go`; `MaxResultPages` has no operator wiring). Neither the host
   nor the SDK validator caps fact count. One claim can therefore carry 64 x
   4,128,768 = 264 MB of facts today. The constants live in
   `nvd_output_budget.go`.
2. **Baseline chunks become record-atomic.** A chunk is the longest prefix of
   records, in (lastModified floored to the UTC millisecond, canonical CVE ID by
   byte order), from the resume position that fits the chunk budget. A cut may
   fall inside an instant. A lone record over the budget is emitted alone. Caps
   cut; they never refuse. `nvd_feed_group_oversize` leaves the baseline path.
   This replaces ADR-0004 item 2 for the baseline only.
3. **Chunk budget: 16 MiB default, 16 MiB ceiling** (the arbiter ruled a 32 MiB
   ceiling; see the coordinator adjustment above), as collector instance config
   `nvd_baseline_chunk_budget_bytes` (range 2,097,152 to 16,777,216; out of
   range is `invalid_config`). The whole chunk is one paged generation: 5 pages
   at the default page bound. The collector clamps the effective budget to 48 x
   the page bound so a lowered `result_page_bound_bytes` cannot cross the
   64-page ceiling. Why 16 and not 32 as the default: at any pace above the
   floor the rate, not the chunk size, sets the duration, and 16 MiB keeps a
   generation at 14,855 facts and 2,917 records, and both real groups (22.9 MB,
   18.7 MB) drain across two chunks, so the new resume path is proven on real
   data. 64 MiB is rejected: 96 MiB retained heap leaves no margin in the 256
   MiB RSS budget.
4. **Resume position is (start_unix_us, from_cve_id)**, an inclusive lower
   bound, not a pointer to a record. Scope id:
   `vuln-intel://nvd/_baseline/<run>/<year>/<start_unix_us>` when the id is
   empty, and `.../<start_unix_us>/<from_cve_id>` otherwise. One position has
   exactly one scope id: an empty id is never written as a trailing segment.
   Canonical id is trimmed, upper-cased, `[A-Z0-9-]{1,64}`; a record id outside
   that is `nvd_feed_decode`, as a blank id is today. `advance_to` names the
   first record not emitted.
5. **Checkpoint stays keyed (run, year).** Migration 300 adds `resume_cve_id
   TEXT COLLATE "C" NOT NULL DEFAULT ''`, `year_generated_unix_micro BIGINT NOT
   NULL DEFAULT 0`, `last_scope_id`, `last_generation_id`, `records_done`,
   `records_total`. The compare-and-advance becomes a row comparison on
   (resume_unix_micro, resume_cve_id). An incomplete verdict must move strictly
   forward in that order; a complete one may equal its start.
6. **Seed rotation: forward resumes, rollback refuses at commit.** The plan-time
   digest equality hold (in `nvd_baseline_planner.go`) is deleted. It can never
   be evaluated: the coordinator has no path to the seed manifest (the volume is
   read-write-once beside the extension host, ADR-0006), and the SDK claim
   carries only the scope (`collector/types.go` in eshu-sdk). Instead the
   collector stamps `nvd_baseline_year_generated_unix_us` (the manifest's
   `lastModified` for the year) beside the sha, and the checkpoint writer,
   inside the commit transaction, first runs a guarded `UPDATE ... WHERE
   year_generated_unix_micro > $new OR (= $new AND year_sha256 <> $sha)`; one
   row affected returns an error, which rolls back the whole generation
   (`generation_state.go`), with class `nvd_baseline_seed_rollback`. Forward is
   safe because a record's stamp only moves later, so no record can move from
   ahead of the position to behind it; a record changed after a chunk read it is
   the window path's job, as ruling decision 4 of #10728 already says. This
   holds no pin, so it agrees with the V_Y rule: every claim reads the current
   pin and `nvd_feed_year_stale` works unchanged. #10746 loses its seed-manifest
   input; years come from config (`last_year`, required; first year 2002).
7. **Years run newest first, each year an independent lane.** One chunk in
   flight per year, so per-year order is preserved. A year whose last claim did
   not advance waits 30 minutes and does not block other years.
   `max_in_flight_years` defaults to 1, ceiling 4. Year files share no CVE id
   (B2 of the superseded measurements), so concurrent years touch disjoint rows.
8. **Pace.** The 30 minutes is a compiled interval in the pure planner
   (`nvdBaselineDefaultPaceInterval`), taken from "one chunk per planner run"
   and budgeted as 10,000 facts per 30 minutes against the dirty-row trip line.
   It is not a coordinator cadence (reconcile defaults to 30 s) and reads no
   queue depth. New rule: a year's next chunk is minted when the trip latch is
   shut, the year's previous chunk has drained (its work item is closed, its
   generation is active, and its `supply_chain_impact` reducer item is done,
   read by key from `last_scope_id` and `last_generation_id`), and the pace
   interval has passed. The interval is `max(floor, 30 min / 2^streak)`, where
   `streak` counts consecutive hourly latch samples with CVE-keyed inflow at or
   under 7,500 an hour and oldest dirty age at or under 96 hours; any other
   sample resets it to
   0. Migration 301 adds `soft_quiet_streak` to the latch row. Here `floor` is
      the configured `min_chunk_interval`: blank means 30 minutes, so the pace
      stays as it is today until an operator lowers it, and the compiled minimum
      for that knob drops from 30 to 5 minutes. The rate at most doubles once an
      hour and only after an hour at or under half the trip rate, so if inflow
      scales with rate one step cannot cross the trip rate; a hot hour resets
      the pace to 30 minutes before the two-hour latch can open.
9. **PR #10804 does not change the baseline's pace.** Its 44 files touch no
   `nvd_baseline_*` file, no pace constant, no chunk cap and no reconcile
   interval (checked in the diff). It terminals an item whose attempt budget is
   spent, caps one retry delay at 1 hour, and expires runs open over 24 hours.
   That is liveness: a baseline claim that keeps refusing goes terminal after 10
   attempts, its run closes, and the planner re-mints. Merge it before enable
   for that reason.

### Rejected

| Option | Evidence for rejection |
|---|---|
| Wire compaction (hoist per-result constants) | About 375 of 1,271 bytes are constant per result, so 1.4x at best, against 8x from paging. It is a new SDK protocol for every collector, and bytes per claim are not binding once a chunk pages. |
| Raising `stdout_limit_bytes` as the mechanism | It is deployed config whose QA value nobody has read. Paging carries 16 MiB in 5 calls at the default. Raising it stays an optional optimization. |
| Bulk core-side loader reading the seed | Collection is 163 s of CPU for the whole corpus; a loader still pays the same fact upsert, projection, reducer and dirty-log path. It would need core to import the collector fold (ADR-0002), a second mount of an RWO volume, and a writer outside claim fencing. |
| Dropping facts from the baseline | All three kinds are first-party translations; none is derived. References (27%) are read by the query layer (the reducer's `AGENTS.md`); deferring them splits a record across scopes. Product de-duplication (3,247,201 to 555,444 distinct pairs) changes a stable key and payload shape, a major contract change. |
| Plan builder, chunk-indexed scopes, pin hold | Superseded; the pin hold contradicts V_Y (ruling on #118 and #107). |
| Keeping group atomicity with a bigger cap | Both real groups fit under 32 MiB, but any larger same-stamp group would stall the year again. |
| Fixed faster interval with no feedback | At the 5-minute floor the planted worst case is 152,000 facts an hour, about 38,000 dirty rows an hour, over the trip rate. Only the measured inflow justifies that speed. |
| Several chunks of one year at once | The next start is unknown until the previous chunk commits. |
| A window-lag gate on the baseline | The window cursor is about 30 days behind and blocked at the API edge (#10791); the baseline would never start. |

### Interaction with #197 and #198

- **#197**: its step zero is answered by decision 1. The window cursor is an
  instant with no id column, so the window path stays group-atomic; the two
  groups are 6 and 5 pages and can ride the existing lone-group paged path once
  its fact cap is lifted. The arbiter does not rule that change. To keep the
  diffs apart, the baseline gets its own record-atomic slice type and does not
  edit `nvdFeedWindowSlice.trim`.
- **#198**: no shared state beyond the manifest. It reads `verified_at` and
  `lastModified` per year; reuse one accessor. It shortens the gap the baseline
  leans on for records changed after a chunk read them. The seed-scan ruling
  that settles #198 is
  [10671-window-seed-scan-ruling-2026-11-16.md](10671-window-seed-scan-ruling-2026-11-16.md).
- **#10735** (newest NVD copy on the product path) stays an enable prerequisite.

## (c) Contract changes and who changes first

1. **eshu-sdk first.**
   `factschema/fixturepack/payloads/nvd_verdict_keys_golden.json` gains keys
   `nvd_baseline_advance_to_cve_id` (string, conditional, read by core) and
   `nvd_baseline_year_generated_unix_us` (int64, always, read by core), cases
   with four-segment scopes, and a `position` block: scope grammar, round-trip
   and rejection cases, and order cases including `CVE-2025-10000` before
   `CVE-2025-9999` and (T, "") before (T, id). No tagged release is needed to
   merge: both consumers already pin untagged pseudo-versions (collector
   `go.mod`, core `go/go.mod`).
2. **Core second.** Core does not read the vector today (#10732 is open and core
   pins a commit before it). Core accepts three- and four-segment scopes and the
   optional keys, so it is safe with an old collector.
3. **Collector third**, pinned through
   `testdata/ifa/eshu-core-protocol-pin.json` to the core commit that reads the
   position. A new collector against an old core would cut inside a group that
   core cannot resume in, and stall.
4. **Host config**: none required. Enable preflight:
   `ESHU_WORKFLOW_CLAIM_PLANE_URL` unset, because that route caps a commit at 4
   MiB (`workflowv1/commit.go` in eshu-workflow). The preflight is tracked as
   [eshu-hq/eshu#10817](https://github.com/eshu-hq/eshu/issues/10817).
5. No data migration: the lane has never planned in production, because nothing
   sets the `NVDBaseline*` request fields
   (`vulnerability_intelligence_service.go`).

## (d) Work list

Merge order: S1, then C1, then K1; C2 after C1; #10746 after C2. Docs, #10736,
#10804, #197 and #198 run in parallel throughout. C1 and K1 can be developed in
parallel once S1 is up.

**S1, eshu-hq/eshu-sdk.** Vector as in (c)1 and its validation test.

**C1, eshu #10809 + #10810 + #10732.**

- Files: `go/internal/vulnsource/nvd_baseline_scope.go`;
  `go/internal/coordinator/vulnerability/nvd_baseline_planner.go` and
  `nvd_baseline_derivation.go`;
  `go/internal/collector/extensionhost/nvd_baseline_cursor.go`;
  `go/internal/storage/postgres/vulnerability/nvd_baseline_checkpoint.go`;
  migration 300 plus DDL parity, checksum manifest, schema order, and the
  retirement census.
- Tests: `TestPlanNVDBaselineResumesInsideAnInstant`,
  `TestPlanNVDBaselineResumesAcrossForwardSeedRotation`,
  `TestPlanNVDBaselineHoldsOnSeedRollback` (the planner re-mints the same
  position and the commit refuses),
  `TestParseNVDBaselineScopeRejectsNonCanonicalPosition`,
  `TestNVDBaselineCheckpointAdvanceOrdersByStampThenCVEID` (live, either commit
  order), `TestNVDBaselineCheckpointRefusesSeedRollbackInCommitTx` (live),
  `TestNVDBaselineCommitPolicyRequiresStrictPositionAdvance`,
  `TestNVDBaselineVerdictVectorReplaysThroughCommitPolicy`,
  `TestPlanNVDBaselineOrdersNewestYearFirst`,
  `TestPlanNVDBaselineSkipsYearInRetryBackoff`,
  `TestPlanNVDBaselineOneChunkPerYearInFlight`.

**K1, collector #199.**

- Files: `nvd_baseline_chunk.go`, a new record-atomic slice file,
  `nvd_baseline_scope.go`, `nvd_paging.go` and `result_pager.go` (cursor carries
  the chunk end and the probed Modified span, so a continuation re-streams one
  range and skips the 6 to 7 MB probe download), `nvd_baseline_unreadable.go`,
  config, ADR-0004 item 2, ADR-0005, a new ADR,
  `docs/reference/configuration.md`.
- Tests: `TestNVDBaselineGroupOverFactCapDrainsAcrossChunks` (both real group
  shapes, no record skipped or doubled),
  `TestNVDBaselineChunkCutsInsideAnInstantByCVEID`,
  `TestNVDBaselineLoneRecordOverBudgetEmitsAlonePaged`,
  `TestNVDBaselineChunkBudgetPagesWholeChunk`,
  `TestNVDBaselinePagedChunkSeedRotatedMidPagingEndsGenerationChanged`,
  `TestNVDBaselineChunkBudgetChangeMidRunConverges`,
  `TestNVDBaselineChunkBudgetClampedToHostPageCeiling`,
  `TestNVDBaselineUnreadableRecordHonorsSubInstantPosition`,
  `TestNVDBaselineContinuationSkipsProbe`,
  `TestNVDBaselineVerdictStampsSeedGeneration`,
  `TestNVDBaselinePositionMatchesSharedVector`,
  `TestNVDBaselineChunkWithinRSSBudgetAtMaxBudget` (child process at the 16 MiB
  ceiling, per the adjustment; the ruling wrote 32 MiB), and an env-gated corpus
  replay proving a chunked year equals its one-pass fold.

**C2, eshu pace (new issue, or under #10736).**

- Files: planner, `nvd_baseline_trip_latch.go`, migration 301, telemetry.
- Tests: `TestPlanNVDBaselineHoldsUntilPriorChunkDrained`,
  `TestPlanNVDBaselinePaceHalvesOnQuietSampleAndResetsOnHot`,
  `TestPlanNVDBaselinePaceNeverBelowFloor`,
  `TestPlanNVDBaselineBlankIntervalKeepsThirtyMinutes`,
  `TestPlanNVDBaselineBoundsInFlightYears`.

**#10746.** Map checkpoints with the new columns, per-year in-flight and drain
state, last planned per year, the latch with its streak, and config. Drop the
seed-manifest input from its acceptance.

**#10811, docs.** Section 2 of `10728-baseline-pacing.md` takes the figures in
(a) with their method; sections 1, 3 and 4 take the position, order and pace
rules.

**Telemetry.**

- Core metrics: `eshu_dp_nvd_baseline_chunk_facts` and `_chunk_records`
  (histograms), `_chunk_cycle_seconds` (plan to drained),
  `_plan_holds_total{reason}` (closed set: in_flight, projection_in_flight,
  reducer_in_flight, pace_wait, retry_wait, trip_open, trip_unloaded,
  config_error), `_pace_interval_seconds`, `_soft_inflow_rows_per_hour`,
  `_years_complete`, `_years_total`, `_records_done`, `_records_total`,
  `_eta_seconds` (remaining records over the trailing 6 hour rate, started years
  only), `_seed_rotations_total{direction}`.
- Plan span: planned count, in-flight count, hold reason, pace interval; never a
  scope id.
- Logs: one INFO line per committed chunk (run, year, records, facts, position,
  year_complete, seed rotated); WARN on rollback refusal.
- Collector stderr line: chunk records, facts, budget, pages,
  cut-inside-instant, year records done and total.
- The collector counts done and total while it streams the file, so progress is
  exact per chunk.

## Required proof

1. Unit and live tests above, red before green, plus `make pre-pr` for C1 and C2
   (queue, lease and schema classes).
2. Corpus replay (K1): the two real groups drain with no record skipped or
   doubled.
3. Canary in QA before lowering `min_chunk_interval`: four chunks at 30 minutes,
   recording commit duration (bar: under 60 s against the 300 s write timeout),
   zero `eshu_dp_supply_chain_impact_evidence_truncated_total` for baseline
   scopes with `evidence_budget_ratio` at or under 0.5, CVE-keyed inflow at or
   under 7,500 an hour, collector peak RSS at or under 256 MiB, and one replay
   of a committed chunk (the idempotent re-run case #10767 asked for). Then set
   the interval to 5 minutes. The canary is tracked as
   [eshu-hq/eshu#10817](https://github.com/eshu-hq/eshu/issues/10817).
4. `max_in_flight_years` above 1 only after the canary shows cycle time and
   inflow headroom.

## What the arbiter will not do

- Skip a record, hold a pin, or move a position past an uncommitted record:
  every rule above advances only inside the commit transaction.
- Serialize lanes: OSV, GHSA and windows are untouched, and a stuck year does
  not block other years.
- Rule the window-path fix (#197) or the selector fix (#198).
- Set the fast pace as a default: the downstream cycle is unmeasured, so blank
  config stays at 30 minutes.

## (e) Unverified, and what would settle each

| Item | Settled by | Run by the arbiter |
|---|---|---|
| Chunk counts, group sizes, byte breakdown, fold time, retained heap | Scratch tests over the cached files | Yes |
| Host limits, pager, planner, checkpoint, #10804 diff | Source read | Yes |
| Commit time for a 12,700-fact generation on QA's 407 GB `fact_records` | Canary chunk 1 | No |
| Projector and reducer time, and expansion envelopes, for a 2,900-record NVD scope (budget 100,000, in `evidence_truncation.go`) | Canary; `evidence_budget_ratio` | No |
| Real dirty rows per NVD fact in QA | Canary; the hourly sample | No |
| Chunk cycle under 5 minutes | `_chunk_cycle_seconds` in the canary | No |
| Collector child RSS at 16 and 32 MiB (the arbiter measured heap, not RSS) | The child-process RSS test; since measured by the coordinator, see the adjustment (#203) | No |
| Deployed `stdout_limit_bytes`, claim lease, and whether the claim plane URL is set | Read the QA deployment env (#10817) | No |
| Whether the extension host runs claims concurrently | Source read of the worker loop; no worker setting was found | No |
| `.meta` `lastModifiedDate` never decreases, and a record's `lastModified` never decreases | Compare successive `.meta` and year files over a week | No |
| Whether NVD re-stamped the two groups since 2026-11-15 | Re-download 2025 and 2026 and rerun the scratch test | No |
| Drain probe uses keyed lookups without a new index | `EXPLAIN` on a fixture in C2 | No |
| Whether an instance config change applies without a coordinator restart | Source read in #10746 | No |
| "Most queried years" is 2026 to 2023 | No query data exists; record share is a proxy | No |
| Fan-out stamp sentinel (cap 200 changed CVE ids) cost at 2,900 records a chunk | Canary; pass mode in the fan-out signals | No |
| Whether either repo's release gate needs a tagged SDK | Read both release workflows | No |

Scratch artifacts, in the arbiter's local scratch directory and not committed:
`measure1.txt`, `records.tsv`, `scripts/chunks.py`, and three collector scratch
tests for the pace, the record dump and the heap measurement.

Refs #10671. Part of #9209.
