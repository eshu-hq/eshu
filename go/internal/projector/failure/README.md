# Failure classification and dead-letter triage

## Purpose

Classify a projection failure and decide what the queue does with it: retry,
dead-letter, or escalate to a human.

## Ownership boundary

This package is a **leaf**: it imports no other projector package. It owns the
failure vocabulary and the triage decision. It does not own the queue, the
retry loop, or the writers that produced the failure — the projector service
and runtime own those and call in here to classify what came back.

## Exported surface

- `FailureClass`, `RetryDisposition`, `FailureClassification`,
  `ClassifyFailure` — the classification vocabulary and the classifier.
- `StageError`, `InputValidationError`, `ResourceExhaustedError` and their
  constructors — the error types the projection stages raise.
- `IsRetryable`, `RetryableError` — the single-error retry question.
- `TriageFailure`, `TriageClass`, `ManualReviewTriageClasses`,
  `TriageDispositionConflicts` — terminal-failure triage.
- `RetryInjector`, `RetryOnceInjector`, `NewRetryOnceInjector` — the fault
  injection the Ifá gates drive.

## Two behaviors worth knowing before changing anything here

**The class set is bounded on purpose.** Classes are reported as a metric
dimension. A new class is a metric-contract change, not a local edit.

**An unrecognized error is a projection bug, not a transient.** `ClassifyFailure`
deliberately falls back to the projection-bug class so an unknown failure
becomes visible in a dashboard instead of being retried forever. Widening the
transient branch to "anything that looks like a network error" is how a real
defect becomes an invisible retry loop.

See `doc.go` for the full godoc contract.

## Detail: dead-letter triage (relocated from the projector root, #6781)

`failure/dead_letter_triage.go` turns a failed work item into an operator-facing triage
class on the durable dead-letter row. `TriageFailure` reconciles two signals that
previously disagreed: the canonical `IsRetryable()` / `Retryable()` retry
authority (the live projector- and reducer-queue path) and the rich
`ClassifyFailure` categorization that issue #3514 flagged as dead code with no
production caller. Retryable() stays the sole authority for whether an item is
retried; `TriageClass` only records why it landed where it did, so the durable
`failure_class` reads `retry_exhausted` / `input_invalid` /
`dependency_unavailable` / `resource_exhausted` / `timeout` / `projection_bug`
instead of the coarse `projection_failed` / `reducer_failed` fallback.

#3514 is resolved by wiring, not deletion: the live dead-letter path in
`storage/postgres` (`projector_queue.go` `Fail`, `reducer_queue_helpers.go`
`failIntent`) now calls `deadLetterTriageMetadata`, which runs `ClassifyFailure`
through `TriageFailure`. `Retryable()` remains the single source of truth for the
retry-vs-dead-letter decision; the requeue/backpressure fix from #3513 is
unchanged because the retry branch is taken before triage classification runs.

The triage class feeds the existing operator requeue path with no new surface:
`eshu admin facts replay --failure-class retry_exhausted` drains the safe
transient bucket, while `projection_bug` / `resource_exhausted` map to
`manual_review` and require `--force` after the cause is addressed. The
`reconcileTriage` disposition can never contradict the authority — a retryable
cause never carries `non_retryable` and vice versa, asserted by
`TriageDispositionConflicts` and `TestTriageFailureConsistencyWithRetryable`.

Performance Evidence: the dead-letter path is the cold failure branch, not the
success hot path. `TriageFailure` adds one `ClassifyFailure` call (the same
`errors.As` / type-switch work the reducer already ran via `queueFailureMetadata`)
plus one `fmt.Sprintf` per dead-lettered item; no new query, index, lease, or
graph write is introduced, and the durable `UPDATE` is byte-for-byte the prior
`failProjectorWorkQuery` / `failReducerWorkQuery` with only the `failure_class`,
message, and details argument values changed.
No-Regression Evidence: `cd go && go test ./internal/projector
./internal/storage/postgres ./internal/reducer -race -count=1` passed (3484
tests, no data races); the retry branch and its
`TestProjectorQueueFailMarksRetryableErrorTerminalWhenAttemptBudgetExhausted` /
`TestReducerQueueFailMarksRetryableErrorTerminalWhenAttemptBudgetExhausted`
proofs are unchanged, and `TestProjectorQueueFailDeadLettersWithTriageClass`,
`TestProjectorQueueFailDeadLettersRetryExhaustedWithTriageClass`, and
`TestReducerQueueFailDeadLettersTerminalWithTriageClass` prove the live queue
writes the triage class on the dead-letter row.

Observability Evidence: the operator-facing signal is the durable
`fact_work_items.failure_class` value on dead-lettered rows, surfaced through the
existing `eshu admin facts list --status dead_letter` query and the
`replay --failure-class` filter. The structured `details` string carries
`stage=`, `triage=`, `class=`, `code=`, `disposition=`, `retryable=`, and
`exhausted=` so an operator inspecting one dead letter at 3 AM can tell a
transient pileup (safe to replay) from a poison projection bug (needs code fix)
without reading the projector source. No new metric series or span is added; the
change relabels an existing durable column.

`ManualReviewTriageClasses` exposes the `manual_review` triage classes
(`projection_bug`, `resource_exhausted`) as the single source of truth for the
admin replay-safety guard in `internal/query`, so a poison item cannot drain via
`POST /api/v0/admin/replay` without `--force`. The disposition table
`terminalTriageDispositions` backs both `reconcileTriage` and
`ManualReviewTriageClasses`, so the guard can never drift from the disposition
actually written on a dead-letter row.

Performance Evidence: `ManualReviewTriageClasses` is an in-memory iteration over
a five-entry map plus a sort, evaluated once at `internal/query` package init
(`buildUnsafeReplayFailureClasses`), not on any request or projection path.
`reconcileTriage` keeps the same single map lookup it already did per
dead-lettered item; no new query, index, lock, or graph write is introduced.
No-Regression Evidence: `cd go && go test ./internal/projector ./internal/query
-race -count=1` passed (3486 tests, no data races); the live-path triage proofs
and the retry-branch proofs are unchanged, and the new
`TestReplayRefusesManualReviewTriageClassWithoutForce` /
`TestUnsafeReplayClassesIncludeManualReviewTriage` prove the guard refuses an
un-forced `projection_bug` replay.
No-Observability-Change: the guard reuses the existing
`replay_refused_unsafe_class` governance-audit reason code and the existing
422 refusal envelope; no new metric, span, or log scope is added.
