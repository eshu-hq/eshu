# 10972: phase-two skip-and-park costs no regression

Issue eshu-hq/eshu#10972 (arbiter ruling, combination A+C), part of #10860.
The deferred backfill now degrades instead of dying when the phase-two ArgoCD
config reload times out on the budget sentinel, and the collector drain
boundary swallows an escaping sentinel as skip-this-pass. This note records
what the change costs on the measured paths.

## What the change does

`LoadDeferredAnchorScopedRelationshipFacts`
(go/internal/storage/postgres/backfill/scoped_load.go) branches only on
`errors.Is(err, ErrRelationshipFactLoadBudgetExceeded)` from
`AppendArgoCDGeneratorConfigFacts`: the fact_load block ends degraded and the
pass continues on the unmerged phase-one facts. `RunAllRelationshipEvidence`
(go/internal/storage/postgres/backfill/engine.go) commits that evidence
through the shared batch half and withholds the whole fan-in. `Service.Run`
(go/internal/collector/service.go) swallows a sentinel drain-hook error with
a consecutive-skip count and keeps polling. No query, page-size, batch-size,
worker-count, timeout, lock, or retry change anywhere: the in-pass retry
budget is zero and the 300s/330s guards are untouched.

## Cost

No-Regression Evidence: the success path executes exactly one added
`errors.Is` check, and only on an error branch that previously returned
immediately — no statement, page, batch, worker, or lock change on any path
that runs green. The batch/fan-in refactor
(`commitDeferredBackfillBatches`, same statements in a shared helper) is
covered by the unchanged hermetic floor
(`go test ./internal/storage/postgres/backfill/`,
`go test ./internal/collector/`, both also under `-race`, plus `go vet` and
`git diff --check` clean) and by the live whole-pass proofs on disposable
local PostgreSQL 18.6
(`TestDeferredBackfillPartitionSourceCoversAllScopes`,
`TestWholePassFanInSkipsUnchangedMemoHitPartitionsLive`,
`TestWholePassFanInRepublishesUnchangedPartitionMissingPhaseRowLive`,
`TestWholePassFanInKeepsTargetedBaselineOnActiveMemoLive`,
`TestDeferredBackfillFanInSkipsPartitionWhoseGenerationAdvanced`,
`TestDeferredBackfillFanInFailureLeavesEvidenceRecoverable`,
`TestDeferredBackfillCrashBetweenBatchesAndFanInConverges`,
`TestFanInActiveGenerationMatchesCorpusLoader` — all PASS). No benchmark is
invented: the claim is structural (no new work on any green path), and the
green floor plus the live clean-pass proofs are the evidence.

Observability Evidence: a degraded pass emits exactly one
`deferred_backfill_block_degraded` line for fact_load (never block_failed)
carrying fallback=true, parked config-repo count, and affected-partition
count; the pass ends with `deferred_backfill_completed_degraded`
(readiness_rows is always 0) instead of the clean completed line; an outer
skip logs `collector drain degraded; skipping this pass, continuing` with
failure_class collector_drain_degraded and a consecutive_skips count that
resets on the next clean drain. Pinned by
`TestRunAllRelationshipEvidenceDegradesOnPhaseTwoTimeout` (plus the seeded
fence tests) and `TestServiceRunSkipsDrainOnBudgetSentinel`.
