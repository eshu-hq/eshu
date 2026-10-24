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
			CollectorKind:    "security_alert",
			InstanceID:       "security-alert-example",
			ScopeID:          "org:example",
			WorkItemID:       "work-1",
			FailureClass:     "auth_denied",
			FailureOperation: "list_org_alerts",
			ObservedAt:       failedAt,
			Acknowledged:     true,
			AcknowledgedAt:   acknowledgedAt,
		}},
	}, failedAt, acknowledgedAt
}

func runtimeRow(t *testing.T, evidence collector.Evidence) collector.RuntimeStatus {
	t.Helper()
	rows := collector.RuntimeStatuses(evidence)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	return rows[0]
}

// TestRuntimeStatusesAcknowledgedFailureReadsPartial pins the AC3 reading
// (#8830, arbiter ruling): an acknowledged failure is a distinct sub-record, not
// LastFailure*; health is partial (never healthy or registered, never degraded),
// and the unresolved-failure fields stay empty.
func TestRuntimeStatusesAcknowledgedFailureReadsPartial(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	evidence, failedAt, acknowledgedAt := acknowledgedFailureEvidence(now)

	row := runtimeRow(t, evidence)
	if row.Health != "partial" {
		t.Fatalf("health = %q, want partial", row.Health)
	}
	if row.LastFailureTerminal || row.LastFailureClass != "" || !row.LastFailureAt.IsZero() {
		t.Fatalf("row = %+v, want LastFailure* empty for an acknowledged-only instance", row)
	}
	ack := row.AcknowledgedFailure
	if ack == nil || ack.FailureClass != "auth_denied" || ack.FailureOperation != "list_org_alerts" ||
		!ack.FailedAt.Equal(failedAt) || !ack.AcknowledgedAt.Equal(acknowledgedAt) {
		t.Fatalf("AcknowledgedFailure = %+v, want the bounded failure with both times", ack)
	}
	if !strings.Contains(row.Detail, "acknowledged") || !strings.Contains(row.Detail, "auth_denied") {
		t.Fatalf("detail = %q, want it to name the acknowledged failure class", row.Detail)
	}

	projected := collector.RuntimeStatusesJSON([]collector.RuntimeStatus{row})[0]
	if projected.AcknowledgedFailure == nil || projected.AcknowledgedFailure.FailureClass != "auth_denied" ||
		projected.AcknowledgedFailure.AcknowledgedAt == "" || projected.LastFailureClass != "" {
		t.Fatalf("projected = %+v, want the acknowledged sub-record and no last_failure_*", projected)
	}
}

// TestRuntimeStatusesUnacknowledgedFailureOnAnotherScopeStaysDegraded pins that
// an acknowledged failure never lifts a real one: the reader returns both rows
// for an instance with an acknowledged scope and an unacknowledged scope, and
// the row stays degraded with both records.
func TestRuntimeStatusesUnacknowledgedFailureOnAnotherScopeStaysDegraded(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	evidence, _, _ := acknowledgedFailureEvidence(now)
	open := collector.TerminalFailure{
		CollectorKind: "security_alert", InstanceID: "security-alert-example",
		ScopeID: "org:other", FailureClass: "not_found", FailureOperation: "list_org_alerts",
		ObservedAt: now.Add(-9 * time.Minute),
	}
	for name, failures := range map[string][]collector.TerminalFailure{
		"unacknowledged first": {open, evidence.TerminalFailures[0]},
		"acknowledged first":   {evidence.TerminalFailures[0], open},
	} {
		evidence.TerminalFailures = failures
		row := runtimeRow(t, evidence)
		if row.Health != "degraded" || !row.LastFailureTerminal || row.LastFailureClass != "not_found" {
			t.Fatalf("%s: row = %+v, want degraded by the unacknowledged not_found failure", name, row)
		}
		if row.AcknowledgedFailure == nil || row.AcknowledgedFailure.FailureClass != "auth_denied" {
			t.Fatalf("%s: AcknowledgedFailure = %+v, want the acknowledged auth_denied kept", name, row.AcknowledgedFailure)
		}
		if !strings.HasPrefix(row.Detail, "terminal fetch failure: not_found") {
			t.Fatalf("%s: detail = %q, want the unacknowledged failure's detail", name, row.Detail)
		}
	}
}

// TestPromotionProofAcknowledgedFailureIsPartialWithBlocker pins the readiness
// reading: an acknowledged failure is promotion_state partial with the
// acknowledged blocker, never implemented and never failed.
func TestPromotionProofAcknowledgedFailureIsPartialWithBlocker(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	evidence, failedAt, acknowledgedAt := acknowledgedFailureEvidence(now)

	for _, proof := range collector.PromotionProofs(evidence, collector.PromotionOptions{AsOf: now}) {
		if proof.InstanceID != "security-alert-example" {
			continue
		}
		if proof.PromotionState != collector.PromotionPartial {
			t.Fatalf("PromotionState = %q, want partial", proof.PromotionState)
		}
		if len(proof.Blockers) != 1 || proof.Blockers[0] != "terminal fetch failure acknowledged: auth_denied (list_org_alerts)" {
			t.Fatalf("blockers = %v, want the acknowledged blocker", proof.Blockers)
		}
		if proof.LastFailureClass != "" {
			t.Fatalf("proof carries LastFailureClass %q for an acknowledged-only instance", proof.LastFailureClass)
		}
		ack := proof.AcknowledgedFailure
		if ack == nil || ack.FailureClass != "auth_denied" || !ack.FailedAt.Equal(failedAt) || !ack.AcknowledgedAt.Equal(acknowledgedAt) {
			t.Fatalf("proof AcknowledgedFailure = %+v, want the failure with both times", ack)
		}
		return
	}
	t.Fatal("no promotion proof for security-alert-example")
}

// TestPromotionProofUnacknowledgedFailureOutranksAcknowledged pins the
// precedence: an unacknowledged failure still fails the proof with its blocker.
func TestPromotionProofUnacknowledgedFailureOutranksAcknowledged(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	evidence, _, _ := acknowledgedFailureEvidence(now)
	evidence.TerminalFailures = append(evidence.TerminalFailures, collector.TerminalFailure{
		CollectorKind: "security_alert", InstanceID: "security-alert-example",
		FailureClass: "not_found", FailureOperation: "list_org_alerts", ObservedAt: now.Add(-9 * time.Minute),
	})
	for _, proof := range collector.PromotionProofs(evidence, collector.PromotionOptions{AsOf: now}) {
		if proof.InstanceID != "security-alert-example" {
			continue
		}
		if proof.PromotionState != collector.PromotionFailed ||
			len(proof.Blockers) != 1 || proof.Blockers[0] != "terminal fetch failure: not_found (list_org_alerts)" {
			t.Fatalf("proof = %+v, want failed with the unacknowledged blocker", proof)
		}
		return
	}
	t.Fatal("no promotion proof for security-alert-example")
}
