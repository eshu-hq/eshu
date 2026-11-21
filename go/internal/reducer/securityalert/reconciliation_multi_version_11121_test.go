// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalert

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestReconciliationReportsTheAffectedVersionOfAMultiVersionLockfile11121 pins
// the #11121 review addition: with one impact finding per installed version
// (#11113), an open alert whose lockfile holds an affected and a fixed version
// matched the first impact in envelope order, so the alert could read the
// fixed copy's not_affected_known_fixed. The decision must report the
// affected version's impact, and its dependency evidence must name that
// version's row, whatever the impact order.
func TestReconciliationReportsTheAffectedVersionOfAMultiVersionLockfile11121(t *testing.T) {
	t.Parallel()

	repoID := "repo://github/eshu-hq/eshu"
	packageID := "npm://registry.npmjs.org/fast-uri"
	alert := securityAlertEnvelope("alert-11121", repoID, map[string]any{
		"provider": "github_dependabot", "provider_alert_number": int64(11121), "provider_state": "open",
		"package_id": packageID, "ecosystem": "npm", "package_name": "fast-uri", "manifest_path": "package-lock.json",
		"ghsa_ids": []string{"GHSA-abcd-1234"}, "cve_ids": []string{"CVE-2026-0001"},
		"updated_at": "2026-05-23T10:15:00Z",
	})
	// The fixed row has the lower fact id, so the single consumption pick
	// lands on it.
	fixedRow := packageConsumptionCorrelationEnvelope("consume-a-fixed", repoID, packageID, "package-lock.json")
	fixedRow.Payload["installed_version"] = "3.1.0"
	affectedRow := packageConsumptionCorrelationEnvelope("consume-z-affected", repoID, packageID, "package-lock.json")
	affectedRow.Payload["installed_version"] = "3.0.3"
	fixedImpact := supplyChainImpactFindingEnvelope("impact-a-fixed", repoID, packageID, "CVE-2026-0001", "not_affected_known_fixed")
	fixedImpact.Payload["observed_version"] = "3.1.0"
	affectedImpact := supplyChainImpactFindingEnvelope("impact-z-affected", repoID, packageID, "CVE-2026-0001", "affected_exact")
	affectedImpact.Payload["observed_version"] = "3.0.3"

	for name, impacts := range map[string][]facts.Envelope{
		"fixed impact first":    {fixedImpact, affectedImpact},
		"affected impact first": {affectedImpact, fixedImpact},
	} {
		decisions := BuildSecurityAlertReconciliations(append([]facts.Envelope{alert, fixedRow, affectedRow}, impacts...), nil)
		if len(decisions) != 1 {
			t.Fatalf("%s: len(decisions) = %d, want 1", name, len(decisions))
		}
		decision := decisions[0]
		if decision.Status != SecurityAlertReconciliationMatched {
			t.Fatalf("%s: Status = %q, want %q", name, decision.Status, SecurityAlertReconciliationMatched)
		}
		if decision.EshuImpactStatus != "affected_exact" || decision.EshuImpactFindingID != "impact-z-affected" {
			t.Fatalf("%s: impact = %q (%s), want affected_exact (impact-z-affected): an affected installed version must not hide behind a fixed one",
				name, decision.EshuImpactStatus, decision.EshuImpactFindingID)
		}
		if decision.ObservedVersion != "3.0.3" || decision.DependencyEvidenceID != "consume-z-affected" {
			t.Fatalf("%s: dependency evidence = %q (%s), want 3.0.3 (consume-z-affected), the row of the reported impact",
				name, decision.ObservedVersion, decision.DependencyEvidenceID)
		}
	}
}
