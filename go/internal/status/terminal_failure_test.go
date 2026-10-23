// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status_test

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

func terminalFailureReport(
	now time.Time,
	instance collector.InstanceSummary,
	failures ...collector.TerminalFailure,
) status.Report {
	return status.BuildReport(
		status.RawSnapshot{
			AsOf: now,
			Coordinator: &status.CoordinatorSnapshot{
				CollectorInstances:        []collector.InstanceSummary{instance},
				CollectorTerminalFailures: failures,
			},
		},
		status.DefaultOptions(),
	)
}

func securityAlertInstance(now time.Time) collector.InstanceSummary {
	return collector.InstanceSummary{
		InstanceID:     "security-alert-example",
		CollectorKind:  "security_alert",
		Mode:           "continuous",
		Enabled:        true,
		ClaimsEnabled:  true,
		LastObservedAt: now.Add(-time.Minute),
		UpdatedAt:      now.Add(-time.Minute),
	}
}

func runtimeFor(t *testing.T, report status.Report, instanceID string) collector.RuntimeStatus {
	t.Helper()
	for _, row := range collector.RuntimeStatuses(report.CollectorEvidence()) {
		if row.InstanceID == instanceID {
			return row
		}
	}
	t.Fatalf("no runtime row for %q", instanceID)
	return collector.RuntimeStatus{}
}

func proofFor(t *testing.T, report status.Report, instanceID string, now time.Time) collector.PromotionProof {
	t.Helper()
	for _, proof := range collector.PromotionProofs(report.CollectorEvidence(), collector.PromotionOptions{AsOf: now}) {
		if proof.InstanceID == instanceID {
			return proof
		}
	}
	t.Fatalf("no promotion proof for %q", instanceID)
	return collector.PromotionProof{}
}

func TestCollectorRuntimeHealthyInstanceCarriesNoFailure(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	report := terminalFailureReport(now, securityAlertInstance(now))

	row := runtimeFor(t, report, "security-alert-example")
	if row.Health != "registered" || row.LastFailureTerminal || row.LastFailureClass != "" {
		t.Fatalf("healthy row = %+v, want registered with no failure fields", row)
	}
	if proof := proofFor(t, report, "security-alert-example", now); proof.PromotionState == collector.PromotionFailed {
		t.Fatalf("healthy proof state = %q, want not failed", proof.PromotionState)
	}
}

func TestCollectorRuntimeTerminalFailureDegradesAndFailsPromotion(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	failedAt := now.Add(-2 * time.Minute)
	report := terminalFailureReport(now, securityAlertInstance(now), collector.TerminalFailure{
		CollectorKind:    "security_alert",
		InstanceID:       "security-alert-example",
		FailureClass:     "auth_denied",
		FailureOperation: "list_org_alerts",
		ObservedAt:       failedAt,
	})

	row := runtimeFor(t, report, "security-alert-example")
	if row.Health != "degraded" {
		t.Fatalf("Health = %q, want degraded", row.Health)
	}
	if row.LastFailureClass != "auth_denied" || row.LastFailureOperation != "list_org_alerts" ||
		!row.LastFailureTerminal || !row.LastFailureAt.Equal(failedAt) {
		t.Fatalf("failure fields = %+v, want auth_denied/list_org_alerts/terminal/%s", row, failedAt)
	}

	proof := proofFor(t, report, "security-alert-example", now)
	if proof.PromotionState != collector.PromotionFailed {
		t.Fatalf("PromotionState = %q, want %q", proof.PromotionState, collector.PromotionFailed)
	}
	want := "terminal provider failure: auth_denied (list_org_alerts)"
	if len(proof.Blockers) != 1 || proof.Blockers[0] != want {
		t.Fatalf("Blockers = %v, want [%q]", proof.Blockers, want)
	}
}

func TestCollectorRuntimeBoundsUnknownFailureClassAndOperation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	report := terminalFailureReport(now, securityAlertInstance(now), collector.TerminalFailure{
		CollectorKind:    "security_alert",
		InstanceID:       "security-alert-example",
		FailureClass:     "ghp_exampleSecretToken123",
		FailureOperation: "GET https://example.invalid/orgs/example-org?token=abc",
		ObservedAt:       now,
	})

	row := runtimeFor(t, report, "security-alert-example")
	if row.LastFailureClass != "unknown" || row.LastFailureOperation != "unknown" {
		t.Fatalf("class/operation = %q/%q, want unknown/unknown", row.LastFailureClass, row.LastFailureOperation)
	}
	if got := proofFor(t, report, "security-alert-example", now).Blockers; len(got) != 1 ||
		got[0] != "terminal provider failure: unknown (unknown)" {
		t.Fatalf("Blockers = %v, want unknown/unknown blocker", got)
	}
}

func TestCollectorTerminalFailureNeverLeaksSecretsIntoPayloads(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	report := terminalFailureReport(now, securityAlertInstance(now), collector.TerminalFailure{
		CollectorKind:    "security_alert",
		InstanceID:       "security-alert-example",
		FailureClass:     "ghp_exampleSecretToken123",
		FailureOperation: "Bearer ghp_exampleSecretToken123",
		ObservedAt:       now,
	})

	payload, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}
	text := status.RenderText(report)
	for _, body := range []string{string(payload), text} {
		if strings.Contains(body, "ghp_exampleSecretToken123") || strings.Contains(body, "Bearer") {
			t.Fatalf("rendered status leaked failure text: %s", body)
		}
	}
	if !strings.Contains(string(payload), `"last_failure_class": "unknown"`) {
		t.Fatalf("RenderJSON() = %s, want bounded last_failure_class", payload)
	}
}

func TestCollectorTerminalFailureKeepsDisabledInstanceDisabled(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	instance := securityAlertInstance(now)
	instance.Enabled = false
	report := terminalFailureReport(now, instance, collector.TerminalFailure{
		CollectorKind: "security_alert",
		InstanceID:    "security-alert-example",
		FailureClass:  "terminal",
		ObservedAt:    now,
	})

	row := runtimeFor(t, report, "security-alert-example")
	if row.Health != "disabled" {
		t.Fatalf("Health = %q, want disabled", row.Health)
	}
	if proof := proofFor(t, report, "security-alert-example", now); proof.PromotionState != collector.PromotionDisabled {
		t.Fatalf("PromotionState = %q, want disabled", proof.PromotionState)
	}
}

func TestCollectorTerminalFailureWithoutRegisteredInstanceIsIgnored(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	report := terminalFailureReport(now, securityAlertInstance(now), collector.TerminalFailure{
		CollectorKind: "security_alert",
		InstanceID:    "some-other-instance",
		FailureClass:  "terminal",
		ObservedAt:    now,
	})

	for _, row := range collector.RuntimeStatuses(report.CollectorEvidence()) {
		if row.InstanceID == "some-other-instance" {
			t.Fatalf("unregistered failure produced runtime row %+v", row)
		}
	}
}
