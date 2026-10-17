// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import (
	"context"
	"fmt"
)

// Retract decision reasons are the closed vocabulary a materializer logs and
// attaches to its span when it decides whether to run its scope-wide retract
// before rewriting (#8167). The set is bounded so the reason is safe as a
// span attribute or metric label.
const (
	// RetractReasonFirstGeneration skips the retract: the intent is a first
	// attempt and the scope has no generation strictly older than the
	// intent's, in activation order, so no earlier write can be left behind.
	RetractReasonFirstGeneration = "first_generation"
	// RetractReasonPriorGeneration runs the retract: a strictly older
	// generation exists, so its edges may be stale.
	RetractReasonPriorGeneration = "prior_generation"
	// RetractReasonRetryAttempt runs the retract: a retried attempt may have
	// left a partial write of its own, so the check is not consulted.
	RetractReasonRetryAttempt = "retry_attempt"
	// RetractReasonCheckUnwired runs the retract: no PriorGenerationCheck is
	// wired, so the safe default is to clean up.
	RetractReasonCheckUnwired = "check_unwired"
	// RetractReasonRecommit runs the retract: the handler is re-committing a
	// generation it already committed (for example a cross-scope target
	// became ready), and the resolved set can shrink between commits.
	RetractReasonRecommit = "recommit"
)

// RetractReasons returns the closed retract decision reason set in a stable
// order.
func RetractReasons() []string {
	return []string{
		RetractReasonFirstGeneration,
		RetractReasonPriorGeneration,
		RetractReasonRetryAttempt,
		RetractReasonCheckUnwired,
		RetractReasonRecommit,
	}
}

// RetractDecision is a materializer's decision about its scope-wide retract.
// Skip is true only for RetractReasonFirstGeneration.
type RetractDecision struct {
	// Skip reports that the retract must not run.
	Skip bool
	// Reason is one of RetractReasons.
	Reason string
}

// DecideFirstGenerationRetract is the one prior-generation predicate every
// first-generation retract skip shares (#8167). A retried attempt (AttemptCount
// above 1) and an unwired check always retract. A first attempt asks check
// whether the scope has a generation strictly older than the intent's; the
// answer must not depend on generations committed after the intent's own, so
// the decision is the same whatever the queue timing. A check error is
// returned wrapped, with a decision that does not skip.
func DecideFirstGenerationRetract(ctx context.Context, check PriorGenerationCheck, intent Intent) (RetractDecision, error) {
	if intent.AttemptCount > 1 {
		return RetractDecision{Reason: RetractReasonRetryAttempt}, nil
	}
	if check == nil {
		return RetractDecision{Reason: RetractReasonCheckUnwired}, nil
	}
	hasPrior, err := check(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return RetractDecision{Reason: RetractReasonPriorGeneration},
			fmt.Errorf("check prior generation for scope %s generation %s: %w", intent.ScopeID, intent.GenerationID, err)
	}
	if hasPrior {
		return RetractDecision{Reason: RetractReasonPriorGeneration}, nil
	}
	return RetractDecision{Skip: true, Reason: RetractReasonFirstGeneration}, nil
}
