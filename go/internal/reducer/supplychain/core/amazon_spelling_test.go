// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	chain "github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// RED for #10034 item 4.
//
// amazon spelling: the join gate accepts an "amazon" ecosystem on an rpm row
// whose vendor or distro reads "amazon" (match.go), but no version matcher
// knows the spelling, so the finding reads unsupported_ecosystem. "amazon" is
// a spelling of the amazonlinux vendor class (vendorForOSPackageClass already
// rules it so), and the matchers must read it as rpm. The three spellings a
// fact can carry in an "amazon" world ("", "os", "amazon") join the same row
// and read the same rpm verdict in every fact order.
//
// per-vendor signal: an emitted finding that reads unsupported_ecosystem for
// an OS-shaped group names the vendor whose matcher is missing, so the
// operator sees which vendor leaves the row unverified.

// amazonRow is an installed row of the "amazon" vendor world: manager rpm,
// vendor and distro spelling "amazon", the only world in which an
// "amazon"-ecosystem fact joins.
func amazonRowForSpelling(version string) installedOSRow {
	return installedOSRow{vendor: "amazon", release: "2023", manager: "rpm", name: "curl", arch: "x86_64", version: version}
}

// amazonFact is the vendor's fact in the "amazon" world, spelled with the
// given ecosystem, listing the installed version without release evidence.
func amazonFactForSpelling(factID, ecosystem, version string) facts.Envelope {
	payload := map[string]any{
		"cve_id": vendorLineCVE, "advisory_id": "ALAS-9990-1", "source": "amazon",
		"package_id": "pkg:rpm/amazon/curl", "package_name": "curl",
		"purl":              "pkg:rpm/amazon/curl",
		"affected_versions": []any{version},
	}
	if ecosystem != "" {
		payload["ecosystem"] = ecosystem
	}
	return facts.Envelope{FactID: factID, FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: payload}
}

// TestAmazonSpellingReadsTheRPMMatcher is the item-4 decision: "amazon" is
// rpm. Every spelling joins the same row and reads the rpm verdict
// (os_release_unverified: the fact carries no readable release line, #9945),
// in every fact order.
func TestAmazonSpellingReadsTheRPMMatcher(t *testing.T) {
	t.Parallel()
	const version = "1.0-3"
	for _, ecosystem := range []string{"", "os", "amazon"} {
		t.Run("ecosystem="+ecosystem, func(t *testing.T) {
			t.Parallel()
			row := amazonRowForSpelling(version).envelope()
			input := func() []facts.Envelope {
				return []facts.Envelope{
					vulnerabilityCVEFact("cve-amazon", vendorLineCVE, 7.5),
					amazonFactForSpelling("affected-amazon", ecosystem, version),
					row,
				}
			}
			var first []SupplyChainImpactFinding
			for _, order := range repJoinPermutations(input()) {
				findings := BuildSupplyChainImpactFindings(order)
				if len(findings) != 1 {
					t.Fatalf("order %s: findings = %d, want 1: %#v", repDirectOrderLabel(order), len(findings), findings)
				}
				got := findings[0]
				if got.Status != SupplyChainImpactPossiblyAffected || got.MatchReason != supplyChainVersionReasonOSReleaseUnverified {
					t.Fatalf("order %s: verdict = %s/%s, want possibly_affected/os_release_unverified", repDirectOrderLabel(order), got.Status, got.MatchReason)
				}
				if first == nil {
					first = findings
				} else if repDirectSummary(findings) != repDirectSummary(first) {
					t.Fatalf("order %s reads %s, want %s", repDirectOrderLabel(order), repDirectSummary(findings), repDirectSummary(first))
				}
			}
		})
	}
}

// TestUnsupportedEcosystemNamesItsVendor is the item-4 signal: an emitted
// finding that reads unsupported_ecosystem for an OS-shaped group carries the
// vendor whose matcher is missing. The shape is an "os"-ecosystem,
// consumer-anchored group with no installed row and no purl to read a family
// from: the exemption keeps it visible, and the token says for whom.
func TestUnsupportedEcosystemNamesItsVendor(t *testing.T) {
	t.Parallel()
	pkg := "pkg:deb/ubuntu/curl"
	unsupported := facts.Envelope{FactID: "affected-os", FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: map[string]any{
		"cve_id": vendorLineCVE, "advisory_id": "USN-9990-1", "source": "ubuntu",
		"ecosystem": "os", "package_id": pkg, "package_name": "curl",
		"affected_versions": []any{repJoinVersion},
	}}
	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5),
		unsupported,
		packageConsumptionFactWithRange("consume-9990", pkg, "repository:r_9990", repJoinVersion),
	})
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
	}
	got := findings[0]
	if got.MatchReason != supplyChainVersionReasonUnsupportedEcosystem {
		t.Fatalf("reason = %q, want unsupported_ecosystem", got.MatchReason)
	}
	if !slices.Contains(got.MissingEvidence, supplyChainMissingUnsupportedMatcherForVendor("ubuntu")) {
		t.Fatalf("missing evidence %v, want the ubuntu vendor token", got.MissingEvidence)
	}
}
