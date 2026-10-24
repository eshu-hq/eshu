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

// TestScopedCallersKeepRedactionForPerKindFailureClasses runs a class only a
// non-security-alert kind writes through the status leaf and then through both
// scoped projections (#8896). The collectors route must still withhold every
// last_failure_* key and the instance id; the readiness route must keep the
// bounded class and blocker while withholding only instance_id. The unscoped
// view is asserted first so the redaction checks cannot pass on an empty row.
func TestScopedCallersKeepRedactionForPerKindFailureClasses(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		kind  string
		class string
	}{
		{kind: "jira", class: "permission_hidden"},
		{kind: "aws", class: "stale_fence"},
		{kind: "oci_registry", class: "registry_auth_denied"},
		{kind: "vendor_kind", class: "vendor.quota-exceeded"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			wantClass := tc.class
			if tc.kind == "vendor_kind" {
				wantClass = collector.ExtensionDefinedFailureToken
			}
			evidence := collector.Evidence{
				AsOf: now,
				Instances: []collector.InstanceSummary{{
					InstanceID:     "instance-1",
					CollectorKind:  tc.kind,
					Mode:           "continuous",
					Enabled:        true,
					ClaimsEnabled:  true,
					LastObservedAt: now.Add(-time.Minute),
					UpdatedAt:      now.Add(-time.Minute),
				}},
				TerminalFailures: []collector.TerminalFailure{{
					CollectorKind:    tc.kind,
					InstanceID:       "instance-1",
					FailureClass:     tc.class,
					FailureOperation: "collect_failure",
					ObservedAt:       now.Add(-time.Minute),
				}},
			}

			var row collector.RuntimeStatus
			for _, candidate := range collector.RuntimeStatuses(evidence) {
				if candidate.InstanceID == "instance-1" {
					row = candidate
				}
			}
			if full := collectorRuntimeStatusToMap(row); full["last_failure_class"] != wantClass {
				t.Fatalf("unscoped last_failure_class = %v, want %s", full["last_failure_class"], wantClass)
			}
			scopedRows := scopedCollectorRuntimeStatusesToSlice([]collector.RuntimeStatus{row})
			if len(scopedRows) != 1 || scopedRows[0]["health"] != "degraded" {
				t.Fatalf("scoped rows = %v, want one degraded aggregate", scopedRows)
			}
			for key := range scopedRows[0] {
				if strings.HasPrefix(key, "last_failure_") || key == "instance_id" {
					t.Errorf("scoped aggregate carries %q, want it withheld", key)
				}
			}

			var proofs []collector.PromotionProof
			for _, proof := range collector.PromotionProofs(evidence, collector.PromotionOptions{AsOf: now}) {
				if proof.InstanceID == "instance-1" {
					proofs = append(proofs, proof)
				}
			}
			entries := collectorReadinessEntries(proofs, true)
			if len(entries) != 1 {
				t.Fatalf("scoped readiness entries = %d, want 1", len(entries))
			}
			entry := entries[0]
			blocker := "terminal fetch failure: " + wantClass + " (collect_failure)"
			if entry.InstanceID != "" {
				t.Errorf("scoped instance id = %q, want redacted", entry.InstanceID)
			}
			if entry.LastFailureClass != wantClass {
				t.Errorf("scoped last_failure_class = %q, want %q", entry.LastFailureClass, wantClass)
			}
			if !slices.Contains(entry.Blockers, blocker) {
				t.Errorf("scoped blockers = %v, want %q", entry.Blockers, blocker)
			}
		})
	}
}
