// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector_test

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// TestRuntimeStatusesFailedTerminalRowDegradesWithoutPersistedFlag pins that
// the failed_terminal status, not the claim runner's last_failure_terminal
// flag, decides a terminal failure (#8803). The scanner worker dead-letters a
// claim to failed_terminal without setting that flag, so a failure built from
// such a row must still degrade the instance and fail its promotion proof.
func TestRuntimeStatusesFailedTerminalRowDegradesWithoutPersistedFlag(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	failedAt := now.Add(-5 * time.Minute)
	evidence := collector.Evidence{
		AsOf: now,
		Instances: []collector.InstanceSummary{{
			InstanceID:     "scanner-example",
			CollectorKind:  "scanner_worker",
			Mode:           "continuous",
			Enabled:        true,
			ClaimsEnabled:  true,
			LastObservedAt: now.Add(-time.Minute),
			UpdatedAt:      now.Add(-time.Minute),
		}},
		TerminalFailures: []collector.TerminalFailure{{
			CollectorKind: "scanner_worker",
			InstanceID:    "scanner-example",
			FailureClass:  "scanner_timeout",
			ObservedAt:    failedAt,
		}},
	}

	var row collector.RuntimeStatus
	for _, candidate := range collector.RuntimeStatuses(evidence) {
		if candidate.InstanceID == "scanner-example" {
			row = candidate
		}
	}
	if row.Health != "degraded" || !row.LastFailureTerminal || !row.LastFailureAt.Equal(failedAt) {
		t.Fatalf("runtime row = %+v, want degraded with a terminal failure at %s", row, failedAt)
	}
	if row.LastFailureClass != collector.UnknownFailureToken || row.LastFailureOperation != collector.UnknownFailureToken {
		t.Fatalf("class/operation = %q/%q, want unknown/unknown", row.LastFailureClass, row.LastFailureOperation)
	}

	for _, proof := range collector.PromotionProofs(evidence, collector.PromotionOptions{AsOf: now}) {
		if proof.InstanceID != "scanner-example" {
			continue
		}
		if proof.PromotionState != collector.PromotionFailed {
			t.Fatalf("PromotionState = %q, want %q", proof.PromotionState, collector.PromotionFailed)
		}
		return
	}
	t.Fatal("no promotion proof for scanner-example")
}
