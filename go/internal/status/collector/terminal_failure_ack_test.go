// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector_test

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

func acknowledgedFailureEvidence(now time.Time) (collector.Evidence, time.Time, time.Time) {
	failedAt := now.Add(-5 * time.Minute)
	acknowledgedAt := now.Add(-2 * time.Minute)
	return collector.Evidence{
		AsOf: now,
		Instances: []collector.InstanceSummary{{
			InstanceID:     "security-alert-example",
			CollectorKind:  "security_alert",
			Mode:           "continuous",
			Enabled:        true,
			ClaimsEnabled:  true,
			LastObservedAt: now.Add(-time.Minute),
			UpdatedAt:      now.Add(-time.Minute),
		}},
		TerminalFailures: []collector.TerminalFailure{{
			CollectorKind:      "security_alert",
			InstanceID:         "security-alert-example",
			ScopeID:            "org:example",
			FailureClass:       "auth_denied",
			FailureOperation:   "list_org_alerts",
			ObservedAt:         failedAt,
			Acknowledged:       true,
			AcknowledgedAt:     acknowledgedAt,
			AcknowledgedReason: "org left the program",
		}},
	}, failedAt, acknowledgedAt
}

// TestRuntimeStatusesAcknowledgedFailureDoesNotDegrade pins the acknowledged
// reading (#8830): the failure stays visible with its bounded class, operation,
// and acknowledgement, but it no longer degrades the instance, so the instance
// is not shown as failing and is not shown as an instance with no failure.
func TestRuntimeStatusesAcknowledgedFailureDoesNotDegrade(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	evidence, failedAt, acknowledgedAt := acknowledgedFailureEvidence(now)

	var row collector.RuntimeStatus
	for _, candidate := range collector.RuntimeStatuses(evidence) {
		if candidate.InstanceID == "security-alert-example" {
			row = candidate
		}
	}
	if row.Health == "degraded" {
		t.Fatalf("health = %q, want an acknowledged failure not to degrade the instance", row.Health)
	}
	if row.LastFailureScopeID != "org:example" {
		t.Fatalf("scope = %q, want org:example", row.LastFailureScopeID)
	}
	if !row.LastFailureTerminal || !row.LastFailureAcknowledged ||
		row.LastFailureClass != "auth_denied" || row.LastFailureOperation != "list_org_alerts" ||
		!row.LastFailureAt.Equal(failedAt) || !row.LastFailureAcknowledgedAt.Equal(acknowledgedAt) ||
		row.LastFailureAcknowledgedReason != "org left the program" {
		t.Fatalf("row = %+v, want the failure kept visible as acknowledged", row)
	}
	if !strings.Contains(row.Detail, "acknowledged") || !strings.Contains(row.Detail, "auth_denied") {
		t.Fatalf("detail = %q, want it to name the acknowledged failure class", row.Detail)
	}

	projected := collector.RuntimeStatusesJSON([]collector.RuntimeStatus{row})[0]
	if projected.LastFailureScopeID != "org:example" || !projected.LastFailureAcknowledged || projected.LastFailureAcknowledgedAt == "" ||
		projected.LastFailureAcknowledgedReason != "org left the program" {
		t.Fatalf("projected = %+v, want the acknowledgement fields", projected)
	}
}

// TestRuntimeStatusesAcknowledgedFailureLeavesHealthAsIfNoFailure pins that an
// acknowledgement only suppresses the terminal failure: the instance reads
// exactly the health it has with no failure at all, and the same failure
// unacknowledged degrades it.
func TestRuntimeStatusesAcknowledgedFailureLeavesHealthAsIfNoFailure(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	acknowledged, _, _ := acknowledgedFailureEvidence(now)

	none := acknowledged
	none.TerminalFailures = nil
	unacknowledged := acknowledged
	unacknowledged.TerminalFailures = []collector.TerminalFailure{{
		CollectorKind: "security_alert", InstanceID: "security-alert-example",
		FailureClass: "auth_denied", FailureOperation: "list_org_alerts", ObservedAt: now.Add(-5 * time.Minute),
	}}

	healthOf := func(evidence collector.Evidence) collector.RuntimeStatus {
		t.Helper()
		rows := collector.RuntimeStatuses(evidence)
		if len(rows) != 1 {
			t.Fatalf("rows = %d, want 1", len(rows))
		}
		return rows[0]
	}
	if got, want := healthOf(acknowledged).Health, healthOf(none).Health; got != want {
		t.Fatalf("acknowledged health = %q, want %q (the health with no failure)", got, want)
	}
	degraded := healthOf(unacknowledged)
	if degraded.Health != "degraded" || degraded.LastFailureAcknowledged {
		t.Fatalf("unacknowledged row = %+v, want degraded and not acknowledged", degraded)
	}
}

// TestPromotionProofAcknowledgedFailureIsNotFailed pins the readiness reading:
// an acknowledged failure carries no failed state and no blocker, and the proof
// still names the failure and its acknowledgement, so it is not mistaken for an
// instance that never failed.
func TestPromotionProofAcknowledgedFailureIsNotFailed(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	evidence, failedAt, acknowledgedAt := acknowledgedFailureEvidence(now)

	for _, proof := range collector.PromotionProofs(evidence, collector.PromotionOptions{AsOf: now}) {
		if proof.InstanceID != "security-alert-example" {
			continue
		}
		if proof.PromotionState == collector.PromotionFailed {
			t.Fatalf("PromotionState = %q, want an acknowledged failure not to fail the proof", proof.PromotionState)
		}
		for _, blocker := range proof.Blockers {
			if strings.Contains(blocker, "terminal fetch failure") {
				t.Fatalf("blockers = %v, want no terminal fetch failure blocker once acknowledged", proof.Blockers)
			}
		}
		if proof.LastFailureScopeID != "org:example" || !proof.LastFailureAcknowledged || proof.LastFailureClass != "auth_denied" ||
			!proof.LastFailureAt.Equal(failedAt) || !proof.LastFailureAcknowledgedAt.Equal(acknowledgedAt) ||
			proof.LastFailureAcknowledgedReason != "org left the program" {
			t.Fatalf("proof = %+v, want the acknowledged failure on the proof", proof)
		}
		return
	}
	t.Fatal("no promotion proof for security-alert-example")
}
