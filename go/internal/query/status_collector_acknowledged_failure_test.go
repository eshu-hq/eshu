// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

func acknowledgedFailureFixture() (*collector.AcknowledgedFailure, time.Time, time.Time) {
	failedAt := time.Date(2026, time.October, 24, 10, 0, 0, 0, time.UTC)
	acknowledgedAt := failedAt.Add(3 * time.Minute)
	return &collector.AcknowledgedFailure{
		FailureClass: "auth_denied", FailureOperation: "list_issues", FailedAt: failedAt, AcknowledgedAt: acknowledgedAt,
	}, failedAt, acknowledgedAt
}

// TestCollectorStatusMapCarriesAcknowledgedFailure pins the collectors route
// projection (#8830): an acknowledged failure is a distinct acknowledged_failure
// object with bounded tokens and times, health reads partial (never healthy),
// the unresolved last_failure_* keys are absent, and a scoped caller's aggregate
// carries none of it.
func TestCollectorStatusMapCarriesAcknowledgedFailure(t *testing.T) {
	t.Parallel()
	ack, failedAt, acknowledgedAt := acknowledgedFailureFixture()
	row := collector.RuntimeStatus{
		InstanceID: "collector-jira", CollectorKind: "jira", RuntimeMode: "claim_driven",
		StatusCategory: "coordinator_managed", Health: "partial", CoordinatorRegistered: true, Enabled: true,
		AcknowledgedFailure: ack,
	}

	got := collectorRuntimeStatusToMap(row)
	object, ok := got["acknowledged_failure"].(map[string]any)
	if !ok || object["failure_class"] != "auth_denied" || object["failure_operation"] != "list_issues" ||
		object["failed_at"] != failedAt.Format(time.RFC3339) || object["acknowledged_at"] != acknowledgedAt.Format(time.RFC3339) {
		t.Fatalf("acknowledged_failure = %v, want the bounded failure with both times", got["acknowledged_failure"])
	}
	if got["health"] != "partial" {
		t.Fatalf("health = %v, want partial", got["health"])
	}
	for key := range got {
		if strings.HasPrefix(key, "last_failure_") {
			t.Fatalf("acknowledged-only instance carries %q; the unresolved-failure keys must stay absent", key)
		}
	}
	encoded, _ := json.Marshal(got)
	for _, secret := range []string{"reason", "scope", "work_item"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("collector status payload mentions %q: %s", secret, encoded)
		}
	}

	for key := range scopedCollectorRuntimeStatusesToSlice([]collector.RuntimeStatus{row})[0] {
		if strings.HasPrefix(key, "last_failure_") || strings.HasPrefix(key, "acknowledged_") {
			t.Fatalf("scoped aggregate carries %q; failure and acknowledgement fields must stay withheld", key)
		}
	}
}

// TestCollectorReadinessEntryCarriesAcknowledgedFailure pins the readiness
// projection (#8830): an acknowledged failure is promotion_state partial with
// the acknowledged blocker and a next action naming the lift conditions, never
// implemented; scoped callers keep the object (like last_failure_*) and lose
// only instance_id; the operator's reason is nowhere on the payload.
func TestCollectorReadinessEntryCarriesAcknowledgedFailure(t *testing.T) {
	t.Parallel()
	ack, failedAt, acknowledgedAt := acknowledgedFailureFixture()
	blocker := "terminal fetch failure acknowledged: auth_denied (list_issues)"
	proofs := []collector.PromotionProof{{
		CollectorKind: "jira", InstanceID: "collector-jira", PromotionState: collector.PromotionPartial,
		Health: "partial", Blockers: []string{blocker}, AcknowledgedFailure: ack,
	}}

	full := collectorReadinessEntries(proofs, false)[0]
	object := full.AcknowledgedFailure
	if object == nil || object.FailureClass != "auth_denied" || object.FailureOperation != "list_issues" ||
		object.FailedAt != nullableRFC3339(failedAt) || object.AcknowledgedAt != nullableRFC3339(acknowledgedAt) {
		t.Fatalf("full entry AcknowledgedFailure = %+v, want the bounded failure with both times", object)
	}
	if full.PromotionState != collector.PromotionPartial || len(full.Blockers) != 1 || full.Blockers[0] != blocker {
		t.Fatalf("full entry = %+v, want partial with the acknowledged blocker", full)
	}
	if full.LastFailureClass != "" {
		t.Fatalf("acknowledged-only entry carries last_failure_class %q", full.LastFailureClass)
	}
	for _, want := range []string{"acknowledged", "class changes", "completed work item"} {
		if !strings.Contains(full.RecommendedNextAction, want) {
			t.Fatalf("next action = %q, want it to mention %q", full.RecommendedNextAction, want)
		}
	}

	scoped := collectorReadinessEntries(proofs, true)[0]
	if scoped.AcknowledgedFailure == nil || scoped.InstanceID != "" {
		t.Fatalf("scoped entry = %+v, want the acknowledged failure kept and instance_id withheld", scoped)
	}
	for _, entry := range []collectorReadinessEntry{full, scoped} {
		encoded, _ := json.Marshal(entry)
		for _, secret := range []string{"reason", "scope_id", "work_item"} {
			if strings.Contains(string(encoded), secret) {
				t.Fatalf("readiness payload mentions %q: %s", secret, encoded)
			}
		}
	}
}
