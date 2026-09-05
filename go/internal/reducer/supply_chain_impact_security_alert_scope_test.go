// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
)

// TestSupplyChainImpactSecurityAlertScopingSurvivesAllMalformedAlerts is the
// codex P1 regression (security_alert_reconciliation_decode.go:74): when every
// security_alert.repository_alert in a security-alert-triggered
// supply_chain_impact intent is missing its required repository_id, the lenient
// pre-filter extractor (securityalert.ExtractProviderSecurityAlerts) must still
// return the alert so supplyChainImpactUsesSecurityAlertScope reports true and
// the evidence-scoping fence still narrows to the alert's package/ecosystem.
// The durable Handle path still quarantines the same fact (proven separately in
// securityalert's own package); only the non-durable scoping signal is proved
// here. This test stayed in the reducer root rather than moving with the rest
// of security_alert_reconciliation's tests (issue #6061) because it exercises
// supplyChainImpactUsesSecurityAlertScope, a supply_chain-owned root function a
// family subpackage cannot import.
//
// Pre-fix, the pure extractor dropped the malformed alert, len(alerts)==0,
// scoping was skipped, and unrelated active dependency/vulnerability facts could
// publish unscoped findings.
func TestSupplyChainImpactSecurityAlertScopingSurvivesAllMalformedAlerts(t *testing.T) {
	t.Parallel()

	malformed := securityAlertEnvelopeMissingRepositoryID("alert-malformed-scope", map[string]any{
		"provider":       "github_dependabot",
		"provider_state": "open",
		"package_id":     "npm://registry.npmjs.org/scoped-pkg",
		"package_name":   "scoped-pkg",
		"ecosystem":      "npm",
		"cve_ids":        []any{"CVE-2026-4242"},
	})

	// The lenient scoping/pre-filter extractor keeps the malformed alert (with an
	// empty RepositoryID but its package identity intact).
	lenient := securityalert.ExtractProviderSecurityAlerts([]facts.Envelope{malformed})
	if len(lenient) != 1 {
		t.Fatalf("lenient ExtractProviderSecurityAlerts kept %d alerts, want 1 (the malformed alert must still scope)", len(lenient))
	}
	if lenient[0].RepositoryID != "" {
		t.Fatalf("lenient alert RepositoryID = %q, want empty (malformed)", lenient[0].RepositoryID)
	}
	if lenient[0].PackageID != "npm://registry.npmjs.org/scoped-pkg" {
		t.Fatalf("lenient alert PackageID = %q, want the malformed alert's package identity", lenient[0].PackageID)
	}

	// The scoping gate must fire for a security-alert-triggered intent even when
	// every alert is malformed.
	intent := Intent{SourceSystem: "security_alert", ScopeID: "security-alert:github:acme/api"}
	if !supplyChainImpactUsesSecurityAlertScope(intent, []facts.Envelope{malformed}) {
		t.Fatal("supplyChainImpactUsesSecurityAlertScope = false for an all-malformed security-alert intent; the scoping fence was dropped")
	}

	// The strict durable path still quarantines the same fact (no double-count on
	// the pre-filter path, dead-letter on the durable path).
	_, quarantined, err := securityalert.ExtractProviderSecurityAlertsWithQuarantine([]facts.Envelope{malformed})
	if err != nil {
		t.Fatalf("strict extract error = %v, want nil", err)
	}
	if len(quarantined) != 1 || quarantined[0].Field != "repository_id" {
		t.Fatalf("strict path quarantined = %+v, want one repository_id quarantine", quarantined)
	}
}
