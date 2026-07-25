// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

func TestSupplyChainImpactDiagnosticSignalsSkippedKey(t *testing.T) {
	signals := supplyChainImpactDiagnosticSignals(
		1,  // scopeFacts
		1,  // repositoryFacts
		0,  // manifestDependencyFacts
		0,  // activeEvidenceFacts
		1,  // osPackageAdvisoryFacts
		3,  // osPackageAdvisoryTargetsSkipped
		1,  // scannerAnalysisScopeFacts
		1,  // resolvedDigestEvidenceFacts
		0,  // pythonReachabilityFacts
		0,  // jvmReachabilityFacts
		1,  // postScopeFacts
		false, // securityAlertScopingApplied
		0,     // securityAlertScopedOutFacts
		1,     // findings
		false, // activeEvidenceTruncated
		1,     // writtenRows
	)
	value, ok := signals["os_package_advisory_targets_skipped"]
	if !ok {
		t.Fatal("os_package_advisory_targets_skipped key missing from diagnostic signals")
	}
	if value != 3 {
		t.Errorf("os_package_advisory_targets_skipped = %v, want 3", value)
	}
}
