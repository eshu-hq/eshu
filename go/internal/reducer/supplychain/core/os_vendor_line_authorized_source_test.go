// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// authorizedSourceBranch builds one affected_package fact of the shared
// advisory for the authorized scan path, sourced from source and describing the
// debian-12 line through its purl: one [introduced, fixed) branch.
func authorizedSourceBranch(id, source, introduced, fixed string) facts.Envelope {
	branch := authorizedVendorBranch(id, "pkg:deb/debian/curl?distro=debian-12", introduced, fixed)
	branch.Payload["source"] = source
	return branch
}

// TestAuthorizedScanOnlyTheHostVendorEarnsTheReleaseLine pins the authorized
// conflict pass against the same rule the row path holds (#9945): a fact earns
// ReleaseLine, and so a fix the verdict may trust, only when its source is the
// installed row's own vendor. A fact from another source (ubuntu, nvd, glad)
// whose purl happens to name the host's line is not the host vendor's word. It
// must read possibly_affected with os_release_unverified when it is the branch
// that holds the host, and must never offer its fix beside a verified fact of
// the host's own vendor. The same advisory is delivered as two authority
// generations, once per source, in both fact orders.
func TestAuthorizedScanOnlyTheHostVendorEarnsTheReleaseLine(t *testing.T) {
	t.Parallel()
	// The host runs debian 12 at 1:1.0-6: a branch fixed at 1:1.0-9 holds it, a
	// branch fixed at 1:1.0-5 does not.
	const hostVersion = "1:1.0-6"
	for _, other := range []string{"ubuntu", "nvd", "glad"} {
		for _, reverse := range []bool{false, true} {
			for _, tc := range []struct {
				name                   string
				ownFixed, otherFixed   string // each source's one branch is [.., fixed)
				wantStatus             SupplyChainImpactStatus
				wantReason, wantFixed  string
				wantFixedSourceIsOwner bool
			}{
				{
					name: "own fix below the host and the other source holds it", ownFixed: "1:1.0-5", otherFixed: "1:1.0-9",
					wantStatus: SupplyChainImpactPossiblyAffected, wantReason: supplyChainVersionReasonOSReleaseUnverified,
					wantFixed: "1:1.0-5", wantFixedSourceIsOwner: true,
				},
				{
					name: "own branch holds the host and the other source fixes below it", ownFixed: "1:1.0-9", otherFixed: "1:1.0-5",
					wantStatus: SupplyChainImpactAffectedExact, wantReason: supplyChainVersionReasonDPKGAffectedRange,
					wantFixed: "1:1.0-9", wantFixedSourceIsOwner: true,
				},
			} {
				t.Run(other+"/"+tc.name+boolSuffix(reverse), func(t *testing.T) {
					t.Parallel()
					ownScope, ownGen := "vuln-intel://debian/feed", "gen-debian"
					otherScope, otherGen := "vuln-intel://"+other+"/feed", "gen-"+other
					cve := vulnerabilityCVEFact("cve-9945", vendorLineCVE, 7.5)
					cve.Payload["source"], cve.Payload["advisory_id"] = "debian", "DSA-9945-1"
					cve.ScopeID, cve.GenerationID = ownScope, ownGen
					own := authorizedSourceBranch("branch-own", "debian", "1:1.0-1", tc.ownFixed)
					own.ScopeID, own.GenerationID = ownScope, ownGen
					foreign := authorizedSourceBranch("branch-other", other, "1:1.0-1", tc.otherFixed)
					foreign.ScopeID, foreign.GenerationID = otherScope, otherGen
					if tc.otherFixed == "1:1.0-9" {
						foreign.Payload["affected_ranges"] = authorizedSourceBranch("x", other, "1:1.0-5", "1:1.0-9").Payload["affected_ranges"]
					}
					if tc.ownFixed == "1:1.0-9" {
						own.Payload["affected_ranges"] = authorizedSourceBranch("x", "debian", "1:1.0-5", "1:1.0-9").Payload["affected_ranges"]
					}
					advisories := []facts.Envelope{cve, own, foreign}
					if reverse {
						slices.Reverse(advisories)
					}
					for i := range advisories {
						advisories[i].SourceRef.SourceRecordID = advisories[i].FactID
					}
					authority := []AdvisoryAuthorityDependency{
						{
							Kind: "public_registration", ID: "registration-debian", RevisionID: "revision-debian",
							AdvisoryScopeID: ownScope, AdvisoryGenerationID: ownGen, ConfigFingerprint: "config-debian",
							ProviderID: "debian", TrustedSourceClass: "debian", ContentProfile: "supply_chain_impact_v1",
						},
						{
							Kind: "public_registration", ID: "registration-" + other, RevisionID: "revision-" + other,
							AdvisoryScopeID: otherScope, AdvisoryGenerationID: otherGen, ConfigFingerprint: "config-" + other,
							ProviderID: other, TrustedSourceClass: other, ContentProfile: "supply_chain_impact_v1",
						},
					}
					target := authorizedOSVTarget(0, advisories, authority, debianCurl("12", hostVersion))
					findings, err := BuildAuthorizedScanImpactFindings(nil, []AuthorizedScanImpactEvidence{target})
					if err != nil {
						t.Fatalf("BuildAuthorizedScanImpactFindings: %v", err)
					}
					if len(findings) != 1 {
						t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
					}
					got := findings[0]
					if got.Status != tc.wantStatus || got.MatchReason != tc.wantReason {
						t.Fatalf("verdict = %s/%s (fix %q from %q), want %s/%s", got.Status, got.MatchReason,
							got.FixedVersion, got.FixedVersionSource, tc.wantStatus, tc.wantReason)
					}
					if got.FixedVersion != tc.wantFixed {
						t.Fatalf("fixed version = %q, want %q", got.FixedVersion, tc.wantFixed)
					}
					if tc.wantFixedSourceIsOwner && got.FixedVersionSource != "debian" {
						t.Fatalf("fixed version source = %q, want debian: a non-host source offered a fix", got.FixedVersionSource)
					}
				})
			}
		}
	}
}

func boolSuffix(reverse bool) string {
	if reverse {
		return "/reversed"
	}
	return "/forward"
}

// TestOSAdvisoryFactIsVendorSourcedReadsAPackageURLPackageID pins that the
// OS shape of a non-OSV fact is also read from a deb, apk or rpm purl carried
// as the package id (#9945). A fact with a blank, dpkg or distro-labelled
// ecosystem, no purl, and a pkg:deb package id is still an OS fact from a
// non-OSV source; leaving it unrecognized lets it sit beside a verified fact of
// the host's vendor and read as a known fix.
func TestOSAdvisoryFactIsVendorSourcedReadsAPackageURLPackageID(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		fact supplychainmodel.AffectedPackage
		want bool
	}{
		{"pkg:deb package id, blank ecosystem", supplychainmodel.AffectedPackage{Source: "nvd", PackageID: "pkg:deb/debian/curl"}, true},
		{"pkg:deb package id, dpkg ecosystem", supplychainmodel.AffectedPackage{Source: "glad", Ecosystem: "dpkg", PackageID: "pkg:deb/debian/curl"}, true},
		{"pkg:rpm package id", supplychainmodel.AffectedPackage{Source: "nvd", PackageID: "pkg:rpm/redhat/curl"}, true},
		// One row per clause, so dropping any single clause is caught: each fact
		// has exactly one OS signal and a package id that is not itself an OS id.
		{"ecosystem is the only OS signal", supplychainmodel.AffectedPackage{Source: "nvd", Ecosystem: "os", PackageID: "curl"}, true},
		{"purl is the only OS signal", supplychainmodel.AffectedPackage{Source: "nvd", PURL: "pkg:deb/debian/curl", PackageID: "curl"}, true},
		{"os:// package id is the only OS signal", supplychainmodel.AffectedPackage{Source: "nvd", PackageID: "os://debian/curl"}, true},
		{"no OS signal at all", supplychainmodel.AffectedPackage{Source: "nvd", PackageID: "curl"}, false},
		{"OSV fact keeps its own path", supplychainmodel.AffectedPackage{Source: "osv", PackageID: "pkg:deb/debian/curl"}, false},
		{"language package id", supplychainmodel.AffectedPackage{Source: "glad", PackageID: "pkg:npm/example"}, false},
	} {
		if got := osAdvisoryFactIsVendorSourced(tc.fact); got != tc.want {
			t.Errorf("%s: osAdvisoryFactIsVendorSourced = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestOSAdvisoryFactIsHostVendorFactComparesTheTrimmedSource pins the source
// compare that the join and the authorized pass share (#9945): case and
// surrounding space do not make the host's own vendor a stranger, and OSV or an
// empty vendor is never the host's.
func TestOSAdvisoryFactIsHostVendorFactComparesTheTrimmedSource(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		source, vendor string
		want           bool
	}{
		{"debian", "debian", true},
		{" Debian ", "debian", true},
		{"ubuntu", "debian", false},
		{"nvd", "debian", false},
		{"osv", "osv", false},
		{"debian", "", false},
	} {
		if got := osAdvisoryFactIsHostVendorFact(tc.source, tc.vendor); got != tc.want {
			t.Errorf("osAdvisoryFactIsHostVendorFact(%q, %q) = %v, want %v", tc.source, tc.vendor, got, tc.want)
		}
	}
}
