// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"reflect"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// TestAuthorizedBranchFilterDropsForeignLineVendorFacts pins the second reader
// of vendor facts (#9945). The authorized-scan conflict pass picks the fix that
// applies to the host from the advisory's branches, so a vendor branch contends
// only when its own purl line is the host's, and it contends already
// line-checked (ReleaseLine set). A branch for another release, one with no
// readable line, and every vendor branch when the host line is unknown are
// dropped: none can be verified as the host's. A tagged OSV branch keeps its
// range rule, and an empty host line leaves it in.
func TestAuthorizedBranchFilterDropsForeignLineVendorFacts(t *testing.T) {
	t.Parallel()
	const dir = "pkg:deb/debian/curl?distro="
	same := vendorLineFact("debian", dir+"debian-12", "1:1.0-3")
	other := vendorLineFact("debian", dir+"debian-11", "1:1.0-3")
	unknown := vendorLineFact("debian", "pkg:deb/debian/curl", "1:1.0-3")
	osv := osvDebianCurlFact() // tagged for debian:12, 13 and 14

	got := filterAuthorizedBranchesToHostLine([]supplychainmodel.AffectedPackage{same, other, unknown, osv}, "debian:12")
	selected := same
	selected.ReleaseLine = "debian:12"
	want := []supplychainmodel.AffectedPackage{selected, osv}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("host debian:12 kept %+v, want %+v", got, want)
	}
	if got := filterAuthorizedBranchesToHostLine([]supplychainmodel.AffectedPackage{same, other, unknown, osv}, ""); !reflect.DeepEqual(got, []supplychainmodel.AffectedPackage{osv}) {
		t.Fatalf("an empty host line must drop every vendor branch and keep the tagged OSV branch, got %+v", got)
	}
}

// TestAuthorizedBranchFilterKeepsNonVendorBranchesWithAForeignLine pins the
// vendor test of the filter (#9945). Only a vendor-sourced OS fact is
// line-checked. An untagged OSV branch and a non-OS branch (a language
// advisory) name a readable foreign line or none, and still contend exactly as
// before the vendor check existed.
func TestAuthorizedBranchFilterKeepsNonVendorBranchesWithAForeignLine(t *testing.T) {
	t.Parallel()
	const foreign = "pkg:deb/debian/curl?distro=debian-11"
	untaggedOSV := osvDebianCurlFact()
	untaggedOSV.AffectedRanges = []supplychainmodel.AffectedRange{{Kind: "ECOSYSTEM", Events: untaggedOSV.AffectedRanges[0].Events}}
	untaggedOSV.PURL = foreign
	language := supplychainmodel.AffectedPackage{
		CVEID: "CVE-2099-9945", Source: "glad", AdvisoryID: "GLAD-1", Ecosystem: "npm",
		PackageID: "pkg:npm/example", PURL: "pkg:npm/example", Name: "example",
	}
	branches := []supplychainmodel.AffectedPackage{untaggedOSV, language}
	for _, hostLine := range []string{"debian:12", ""} {
		if got := filterAuthorizedBranchesToHostLine(branches, hostLine); !reflect.DeepEqual(got, branches) {
			t.Fatalf("host %q kept %+v, want every non-vendor branch kept: %+v", hostLine, got, branches)
		}
	}
	// A vendor branch with the same foreign line is dropped, and so is a
	// blank-source OS fact, which classifies as a non-OSV source.
	dropped := append(slices.Clone(branches), vendorLineFact("debian", foreign, "1:1.0-3"), vendorLineFact("", foreign, "1:1.0-3"))
	if got := filterAuthorizedBranchesToHostLine(dropped, "debian:12"); !reflect.DeepEqual(got, branches) {
		t.Fatalf("kept %+v, want only the vendor-sourced OS branches dropped", got)
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
