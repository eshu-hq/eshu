// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// The issue's fixture host: an Ubuntu row. "ubuntu" ranks 999 under the deb and
// language tables and loses a rank-1 tie to "debian" under the os table (#9990).
const (
	repJoinPackageID = "pkg:deb/ubuntu/curl"
	repJoinPURL      = "pkg:deb/ubuntu/curl?arch=amd64&distro=ubuntu-22.04"
	repJoinVersion   = "1.0-3"
)

// repJoinOwnFact is the fact of the host's own vendor. Its purl names the host's
// release line and its exact-version list names the installed version.
func repJoinOwnFact(ecosystem string) facts.Envelope {
	payload := map[string]any{
		"cve_id": vendorLineCVE, "advisory_id": "USN-9990-1", "source": "ubuntu",
		"package_id": repJoinPackageID, "package_name": "curl", "purl": repJoinPURL,
		"affected_versions": []any{repJoinVersion},
	}
	if ecosystem != "" {
		payload["ecosystem"] = ecosystem
	}
	return facts.Envelope{FactID: "affected-own", FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: payload}
}

// repJoinOtherFact is a fact from a source that is not the host's vendor. It
// carries a fix below the host so a leak of that fix shows on the finding. A
// source of osv with the ecosystem os reads the vendor from the purl, so such a
// fact is given no purl: it stays a fact that cannot join.
func repJoinOtherFact(factID, source, ecosystem string) facts.Envelope {
	payload := map[string]any{
		"cve_id": vendorLineCVE, "advisory_id": "EXT-" + factID, "package_id": repJoinPackageID,
		"package_name": "curl", "fixed_versions": []any{"1.0-2"},
	}
	if source != "" {
		payload["source"] = source
	}
	if ecosystem != "" {
		payload["ecosystem"] = ecosystem
	}
	return facts.Envelope{FactID: factID, FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: payload}
}

// repJoinCases are the issue's n=0 shapes: an ecosystem and another source.
func repJoinCases() []struct{ ecosystem, other string } {
	var cases []struct{ ecosystem, other string }
	for _, ecosystem := range []string{"deb", "dpkg", ""} {
		for _, other := range []string{"nvd", "debian", "glad", "alpine", "", "osv"} {
			cases = append(cases, struct{ ecosystem, other string }{ecosystem, other})
		}
	}
	return append(cases, struct{ ecosystem, other string }{"os", "debian"})
}

// repJoinPermutations returns every ordering of envelopes.
func repJoinPermutations(envelopes []facts.Envelope) [][]facts.Envelope {
	if len(envelopes) < 2 {
		return [][]facts.Envelope{slices.Clone(envelopes)}
	}
	var out [][]facts.Envelope
	for i := range envelopes {
		rest := slices.Delete(slices.Clone(envelopes), i, i+1)
		for _, tail := range repJoinPermutations(rest) {
			out = append(out, append([]facts.Envelope{envelopes[i]}, tail...))
		}
	}
	return out
}

// assertRepJoinFinding holds every issue shape to one verdict: a verified line and
// a list that names the host are exact, and a blank ecosystem reads the version
// family from the purl, as the os ecosystem does.
func assertRepJoinFinding(t *testing.T, findings []SupplyChainImpactFinding, other string) {
	t.Helper()
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
	}
	got := findings[0]
	if got.Status != SupplyChainImpactAffectedExact || got.MatchReason != supplyChainVersionReasonDPKGExactAffected {
		t.Fatalf("verdict = %s/%s, want %s/%s", got.Status, got.MatchReason, SupplyChainImpactAffectedExact, supplyChainVersionReasonDPKGExactAffected)
	}
	if got.PURL == "" || got.ObservedVersion != repJoinVersion {
		t.Fatalf("purl %q, observed %q, want the installed row's", got.PURL, got.ObservedVersion)
	}
	if !slices.Contains(got.EvidencePath, chain.VulnerabilityOSPackageFactKind) {
		t.Fatalf("evidence path %v, want the installed row on it", got.EvidencePath)
	}
	// A non-OSV fact from another source offers no fix on the OS path (#9945). An
	// untagged OSV fact keeps the untagged union rules, as it does on the shape
	// that survives today (ecosystem os, other source osv).
	wantFixed := ""
	if other == "osv" {
		wantFixed = "1.0-2"
	}
	if got.FixedVersion != wantFixed {
		t.Fatalf("fixed version = %q, want %q", got.FixedVersion, wantFixed)
	}
}

// TestBlankEcosystemReadsTheFamilyAndASpelledOneKeepsItsMatcher pins how far the
// family lookup reaches (#9990). A blank ecosystem names no matcher, so it reads the
// family from the group's OS purl. dpkg also normalizes to nothing but names a real
// matcher, and a finding that exists today reads it: an apk purl in the group must
// not move that verdict.
func TestBlankEcosystemReadsTheFamilyAndASpelledOneKeepsItsMatcher(t *testing.T) {
	t.Parallel()
	group := func(purl string) []supplychainmodel.AffectedPackage {
		return []supplychainmodel.AffectedPackage{{PURL: purl, AffectedVersions: []string{repJoinVersion}}}
	}
	decide := func(ecosystem, purl string) supplyChainVersionMatchDecision {
		return evaluateSupplyChainVersionMatch(ecosystem, repJoinVersion, "", "", group(purl))
	}
	if got, want := decide("", repJoinPURL), decide("deb", repJoinPURL); !reflect.DeepEqual(got, want) {
		t.Fatalf("blank with a deb purl = %+v, want what deb earns: %+v", got, want)
	}
	if got, want := decide(" \t", repJoinPURL), decide("deb", repJoinPURL); !reflect.DeepEqual(got, want) {
		t.Fatalf("whitespace ecosystem with a deb purl = %+v, want what deb earns: %+v", got, want)
	}
	if got := decide("", "pkg:npm/example").Reason; got != supplyChainVersionReasonUnsupportedEcosystem {
		t.Fatalf("blank with a language purl reason = %q, want %q", got, supplyChainVersionReasonUnsupportedEcosystem)
	}
	if got := decide("", "").Reason; got != supplyChainVersionReasonUnsupportedEcosystem {
		t.Fatalf("blank with no purl reason = %q, want %q", got, supplyChainVersionReasonUnsupportedEcosystem)
	}
	if got, want := decide("dpkg", "pkg:apk/alpine/curl"), decide("dpkg", ""); !reflect.DeepEqual(got, want) {
		t.Fatalf("dpkg with an apk purl = %+v, want the dpkg matcher: %+v", got, want)
	}
}

// TestRepresentativeJoinsTheInstalledRowForEveryIssueShape is the issue's
// red-first case on the fleet path (#9990): the fact that can join the row names
// the finding whichever unrelated source won source order, in every fact order.
func TestRepresentativeJoinsTheInstalledRowForEveryIssueShape(t *testing.T) {
	t.Parallel()
	for _, tc := range repJoinCases() {
		t.Run(fmt.Sprintf("ecosystem=%q/other=%q", tc.ecosystem, tc.other), func(t *testing.T) {
			t.Parallel()
			cve := vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5)
			own := repJoinOwnFact(tc.ecosystem)
			other := repJoinOtherFact("affected-other", tc.other, tc.ecosystem)
			host := ubuntuCurl("22.04", repJoinVersion).envelope()
			var first []SupplyChainImpactFinding
			for _, order := range repJoinPermutations([]facts.Envelope{cve, own, other, host}) {
				got := BuildSupplyChainImpactFindings(order)
				assertRepJoinFinding(t, got, tc.other)
				if first == nil {
					first = got
				} else if !reflect.DeepEqual(got, first) {
					t.Fatalf("findings depend on fact order:\n%#v\n%#v", got, first)
				}
			}
		})
	}
}

// repJoinAdvisory stamps one advisory envelope with the feed scope its source
// authorizes, as the authorized loader selects it.
func repJoinAdvisory(envelope facts.Envelope, source string) facts.Envelope {
	envelope.ScopeID = "vuln-intel://" + source + "/feed"
	envelope.GenerationID = "gen-" + source
	envelope.SourceRef.SourceRecordID = envelope.FactID
	return envelope
}

// repJoinAuthority authorizes one source's feed scope.
func repJoinAuthority(source string) AdvisoryAuthorityDependency {
	return AdvisoryAuthorityDependency{
		Kind: "public_registration", ID: "registration-" + source, RevisionID: "revision-" + source,
		AdvisoryScopeID: "vuln-intel://" + source + "/feed", AdvisoryGenerationID: "gen-" + source,
		ConfigFingerprint: "config-" + source, ProviderID: source, TrustedSourceClass: source,
		ContentProfile: "supply_chain_impact_v1",
	}
}

// TestAuthorizedScanRepresentativeJoinsTheInstalledRow is the same case on the
// authorized per-target path. A blank source has no trusted class, so the
// boundary refuses it with an error before any selection: it is not silent.
func TestAuthorizedScanRepresentativeJoinsTheInstalledRow(t *testing.T) {
	t.Parallel()
	for _, tc := range repJoinCases() {
		t.Run(fmt.Sprintf("ecosystem=%q/other=%q", tc.ecosystem, tc.other), func(t *testing.T) {
			t.Parallel()
			cve := vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5)
			cve.Payload["source"], cve.Payload["advisory_id"] = "ubuntu", "USN-9990-1"
			own := repJoinAdvisory(repJoinOwnFact(tc.ecosystem), "ubuntu")
			other := repJoinOtherFact("affected-other", tc.other, tc.ecosystem)
			otherScope := tc.other
			if otherScope == "" {
				otherScope = "ubuntu"
			}
			other = repJoinAdvisory(other, otherScope)
			authority := []AdvisoryAuthorityDependency{repJoinAuthority("ubuntu")}
			if otherScope != "ubuntu" {
				authority = append(authority, repJoinAuthority(otherScope))
			}
			base := []facts.Envelope{repJoinAdvisory(cve, "ubuntu"), own, other}
			var first []SupplyChainImpactFinding
			for _, order := range repJoinPermutations(base) {
				target := authorizedOSVTarget(0, order, authority, ubuntuCurl("22.04", repJoinVersion))
				got, err := BuildAuthorizedScanImpactFindings(nil, []AuthorizedScanImpactEvidence{target})
				if tc.other == "" {
					if err == nil {
						t.Fatalf("a blank-source advisory must be refused at the boundary, got %#v", got)
					}
					continue
				}
				if err != nil {
					t.Fatalf("BuildAuthorizedScanImpactFindings: %v", err)
				}
				assertRepJoinFinding(t, got, tc.other)
				if first == nil {
					first = got
				} else if !reflect.DeepEqual(got, first) {
					t.Fatalf("findings depend on fact order:\n%#v\n%#v", got, first)
				}
			}
		})
	}
}
