# AGENTS.md — cicdrun/ghactionsruntime guidance

## Read First

1. `README.md` — package purpose, provider contract, and safety rules.
2. `source.go` — claim-to-fact flow (`buildRunEnvelopes`, one fixture
   normalization per fetched run) and runtime target validation
   (`validateTarget`, including the `defaultMaxRuns` fill).
3. `run_watermark.go` — #5429 cross-cycle gap detection
   (`detectRunBackfillGap`) and the `runwatermark.Store` load/save wiring.
4. `pending_watermark.go` and `source_commit_observer.go` — the #5429
   commit-ordering fix: `NextClaimed` stashes, `ObserveClaimedGenerationCommitted`
   (called by `collector.ClaimedService` only after a durable commit) saves.
   Read these before changing anything about WHEN the watermark advances.
5. `source_telemetry.go` — tracing/metrics recording split out of `source.go`
   for the 500-line cap.
6. `client.go` — GitHub REST pagination, request bounding, and the
   `runsPageTruncated` truncation signal.
7. `../runwatermark/types.go` — the watermark data contract this package
   reads and writes.
8. `../AGENTS.md` — fixture normalizer boundary. Do not move live HTTP code
   into the parent package.
9. `../../claimed_service_commit_observer.go` — the generic
   `collector.ClaimedGenerationCommitObserver` optional hook this package
   implements, and the exact point in `collector.ClaimedService.processClaimed`
   where it fires.

## Invariants

- Keep GitHub Actions provider polling in this runtime package, not in
  `internal/collector/cicdrun`.
- Fetch every run in the bounded window (`max_runs`, default 10, hard cap
  100), not just the newest one. `GitHubClient.FetchRuns` fetches bounded
  workflow-run, job, and artifact pages per run in the window.
- An omitted/zero `max_runs` resolves to `defaultMaxRuns` (10) in
  `validateTarget`; only an explicit out-of-range value (negative, or above
  the hard cap) is rejected. Bound the collector with the DEFAULT, not the
  mechanism — do not reintroduce a hard requirement that every target spell
  out `max_runs`.
- Every run's normalized facts are keyed by provider run ID
  (`stable_fact_key`), independent of fetch/emission order and independent of
  `generation_id`, so re-fetching the same window on a later claim cycle is
  an idempotent upsert at projection. The run-ID keying is what makes
  re-fetching a window safe WITHOUT a resume cursor -- do not conflate this
  with the separate `runwatermark.Store` (#5429), which exists to DETECT a
  cross-cycle gap, not to resume collection.
- `SourceConfig.Watermarks` is optional; a nil `Store` disables gap
  detection with no error and no behavior change (see
  `TestClaimedSourceSkipsGapDetectionWithoutWatermarkStore`). When wired,
  `detectRunBackfillGap` only fires when a prior watermark exists, the
  fetched page is truncated, and the window's oldest run is strictly newer
  than the watermark. Do not weaken any of those three conditions without
  re-deriving the false-positive/false-negative tradeoff in
  `run_watermark_test.go`.
- `saveWatermark` is called ONLY from `ObserveClaimedGenerationCommitted`
  (`source_commit_observer.go`), which `collector.ClaimedService` invokes
  once a claim cycle's generation has committed durably (#5429). `NextClaimed`
  itself never calls `saveWatermark`; it only stashes the observed newest run
  ID in `pending_watermark.go`'s `pendingWatermarks` map. Saving the
  watermark on `NextClaimed`'s own success path (independent of whether the
  commit later succeeds) was the #5429 bug itself: a commit failure followed
  by a retry compared the retry's re-fetched window against an
  already-advanced watermark and silently stopped re-detecting the gap. Do
  not move the save back into `NextClaimed` without re-deriving why that
  regresses #5429.
- `pendingWatermarks` (`pending_watermark.go`) is a mutex-guarded map shared
  via a pointer field on `ClaimedSource`, keyed by `(ScopeID, GenerationID)`.
  `collector.MultiSourceCollectorHost` can run several `ClaimedService`
  workers against the SAME registered source concurrently; they only ever
  claim DIFFERENT work items (per-scope claim exclusivity via
  `FOR UPDATE SKIP LOCKED`), so the mutex protects the map structure, not a
  contested per-key read-modify-write. A stashed entry that is never
  consumed (a permanently terminal-failed claim) is a documented, bounded
  memory-leak tradeoff -- see the type's doc comment before "fixing" it with
  a TTL or size cap that could instead drop a still-pending entry.
- When the fetched runs page is full (more runs may exist beyond the
  window), attach a `runs_truncated` warning to the newest run's Warnings
  (`attachRunsTruncatedWarning`) and record the matching partial-generation
  metric. Do not silently drop the truncation signal.
- Strip query strings and fragments from artifact download URLs before facts are
  emitted.
- Preserve provider-native run IDs, run attempts, job IDs, and artifact IDs.
- Emit warnings for partial job or artifact metadata instead of publishing
  complete-looking facts.
- Do not infer deployment truth from workflow success, job names, artifact names,
  environment names, tags, or repository names.
- Never assume the only `ci.run` fact in a generation is the latest run:
  GitHub returns runs newest-first, but nothing downstream preserves
  emission order as recency. Any future "latest run" consumer must select
  explicitly by `created_at`/run ID.
