// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestDigestlessFleetRepresentativeIsStable pins which row a group reads: the
// least by purl, then scope, generation, and fact ID, in any arrival order. Each
// case's least row loses on every later field, so dropping a field changes it.
func TestDigestlessFleetRepresentativeIsStable(t *testing.T) {
	t.Parallel()
	arm := debianCurl("12", "1:1.0-3")
	arm.arch = "arm64"
	amd := debianCurl("12", "1:1.0-3")
	build := func(row installedOSRow, factID, scope, generation string) facts.Envelope {
		envelope := row.envelope()
		envelope.FactID, envelope.ScopeID, envelope.GenerationID = factID, scope, generation
		return envelope
	}
	cases := []struct {
		name string
		rows []facts.Envelope
		want string
	}{
		{
			name: "raw version decides before purl",
			rows: []facts.Envelope{
				build(arm, "a-fact", "a-scope", "a-gen"),
				build(debianCurl("12", "1:1.0-3 "), "z-fact", "a-scope", "a-gen"),
			},
			want: "a-fact",
		},
		{
			name: "purl decides before scope",
			rows: []facts.Envelope{
				build(arm, "a-fact", "a-scope", "a-gen"),
				build(amd, "z-fact", "z-scope", "z-gen"),
			},
			want: "z-fact",
		},
		{
			name: "scope decides before generation",
			rows: []facts.Envelope{
				build(amd, "z-fact", "a-scope", "z-gen"),
				build(amd, "a-fact", "z-scope", "a-gen"),
			},
			want: "z-fact",
		},
		{
			name: "generation decides before fact ID",
			rows: []facts.Envelope{
				build(amd, "z-fact", "a-scope", "a-gen"),
				build(amd, "a-fact", "a-scope", "z-gen"),
			},
			want: "z-fact",
		},
		{
			name: "fact ID decides last",
			rows: []facts.Envelope{
				build(amd, "z-fact", "a-scope", "a-gen"),
				build(amd, "m-fact", "a-scope", "a-gen"),
				build(amd, "a-fact", "a-scope", "a-gen"),
			},
			want: "a-fact",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			findings := assertFleetOrderIndependent(t, vendorDebian11Facts(), tc.rows)
			if len(findings) != 1 {
				t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
			}
			if !slices.Contains(findings[0].EvidenceFactIDs, tc.want) {
				t.Fatalf("EvidenceFactIDs = %v, want the least row %s", findings[0].EvidenceFactIDs, tc.want)
			}
		})
	}
}

// TestSBOMBaseRowsSplitLikeTheFleet pins the SBOM branch's base, which holds
// digest-less rows and digests no attached SBOM names. The attached subject
// keeps its own finding, and the base splits exactly as the no-SBOM fleet does.
func TestSBOMBaseRowsSplitLikeTheFleet(t *testing.T) {
	t.Parallel()
	subject := fmt.Sprintf("sha256:%064x", 0xc3)
	unattached := fmt.Sprintf("sha256:%064x", 0xd4)
	other := fmt.Sprintf("sha256:%064x", 0xe5)
	packageID := vendorLineScenarios[0].packageID
	withUnattached := fleetOSRow(debianCurl("12", "1:1.0-3"), "image://fleet/unattached", "gen-unattached", unattached)
	withOther := fleetOSRow(debianCurl("12", "1:1.0-3"), "image://fleet/other", "gen-other", other)
	advisory := append(vendorDebian11Facts(),
		sbomComponentImpactFactWithPackageIDVersion("component-subject", "doc-subject", packageID+"@1:1.0-3", packageID, "1:1.0-3"),
		sbomAttachmentImpactFact("attachment-subject", "doc-subject", subject),
		containerImageIdentityImpactFact("image-subject", subject, testImpactRepositoryID),
		withUnattached[1],
		withOther[1],
	)
	findings := assertFleetOrderIndependent(t, advisory, []facts.Envelope{
		withUnattached[0],
		withOther[0],
		digestlessOSRow(debianCurl("11", "1:1.0-3"), "bare-11"),
		digestlessOSRow(debianCurl("12", "1:1.0-3"), "bare-12"),
	})
	seen := map[string]int{}
	for _, finding := range findings {
		seen[finding.SubjectDigest]++
	}
	if seen[subject] != 1 || seen[unattached] != 1 || seen[other] != 1 || seen[""] != 2 {
		t.Fatalf("findings by digest = %v, want one per digest and two digest-less: %#v", seen, findings)
	}
}

// TestDigestlessFleetPURLWithoutDistroCoalescesAcrossLines pins the limit of the
// split at the writer identity, which holds the purl and not the line. Rows whose
// purl names no distro qualifier share an identity across lines, so shards with
// one verdict merge their evidence into one finding, in every order.
func TestDigestlessFleetPURLWithoutDistroCoalescesAcrossLines(t *testing.T) {
	t.Parallel()
	bare := func(release, scope string) facts.Envelope {
		envelope := digestlessOSRow(debianCurl(release, "1:1.0-3"), scope)
		payload := make(map[string]any, len(envelope.Payload))
		for key, value := range envelope.Payload {
			payload[key] = value
		}
		payload["purl"] = "pkg:deb/debian/curl@1:1.0-3?arch=amd64"
		envelope.Payload = payload
		return envelope
	}
	findings := assertFleetOrderIndependent(t, vendorDebian11Facts(), []facts.Envelope{
		bare("11", "d11"),
		bare("12", "d12"),
		bare("10", "d10"),
	})
	if len(findings) != 2 {
		t.Fatalf("findings = %d, want the exact line and the merged foreign lines: %#v", len(findings), findings)
	}
	for _, finding := range findings {
		if finding.Status != SupplyChainImpactPossiblyAffected {
			continue
		}
		if !slices.Contains(finding.EvidenceFactIDs, "os-d10") || !slices.Contains(finding.EvidenceFactIDs, "os-d12") {
			t.Fatalf("EvidenceFactIDs = %v, want both foreign-line rows", finding.EvidenceFactIDs)
		}
	}
}

// TestDigestlessFleetCoalescedShardsKeepOneStableFix pins why the shards run in
// key order. Two lines with one purl and one verdict share a writer identity but
// not a fix, and coalesce keeps the first finding's fields, so the shard order
// has to be stable from one build to the next. The repeats are there because an
// unordered pass would differ by map iteration, not by row order.
func TestDigestlessFleetCoalescedShardsKeepOneStableFix(t *testing.T) {
	t.Parallel()
	bare := func(release, scope string) facts.Envelope {
		envelope := digestlessOSRow(debianCurl(release, "7.0.0-1"), scope)
		payload := make(map[string]any, len(envelope.Payload))
		for key, value := range envelope.Payload {
			payload[key] = value
		}
		payload["purl"] = "pkg:deb/debian/curl@7.0.0-1?arch=amd64"
		envelope.Payload = payload
		return envelope
	}
	advisory := convertedOSVFixture(t, "debian-cve-2023-38039.json", false)
	rows := []facts.Envelope{bare("12", "d12"), bare("13", "d13")}
	want, findings := fleetFindingsJSON(t, advisory, rows)
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want the two lines coalesced: %#v", len(findings), findings)
	}
	if findings[0].FixedVersion != "7.88.1-10+deb12u3" {
		t.Fatalf("FixedVersion = %q, want the first line's fix", findings[0].FixedVersion)
	}
	for attempt := range 40 {
		if got, _ := fleetFindingsJSON(t, advisory, rows); got != want {
			t.Fatalf("build %d changed the findings:\n%s\n%s", attempt, want, got)
		}
	}
}
