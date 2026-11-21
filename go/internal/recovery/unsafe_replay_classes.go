// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recovery

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/semanticqueue"
)

// unsafeReplayFailureClassGuidance is the one source of the durable
// fact_work_items failure classes that must not be replayed blindly, each
// mapped to the operator guidance that explains why. It joins the projector's
// non-retryable input class, the semantic queue's terminal quarantine, and
// the projector's manual-review dead-letter triage classes. The admin replay
// endpoint refuses these classes without force, and the supply_chain_impact
// holder fan-out does not reopen a dead letter in one of them (#11147).
// Replaying one of these without fixing the input, addressing the cause, or
// clearing the quarantine either fails the same way again or repeats the
// unsafe condition.
var unsafeReplayFailureClassGuidance = buildUnsafeReplayFailureClassGuidance()

func buildUnsafeReplayFailureClassGuidance() map[string]string {
	classes := map[string]string{
		string(failure.FailureClassInputInvalid):  "the work item failed input validation; replaying the same input will fail again. Fix or re-ingest the source, then refinalize the scope.",
		string(semanticqueue.StatusUnsafePayload): "the payload was quarantined as unsafe; replaying could re-trigger the unsafe condition. Investigate the source before forcing a replay.",
	}
	for _, class := range failure.ManualReviewTriageClasses() {
		if _, exists := classes[class]; exists {
			continue
		}
		classes[class] = "the work item was dead-lettered as a manual-review triage class; " +
			"replaying it unchanged risks an immediate re-failure. Investigate the projector/reducer " +
			"cause or constrained resource, then force the replay."
	}
	return classes
}

// UnsafeReplayFailureClassGuidance returns a copy of the unsafe-replay class
// set with the operator guidance for each class. Callers may change the copy.
func UnsafeReplayFailureClassGuidance() map[string]string {
	out := make(map[string]string, len(unsafeReplayFailureClassGuidance))
	for class, guidance := range unsafeReplayFailureClassGuidance {
		out[class] = guidance
	}
	return out
}

// UnsafeReplayFailureClasses returns the unsafe-replay failure classes in
// sorted order, for SQL exclusion predicates.
func UnsafeReplayFailureClasses() []string {
	classes := make([]string, 0, len(unsafeReplayFailureClassGuidance))
	for class := range unsafeReplayFailureClassGuidance {
		classes = append(classes, class)
	}
	sort.Strings(classes)
	return classes
}
