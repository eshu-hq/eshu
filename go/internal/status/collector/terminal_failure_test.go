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

// TestRuntimeStatusesRunnerTerminalClassesStayBounded pins that every class
// the claim runner itself persists on a terminal row reads as that class, not
// as unknown (#8803). The runner writes attempt_budget_exhausted,
// identity_mismatch, collect_failure, and commit_failure without any provider
// class, and retryable when the attempt budget runs out on a transient
// provider failure.
func TestRuntimeStatusesRunnerTerminalClassesStayBounded(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		class     string
		operation string
	}{
		{class: "attempt_budget_exhausted", operation: "collect_failure"},
		{class: "identity_mismatch", operation: "identity_mismatch"},
		{class: "collect_failure", operation: "collect_failure"},
		{class: "commit_failure", operation: "commit_failure"},
		{class: "retryable", operation: "collect_failure"},
	}
	for _, tc := range cases {
		t.Run(tc.class, func(t *testing.T) {
			t.Parallel()
			evidence := collector.Evidence{
				AsOf:      now,
				Instances: []collector.InstanceSummary{runnerInstance(now, true)},
				TerminalFailures: []collector.TerminalFailure{{
					CollectorKind:    "security_alert",
					InstanceID:       "runner-example",
					FailureClass:     tc.class,
					FailureOperation: tc.operation,
					ObservedAt:       now.Add(-time.Minute),
				}},
			}
			row := runnerRow(t, evidence)
			if row.Health != "degraded" || row.LastFailureClass != tc.class || row.LastFailureOperation != tc.operation {
				t.Fatalf("runtime row = %+v, want degraded with %s (%s)", row, tc.class, tc.operation)
			}
			want := "terminal fetch failure: " + tc.class + " (" + tc.operation + ")"
			if row.Detail != want {
				t.Fatalf("Detail = %q, want %q", row.Detail, want)
			}
		})
	}
}

// TestRuntimeStatusesDisabledInstanceCarriesNoFailureFields pins that a
// failure reaching a disabled instance leaves the row without failure fields,
// so the JSON payload matches the documented "disabled instances report no
// failure fields" contract even if a caller passes such a failure.
func TestRuntimeStatusesDisabledInstanceCarriesNoFailureFields(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	evidence := collector.Evidence{
		AsOf:      now,
		Instances: []collector.InstanceSummary{runnerInstance(now, false)},
		TerminalFailures: []collector.TerminalFailure{{
			CollectorKind:    "security_alert",
			InstanceID:       "runner-example",
			FailureClass:     "auth_denied",
			FailureOperation: "list_org_alerts",
			ObservedAt:       now.Add(-time.Minute),
		}},
	}
	row := runnerRow(t, evidence)
	if row.Health != "disabled" || row.LastFailureTerminal || row.LastFailureClass != "" ||
		row.LastFailureOperation != "" || !row.LastFailureAt.IsZero() {
		t.Fatalf("disabled row = %+v, want disabled with no failure fields", row)
	}
	for _, projected := range collector.RuntimeStatusesJSON([]collector.RuntimeStatus{row}) {
		if projected.LastFailureClass != "" || projected.LastFailureAt != "" || projected.LastFailureTerminal {
			t.Fatalf("disabled JSON = %+v, want no failure fields", projected)
		}
	}
}

func runnerInstance(now time.Time, enabled bool) collector.InstanceSummary {
	return collector.InstanceSummary{
		InstanceID:     "runner-example",
		CollectorKind:  "security_alert",
		Mode:           "continuous",
		Enabled:        enabled,
		ClaimsEnabled:  true,
		LastObservedAt: now.Add(-time.Minute),
		UpdatedAt:      now.Add(-time.Minute),
	}
}

func runnerRow(t *testing.T, evidence collector.Evidence) collector.RuntimeStatus {
	t.Helper()
	for _, candidate := range collector.RuntimeStatuses(evidence) {
		if candidate.InstanceID == "runner-example" {
			return candidate
		}
	}
	t.Fatal("no runtime row for runner-example")
	return collector.RuntimeStatus{}
}
