// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
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

// bareDebianCurl is a digest-less curl row whose purl names no distro
// qualifier, so the writer identity cannot tell its release line apart.
func bareDebianCurl(release, scope string) facts.Envelope {
	envelope := digestlessOSRow(debianCurl(release, "1:1.0-3"), scope)
	payload := make(map[string]any, len(envelope.Payload))
	for key, value := range envelope.Payload {
		payload[key] = value
	}
	payload["purl"] = "pkg:deb/debian/curl@1:1.0-3?arch=amd64"
	envelope.Payload = payload
	return envelope
}

// TestDigestlessFleetPURLWithoutDistroKeepsReasonWithoutSBOM pins what a merge
// of two shards says when no SBOM exists (#9984). A debian 12 host that the
// advisory does not list and a "sid" host the line table cannot read share a
// purl and a status, so they share a writer identity, but their reasons differ.
// The merged finding must not claim a conflict between SBOM components, SBOM
// reachability, or the missing-evidence entry for one: there is no SBOM. It
// keeps the lesser of the two reasons, both missing-evidence entries, and both
// rows, in every arrival order.
func TestDigestlessFleetPURLWithoutDistroKeepsReasonWithoutSBOM(t *testing.T) {
	t.Parallel()
	foreign, unreadable := bareDebianCurl("12", "d12"), bareDebianCurl("sid", "sid")
	advisory := vendorDebian11Facts()
	alone := func(row facts.Envelope) SupplyChainImpactFinding {
		t.Helper()
		_, found := fleetFindingsJSON(t, advisory, []facts.Envelope{row})
		if len(found) != 1 {
			t.Fatalf("one row built %d findings, want 1: %#v", len(found), found)
		}
		return found[0]
	}
	a, b := alone(foreign), alone(unreadable)
	if a.MatchReason == b.MatchReason || a.Status != b.Status {
		t.Fatalf("fixture drifted: reasons %q and %q, statuses %q and %q; want two reasons under one status",
			a.MatchReason, b.MatchReason, a.Status, b.Status)
	}
	findings := assertFleetOrderIndependent(t, advisory, []facts.Envelope{foreign, unreadable})
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want the two lines merged into one: %#v", len(findings), findings)
	}
	got := findings[0]
	if got.MatchReason == supplyChainVersionReasonSBOMVersionConflict {
		t.Fatalf("MatchReason = %q on a finding with no SBOM", got.MatchReason)
	}
	if got.RuntimeReachability == "image_sbom" {
		t.Fatalf("RuntimeReachability = %q on a finding with no SBOM", got.RuntimeReachability)
	}
	if want := min(a.MatchReason, b.MatchReason); got.MatchReason != want {
		t.Fatalf("MatchReason = %q, want the lesser input reason %q", got.MatchReason, want)
	}
	if slices.Contains(got.MissingEvidence, supplyChainMissingSBOMVersionConflict) {
		t.Fatalf("MissingEvidence = %v names an SBOM conflict with no SBOM", got.MissingEvidence)
	}
	for _, input := range []SupplyChainImpactFinding{a, b} {
		for _, entry := range input.MissingEvidence {
			if !slices.Contains(got.MissingEvidence, entry) {
				t.Fatalf("MissingEvidence = %v lost %q from an input finding", got.MissingEvidence, entry)
			}
		}
	}
	for _, id := range []string{"os-d12", "os-sid"} {
		if !slices.Contains(got.EvidenceFactIDs, id) {
			t.Fatalf("EvidenceFactIDs = %v, want both rows", got.EvidenceFactIDs)
		}
	}
}

// TestCoalesceNamesAnSBOMConflictOnlyWhenAnSBOMWasRead pins both arms of the
// merge on a writer identity shared by findings with different reasons. With an
// SBOM component read for either finding, the disagreement is an SBOM version
// conflict. With none, it is not, and the merge keeps the lesser reason.
func TestCoalesceNamesAnSBOMConflictOnlyWhenAnSBOMWasRead(t *testing.T) {
	t.Parallel()
	build := func(reason, reachability string, path ...string) SupplyChainImpactFinding {
		return SupplyChainImpactFinding{
			CVEID: "CVE-2099-9945", PackageID: "pkg:deb/debian/curl",
			PURL: "pkg:deb/debian/curl@1:1.0-3", ObservedVersion: "1:1.0-3",
			Status: SupplyChainImpactPossiblyAffected, Confidence: "partial",
			MatchReason: reason, RuntimeReachability: reachability,
			SubjectDigest: "sha256:" + strings.Repeat("a", 64),
			EvidencePath:  path, MissingEvidence: []string{"missing " + reason},
		}
	}
	sbom := []string{chain.SBOMComponentFactKind}
	cases := []struct {
		name          string
		first, second SupplyChainImpactFinding
		wantConflict  bool
	}{
		{"both read an SBOM", build("reason_b", "image_sbom", sbom...), build("reason_a", "image_sbom", sbom...), true},
		{"one read an SBOM", build("reason_a", "image_sbom", sbom...), build("reason_b", "image_os_package"), true},
		{"neither read an SBOM", build("reason_a", "image_os_package"), build("reason_b", "unknown"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var want string
			for at, in := range [][]SupplyChainImpactFinding{{tc.first, tc.second}, {tc.second, tc.first}} {
				out := coalesceAndSortSupplyChainImpactFindings(slices.Clone(in))
				if len(out) != 1 {
					t.Fatalf("findings = %d, want 1: %#v", len(out), out)
				}
				got := out[0]
				if isConflict := got.MatchReason == supplyChainVersionReasonSBOMVersionConflict; isConflict != tc.wantConflict {
					t.Fatalf("MatchReason = %q, conflict = %t, want %t", got.MatchReason, isConflict, tc.wantConflict)
				}
				if has := slices.Contains(got.MissingEvidence, supplyChainMissingSBOMVersionConflict); has != tc.wantConflict {
					t.Fatalf("MissingEvidence = %v, names a conflict = %t, want %t", got.MissingEvidence, has, tc.wantConflict)
				}
				if !tc.wantConflict && got.MatchReason != "reason_a" {
					t.Fatalf("MatchReason = %q, want the lesser reason", got.MatchReason)
				}
				if !slices.Contains(got.MissingEvidence, "missing reason_a") || !slices.Contains(got.MissingEvidence, "missing reason_b") {
					t.Fatalf("MissingEvidence = %v, want both inputs kept", got.MissingEvidence)
				}
				raw, err := json.Marshal(got)
				if err != nil {
					t.Fatalf("marshal finding: %v", err)
				}
				encoded := string(raw)
				if at == 0 {
					want = encoded
				} else if encoded != want {
					t.Fatalf("arrival order changed the merge:\n%s\n%s", want, encoded)
				}
			}
		})
	}
}
