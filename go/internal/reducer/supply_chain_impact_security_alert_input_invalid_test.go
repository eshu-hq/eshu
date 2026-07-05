// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSupplyChainImpactSecurityAlertSeededFindingsUnchangedByTypedDecode is the
// output-preserving equivalence proof for the SUPPLY_CHAIN_IMPACT consumer of
// the security_alert.repository_alert fact kind: a valid provider alert plus its
// owned dependency evidence seeds exactly the same SupplyChainImpactFinding
// after the typed-decode migration as before, so no supply-chain-impact truth
// changed for valid facts. The finding is compared field-by-field against the
// same alert decoded alone.
func TestSupplyChainImpactSecurityAlertSeededFindingsUnchangedByTypedDecode(t *testing.T) {
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

	findings, quarantined, err := buildSupplyChainImpactFindingsWithQuarantine(
		[]facts.Envelope{alert, dependency},
	)
	if err != nil {
		t.Fatalf("buildSupplyChainImpactFindingsWithQuarantine() error = %v, want nil", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %d, want 0 for a valid alert", len(quarantined))
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1 security-alert-seeded finding", len(findings))
	}
	finding := findings[0]
	if finding.CVEID != "CVE-2026-1586" || finding.PackageID != packageID {
		t.Fatalf("finding identity = {%q, %q}, want {CVE-2026-1586, %q}", finding.CVEID, finding.PackageID, packageID)
	}
	if finding.RepositoryID != repoID {
		t.Fatalf("finding RepositoryID = %q, want %q", finding.RepositoryID, repoID)
	}
	if finding.ObservedVersion != "2.4.1" || finding.FixedVersion != "2.4.2" {
		t.Fatalf("finding versions = {observed %q, fixed %q}, want {2.4.1, 2.4.2}", finding.ObservedVersion, finding.FixedVersion)
	}
}

// TestSupplyChainImpactQuarantinesMalformedSecurityAlertWithoutPoisoningGeneration
// proves the per-fact isolation on the SUPPLY_CHAIN_IMPACT consumer: a malformed
// security_alert.repository_alert fact (missing its required repository_id) in a
// batch alongside a valid alert+dependency is quarantined per-fact, the valid
// alert still seeds its finding, NO empty-identity finding is produced, and the
// whole generation still succeeds (buildSupplyChainImpactFindingsWithQuarantine
// returns no fatal error). A poisoned security_alert fact must never take down
// the supply_chain_impact generation.
func TestSupplyChainImpactQuarantinesMalformedSecurityAlertWithoutPoisoningGeneration(t *testing.T) {
	t.Parallel()

	repoID := "repo://github/acme/api"
	packageID := "npm://registry.npmjs.org/@scope/provider-owned"
	observedAt := time.Date(2026, 6, 7, 9, 30, 0, 0, time.UTC)
	validAlert := securityAlertEnvelope("alert-valid-impact", repoID, map[string]any{
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
	malformed := securityAlertEnvelopeMissingRepositoryID("alert-malformed-impact", map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(23),
		"provider_state":        "open",
		"package_id":            "npm://registry.npmjs.org/@scope/other",
		"package_name":          "@scope/other",
		"ecosystem":             "npm",
		"cve_ids":               []string{"CVE-2026-9999"},
		"ghsa_ids":              []string{"GHSA-scoped-9999"},
	})

	// Baseline: the valid alert + dependency without the malformed fact.
	baselineFindings, _, err := buildSupplyChainImpactFindingsWithQuarantine(
		[]facts.Envelope{validAlert, dependency},
	)
	if err != nil {
		t.Fatalf("baseline buildSupplyChainImpactFindingsWithQuarantine() error = %v, want nil", err)
	}

	// With the malformed fact added, the generation must still succeed, the
	// malformed fact must be quarantined, and the valid alert's finding must be
	// byte-identical to the baseline.
	findings, quarantined, err := buildSupplyChainImpactFindingsWithQuarantine(
		[]facts.Envelope{validAlert, dependency, malformed},
	)
	if err != nil {
		t.Fatalf("buildSupplyChainImpactFindingsWithQuarantine() error = %v, want nil (a poisoned alert must not fail the generation)", err)
	}
	if got, want := len(quarantined), 1; got != want {
		t.Fatalf("quarantined = %d, want %d", got, want)
	}
	if quarantined[0].factID != "alert-malformed-impact" || quarantined[0].field != "repository_id" {
		t.Fatalf("quarantined[0] = %+v, want {alert-malformed-impact, repository_id}", quarantined[0])
	}
	if !reflect.DeepEqual(findings, baselineFindings) {
		t.Fatalf("findings with malformed fact present differ from baseline:\n got = %#v\nwant = %#v", findings, baselineFindings)
	}
	for _, finding := range findings {
		if finding.RepositoryID == "" || finding.PackageID == "" {
			t.Fatalf("an impact finding has an empty identity segment: %#v", finding)
		}
		if finding.CVEID == "CVE-2026-9999" {
			t.Fatal("the malformed alert seeded an impact finding; it must be quarantined, not seeded")
		}
	}
}

// TestSecurityAlertReconciliationQuarantineReplayIsIdempotent proves replaying
// the identical batch (including the malformed fact) through the reconciliation
// build converges on the same decisions and the same quarantine set both times —
// the decode outcome for a given payload is pure, so the quarantine never
// becomes intermittent across replays.
func TestSecurityAlertReconciliationQuarantineReplayIsIdempotent(t *testing.T) {
	t.Parallel()

	validRepoID := "repo://github/eshu-hq/eshu"
	valid := securityAlertEnvelope("alert-valid", validRepoID, map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(7),
		"provider_state":        "open",
		"package_id":            "npm://registry.npmjs.org/left-pad",
		"package_name":          "left-pad",
		"ecosystem":             "npm",
		"cve_ids":               []string{"CVE-2026-0002"},
	})
	malformed := securityAlertEnvelopeMissingRepositoryID("alert-malformed", map[string]any{
		"provider":       "github_dependabot",
		"provider_state": "open",
		"package_id":     "npm://registry.npmjs.org/other-pkg",
	})
	batch := []facts.Envelope{valid, malformed}

	firstDecisions, firstQuarantined, err := BuildSecurityAlertReconciliationsWithQuarantine(batch)
	if err != nil {
		t.Fatalf("first build error = %v, want nil", err)
	}
	secondDecisions, secondQuarantined, err := BuildSecurityAlertReconciliationsWithQuarantine(batch)
	if err != nil {
		t.Fatalf("second build error = %v, want nil", err)
	}
	if !reflect.DeepEqual(firstDecisions, secondDecisions) {
		t.Fatal("reconciliation decisions differ across replay; the decode must be pure")
	}
	if !reflect.DeepEqual(firstQuarantined, secondQuarantined) {
		t.Fatal("quarantine set differs across replay; the decode must be pure")
	}
	if len(firstQuarantined) != 1 {
		t.Fatalf("quarantined = %d, want 1 on each replay", len(firstQuarantined))
	}
}
