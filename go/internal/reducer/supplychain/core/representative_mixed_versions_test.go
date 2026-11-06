// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// mixedTopology is a set of installed rows for the ruling 9990-f6 tests: one or more
// rows with an installed version and one or more without, placed in scopes with or
// without an image digest.
type mixedTopology struct {
	name string
	rows []mixedRow
}

// mixedRow is one installed row: its fact id, installed version, scope and image digest.
type mixedRow struct{ id, ver, scope, digest string }

// mixedHexDigest returns a 64-character image digest made of one repeated byte.
func mixedHexDigest(c byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = c
	}
	return "sha256:" + string(b)
}

// mixedEnvs returns the installed-row facts of rows and one scanner analysis per
// digest-bearing scope.
func mixedEnvs(rows []mixedRow) (out []facts.Envelope, analyses []facts.Envelope) {
	seen := map[string]bool{}
	for _, r := range rows {
		e := ubuntuCurl("22.04", r.ver).envelope()
		e.FactID, e.ScopeID, e.GenerationID = r.id, "image://"+r.scope, "g-"+r.scope
		out = append(out, e)
		if r.digest != "" && !seen[r.scope] {
			seen[r.scope] = true
			analyses = append(analyses, scannerWorkerAnalysisFact("image://"+r.scope, "g-"+r.scope, r.digest, "registry.example/"+r.scope+"@"+r.digest))
		}
	}
	return out, analyses
}

var (
	mixedDigestA, mixedDigestB, mixedDigestC = mixedHexDigest('a'), mixedHexDigest('b'), mixedHexDigest('c')
	mixedTopologies                          = []mixedTopology{
		{"two digests", []mixedRow{{"row-v", repJoinVersion, "a", mixedDigestA}, {"row-n", "", "b", mixedDigestB}}},
		{"digest and digest-less", []mixedRow{{"row-v", repJoinVersion, "a", mixedDigestA}, {"row-n", "", "b", ""}}},
		{"digest-less and digest", []mixedRow{{"row-v", repJoinVersion, "a", ""}, {"row-n", "", "b", mixedDigestB}}},
		{"two digest-less", []mixedRow{{"row-v", repJoinVersion, "a", ""}, {"row-n", "", "b", ""}}},
		{"one digest holds both", []mixedRow{{"row-v", repJoinVersion, "a", mixedDigestA}, {"row-n", "", "a", mixedDigestA}}},
		{"whitespace version", []mixedRow{{"row-v", repJoinVersion, "a", mixedDigestA}, {"row-n", "  ", "b", mixedDigestB}}},
		{"three digests", []mixedRow{{"row-v", repJoinVersion, "a", mixedDigestA}, {"row-w", repJoinVersion, "b", mixedDigestB}, {"row-n", "", "c", mixedDigestC}}},
	}
)

// mixedFindingForRow returns the finding whose evidence names the installed row.
func mixedFindingForRow(fs []SupplyChainImpactFinding, row string) (SupplyChainImpactFinding, bool) {
	for _, f := range fs {
		if slices.Contains(f.EvidenceFactIDs, row) {
			return f, true
		}
	}
	return SupplyChainImpactFinding{}, false
}

// TestVersionedRowEqualsTheOwnOnlyControl is test 4.2 of ruling 9990-f6: a row with an
// installed version has no base row, so the blank vendor fact alone decides it, in every
// order of facts and rows, whatever the other rows of the host look like.
func TestVersionedRowEqualsTheOwnOnlyControl(t *testing.T) {
	t.Parallel()
	for _, topo := range mixedTopologies {
		t.Run(topo.name, func(t *testing.T) {
			rows, analyses := mixedEnvs(topo.rows)
			cve := vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5)
			own, other := repJoinVendorFixFact(""), repJoinOtherFact(repDirectOtherID, "osv", "ubuntu")
			var versionedRows []facts.Envelope
			for _, e := range rows {
				if strings.TrimSpace(fmt.Sprint(e.Payload["installed_version"])) != "" && e.FactID != "row-n" {
					versionedRows = append(versionedRows, e)
				}
			}
			// The control holds only the versioned rows: on a shard that holds both, the
			// classifier reads one row, so the own fact alone can read the other row.
			control := BuildSupplyChainImpactFindings(append(append([]facts.Envelope{cve, own}, versionedRows...), analyses...))
			for _, rowOrder := range repJoinPermutations(rows) {
				for _, order := range repJoinPermutations([]facts.Envelope{cve, own, other}) {
					got := BuildSupplyChainImpactFindings(append(append(slices.Clone(order), rowOrder...), analyses...))
					for _, r := range topo.rows {
						if r.ver != repJoinVersion {
							continue
						}
						want, ok := mixedFindingForRow(control, r.id)
						if !ok {
							t.Fatalf("control has no finding on %s", r.id)
						}
						f, ok := mixedFindingForRow(got, r.id)
						if !ok {
							t.Fatalf("order %s: no finding on %s: %s", repDirectOrderLabel(order), r.id, repDirectSummary(got))
						}
						if !reflect.DeepEqual(f, want) {
							t.Fatalf("order %s rows %s: %s\nwant %s", repDirectOrderLabel(order), repDirectOrderLabel(rowOrder), repDirectSummary([]SupplyChainImpactFinding{f}), repDirectSummary([]SupplyChainImpactFinding{want}))
						}
					}
				}
			}
		})
	}
}

// TestVersionlessRowKeepsTheBaseWholeGroupRead pins the row that exists at the base: a
// row with no installed version stays installed_version_missing with the other fact in
// its evidence in the order where the blank fact is the source-order representative.
func TestVersionlessRowKeepsTheBaseWholeGroupRead(t *testing.T) {
	t.Parallel()
	rows, analyses := mixedEnvs(mixedTopologies[0].rows)
	cve := vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5)
	own, other := repJoinVendorFixFact(""), repJoinOtherFact(repDirectOtherID, "osv", "ubuntu")
	seen := 0
	for _, order := range repJoinPermutations([]facts.Envelope{cve, own, other}) {
		_, _, stats, err := buildSupplyChainImpactFindingsAndExclusions(append(append(slices.Clone(order), rows...), analyses...))
		if err != nil {
			t.Fatal(err)
		}
		got := BuildSupplyChainImpactFindings(append(append(slices.Clone(order), rows...), analyses...))
		f, ok := mixedFindingForRow(got, "row-n")
		if !ok {
			t.Fatalf("no finding on row-n: %s", repDirectSummary(got))
		}
		if f.Status != SupplyChainImpactPossiblyAffected || f.MatchReason != supplyChainVersionReasonMissingInstalled {
			t.Fatalf("row-n verdict %s/%s", f.Status, f.MatchReason)
		}
		if stats.osRepresentativeOutcomes[osRepresentativeOutcomeKey{vendor: "ubuntu", outcome: osRepresentativeJoined}] != 1 {
			continue
		}
		seen++
		if !slices.Contains(f.EvidenceFactIDs, repDirectOtherID) {
			t.Fatalf("order %s: row-n lost the other fact: %v", repDirectOrderLabel(order), f.EvidenceFactIDs)
		}
	}
	if seen == 0 {
		t.Fatal("no order joined directly")
	}
}

// TestMixedVersionGroupCountsOnce wants one outcome count per group, however its rows split.
func TestMixedVersionGroupCountsOnce(t *testing.T) {
	t.Parallel()
	rows, analyses := mixedEnvs(mixedTopologies[0].rows)
	cve := vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5)
	own, other := repJoinVendorFixFact(""), repJoinOtherFact(repDirectOtherID, "osv", "ubuntu")
	for _, order := range repJoinPermutations([]facts.Envelope{cve, own, other}) {
		_, _, stats, err := buildSupplyChainImpactFindingsAndExclusions(append(append(slices.Clone(order), rows...), analyses...))
		if err != nil {
			t.Fatal(err)
		}
		total := 0
		for v := range maps.Values(stats.osRepresentativeOutcomes) {
			total += v
		}
		if total != 1 {
			t.Fatalf("order %s: outcomes %v, want one count", repDirectOrderLabel(order), stats.osRepresentativeOutcomes)
		}
	}
}

// assertMixedVersionedKeepsOther wants the other fact in the evidence of the versioned
// row, in every order where the group reached a direct finding.
func assertMixedVersionedKeepsOther(t *testing.T, own, other facts.Envelope, extra []facts.Envelope, rows []mixedRow) {
	t.Helper()
	envs, analyses := mixedEnvs(rows)
	cve := vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5)
	checked := 0
	for _, order := range repJoinPermutations([]facts.Envelope{cve, own, other}) {
		input := append(append(append(slices.Clone(order), envs...), analyses...), extra...)
		_, _, stats, err := buildSupplyChainImpactFindingsAndExclusions(slices.Clone(input))
		if err != nil {
			t.Fatal(err)
		}
		if stats.osRepresentativeOutcomes[osRepresentativeOutcomeKey{vendor: "ubuntu", outcome: osRepresentativeJoined}] == 0 &&
			stats.osRepresentativeOutcomes[osRepresentativeOutcomeKey{vendor: "ubuntu", outcome: osRepresentativeOtherAnchor}] == 0 {
			continue
		}
		got := BuildSupplyChainImpactFindings(slices.Clone(input))
		if f, ok := mixedFindingForRow(got, "row-v"); ok {
			checked++
			if !slices.Contains(f.EvidenceFactIDs, repDirectOtherID) {
				t.Fatalf("order %s: row-v evidence %v lost the other fact", repDirectOrderLabel(order), f.EvidenceFactIDs)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no order reached a direct finding on row-v")
	}
}

// The four guards below keep the group whole where a row exists at the base or an anchor
// owns the finding.
func TestMixedRowsSpelledOSKeepsTheWholeGroup(t *testing.T) {
	t.Parallel()
	assertMixedVersionedKeepsOther(t, repJoinOwnFact("os"), repJoinOtherFact(repDirectOtherID, "nvd", ""), nil, mixedTopologies[0].rows)
}

func TestMixedRowsSpelledDpkgKeepsTheWholeGroup(t *testing.T) {
	t.Parallel()
	assertMixedVersionedKeepsOther(t, repJoinOwnFact("dpkg"), repJoinOtherFact(repDirectOtherID, "nvd", "os"), nil, mixedTopologies[0].rows)
}

func TestMixedRowsConsumerKeepsTheWholeGroup(t *testing.T) {
	t.Parallel()
	consume := []facts.Envelope{packageConsumptionFactWithRange("consume-9990", repJoinPackageID, "repository:r_9990", repJoinVersion)}
	assertMixedVersionedKeepsOther(t, repJoinOwnFact(""), repJoinOtherFact(repDirectOtherID, "nvd", "os"), consume, mixedTopologies[0].rows)
}

func TestMixedRowsSBOMPathKeepsTheWholeGroup(t *testing.T) {
	t.Parallel()
	sbom := []facts.Envelope{
		sbomComponentImpactFactWithPackageIDVersion("component", "doc", repJoinPackageID+"@"+repJoinVersion, repJoinPackageID, repJoinVersion),
		sbomAttachmentImpactFact("attachment", "doc", mixedDigestA),
		containerImageIdentityImpactFact("image", mixedDigestA, "repository:r_1"),
	}
	assertMixedVersionedKeepsOther(t, repJoinOwnFact(""), repJoinOtherFact(repDirectOtherID, "nvd", "os"), sbom, mixedTopologies[0].rows)
}

// TestMixedRowsSBOMPathDoesNotSplit keeps the split inside the no-SBOM-path branch (ruling
// 9990-f6, 2.3). A split above that check changes the answer for an SBOM-anchored group: a
// blank fact whose group has an SBOM path would gain a third finding in some orders, one
// more than the two the rows of the host earn. The finding of an anchored group exists at
// the base, so a slip here would add to a group the rule leaves alone (review 5, F9).
func TestMixedRowsSBOMPathDoesNotSplit(t *testing.T) {
	t.Parallel()
	envs, analyses := mixedEnvs(mixedTopologies[0].rows)
	rest := append(append(envs, analyses...),
		sbomComponentImpactFactWithPackageIDVersion("component", "doc", repJoinPackageID+"@"+repJoinVersion, repJoinPackageID, repJoinVersion),
		sbomAttachmentImpactFact("attachment", "doc", mixedDigestA),
		containerImageIdentityImpactFact("image", mixedDigestA, "repository:r_1"))
	cve := vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5)
	for _, order := range repJoinPermutations([]facts.Envelope{cve, repJoinVendorFixFact(""), repJoinOtherFact(repDirectOtherID, "osv", "ubuntu")}) {
		if got := BuildSupplyChainImpactFindings(append(slices.Clone(order), rest...)); len(got) > 2 {
			t.Fatalf("order %s: %d findings, want at most 2: %s", repDirectOrderLabel(order), len(got), repDirectSummary(got))
		}
	}
}

// TestMixedVersionRowsAuthorizedAgreesWithFleet wants the authorized path to read the
// versioned row as the fleet path does, with the rows in two targets and in one.
func TestMixedVersionRowsAuthorizedAgreesWithFleet(t *testing.T) {
	t.Parallel()
	cve := vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5)
	cve.Payload["source"], cve.Payload["advisory_id"] = "ubuntu", "USN-9990-1"
	own, other := repJoinVendorFixFact(""), repJoinOtherFact(repDirectOtherID, "osv", "ubuntu")
	authority := []AdvisoryAuthorityDependency{repJoinAuthority("ubuntu"), repJoinAuthority("osv")}
	scoped := []facts.Envelope{repJoinAdvisory(cve, "ubuntu"), repJoinAdvisory(own, "ubuntu"), repJoinAdvisory(other, "osv")}
	for _, order := range repJoinPermutations(scoped) {
		targets := []AuthorizedScanImpactEvidence{
			authorizedOSVTarget(0, order, authority, ubuntuCurl("22.04", repJoinVersion)),
			authorizedOSVTarget(1, order, authority, ubuntuCurl("22.04", "")),
		}
		got, err := BuildAuthorizedScanImpactFindings(nil, targets)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range got {
			if f.ObservedVersion == repJoinVersion && (f.FixedVersion != "1.0-5" || f.Status == SupplyChainImpactNotAffectedKnownFixed) {
				t.Fatalf("order %s: %s", repDirectOrderLabel(order), repDirectSummary([]SupplyChainImpactFinding{f}))
			}
		}
		// One target that holds both rows: its index is not split by target.
		one := authorizedOSVTarget(0, order, authority, ubuntuCurl("22.04", repJoinVersion))
		rowN := ubuntuCurl("22.04", "").envelope()
		rowN.FactID, rowN.ScopeID, rowN.GenerationID = "row-n", one.OSPackages[0].ScopeID, one.OSPackages[0].GenerationID
		one.OSPackages = append(one.OSPackages, rowN)
		got, err = BuildAuthorizedScanImpactFindings(nil, []AuthorizedScanImpactEvidence{one})
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range got {
			if f.ObservedVersion == repJoinVersion && (f.FixedVersion != "1.0-5" || f.Status == SupplyChainImpactNotAffectedKnownFixed) {
				t.Fatalf("one target, order %s: %s", repDirectOrderLabel(order), repDirectSummary([]SupplyChainImpactFinding{f}))
			}
		}
	}
}

// TestVersionedRowBesideAVersionlessRowIsDecidedByTheFactsThatJoin is the review's F6
// test. Two images, one row has no version. The versioned row has no base row, so it is a
// row this change creates, and a fact that cannot join must not decide it.
func TestVersionedRowBesideAVersionlessRowIsDecidedByTheFactsThatJoin(t *testing.T) {
	t.Parallel()
	const digestV, digestN = "sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "sha256:" + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	rowV := ubuntuCurl("22.04", repJoinVersion).envelope()
	rowV.FactID, rowV.ScopeID, rowV.GenerationID = "row-v", "image://v", "g-v"
	rowN := ubuntuCurl("22.04", "").envelope()
	rowN.FactID, rowN.ScopeID, rowN.GenerationID = "row-n", "image://n", "g-n"
	rest := []facts.Envelope{
		rowV, rowN,
		scannerWorkerAnalysisFact("image://v", "g-v", digestV, "registry.example/v@"+digestV),
		scannerWorkerAnalysisFact("image://n", "g-n", digestN, "registry.example/n@"+digestN),
	}
	cve := vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5)
	wrong, orders := 0, 0
	for _, order := range repJoinPermutations([]facts.Envelope{cve, repJoinVendorFixFact(""), repJoinOtherFact(repDirectOtherID, "osv", "ubuntu")}) {
		for _, finding := range BuildSupplyChainImpactFindings(append(slices.Clone(order), rest...)) {
			if !slices.Contains(finding.EvidenceFactIDs, "row-v") {
				continue
			}
			orders++
			if finding.Status == SupplyChainImpactNotAffectedKnownFixed || finding.FixedVersion != "1.0-5" {
				wrong++
			}
		}
	}
	if orders == 0 {
		t.Fatalf("no finding on the versioned row")
	}
	if wrong > 0 {
		t.Fatalf("%d of %d orders read the fix of a fact that cannot join on the versioned row", wrong, orders)
	}
}

// TestMixedRowsWithdrawalAcrossTheRowSplit pins the liveness filter's second
// pass over the row split (ruling 9990-f6 5.1): the recursive classify call runs
// liveAdvisoryPackages again on each part, and that pass is idempotent there,
// so withdrawal moves nothing the split does not own.
func TestMixedRowsWithdrawalAcrossTheRowSplit(t *testing.T) {
	t.Parallel()
	rows, analyses := mixedEnvs(mixedTopologies[0].rows)
	own := repJoinVendorFixFact("")
	other := repJoinOtherFact(repDirectOtherID, "osv", "ubuntu")
	mkCVE := func(withdrawn bool, factID, source, advisory string) facts.Envelope {
		if withdrawn {
			return vulnerabilityCVEFactWithdrawn(factID, vendorLineCVE, source, advisory, 7.5, "HIGH", "2026-10-01T00:00:00Z", "2026-11-01T00:00:00Z")
		}
		return vulnerabilityCVEFactWithProvenance(factID, vendorLineCVE, source, advisory, 7.5, "", "HIGH", "2026-10-01T00:00:00Z")
	}
	build := func(ownWithdrawn, otherWithdrawn bool, pkgs []facts.Envelope) []SupplyChainImpactFinding {
		envs := append([]facts.Envelope{
			mkCVE(ownWithdrawn, "cve-own", "ubuntu", "USN-9990-1"),
			mkCVE(otherWithdrawn, "cve-other", "osv", "EXT-"+repDirectOtherID),
		}, pkgs...)
		envs = append(envs, rows...)
		return BuildSupplyChainImpactFindings(append(envs, analyses...))
	}
	byRow := func(t *testing.T, findings []SupplyChainImpactFinding) map[string]SupplyChainImpactFinding {
		t.Helper()
		out := map[string]SupplyChainImpactFinding{}
		for _, r := range mixedTopologies[0].rows {
			f, ok := mixedFindingForRow(findings, r.id)
			if !ok {
				t.Fatalf("no finding on %s: %s", r.id, repDirectSummary(findings))
			}
			out[r.id] = f
		}
		if len(findings) != len(out) {
			t.Fatalf("findings = %d, want one per row: %s", len(findings), repDirectSummary(findings))
		}
		return out
	}
	pkgOrders := [][]facts.Envelope{{own, other}, {other, own}}

	// Withdrawing the only record that joins leaves nothing that anchors: the
	// blank ecosystem is outside the OS class the retraction rule covers
	// (9914-r5 limit 1.7a), so the withdrawn fact is dropped and the live fact
	// that cannot join yields no finding, in either fact order.
	for _, pkgs := range pkgOrders {
		if got := build(true, false, pkgs); len(got) != 0 {
			t.Fatalf("own withdrawn: findings = %d, want 0: %s", len(got), repDirectSummary(got))
		}
	}

	// Withdrawing the record that cannot join changes nothing but the name: the
	// live record decides both rows with no retraction shown, in either order.
	for _, pkgs := range pkgOrders {
		for id, f := range byRow(t, build(false, true, pkgs)) {
			if f.AdvisoryRetraction != nil {
				t.Fatalf("%s: retraction shown, want the live record's answer: %+v", id, f.AdvisoryRetraction)
			}
			if f.AdvisoryID != "USN-9990-1" || f.FixedVersion != "1.0-5" {
				t.Fatalf("%s: advisory %q fix %q, want the live record USN-9990-1/1.0-5", id, f.AdvisoryID, f.FixedVersion)
			}
		}
	}

	// Withdrawing both keeps both rows with the retraction shown. The versioned
	// row never carries the fact that cannot join; the verdicts match the live
	// baseline row for row. The versionless row's evidence still differs by fact
	// order there, which is #10034 item 1's class, so only the versioned row's
	// evidence is pinned.
	live := map[string]SupplyChainImpactFinding{}
	for _, pkgs := range pkgOrders {
		for id, f := range byRow(t, build(false, false, pkgs)) {
			live[repDirectOrderLabel(pkgs)+id] = f
		}
	}
	for _, pkgs := range pkgOrders {
		for id, f := range byRow(t, build(true, true, pkgs)) {
			if f.AdvisoryRetraction == nil {
				t.Fatalf("%s: no retraction shown, want both records withdrawn", id)
			}
			if want := live[repDirectOrderLabel(pkgs)+id]; f.Status != want.Status || f.MatchReason != want.MatchReason {
				t.Fatalf("%s: verdict %s/%s, want the live baseline %s/%s", id, f.Status, f.MatchReason, want.Status, want.MatchReason)
			}
			if id == "row-v" && slices.Contains(f.EvidenceFactIDs, repDirectOtherID) {
				t.Fatalf("versioned row carries the fact that cannot join: %v", f.EvidenceFactIDs)
			}
		}
	}
}
