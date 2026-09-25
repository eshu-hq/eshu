// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import (
	"errors"
	"fmt"
)

// ErrGenerationNotYetActive is the sentinel a GenerationFreshnessCheck wraps
// when an intent's generation is newer than the scope's active generation and
// is still pending: its projector has enqueued reducer work but has not yet
// acknowledged, so the generation is about to activate rather than superseded.
// Skipping that intent as superseded would ack it succeeded, and nothing
// re-drives it after activation (#6686), so the check returns this error
// instead and the durable queue retries the intent until the generation
// activates (the handler then runs) or ends failed or superseded (the retry
// then acks it as superseded).
var ErrGenerationNotYetActive = errors.New("reducer intent generation is newer than the active generation and not yet active")

// GenerationActivationNotReadyFailureClass is the durable failure_class a
// not-yet-active generation deferral self-classifies with. It carries the
// *_not_ready suffix so TestEveryReadinessFailureClassIsEnrolled
// (go/internal/storage/postgres/reducer_queue_readiness_enrollment_test.go)
// requires its enrollment in nonCountingReducerRetryFailureClasses: waiting on
// the projector's Ack is not a failure on the intent's own merits, so it must
// never erode the retry budget or dead-letter. Retries in this class are
// counted by eshu_dp_reducer_retry_surge_total{failure_class}.
const GenerationActivationNotReadyFailureClass = "generation_activation_not_ready"

// GenerationNotYetActiveError reports that an intent's generation is a newer,
// still-pending generation of its scope. It is retryable, self-classifies as
// GenerationActivationNotReadyFailureClass, and unwraps to
// ErrGenerationNotYetActive.
type GenerationNotYetActiveError struct {
	// ScopeID is the intent's ingestion scope.
	ScopeID string
	// GenerationID is the intent's pending generation.
	GenerationID string
	// ActiveGenerationID is the scope's active generation when the check ran.
	ActiveGenerationID string
}

// Error describes the scope, the pending generation, and the active one.
func (e GenerationNotYetActiveError) Error() string {
	return fmt.Sprintf("%s: scope %s generation %s pending, active generation %s",
		ErrGenerationNotYetActive.Error(), e.ScopeID, e.GenerationID, e.ActiveGenerationID)
}

// Unwrap exposes ErrGenerationNotYetActive to errors.Is.
func (GenerationNotYetActiveError) Unwrap() error { return ErrGenerationNotYetActive }

// Retryable reports true: the durable queue re-runs the intent after its
// retry delay.
func (GenerationNotYetActiveError) Retryable() bool { return true }

// FailureClass returns GenerationActivationNotReadyFailureClass.
func (GenerationNotYetActiveError) FailureClass() string {
	return GenerationActivationNotReadyFailureClass
}
