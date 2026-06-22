package projector

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/queue"
)

// TriageClass is the stable, operator-facing reason a work item is sitting in a
// non-success terminal or retrying state. It is the durable triage label written
// to fact_work_items.failure_class so an operator inspecting the dead-letter
// queue can tell, at a glance, why each item died and whether replaying it
// unchanged could ever succeed.
//
// TriageClass reconciles two independent signals that previously disagreed (see
// issue #3514): the canonical Retryable() retry-decision authority used on the
// live queue path, and the rich ClassifyFailure categorization that was built
// but never wired into that path. Retryable() remains the authority for whether
// an item is retried; TriageClass records why it ultimately landed where it did.
type TriageClass string

const (
	// TriageClassRetrying marks an item the live path is still retrying: the
	// cause was retryable and the attempt budget is not yet exhausted. It is not
	// a dead-letter class; it lets the triage surface report in-flight retry
	// pressure distinctly from terminal failures.
	TriageClassRetrying TriageClass = "retrying"

	// TriageClassRetryExhausted marks a transient (retryable) cause that
	// exhausted its retry budget and was dead-lettered. These items are the
	// safest to replay once the underlying dependency recovers.
	TriageClassRetryExhausted TriageClass = "retry_exhausted"

	// TriageClassInputInvalid marks a terminal, non-retryable input-validation
	// failure. Replaying it unchanged will fail again; the upstream fact or
	// scope must be corrected first.
	TriageClassInputInvalid TriageClass = "input_invalid"

	// TriageClassDependencyUnavailable marks a dependency failure the retry
	// authority classified as terminal (no retryable wrapper). It is surfaced
	// distinctly so an operator can confirm the dependency is healthy before
	// replaying.
	TriageClassDependencyUnavailable TriageClass = "dependency_unavailable"

	// TriageClassResourceExhausted marks a resource-exhaustion failure (e.g.
	// OOM) that needs operator review of capacity before replay.
	TriageClassResourceExhausted TriageClass = "resource_exhausted"

	// TriageClassTimeout marks a terminal timeout that the retry authority did
	// not mark retryable.
	TriageClassTimeout TriageClass = "timeout"

	// TriageClassProjectionBug marks an unclassified terminal failure — the
	// poison bucket. It requires manual review of the projector/reducer code
	// path; replaying it blindly risks an immediate re-failure.
	TriageClassProjectionBug TriageClass = "projection_bug"
)

// TriageFailure produces a durable queue.FailureRecord that labels one failed
// work item with an operator-facing TriageClass and a disposition that never
// contradicts the canonical retry authority.
//
// retryable is the result of IsRetryable(cause) — the live-path authority for
// whether the item is retried at all. attemptsExhausted reports whether the
// retry budget is spent. The two together place the item in the right bucket:
//
//   - retryable && !attemptsExhausted -> still retrying (TriageClassRetrying)
//   - retryable && attemptsExhausted  -> transient that gave up (retry_exhausted)
//   - !retryable                      -> terminal; the underlying error class
//     from ClassifyFailure selects the bucket (input_invalid, projection_bug, …)
//
// Wiring this onto the live Fail path is the resolution of issue #3514:
// ClassifyFailure stops being dead code and becomes the triage enrichment,
// while Retryable() stays the retry-decision authority.
func TriageFailure(cause error, stage string, retryable bool, attemptsExhausted bool) queue.FailureRecord {
	classification := ClassifyFailure(cause, stage)
	triageClass, disposition := reconcileTriage(classification, retryable, attemptsExhausted)

	message := cause.Error()
	details := fmt.Sprintf(
		"stage=%s triage=%s class=%s code=%s disposition=%s retryable=%t exhausted=%t message=%s",
		classification.FailureStage,
		triageClass,
		classification.FailureClass,
		classification.FailureCode,
		disposition,
		retryable,
		attemptsExhausted,
		message,
	)

	return queue.FailureRecord{
		FailureClass: string(triageClass),
		Message:      message,
		Details:      details,
	}
}

// reconcileTriage chooses the durable triage class and disposition so they agree
// with the retry authority. The authority (retryable) wins on disposition; the
// classification only selects which terminal bucket a non-retryable failure
// lands in.
func reconcileTriage(
	classification FailureClassification,
	retryable bool,
	attemptsExhausted bool,
) (TriageClass, RetryDisposition) {
	if retryable {
		if attemptsExhausted {
			return TriageClassRetryExhausted, RetryDispositionRetryable
		}
		return TriageClassRetrying, RetryDispositionRetryable
	}

	// Non-retryable per the authority: pick the terminal bucket from the
	// underlying error classification, but force a disposition that does not
	// invite an operator to blindly replay a transient-looking-but-terminal
	// failure.
	switch classification.FailureClass {
	case FailureClassInputInvalid:
		return TriageClassInputInvalid, RetryDispositionNonRetryable
	case FailureClassDependencyUnavailable:
		return TriageClassDependencyUnavailable, RetryDispositionNonRetryable
	case FailureClassResourceExhausted:
		return TriageClassResourceExhausted, RetryDispositionManualReview
	case FailureClassTimeout:
		return TriageClassTimeout, RetryDispositionNonRetryable
	default:
		return TriageClassProjectionBug, RetryDispositionManualReview
	}
}

// TriageDispositionConflicts reports whether a (retryable, disposition) pair is
// internally inconsistent — a retryable cause must never carry a non_retryable
// disposition, and a non-retryable cause must never carry a retryable one.
// Tests and the live path use this to guarantee the triage record can never
// mislead an operator about replay safety.
func TriageDispositionConflicts(retryable bool, disposition RetryDisposition) bool {
	if retryable && disposition == RetryDispositionNonRetryable {
		return true
	}
	if !retryable && disposition == RetryDispositionRetryable {
		return true
	}
	return false
}
