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
  `FailureClassAdmissionTimeout` (`admission_timeout`, #10502) names a
  backpressure admission wait timeout that retried to the budget and gave
  up; `AdmissionTimeoutError` is the narrow contract producers implement so
  `ClassifyFailure` recognizes one without importing the producer.
- `StageError`, `InputValidationError`, `ResourceExhaustedError` and their
  constructors — the error types the projection stages raise.
- `IsRetryable`, `RetryableError` — the single-error retry question.
- `TriageFailure`, `TriageClass`, `ManualReviewTriageClasses`,
  `TriageDispositionConflicts` — terminal-failure triage.
- `ReplayGenerationSupersededClass` — the one `failure_class` spelling the
  recovery store and the admin replay handler share for the #7130 replay fence
  (metric label and 422 refusal body).
- `RetryInjector`, `RetryOnceInjector`, `NewRetryOnceInjector` — the fault
  injection the Ifá gates drive.
- `SourcePacedRelease`, `RetryAfterHint`, `ResumeBoundHint` — the cause a
  claimed item is released with when its source is slowed (#10882/#11186): the
  queue retries it without spending the projection or admission budget and never
  dead-letters it. `RetryAfterHint` is the earliest re-claim (the grant delay) and
  `ResumeBoundHint` the latest (the source's release age) the queue may schedule;
  both read through wrapping, and a cause with no bound keeps the queue's cap.

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
instead of the coarse `projection_failed` / `reducer_failed` fallback. A
backpressure admission wait timeout (#10502) dead-letters under its own
`admission_timeout` durable class instead: the error self-classifies (the
precedence the queue documents), while the triage detail keeps
`triage=retry_exhausted` for the exhausted budget and now carries
`class=admission_timeout` instead of the `projection_bug` default. It replays
without force like any other exhausted transient; only the `manual_review`
classes (`projection_bug`, `resource_exhausted`) need `--force`.

#3514 is resolved by wiring, not deletion: the live dead-letter path in
`storage/postgres` (`projector_queue.go` `Fail`, `reducer_queue_helpers.go`
`failIntent`) now calls `queuestore.DeadLetterTriageMetadata`, which runs `ClassifyFailure`
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

`ManualReviewTriageClasses` exposes the `manual_review` triage classes
(`projection_bug`, `resource_exhausted`) as the single source of truth for the
admin replay-safety guard in `internal/query`, so a poison item cannot drain via
`POST /api/v0/admin/replay` without `--force`. The disposition table
`terminalTriageDispositions` backs both `reconcileTriage` and
`ManualReviewTriageClasses`, so the guard can never drift from the disposition
actually written on a dead-letter row.
