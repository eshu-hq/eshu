// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// Replay request status values persisted in the admin_replay_requests ledger.
const (
	replayRequestStatusInProgress = "in_progress"
	replayRequestStatusCompleted  = "completed"
)

// unsafeReplayFailureClasses maps a durable fact_work_items failure_class that
// must not be blindly replayed to the actionable operator guidance explaining
// the refusal. The set mirrors the non-retryable and quarantined classes from
// internal/projector (FailureClassInputInvalid is non_retryable, plus the
// manual_review dead-letter triage classes from ManualReviewTriageClasses) and
// internal/semanticqueue (StatusUnsafePayload is a terminal quarantine); a
// dedicated test pins these strings to those source constants so the set never
// drifts. Replaying these without fixing the input, addressing the cause, or
// clearing the quarantine either re-fails identically or re-triggers the unsafe
// condition.
//
// It is built once at package init from buildUnsafeReplayFailureClasses so the
// manual_review triage classes stay sourced from the projector package — the
// single source of truth — rather than re-listed here where they could drift.
var unsafeReplayFailureClasses = buildUnsafeReplayFailureClasses()

// buildUnsafeReplayFailureClasses assembles the unsafe-replay set from the base
// non-retryable/quarantine classes and the projector's manual_review triage
// classes. Each manual_review class (projection_bug, resource_exhausted) needs
// force because draining it unchanged risks an immediate re-failure or
// re-exhausts a constrained resource.
func buildUnsafeReplayFailureClasses() map[string]string {
	classes := map[string]string{
		"input_invalid":  "the work item failed input validation; replaying the same input will fail again. Fix or re-ingest the source, then refinalize the scope.",
		"unsafe_payload": "the payload was quarantined as unsafe; replaying could re-trigger the unsafe condition. Investigate the source before forcing a replay.",
	}
	for _, class := range projector.ManualReviewTriageClasses() {
		if _, exists := classes[class]; exists {
			continue
		}
		classes[class] = "the work item was dead-lettered as a manual-review triage class; " +
			"replaying it unchanged risks an immediate re-failure. Investigate the projector/reducer " +
			"cause or constrained resource, then force the replay."
	}
	return classes
}

// unsafeReplayFailureClassList returns the unsafe failure classes in stable
// order for SQL exclusion predicates.
func unsafeReplayFailureClassList() []string {
	classes := make([]string, 0, len(unsafeReplayFailureClasses))
	for class := range unsafeReplayFailureClasses {
		classes = append(classes, class)
	}
	sort.Strings(classes)
	return classes
}

// unsafeReplayRefusal returns the actionable refusal guidance for a failure
// class and whether the class is unsafe to replay. An empty class is safe (a
// broad selector relies on the store-side exclusion instead).
func unsafeReplayRefusal(failureClass string) (string, bool) {
	guidance, ok := unsafeReplayFailureClasses[strings.TrimSpace(failureClass)]
	return guidance, ok
}

// replayRequestFingerprint derives a stable, non-sensitive fingerprint of a
// replay request's selectors so a reused idempotency key with different
// parameters is detected and refused rather than silently returning the prior
// outcome. Work-item IDs are sorted so ordering does not change the fingerprint.
// The limit is included because it changes which work items the replay selects.
func replayRequestFingerprint(workItemIDs []string, scopeID, stage, failureClass string, limit int, force bool) string {
	sorted := append([]string(nil), workItemIDs...)
	for i, id := range sorted {
		sorted[i] = strings.TrimSpace(id)
	}
	sort.Strings(sorted)

	var builder strings.Builder
	builder.WriteString("work_items=")
	builder.WriteString(strings.Join(sorted, ","))
	builder.WriteString("\nscope=")
	builder.WriteString(strings.TrimSpace(scopeID))
	builder.WriteString("\nstage=")
	builder.WriteString(strings.TrimSpace(stage))
	builder.WriteString("\nfailure_class=")
	builder.WriteString(strings.TrimSpace(failureClass))
	fmt.Fprintf(&builder, "\nlimit=%d", limit)
	if force {
		builder.WriteString("\nforce=true")
	} else {
		builder.WriteString("\nforce=false")
	}

	sum := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(sum[:])
}
