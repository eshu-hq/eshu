// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// TestAuthorizedBranchFilterDropsForeignLineVendorFacts pins the second reader
// of vendor facts (#9945). The authorized-scan conflict pass picks the fix that
// applies to the host from the advisory's branches, so a vendor branch for
// another release must not contend there: its fix is that release's, not the
// host's. A branch with no readable line contends exactly as it did before, and
// an empty host line disables the filter.
func TestAuthorizedBranchFilterDropsForeignLineVendorFacts(t *testing.T) {
	t.Parallel()
	const dir = "pkg:deb/debian/curl?distro="
	same := vendorLineFact("debian", dir+"debian-12", "1:1.0-3")
	other := vendorLineFact("debian", dir+"debian-11", "1:1.0-3")
	unknown := vendorLineFact("debian", "pkg:deb/debian/curl", "1:1.0-3")
	osv := osvDebianCurlFact() // tagged for debian:12, 13 and 14

	got := filterAuthorizedBranchesToHostLine([]supplychainmodel.AffectedPackage{same, other, unknown, osv}, "debian:12")
	want := []supplychainmodel.AffectedPackage{same, unknown, osv}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("host debian:12 kept %+v, want %+v", got, want)
	}
	all := []supplychainmodel.AffectedPackage{same, other, unknown}
	if got := filterAuthorizedBranchesToHostLine(all, ""); !reflect.DeepEqual(got, all) {
		t.Fatalf("an empty host line must keep every branch, got %+v", got)
	}
}

// authorizedVendorBranch builds one vendor-sourced affected_package fact for the
// authorized scan path: one disjoint [introduced, fixed) branch of the advisory.
func authorizedVendorBranch(id, purl, introduced, fixed string) facts.Envelope {
	payload := map[string]any{
		"cve_id": vendorLineCVE, "advisory_id": "DSA-9945-1", "source": "debian",
		"package_id": "pkg:deb/debian/curl", "ecosystem": "deb", "package_name": "curl",
		"fixed_versions": []any{fixed},
		"affected_ranges": []any{map[string]any{"type": "ECOSYSTEM", "events": []any{
			map[string]any{"introduced": introduced}, map[string]any{"fixed": fixed},
		}}},
	}
	if purl != "" {
		payload["purl"] = purl
	}
	return facts.Envelope{FactID: id, FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: payload}
}

// TestAuthorizedScanNeverTrustsAnUnverifiedVendorBranch pins the third reader of
// vendor facts (#9945): the authorized conflict pass picks the one applicable
// branch of an advisory and re-evaluates it, so the branch it picks must pass
// the same line check the primary join applies. A branch whose release line is
// foreign to the host, unreadable, or checked against an unknown host line must
// read possibly_affected with os_release_unverified, never affected_exact.
func TestAuthorizedScanNeverTrustsAnUnverifiedVendorBranch(t *testing.T) {
	t.Parallel()
	const (
		bookworm = "pkg:deb/debian/curl?distro=bookworm"
		debian11 = "pkg:deb/debian/curl?distro=debian-11"
		debian12 = "pkg:deb/debian/curl?distro=debian-12"
		// The host runs 1:1.0-6: the branch fixed at 1:1.0-9 holds it.
		hostVersion = "1:1.0-6"
	)
	for _, tc := range []struct {
		name        string
		release     string
		lower       string // purl of the branch that does not hold the host
		upper       string // purl of the branch that holds the host
		wantStatus  SupplyChainImpactStatus
		wantReason  string
		wantFixedBy string // checked only for the exact controls
	}{
		{"both branches name a codename", "12", bookworm, bookworm, SupplyChainImpactPossiblyAffected, supplyChainVersionReasonOSReleaseUnverified, ""},
		{"both branches carry no purl", "12", "", "", SupplyChainImpactPossiblyAffected, supplyChainVersionReasonOSReleaseUnverified, ""},
		{"both branches are another release on an unknown host line", "sid", debian11, debian11, SupplyChainImpactPossiblyAffected, supplyChainVersionReasonOSReleaseUnverified, ""},
		{"own line elsewhere, the codename branch holds the host", "12", debian12, bookworm, SupplyChainImpactPossiblyAffected, supplyChainVersionReasonOSReleaseUnverified, ""},
		// Controls: branches verified as the host's own line stay exact.
		{"both branches are the host's line", "12", debian12, debian12, SupplyChainImpactAffectedExact, supplyChainVersionReasonDPKGAffectedRange, "1:1.0-9"},
		{"the branch holding the host is the host's line", "12", bookworm, debian12, SupplyChainImpactAffectedExact, supplyChainVersionReasonDPKGAffectedRange, "1:1.0-9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			scope, generation := "vuln-intel://debian/feed", "gen-feed"
			cve := vulnerabilityCVEFact("cve-9945", vendorLineCVE, 7.5)
			cve.Payload["source"], cve.Payload["advisory_id"] = "debian", "DSA-9945-1"
			advisories := []facts.Envelope{
				cve,
				authorizedVendorBranch("branch-low", tc.lower, "1:1.0-1", "1:1.0-5"),
				authorizedVendorBranch("branch-high", tc.upper, "1:1.0-5", "1:1.0-9"),
			}
			for i := range advisories {
				advisories[i].ScopeID, advisories[i].GenerationID = scope, generation
				advisories[i].SourceRef.SourceRecordID = advisories[i].FactID
			}
			authority := []AdvisoryAuthorityDependency{{
				Kind: "public_registration", ID: "registration-debian", RevisionID: "revision-debian",
				AdvisoryScopeID: scope, AdvisoryGenerationID: generation, ConfigFingerprint: "config-debian",
				ProviderID: "debian", TrustedSourceClass: "debian", ContentProfile: "supply_chain_impact_v1",
			}}
			target := authorizedOSVTarget(0, advisories, authority, debianCurl(tc.release, hostVersion))
			findings, err := BuildAuthorizedScanImpactFindings(nil, []AuthorizedScanImpactEvidence{target})
			if err != nil {
				t.Fatalf("BuildAuthorizedScanImpactFindings: %v", err)
			}
			if len(findings) != 1 {
				t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
			}
			got := findings[0]
			if got.Status != tc.wantStatus || got.MatchReason != tc.wantReason {
				t.Fatalf("verdict = %s/%s, want %s/%s", got.Status, got.MatchReason, tc.wantStatus, tc.wantReason)
			}
			if tc.wantFixedBy != "" && got.FixedVersion != tc.wantFixedBy {
				t.Fatalf("fixed version = %q, want %q", got.FixedVersion, tc.wantFixedBy)
			}
		})
	}
}
