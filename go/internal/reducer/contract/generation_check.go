// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import "context"

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
