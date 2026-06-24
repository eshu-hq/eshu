// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSecurityAlertReconciliationsUsesScopedNpmLockfileEvidence(t *testing.T) {
	t.Parallel()

	repoID := "repo://github/acme/api"
	packageID := "npm://registry.npmjs.org/@scope/provider-owned"
	observedAt := time.Date(2026, 6, 7, 9, 0, 0, 0, time.UTC)
	envelopes := []facts.Envelope{
		securityAlertEnvelope("alert-scoped-lockfile", repoID, map[string]any{
			"provider":              "github_dependabot",
			"provider_alert_number": int64(21),
			"provider_state":        "open",
			"package_id":            packageID,
			"package_name":          "@scope/provider-owned",
			"ecosystem":             "npm",
			"manifest_path":         "package-lock.json",
			"relationship":          "transitive",
			"cve_ids":               []string{"CVE-2026-1585"},
			"ghsa_ids":              []string{"GHSA-scoped-1585"},
			"vulnerable_range":      "<2.4.2",
			"patched_version":       "2.4.2",
		}),
		packageManifestDependencyFactWithMetadata(
			repoID,
			"api",
			"package-lock.json",
			"provider-owned",
			"npm",
			"2.4.1",
			observedAt,
			map[string]any{
				"namespace":         "@scope",
				"section":           "package-lock",
				"lockfile":          true,
				"dependency_path":   []any{"root", "@scope/provider-owned"},
				"dependency_depth":  2,
				"direct_dependency": false,
			},
		),
		supplyChainImpactFindingEnvelope("impact-scoped-lockfile", repoID, packageID, "CVE-2026-1585", "affected_exact"),
	}

	decisions := securityAlertDecisionsByFactID(BuildSecurityAlertReconciliations(envelopes))
	decision := decisions["alert-scoped-lockfile"]
	if got, want := decision.Status, SecurityAlertReconciliationMatched; got != want {
		t.Fatalf("Status = %q, want %q; reason=%q", got, want, decision.Reason)
	}
	if got, want := decision.ObservedVersion, "2.4.1"; got != want {
		t.Fatalf("ObservedVersion = %q, want scoped lockfile version %q", got, want)
	}
	if got, want := decision.RequestedRange, "2.4.1"; got != want {
		t.Fatalf("RequestedRange = %q, want scoped lockfile range %q", got, want)
	}
	if !strings.Contains(strings.Join(decision.EvidenceFactIDs, ","), "manifest-dep:"+repoID+":provider-owned") {
		t.Fatalf("EvidenceFactIDs = %#v, want scoped lockfile dependency evidence", decision.EvidenceFactIDs)
	}
}

func TestBuildSecurityAlertReconciliationsKeepsScopedNpmProviderOnlyWithoutOwnedEvidence(t *testing.T) {
	t.Parallel()

	repoID := "repo://github/acme/api"
	decisions := securityAlertDecisionsByFactID(BuildSecurityAlertReconciliations([]facts.Envelope{
		securityAlertEnvelope("alert-scoped-provider-only", repoID, map[string]any{
			"provider":              "github_dependabot",
			"provider_alert_number": int64(23),
			"provider_state":        "open",
			"package_id":            "npm://registry.npmjs.org/@scope/provider-owned",
			"package_name":          "@scope/provider-owned",
			"ecosystem":             "npm",
			"manifest_path":         "package-lock.json",
			"relationship":          "transitive",
			"cve_ids":               []string{"CVE-2026-1587"},
			"ghsa_ids":              []string{"GHSA-scoped-1587"},
			"vulnerable_range":      "<2.4.2",
			"patched_version":       "2.4.2",
		}),
	}))

	decision := decisions["alert-scoped-provider-only"]
	if got, want := decision.Status, SecurityAlertReconciliationProviderOnly; got != want {
		t.Fatalf("Status = %q, want fail-closed %q", got, want)
	}
	if got, want := decision.ReasonCode, "owned_dependency_missing"; got != want {
		t.Fatalf("ReasonCode = %q, want %q", got, want)
	}
	if got, want := len(decision.MissingEvidence), 1; got != want {
		t.Fatalf("len(MissingEvidence) = %d, want %d: %#v", got, want, decision.MissingEvidence)
	}
	missing := decision.MissingEvidence[0]
	if missing.Kind != "owned_dependency" || missing.Reason != "no_owned_dependency_evidence" {
		t.Fatalf("MissingEvidence = %#v, want owned dependency gap reason", decision.MissingEvidence)
	}
}

func TestBuildSupplyChainImpactFindingsUsesScopedProviderAlertLockfileEvidence(t *testing.T) {
	t.Parallel()

	repoID := "repo://github/acme/api"
	packageID := "npm://registry.npmjs.org/@scope/provider-owned"
	observedAt := time.Date(2026, 6, 7, 9, 30, 0, 0, time.UTC)
	alert := securityAlertEnvelope("alert-scoped-impact", repoID, map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(22),
		"provider_state":        "open",
		"package_id":            packageID,
		"package_name":          "@scope/provider-owned",
		"ecosystem":             "npm",
		"manifest_path":         "package-lock.json",
		"relationship":          "transitive",
		"cve_ids":               []string{"CVE-2026-1586"},
		"ghsa_ids":              []string{"GHSA-scoped-1586"},
		"vulnerable_range":      "< 2.4.2",
		"patched_version":       "2.4.2",
		"severity":              "high",
	})
	dependency := packageManifestDependencyFactWithMetadata(
		repoID,
		"api",
		"package-lock.json",
		"provider-owned",
		"npm",
		"2.4.1",
		observedAt,
		map[string]any{
			"namespace":         "@scope",
			"section":           "package-lock",
			"lockfile":          true,
			"dependency_path":   []any{"root", "@scope/provider-owned"},
			"dependency_depth":  2,
			"direct_dependency": false,
		},
	)

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{alert, dependency})

	if got, want := len(findings), 1; got != want {
		t.Fatalf("len(findings) = %d, want %d: %#v", got, want, findings)
	}
	finding := findings[0]
	assertSupplyChainImpactStatus(t, finding, SupplyChainImpactAffectedExact)
	if got, want := finding.ObservedVersion, "2.4.1"; got != want {
		t.Fatalf("ObservedVersion = %q, want scoped lockfile version %q", got, want)
	}
	if got, want := finding.RequestedRange, "2.4.1"; got != want {
		t.Fatalf("RequestedRange = %q, want scoped lockfile range %q", got, want)
	}
	if got, want := finding.VulnerableRange, "< 2.4.2"; got != want {
		t.Fatalf("VulnerableRange = %q, want provider vulnerable range %q", got, want)
	}
	if got, want := finding.FixedVersion, "2.4.2"; got != want {
		t.Fatalf("FixedVersion = %q, want provider fixed version %q", got, want)
	}
	if got, want := finding.MatchReason, supplyChainVersionReasonNPMSemverAffectedRange; got != want {
		t.Fatalf("MatchReason = %q, want %q", got, want)
	}
	if finding.DirectDependency == nil || *finding.DirectDependency {
		t.Fatalf("DirectDependency = %#v, want transitive lockfile evidence", finding.DirectDependency)
	}
	if got, want := finding.DependencyDepth, 2; got != want {
		t.Fatalf("DependencyDepth = %d, want %d", got, want)
	}
}
