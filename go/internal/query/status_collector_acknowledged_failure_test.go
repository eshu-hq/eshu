// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

func acknowledgedRuntimeRow() (collector.RuntimeStatus, time.Time, time.Time) {
	failedAt := time.Date(2026, time.October, 24, 10, 0, 0, 0, time.UTC)
	acknowledgedAt := failedAt.Add(3 * time.Minute)
	return collector.RuntimeStatus{
		InstanceID:                    "collector-jira",
		CollectorKind:                 "jira",
		RuntimeMode:                   "claim_driven",
		StatusCategory:                "coordinator_managed",
		Health:                        "registered",
		CoordinatorRegistered:         true,
		Enabled:                       true,
		LastFailureClass:              "auth_denied",
		LastFailureOperation:          "list_issues",
		LastFailureTerminal:           true,
		LastFailureAt:                 failedAt,
		LastFailureScopeID:            "jira:site-a",
		LastFailureAcknowledged:       true,
		LastFailureAcknowledgedAt:     acknowledgedAt,
		LastFailureAcknowledgedReason: "site decommissioned",
	}, failedAt, acknowledgedAt
}

// TestCollectorStatusMapCarriesAcknowledgedFailure pins the collectors route
// projection (#8830): an acknowledged failure keeps its bounded class and
// operation, names the scope an operator acknowledges, and adds the
// acknowledgement flag, time, and reason. An unacknowledged failure carries
// the scope but none of the acknowledgement keys.
func TestCollectorStatusMapCarriesAcknowledgedFailure(t *testing.T) {
	t.Parallel()
	row, _, acknowledgedAt := acknowledgedRuntimeRow()

	got := collectorRuntimeStatusToMap(row)
	if got["last_failure_acknowledged"] != true ||
		got["last_failure_acknowledged_reason"] != "site decommissioned" ||
		got["last_failure_acknowledged_at"] != acknowledgedAt.Format(time.RFC3339) ||
		got["last_failure_scope_id"] != "jira:site-a" || got["last_failure_class"] != "auth_denied" {
		t.Fatalf("acknowledged map = %v, want the failure with its scope and acknowledgement", got)
	}

	row.LastFailureAcknowledged, row.LastFailureAcknowledgedAt, row.LastFailureAcknowledgedReason = false, time.Time{}, ""
	open := collectorRuntimeStatusToMap(row)
	if open["last_failure_scope_id"] != "jira:site-a" {
		t.Fatalf("open failure map = %v, want last_failure_scope_id", open)
	}
	for key := range open {
		if strings.HasPrefix(key, "last_failure_acknowledged") {
			t.Fatalf("unacknowledged failure carries %q", key)
		}
	}

	scoped := scopedCollectorRuntimeStatusesToSlice([]collector.RuntimeStatus{func() collector.RuntimeStatus {
		r, _, _ := acknowledgedRuntimeRow()
		return r
	}()})
	for key := range scoped[0] {
		if strings.HasPrefix(key, "last_failure_") {
			t.Fatalf("scoped aggregate carries %q; failure fields must stay withheld", key)
		}
	}
}

// TestCollectorReadinessEntryCarriesAcknowledgedFailure pins the readiness
// projection (#8830): an acknowledged failure reads implemented (not failed)
// but carries the failure, flag, time, and next action that says it is
// acknowledged, so it cannot be mistaken for an instance that never failed. A
// scoped caller keeps the flag and time and loses the scope id and the reason.
func TestCollectorReadinessEntryCarriesAcknowledgedFailure(t *testing.T) {
	t.Parallel()
	row, failedAt, acknowledgedAt := acknowledgedRuntimeRow()
	proofs := []collector.PromotionProof{{
		CollectorKind:                 row.CollectorKind,
		InstanceID:                    row.InstanceID,
		PromotionState:                collector.PromotionImplemented,
		Health:                        row.Health,
		LastFailureClass:              row.LastFailureClass,
		LastFailureOperation:          row.LastFailureOperation,
		LastFailureAt:                 failedAt,
		LastFailureScopeID:            row.LastFailureScopeID,
		LastFailureAcknowledged:       true,
		LastFailureAcknowledgedAt:     acknowledgedAt,
		LastFailureAcknowledgedReason: row.LastFailureAcknowledgedReason,
	}}

	full := collectorReadinessEntries(proofs, false)[0]
	if !full.LastFailureAcknowledged || full.LastFailureAcknowledgedAt == nil ||
		full.LastFailureAcknowledgedReason != "site decommissioned" || full.LastFailureScopeID != "jira:site-a" ||
		full.LastFailureClass != "auth_denied" || full.PromotionState != collector.PromotionImplemented {
		t.Fatalf("full entry = %+v, want an implemented entry carrying the acknowledged failure", full)
	}
	if !strings.Contains(full.RecommendedNextAction, "acknowledged") {
		t.Fatalf("next action = %q, want it to say the failure is acknowledged", full.RecommendedNextAction)
	}

	scoped := collectorReadinessEntries(proofs, true)[0]
	if !scoped.LastFailureAcknowledged || scoped.LastFailureAcknowledgedAt == nil || scoped.LastFailureClass != "auth_denied" {
		t.Fatalf("scoped entry = %+v, want the flag, time, and class", scoped)
	}
	if scoped.InstanceID != "" || scoped.LastFailureScopeID != "" || scoped.LastFailureAcknowledgedReason != "" {
		t.Fatalf("scoped entry = %+v, want instance id, scope id, and reason withheld", scoped)
	}
}
