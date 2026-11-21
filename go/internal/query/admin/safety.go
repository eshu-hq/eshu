// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// Replay request status values persisted in the admin_replay_requests ledger.
const (
	ReplayRequestStatusInProgress = "in_progress"
	ReplayRequestStatusCompleted  = "completed"
)

// unsafeReplayFailureClasses maps a durable fact_work_items failure_class that
// must not be blindly replayed to the actionable operator guidance explaining
// the refusal. The set and its guidance live in recovery, the single source
// shared with the supply_chain_impact holder fan-out (#11147), which does not
// reopen a dead letter in one of these classes. Replaying these without fixing
// the input, addressing the cause, or clearing the quarantine either re-fails
// identically or re-triggers the unsafe condition.
var unsafeReplayFailureClasses = recovery.UnsafeReplayFailureClassGuidance()

// unsafeReplayFailureClassList returns the unsafe failure classes in stable
// order for SQL exclusion predicates.
func unsafeReplayFailureClassList() []string {
	return recovery.UnsafeReplayFailureClasses()
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
