// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// The reducer scan gate must admit the cve_list trusted source class the
// storage authority already grants (eshu-hq/eshu#10546): a scan target whose
// advisory generation cites a cve_list authority fails closed without it.
func TestAuthorizedScanImpactAdmitsCVEListAuthority(t *testing.T) {
	if !supportedTrustedAdvisorySourceClass("cve_list") {
		t.Fatal(`supportedTrustedAdvisorySourceClass("cve_list") = false, want true`)
	}
	const targetID = "image://derived/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const digest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	scan := derivedPartitionScan("cvelist", targetID, "generation-a", digest, "3.0.11-1~deb12u2")
	cve := vulnerabilityCVEFactWithProvenance(
		"cve-cvelist", "CVE-2026-9101", "cve_list", "CVE-2026-9101", 7.5, "", "HIGH", "2026-10-28T00:00:00Z",
	)
	cve.ScopeID = "vuln-intel://cve_list/cve"
	cve.GenerationID = "generation-cvelist"
	target := AuthorizedScanImpactEvidence{
		ScanTargetID: targetID, ScanGenerationID: "generation-a",
		OSPackages: []facts.Envelope{scan[0]}, Analyses: []facts.Envelope{scan[1]},
		Advisories: []facts.Envelope{cve},
		AuthorityDependencies: []AdvisoryAuthorityDependency{{
			Kind: "public_registration", ID: "registration-cvelist", RevisionID: "revision-cvelist",
			AdvisoryScopeID: cve.ScopeID, AdvisoryGenerationID: cve.GenerationID,
			ConfigFingerprint: "config-cvelist", ProviderID: "cve_list",
			TrustedSourceClass: "cve_list", ContentProfile: "supply_chain_impact_v1",
		}},
		Complete: true,
	}
	if err := validateAuthorizedScanImpactEvidence(target); err != nil {
		t.Fatalf("cve_list authority evidence rejected: %v", err)
	}
}
