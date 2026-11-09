// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/vulnsource"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Host failure classes for GHSA window commit-policy refusals (#10037).
// Every one is retryable and commits nothing: the cursor cannot pass
// unstored facts, and a later claim re-plans the same write-once scope.
const (
	// FailureClassGHSAWindowNoProgress marks a window whose stated
	// covered_through does not pass its start, so committing it would
	// wedge the stream on a scope that can never advance.
	FailureClassGHSAWindowNoProgress = "ghsa_window_no_progress"
	// FailureClassGHSAWindowUnarmed marks a GHSA window claim on a host
	// with no commit policy able to arm its cursor writer.
	FailureClassGHSAWindowUnarmed = "ghsa_window_unarmed"
	// FailureClassGHSAWindowHeld marks a GHSA window whose snapshot does
	// not state a usable covered_through, so no writer can be armed.
	FailureClassGHSAWindowHeld = "ghsa_window_held"
	// FailureClassGHSACoveredThroughOutOfRange marks a covered_through
	// past the claimed window end: the satellite proved coverage it was
	// never asked for, so nothing commits.
	FailureClassGHSACoveredThroughOutOfRange = "ghsa_covered_through_out_of_range"
	// FailureClassGHSACacheReplay marks a window result served from the
	// satellite cache (cache_mode is not refresh): a replay never
	// advances the cursor (NVD precedent).
	FailureClassGHSACacheReplay = "ghsa_cache_replay"
)

// ClaimCommitPolicy is the optional host seam that decides
// commit-transaction state for one collected generation (#10037 S3c):
// which GenerationStateWriter runs inside the commit and whether the
// generation is write-once. The satellite states what it covered in its
// facts; the host owns the advance decision because the extracted
// collector holds no storage handle by design. A policy that does not own
// a scope family returns Handled=false and the claim keeps its existing
// behavior exactly.
type ClaimCommitPolicy interface {
	// CommitPolicy decides one collected generation. A non-nil error
	// must wrap a *CommitPolicyRefusal: the claim then fails with the
	// refusal's class and terminality and nothing commits. Any other
	// error fails the claim retryable as a launch failure.
	CommitPolicy(item workflow.WorkItem, observedAt time.Time, envelopes []facts.Envelope) (ClaimCommitDecision, error)
}

// ClaimCommitDecision is one policy verdict for a collected generation.
type ClaimCommitDecision struct {
	// Handled reports that the policy owns this claim's scope family.
	// When false the other fields are ignored.
	Handled bool
	// StateWriter, when set, replaces any writer the mapping armed and
	// runs inside the generation's commit transaction. Nil on a handled
	// decision means the policy deliberately arms nothing.
	StateWriter collector.GenerationStateWriter
	// WriteOnce marks the generation write-once for the successor guard.
	WriteOnce bool
}

// CommitPolicyRefusal is a policy's fail-the-claim verdict: a bounded
// failure class and whether retrying can ever succeed. It carries no
// payload value, so it is safe to log and store.
type CommitPolicyRefusal struct {
	// Class is the closed failure class ClaimedService records.
	Class string
	// Terminal reports whether the claim fails terminal instead of
	// retryable.
	Terminal bool
	// Err is the bounded cause, for logs.
	Err error
}

// Error returns the refusal class and cause.
func (r *CommitPolicyRefusal) Error() string {
	if r == nil || r.Err == nil {
		return "claim commit policy refusal"
	}
	return fmt.Sprintf("claim commit policy refusal %s: %v", r.Class, r.Err)
}

// Unwrap returns the bounded cause.
func (r *CommitPolicyRefusal) Unwrap() error {
	if r == nil {
		return nil
	}
	return r.Err
}

// applyCommitPolicy runs the configured commit policy over one collected
// generation that carries facts. A handled decision replaces the armed
// state writer, stamps write-once, and clears any trusted-deletion
// attestation on a write-once generation (a window claim never attests
// deletion, whatever the satellite sent). A GHSA window scope the policy
// does not handle — including no policy at all — fails retryable and
// commits nothing: committing its facts without advancing the cursor
// would re-plan the same write-once scope forever (the #9971 F-2 wedge).
func (s *Source) applyCommitPolicy(
	item workflow.WorkItem,
	observedAt time.Time,
	envelopes []facts.Envelope,
	collected *collector.CollectedGeneration,
) error {
	var decision ClaimCommitDecision
	if s.commitPolicy != nil {
		var err error
		decision, err = s.commitPolicy.CommitPolicy(item, observedAt, envelopes)
		if err != nil {
			var refusal *CommitPolicyRefusal
			if errors.As(err, &refusal) {
				return extensionFailure{class: refusal.Class, terminal: refusal.Terminal, cause: err}
			}
			return extensionFailure{class: FailureClassLaunchFailure, cause: err}
		}
	}
	if decision.Handled {
		collected.StateWriter = decision.StateWriter
		collected.Generation.WriteOnce = decision.WriteOnce
		if decision.WriteOnce {
			collected.Generation.TrustedDeletion = false
		}
		return nil
	}
	if vulnsource.IsGHSAWindowScope(item.ScopeID) {
		return extensionFailure{
			class: FailureClassGHSAWindowUnarmed,
			cause: errors.New("ghsa window claim on a host with no commit policy able to arm its cursor writer"),
		}
	}
	return nil
}
