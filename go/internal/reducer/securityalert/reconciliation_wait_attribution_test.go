// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalert

import "testing"

// TestWaitAttributionNamesThePackageThatActuallyWaits pins the log and error
// attribution of a defer or give-up: it must name a decision that waits on
// impact evidence (unmatched, owned dependency evidence, no impact evidence),
// never an earlier unmatched decision that already carries impact evidence.
func TestWaitAttributionNamesThePackageThatActuallyWaits(t *testing.T) {
	t.Parallel()

	decisions := []SecurityAlertReconciliationDecision{
		{
			Status:               SecurityAlertReconciliationUnmatched,
			PackageID:            "pkg:already-has-impact",
			DependencyEvidenceID: "dep-1",
			ImpactEvidenceID:     "impact-1",
		},
		{
			Status:               SecurityAlertReconciliationUnmatched,
			PackageID:            "pkg:waiting",
			DependencyEvidenceID: "dep-2",
		},
	}
	if got := firstWaitingPackage(decisions); got != "pkg:waiting" {
		t.Fatalf("firstWaitingPackage() = %q, want pkg:waiting", got)
	}
	if got := firstWaitingPackage(decisions[:1]); got != "" {
		t.Fatalf("firstWaitingPackage() with no waiting decision = %q, want empty", got)
	}
}
