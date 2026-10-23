// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// TestScopedCollectorRuntimeStatusesWithholdTerminalFailureFields pins the
// scoped-caller rule documented for GET /api/v0/status/collectors: a scoped
// caller sees the per-kind health aggregate, and a degraded instance carries
// no last_failure_* key.
func TestScopedCollectorRuntimeStatusesWithholdTerminalFailureFields(t *testing.T) {
	t.Parallel()

	failedAt := time.Date(2026, time.May, 1, 10, 0, 0, 0, time.UTC)
	rows := []collector.RuntimeStatus{{
		InstanceID:            "collector-jira",
		CollectorKind:         "jira",
		RuntimeMode:           "claim_driven",
		StatusCategory:        "coordinator_managed",
		Health:                "degraded",
		CoordinatorRegistered: true,
		Enabled:               true,
		LastFailureClass:      "auth_denied",
		LastFailureOperation:  "list_issues",
		LastFailureTerminal:   true,
		LastFailureAt:         failedAt,
	}}

	got := scopedCollectorRuntimeStatusesToSlice(rows)
	if len(got) != 1 {
		t.Fatalf("scoped aggregates = %d, want 1", len(got))
	}
	if health := got[0]["health"]; health != "degraded" {
		t.Fatalf("scoped health = %v, want degraded", health)
	}
	for key := range got[0] {
		if strings.HasPrefix(key, "last_failure_") || key == "instance_id" {
			t.Errorf("scoped aggregate carries %q; the failure fields and instance id must be withheld", key)
		}
	}

	// The unscoped projection of the same row keeps the fields, so the test
	// above fails for the intended reason and not because the fixture is empty.
	full := collectorRuntimeStatusToMap(rows[0])
	if full["last_failure_class"] != "auth_denied" {
		t.Fatalf("unscoped last_failure_class = %v, want auth_denied", full["last_failure_class"])
	}
}

// TestCollectorReadinessScopedCallerKeepsTerminalFailureFields pins the
// readiness half of the scoped rule: a scoped caller keeps the failure class,
// operation, time, and blocker, and only instance_id is withheld.
func TestCollectorReadinessScopedCallerKeepsTerminalFailureFields(t *testing.T) {
	t.Parallel()

	failedAt := time.Date(2026, time.May, 1, 10, 0, 0, 0, time.UTC)
	blocker := "terminal fetch failure: auth_denied (list_issues)"
	proofs := []collector.PromotionProof{{
		CollectorKind:        "jira",
		InstanceID:           "collector-jira",
		PromotionState:       collector.PromotionFailed,
		Health:               "degraded",
		Blockers:             []string{blocker},
		LastFailureClass:     "auth_denied",
		LastFailureOperation: "list_issues",
		LastFailureAt:        failedAt,
	}}

	scoped := collectorReadinessEntries(proofs, true)
	if len(scoped) != 1 {
		t.Fatalf("scoped entries = %d, want 1", len(scoped))
	}
	entry := scoped[0]
	if entry.InstanceID != "" {
		t.Errorf("scoped instance id = %q, want empty (redacted)", entry.InstanceID)
	}
	if entry.LastFailureClass != "auth_denied" {
		t.Errorf("scoped last_failure_class = %q, want auth_denied", entry.LastFailureClass)
	}
	if entry.LastFailureOperation != "list_issues" {
		t.Errorf("scoped last_failure_operation = %q, want list_issues", entry.LastFailureOperation)
	}
	if entry.LastFailureAt == nil {
		t.Error("scoped last_failure_at is empty, want the failure time")
	}
	if !slices.Contains(entry.Blockers, blocker) {
		t.Errorf("scoped blockers = %v, want %q", entry.Blockers, blocker)
	}
}
