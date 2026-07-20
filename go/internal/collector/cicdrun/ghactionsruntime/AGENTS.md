# AGENTS.md — cicdrun/ghactionsruntime guidance

## Read First

1. `README.md` — package purpose, provider contract, and safety rules.
2. `source.go` — claim-to-fact flow (`buildRunEnvelopes`, one fixture
   normalization per fetched run) and runtime target validation
   (`validateTarget`, including the `defaultMaxRuns` fill).
3. `source_telemetry.go` — tracing/metrics recording split out of `source.go`
   for the 500-line cap.
4. `client.go` — GitHub REST pagination, request bounding, and the
   `runsPageTruncated` truncation signal.
5. `../AGENTS.md` — fixture normalizer boundary. Do not move live HTTP code
   into the parent package.

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
  an idempotent upsert at projection. Do not add a persistent
  watermark/cursor here; the run-ID keying is the intended
  stateless-idempotent substitute.
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
