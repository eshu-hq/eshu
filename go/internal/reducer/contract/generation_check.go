// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import (
	"context"
	"errors"
	"fmt"
)

// GenerationFreshnessCheck reports whether the given generation is still
// the active generation for the scope. Returns (true, nil) if current,
// (false, nil) if superseded, or (false, err) on lookup failure. A generation
// that is newer than the active one but not yet activated is neither: the
// check returns (false, GenerationNotYetActiveError), a retryable,
// self-classifying error, so callers must propagate the error (wrapped with
// %w) rather than treat false as supersession (#6686).
type GenerationFreshnessCheck func(ctx context.Context, scopeID, generationID string) (bool, error)

// PriorGenerationCheck reports whether the scope has any generation before the
// given generation. Retract paths use it to skip no-op cleanup on first writes
// while preserving cleanup on refreshes and retries.
type PriorGenerationCheck func(ctx context.Context, scopeID, generationID string) (bool, error)

// PriorGenerationID returns the scope's generation immediately before the
// given generation, ordered by observation time with the generation id as the
// deterministic tie-break. It reports found=false when the scope has no
// earlier generation (first write: no diff source, no retract). Generation
// diff retracts (#6887) load the predecessor's facts through the standard
// FactLoader and diff their extracted uids against the current generation's
// to enumerate delete candidates.
type PriorGenerationID func(ctx context.Context, scopeID, generationID string) (prior string, found bool, err error)

// EC2PostureCandidate is one EC2 instance identity for the #6887 liveness
// probe: the graph node uid a handler extracted plus the posture identity
// tuple that proves it. The tuple fields may be unnormalized (blank
// resource_type, surrounding whitespace); the probe normalizes them with the
// unscoped reader's semantics before matching, so handlers pass extraction
// output through unchanged.
type EC2PostureCandidate struct {
	// UID is the canonical cloud_resource_uid of the candidate node.
	UID string
	// AccountID is the raw provider account identifier.
	AccountID string
	// Region is the provider region.
	Region string
	// ResourceType is the posture resource_type (blank means the EC2 default).
	ResourceType string
	// InstanceID is the instance id when known.
	InstanceID string
	// ARN is the instance ARN fallback when the instance id is unknown.
	ARN string
}

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
