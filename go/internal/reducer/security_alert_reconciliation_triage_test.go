// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSecurityAlertReconciliationsExplainsTriageOutcomes(t *testing.T) {
	t.Parallel()

	repoID := "repo://github/example-org/payments-api"
	packageID := "npm://registry.npmjs.org/left-pad"
	staleObserved := time.Date(2026, time.June, 6, 10, 0, 0, 0, time.UTC)
	envelopes := []facts.Envelope{
		securityAlertEnvelope("alert-matched", repoID, map[string]any{
			"provider":              "github_dependabot",
			"provider_alert_number": int64(1),
			"provider_state":        "open",
			"package_id":            packageID,
			"ecosystem":             "npm",
			"package_name":          "left-pad",
			"manifest_path":         "package-lock.json",
			"cve_ids":               []string{"CVE-2026-1001"},
		}),
		securityAlertEnvelope("alert-provider-only", repoID, map[string]any{
			"provider":              "github_dependabot",
			"provider_alert_number": int64(2),
			"provider_state":        "open",
			"package_id":            "npm://registry.npmjs.org/no-owned-evidence",
			"ecosystem":             "npm",
			"package_name":          "no-owned-evidence",
			"manifest_path":         "package-lock.json",
			"cve_ids":               []string{"CVE-2026-1002"},
		}),
		securityAlertEnvelope("alert-stale", repoID, map[string]any{
			"provider":              "github_dependabot",
			"provider_alert_number": int64(3),
			"provider_state":        "open",
			"package_id":            packageID,
			"ecosystem":             "npm",
			"package_name":          "left-pad",
			"manifest_path":         "old-package-lock.json",
			"updated_at":            "2026-06-01T00:00:00Z",
			"cve_ids":               []string{"CVE-2026-1003"},
		}),
		securityAlertEnvelope("alert-unsupported", repoID, map[string]any{
			"provider":              "github_dependabot",
			"provider_alert_number": int64(4),
			"provider_state":        "open",
			"package_id":            "pkg:unsupported/example",
			"ecosystem":             "unsupported",
			"package_name":          "example",
			"manifest_path":         "packages.lock",
			"cve_ids":               []string{"CVE-2026-1004"},
		}),
		packageConsumptionCorrelationEnvelope("consume-exact", repoID, packageID, "package-lock.json"),
		supplyChainImpactFindingEnvelope("impact-matched", repoID, packageID, "CVE-2026-1001", "affected_exact"),
	}
	envelopes[len(envelopes)-2].ObservedAt = staleObserved

	decisions := indexSecurityAlertDecisions(BuildSecurityAlertReconciliations(envelopes))

	assertSecurityAlertTriage(t, decisions["alert-matched"], SecurityAlertReconciliationMatched, "matched_exact_impact", nil)
	assertSecurityAlertTriage(t, decisions["alert-provider-only"], SecurityAlertReconciliationProviderOnly, "owned_dependency_missing", []SecurityAlertReconciliationMissingEvidence{
		{Kind: "owned_dependency", Reason: "no_owned_dependency_evidence"},
	})
	assertSecurityAlertTriage(t, decisions["alert-stale"], SecurityAlertReconciliationStale, "provider_alert_stale", []SecurityAlertReconciliationMissingEvidence{
		{Kind: "current_manifest", Reason: "provider_manifest_no_longer_observed", EvidenceID: "consume-exact"},
	})
	assertSecurityAlertTriage(t, decisions["alert-unsupported"], SecurityAlertReconciliationUnsupported, "unsupported_ecosystem", []SecurityAlertReconciliationMissingEvidence{
		{Kind: "ecosystem_matcher", Reason: "unsupported_ecosystem"},
	})
	if got, want := decisions["alert-matched"].EshuImpactFindingID, "impact-matched"; got != want {
		t.Fatalf("matched EshuImpactFindingID = %q, want %q", got, want)
	}
}

func TestBuildSecurityAlertReconciliationsExplainsAmbiguousOwnedEvidence(t *testing.T) {
	t.Parallel()

	alert := securityAlertEnvelope("alert-ambiguous", "security-alert:github:example-org/payments-api", map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(5),
		"provider_state":        "open",
		"package_id":            "npm://registry.npmjs.org/left-pad",
		"ecosystem":             "npm",
		"package_name":          "left-pad",
		"manifest_path":         "package-lock.json",
		"cve_ids":               []string{"CVE-2026-1005"},
	})
	first := packageConsumptionCorrelationEnvelope(
		"consume-ambiguous-a",
		"repo://github/example-org/payments-api-a",
		"npm://registry.npmjs.org/left-pad",
		"package-lock.json",
	)
	first.Payload["repository_name"] = "payments-api"
	second := packageConsumptionCorrelationEnvelope(
		"consume-ambiguous-b",
		"repo://github/example-org/payments-api-b",
		"npm://registry.npmjs.org/left-pad",
		"package-lock.json",
	)
	second.Payload["repository_name"] = "payments-api"

	decisions := BuildSecurityAlertReconciliations([]facts.Envelope{alert, first, second})
	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	assertSecurityAlertTriage(t, decisions[0], SecurityAlertReconciliationAmbiguous, "owned_dependency_ambiguous", []SecurityAlertReconciliationMissingEvidence{
		{Kind: "owned_dependency", Reason: "multiple_repository_candidates"},
	})
}

func TestBuildSecurityAlertReconciliationsKeepsOwnedEvidenceForUnsupportedEcosystem(t *testing.T) {
	t.Parallel()

	repoID := "repo://github/example-org/payments-api"
	packageID := "unsupported://packages/example"
	envelopes := []facts.Envelope{
		securityAlertEnvelope("alert-unsupported-owned", repoID, map[string]any{
			"provider":              "github_dependabot",
			"provider_alert_number": int64(6),
			"provider_state":        "open",
			"package_id":            packageID,
			"ecosystem":             "unsupported",
			"package_name":          "example",
			"manifest_path":         "packages.lock",
			"cve_ids":               []string{"CVE-2026-1006"},
		}),
		packageConsumptionCorrelationEnvelope("consume-unsupported-owned", repoID, packageID, "packages.lock"),
	}

	decisions := indexSecurityAlertDecisions(BuildSecurityAlertReconciliations(envelopes))
	decision := decisions["alert-unsupported-owned"]
	assertSecurityAlertTriage(t, decision, SecurityAlertReconciliationUnsupported, "unsupported_ecosystem", []SecurityAlertReconciliationMissingEvidence{
		{Kind: "ecosystem_matcher", Reason: "unsupported_ecosystem"},
	})
	if got, want := decision.DependencyEvidenceID, "consume-unsupported-owned"; got != want {
		t.Fatalf("DependencyEvidenceID = %q, want %q", got, want)
	}
	wantEvidence := []string{"alert-unsupported-owned", "consume-unsupported-owned"}
	if !reflect.DeepEqual(decision.EvidenceFactIDs, wantEvidence) {
		t.Fatalf("EvidenceFactIDs = %#v, want %#v", decision.EvidenceFactIDs, wantEvidence)
	}
}

func TestBuildSecurityAlertReconciliationsTreatsOSAliasesAsSupported(t *testing.T) {
	t.Parallel()

	repoID := "repo://github/example-org/payments-api"
	packageID := "pkg:apk/alpine/openssl"
	envelopes := []facts.Envelope{
		securityAlertEnvelope("alert-os-provider-only", repoID, map[string]any{
			"provider":              "github_dependabot",
			"provider_alert_number": int64(7),
			"provider_state":        "open",
			"package_id":            "pkg:apk/alpine/busybox",
			"ecosystem":             "apk",
			"package_name":          "busybox",
			"manifest_path":         "rootfs/apk/installed",
			"cve_ids":               []string{"CVE-2026-1007"},
		}),
		securityAlertEnvelope("alert-os-owned", repoID, map[string]any{
			"provider":              "github_dependabot",
			"provider_alert_number": int64(8),
			"provider_state":        "open",
			"package_id":            packageID,
			"ecosystem":             "apk",
			"package_name":          "openssl",
			"manifest_path":         "rootfs/apk/installed",
			"cve_ids":               []string{"CVE-2026-1008"},
		}),
		packageConsumptionCorrelationEnvelope("consume-os-owned", repoID, packageID, "rootfs/apk/installed"),
	}

	decisions := indexSecurityAlertDecisions(BuildSecurityAlertReconciliations(envelopes))

	assertSecurityAlertTriage(t, decisions["alert-os-provider-only"], SecurityAlertReconciliationProviderOnly, "owned_dependency_missing", []SecurityAlertReconciliationMissingEvidence{
		{Kind: "owned_dependency", Reason: "no_owned_dependency_evidence"},
	})
	assertSecurityAlertTriage(t, decisions["alert-os-owned"], SecurityAlertReconciliationUnmatched, "impact_finding_missing", []SecurityAlertReconciliationMissingEvidence{
		{Kind: "impact_finding", Reason: "no_matching_impact_finding", EvidenceID: "consume-os-owned"},
	})
}

func indexSecurityAlertDecisions(
	decisions []SecurityAlertReconciliationDecision,
) map[string]SecurityAlertReconciliationDecision {
	out := make(map[string]SecurityAlertReconciliationDecision, len(decisions))
	for _, decision := range decisions {
		out[decision.ProviderAlertFactID] = decision
	}
	return out
}

func assertSecurityAlertTriage(
	t *testing.T,
	decision SecurityAlertReconciliationDecision,
	wantStatus SecurityAlertReconciliationStatus,
	wantCode string,
	wantMissing []SecurityAlertReconciliationMissingEvidence,
) {
	t.Helper()
	if got := decision.Status; got != wantStatus {
		t.Fatalf("Status = %q, want %q for %#v", got, wantStatus, decision)
	}
	if got := decision.ReasonCode; got != wantCode {
		t.Fatalf("ReasonCode = %q, want %q for %#v", got, wantCode, decision)
	}
	if !reflect.DeepEqual(decision.MissingEvidence, wantMissing) {
		t.Fatalf("MissingEvidence = %#v, want %#v", decision.MissingEvidence, wantMissing)
	}
}
