// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/ifa/familyodu"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// The recorded CVEs the expectations name. Each is one shape the first NVD
// window carries; the window's own scope id is vulnWindow1Scope.
const (
	cveExactWidget  = "CVE-2026-90001" // exact CPE, reported 9.8 CRITICAL, in KEV and EPSS, CWE-20 and CWE-502
	cveWildcardGate = "CVE-2026-90002" // wildcard CPE with a 1.0 to 2.0 version range
	cveRejected     = "CVE-2026-90003" // exact CPE, status Rejected
	cveVectorOnly   = "CVE-2026-90004" // CVSS vector and no score
	cveLabelOnly    = "CVE-2026-90005" // severity label and neither score nor vector
	cveOldWindow    = "CVE-2025-90006" // carried only by the older window
)

// CPEs of the recorded products the severity-tier and old-window cases plant.
const (
	vulnVectorOnlyCPE = "cpe:2.3:a:ifa-vendor:vector-only:1.0.0:*:*:*:*:*:*:*"
	vulnLabelOnlyCPE  = "cpe:2.3:a:ifa-vendor:label-only:1.0.0:*:*:*:*:*:*:*"
)

// wantProductEvidencePath is the kinds the NVD product path walks from the
// advisory to an owned image, in order.
var wantProductEvidencePath = []string{
	"vulnerability.cve",
	"vulnerability.affected_product",
	"sbom.component",
	"reducer_sbom_attestation_attachment",
	"reducer_container_image_identity",
}

// catalogOdu returns one cataloged vulnerability-collector Odù.
func catalogOdu(t *testing.T, name string) familyodu.Odu {
	t.Helper()
	catalog, err := CatalogByNameAt(repoRootDir(t))
	if err != nil {
		t.Fatalf("CatalogByNameAt() error = %v", err)
	}
	odu, ok := catalog[name]
	if !ok {
		t.Fatalf("CatalogByNameAt() is missing %q", name)
	}
	return odu
}

// adHocOdu builds an Odù from the recording the way the catalog does, for a
// combination of scopes and planted components the catalog does not hold.
func adHocOdu(t *testing.T, scopes []string, cpes ...string) familyodu.Odu {
	t.Helper()
	spec := cassetteOduSpec{
		name: "odu:ad-hoc-test", cassette: vulnCollectorCassetteRelPath,
		scopes: scopes, planted: plantedSBOMFacts(cpes...),
	}
	byScope, err := readRecordedScopes(repoRootDir(t), spec)
	if err != nil {
		t.Fatalf("readRecordedScopes() error = %v", err)
	}
	odu, err := spec.build(byScope)
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	return odu
}

// impactFindings runs the reducer's pure impact builder, the function the
// supply-chain impact handler calls, over an Odù's facts.
func impactFindings(odu familyodu.Odu) []core.SupplyChainImpactFinding {
	return core.BuildSupplyChainImpactFindings(odu.Facts)
}

// findingFor returns the one finding for cveID and fails when there is not
// exactly one.
func findingFor(t *testing.T, findings []core.SupplyChainImpactFinding, cveID string) core.SupplyChainImpactFinding {
	t.Helper()
	var matched []core.SupplyChainImpactFinding
	for _, finding := range findings {
		if finding.CVEID == cveID {
			matched = append(matched, finding)
		}
	}
	if len(matched) != 1 {
		t.Fatalf("findings for %s = %d, want exactly 1 (all findings: %d)", cveID, len(matched), len(findings))
	}
	return matched[0]
}

func TestVulnCollectorOduReachesTheProductPathFinding(t *testing.T) {
	t.Parallel()

	odu := catalogOdu(t, VulnCollectorOduName)
	findings := impactFindings(odu)
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want exactly 1: only the exact-CPE record has a planted component", len(findings))
	}
	got := findings[0]

	checks := []struct {
		field     string
		got, want any
	}{
		{"CVEID", got.CVEID, cveExactWidget},
		{"Status", got.Status, core.SupplyChainImpactAffectedDerived},
		{"Confidence", got.Confidence, "derived_product"},
		{"ProductCriteria", got.ProductCriteria, vulnWidgetCPE},
		{"RuntimeReachability", got.RuntimeReachability, "image_sbom"},
		{"RepositoryID", got.RepositoryID, plantedRepositoryID},
		{"SubjectDigest", got.SubjectDigest, plantedSubjectDigest},
		{"ImageRef", got.ImageRef, plantedImageRef},
		{"CVSSScore", got.CVSSScore, 9.8},
		{"SeverityLabel", got.SeverityLabel, "CRITICAL"},
		{"SeverityVector", got.SeverityVector, "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"},
		{"SeverityScoreSource", got.SeverityScoreSource, ""},
		{"KnownExploited", got.KnownExploited, true},
		{"ExploitedTier", got.ExploitedTier, core.SupplyChainExploitedTierKEV},
		{"EPSSProbability", got.EPSSProbability, "0.71"},
		{"EPSSPercentile", got.EPSSPercentile, "0.98"},
		{"EPSSScoreDate", got.EPSSScoreDate, "2026-11-05"},
		{"PriorityBucket", got.PriorityBucket, "critical"},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("finding.%s = %#v, want %#v", check.field, check.got, check.want)
		}
	}
	if got.AdvisoryRetraction != nil {
		t.Errorf("finding.AdvisoryRetraction = %+v, want nil: the first window's copy is Analyzed", *got.AdvisoryRetraction)
	}
	if !slices.Equal(got.EvidencePath, wantProductEvidencePath) {
		t.Errorf("finding.EvidencePath = %q, want %q", got.EvidencePath, wantProductEvidencePath)
	}
}

// TestVulnCollectorFindingIsBuiltFromTheRecordedFacts: the severity, KEV and
// EPSS values above could in principle come from anywhere. This ties the
// finding's evidence to the recorded facts of the same CVE, so KEV and EPSS
// attach because the collector emitted them for it.
func TestVulnCollectorFindingIsBuiltFromTheRecordedFacts(t *testing.T) {
	t.Parallel()

	odu := catalogOdu(t, VulnCollectorOduName)
	findings := impactFindings(odu)
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want exactly 1", len(findings))
	}
	got := findings[0]

	byID := make(map[string]facts.Envelope, len(odu.Facts))
	for _, fact := range odu.Facts {
		byID[fact.FactID] = fact
	}
	kinds := map[string]int{}
	for _, id := range got.EvidenceFactIDs {
		fact, ok := byID[id]
		if !ok {
			t.Fatalf("evidence fact id %q is not a fact of the Odù", id)
		}
		kinds[fact.FactKind]++
		if strings.HasPrefix(fact.FactKind, "vulnerability.") && fact.Payload["cve_id"] != cveExactWidget {
			t.Errorf("evidence %s fact is for %v, want %s", fact.FactKind, fact.Payload["cve_id"], cveExactWidget)
		}
	}
	wantKinds := map[string]int{
		"vulnerability.cve": 1, "vulnerability.affected_product": 1,
		"vulnerability.known_exploited": 1, "vulnerability.epss_score": 1,
		"sbom.component": 1, "reducer_sbom_attestation_attachment": 1, "reducer_container_image_identity": 1,
	}
	for kind, want := range wantKinds {
		if kinds[kind] != want {
			t.Errorf("evidence carries %d %s fact(s), want %d", kinds[kind], kind, want)
		}
	}
	if len(got.EvidenceFactIDs) != 7 {
		t.Errorf("len(EvidenceFactIDs) = %d, want 7", len(got.EvidenceFactIDs))
	}
}

// TestVulnCollectorCVECarriesItsCWEs pins what the collector emits, and only
// that. The impact builder does not read CWEs, and the vulnerability.cve v1
// schema does not declare the field (it allows additional properties), so the
// typed contract does not carry it either. What an Odù can show is that the
// collector's cve fact holds the CWEs NVD lists, and that they survive the
// recording.
func TestVulnCollectorCVECarriesItsCWEs(t *testing.T) {
	t.Parallel()

	odu := catalogOdu(t, VulnCollectorOduName)
	for _, fact := range odu.Facts {
		if fact.FactKind != "vulnerability.cve" || fact.Payload["cve_id"] != cveExactWidget {
			continue
		}
		got, ok := fact.Payload["cwes"].([]any)
		if !ok {
			t.Fatalf("cwes = %#v, want a list of CWE ids", fact.Payload["cwes"])
		}
		want := []any{"CWE-20", "CWE-502"}
		if !slices.Equal(got, want) {
			t.Errorf("cwes = %v, want %v", got, want)
		}
		return
	}
	t.Fatalf("the Odù carries no vulnerability.cve fact for %s", cveExactWidget)
}

// TestVulnCollectorOduDetailMatchesTheFinding ties the catalog detail operators
// read to the finding the reducer builds. Each attachment the detail names in
// its "finding with ... attached" clause must be populated on that finding, and
// a name the finding has no field for (CWE, which joins only at read time) fails.
func TestVulnCollectorOduDetailMatchesTheFinding(t *testing.T) {
	t.Parallel()

	entries, err := CatalogAt(repoRootDir(t))
	if err != nil {
		t.Fatalf("CatalogAt() error = %v", err)
	}
	var detail string
	for _, entry := range entries {
		if entry.Odu.Name == VulnCollectorOduName {
			detail = entry.Detail
		}
	}
	_, clause, found := strings.Cut(detail, "finding with ")
	clause, _, closed := strings.Cut(clause, " attached")
	if !found || !closed {
		t.Fatalf("detail %q does not say what the finding carries (want \"finding with ... attached\")", detail)
	}
	findings := impactFindings(catalogOdu(t, VulnCollectorOduName))
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want exactly 1", len(findings))
	}
	got := findings[0]
	carried := map[string]bool{
		"KEV": got.KnownExploited, "EPSS": got.EPSSProbability != "", "severity": got.SeverityLabel != "",
	}
	for _, label := range strings.Split(strings.ReplaceAll(clause, " and ", ", "), ", ") {
		populated, known := carried[label]
		switch {
		case !known:
			t.Errorf("detail says the finding carries %q, but the finding has no such field: %q", label, detail)
		case !populated:
			t.Errorf("detail says the finding carries %q, but it is empty on the finding", label)
		}
	}
}

// TestVulnCollectorSeverityTiers covers the three ways a finding gets its
// severity: the score the source reported, a score derived from the CVSS vector
// when the source reported none, and the label alone when it reported neither.
func TestVulnCollectorSeverityTiers(t *testing.T) {
	t.Parallel()

	odu := adHocOdu(t, []string{vulnWindow1Scope, vulnKEVScope, vulnEPSSScope},
		vulnWidgetCPE, vulnVectorOnlyCPE, vulnLabelOnlyCPE)
	findings := impactFindings(odu)
	if len(findings) != 3 {
		t.Fatalf("findings = %d, want 3 (one per planted component)", len(findings))
	}

	reported := findingFor(t, findings, cveExactWidget)
	if reported.CVSSScore != 9.8 || reported.SeverityScoreSource != "" || reported.SeverityLabel != "CRITICAL" {
		t.Errorf("reported tier = score %v source %q label %q, want 9.8, empty, CRITICAL",
			reported.CVSSScore, reported.SeverityScoreSource, reported.SeverityLabel)
	}

	derived := findingFor(t, findings, cveVectorOnly)
	if derived.CVSSScore != 9.1 || derived.SeverityScoreSource != "derived_from_vector" {
		t.Errorf("vector tier = score %v source %q, want 9.1 derived_from_vector", derived.CVSSScore, derived.SeverityScoreSource)
	}
	if want := "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:N"; derived.SeverityVector != want {
		t.Errorf("vector tier vector = %q, want %q", derived.SeverityVector, want)
	}
	if derived.PriorityBucket != "critical" {
		t.Errorf("vector tier bucket = %q, want critical: a derived 9.1 is critical", derived.PriorityBucket)
	}

	labelled := findingFor(t, findings, cveLabelOnly)
	if labelled.CVSSScore != 0 || labelled.SeverityLabel != "HIGH" || labelled.SeverityVector != "" {
		t.Errorf("label tier = score %v label %q vector %q, want 0, HIGH, empty",
			labelled.CVSSScore, labelled.SeverityLabel, labelled.SeverityVector)
	}
	if labelled.PriorityBucket != "high" {
		t.Errorf("label tier bucket = %q, want high: the label alone sets it", labelled.PriorityBucket)
	}
}

// TestVulnCollectorWildcardRangeIsNotMatched pins that the product path
// compares CPEs exactly. The recorded gadget record names a wildcard CPE with a
// 1.0 to 2.0 version range, and the planted component, 1.2.0, is inside it; the
// reducer still reports no finding. If the reducer gains range matching this
// test fails, and the expectation moves to a finding.
func TestVulnCollectorWildcardRangeIsNotMatched(t *testing.T) {
	t.Parallel()

	odu := catalogOdu(t, VulnCollectorWildcardRangeOduName)

	// Control: the recording really holds the range, so an empty result is not
	// an artifact of a plant that sits outside it.
	var found bool
	for _, fact := range odu.Facts {
		if fact.FactKind != "vulnerability.affected_product" || fact.Payload["cve_id"] != cveWildcardGate {
			continue
		}
		found = true
		if fact.Payload["criteria"] != "cpe:2.3:a:ifa-vendor:gadget:*:*:*:*:*:*:*:*" ||
			fact.Payload["version_start_including"] != "1.0" || fact.Payload["version_end_excluding"] != "2.0" {
			t.Errorf("recorded gadget product = %v, want a wildcard CPE with range [1.0, 2.0)", fact.Payload)
		}
	}
	if !found {
		t.Fatalf("the Odù carries no affected_product fact for %s", cveWildcardGate)
	}
	if _, version := cpeProductVersion(vulnGadgetCPE); version != "1.2.0" {
		t.Fatalf("planted gadget version = %q, want 1.2.0, inside [1.0, 2.0)", version)
	}

	if findings := impactFindings(odu); len(findings) != 0 {
		t.Errorf("findings = %d (%s), want 0: the exact compare does not match a version range", len(findings), findings[0].CVEID)
	}
}

// TestVulnCollectorRejectedCVEKeepsItsRetraction: a CVE NVD rejected still
// reaches the image, and the finding says its source retracted it.
func TestVulnCollectorRejectedCVEKeepsItsRetraction(t *testing.T) {
	t.Parallel()

	findings := impactFindings(catalogOdu(t, VulnCollectorRejectedOduName))
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want exactly 1", len(findings))
	}
	got := findings[0]
	if got.CVEID != cveRejected || got.ProductCriteria != vulnLegacyToolCPE {
		t.Errorf("finding = %s on %s, want %s on %s", got.CVEID, got.ProductCriteria, cveRejected, vulnLegacyToolCPE)
	}
	if got.AdvisoryRetraction == nil {
		t.Fatal("finding.AdvisoryRetraction = nil, want a rejected retraction")
	}
	if got.AdvisoryRetraction.ReasonCode != core.AdvisoryRetractionRejected {
		t.Errorf("retraction reason code = %q, want %q", got.AdvisoryRetraction.ReasonCode, core.AdvisoryRetractionRejected)
	}
	if len(got.AdvisoryRetraction.Sources) != 1 {
		t.Fatalf("retraction sources = %+v, want exactly the NVD record", got.AdvisoryRetraction.Sources)
	}
	source := got.AdvisoryRetraction.Sources[0]
	if source.Source != "nvd" || source.AdvisoryID != cveRejected || source.Code != core.AdvisoryRetractionRejected || source.Status != "Rejected" {
		t.Errorf("retraction source = %+v, want the nvd record of %s with status Rejected", source, cveRejected)
	}
	if state := suppressionState(got); state != core.SupplyChainSuppressionStateAdvisoryRetracted {
		t.Errorf("suppression state = %q, want %q: a retracted finding is hidden from the default view",
			state, core.SupplyChainSuppressionStateAdvisoryRetracted)
	}
}

// suppressionAt is the fixed instant the suppression decisions in these tests
// are evaluated at, so no test reads the wall clock. A retraction decides the
// state before any expiry is compared, so the value does not change a result.
var suppressionAt = time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)

// suppressionState returns the state the supply-chain impact handler gives a
// finding when no operator or VEX suppression is on record. The pure builder
// leaves Suppression unset; the handler fills it with
// EvaluateSupplyChainSuppression, the call made here.
func suppressionState(finding core.SupplyChainImpactFinding) core.SupplyChainSuppressionState {
	return core.EvaluateSupplyChainSuppression(finding, nil, suppressionAt).State
}

// TestVulnCollectorOldWindowCVEHasNoBaseline: a CVE only an older window carries
// has no finding until that window is selected. The control proves the planted
// component is valid, so the empty result is about the window and not the plant.
func TestVulnCollectorOldWindowCVEHasNoBaseline(t *testing.T) {
	t.Parallel()

	if findings := impactFindings(catalogOdu(t, VulnCollectorNoBaselineOduName)); len(findings) != 0 {
		t.Errorf("findings = %d (%s), want 0: the first window does not carry %s", len(findings), findings[0].CVEID, cveOldWindow)
	}

	selected := adHocOdu(t, []string{vulnWindowXScope}, vulnBaselineOnlyCPE)
	findings := impactFindings(selected)
	if len(findings) != 1 {
		t.Fatalf("findings with the older window selected = %d, want exactly 1", len(findings))
	}
	got := findings[0]
	if got.CVEID != cveOldWindow || got.CVSSScore != 6.5 || got.SeverityLabel != "MEDIUM" || got.AdvisoryRetraction != nil {
		t.Errorf("older-window finding = %s score %v label %q retraction %v, want %s 6.5 MEDIUM nil",
			got.CVEID, got.CVSSScore, got.SeverityLabel, got.AdvisoryRetraction, cveOldWindow)
	}
}

// withDateOnlyStamp returns clones of the facts with the modified_at of one
// vulnerability.cve copy (the copy of cveID in scopeID) set to a date with no
// time, which the reducer cannot read as an instant. It also returns how many
// facts it rewrote, so a caller can tell a real rewrite from a typo that
// matched nothing.
func withDateOnlyStamp(input []facts.Envelope, scopeID, cveID string) ([]facts.Envelope, int) {
	out := cloneFacts(input)
	rewritten := 0
	for _, fact := range out {
		if fact.FactKind != "vulnerability.cve" || fact.ScopeID != scopeID || fact.Payload["cve_id"] != cveID {
			continue
		}
		fact.Payload["modified_at"] = "2026-05-20"
		rewritten++
	}
	return out, rewritten
}

// TestVulnCollectorNewestCopyRetracts: the second window re-issues
// CVE-2026-90001 as Rejected, newer than the first window's Analyzed copy —
// and the second window carries no affected_product facts. Stacked on eshu-hq/
// eshu#10735, the elected Rejected copy alone supplies the product set, so the
// first window's widget product is dropped (kept R-B10: a CPE present only in
// an older NVD copy forms no finding): the Odù reaches zero findings, not a
// retracted one. It holds on the facts exactly as recorded: NVD stamps its
// records without a zone ("2026-11-10T07:00:00.000"), and the reducer reads that
// stamp as UTC when it picks the newest copy (eshu-hq/eshu#10741, #10750).
//
// The test keeps its pre-reconciliation name for continuity with the #10735
// handoff (docs/internal/arbiter/10735-ifa-breakage-ruling-2026-11-20.md),
// which transfers this scenario to #10734. The reconciled expectation fails on
// main (which still returns the retracted finding) and passes only stacked on
// #10735, so this update cannot land on main first.
//
// The control shows the result depends on the Rejected copy's stamp. With that
// one stamp replaced by a date and no time, the copy ranks oldest, the Analyzed
// copy wins with its own product, and the finding stays active.
func TestVulnCollectorNewestCopyRetracts(t *testing.T) {
	t.Parallel()

	odu := catalogOdu(t, VulnCollectorNewestCopyOduName)

	if findings := impactFindings(odu); len(findings) != 0 {
		t.Fatalf("findings = %d, want 0: the widget product lives only in the older window, "+
			"so the newer Rejected copy's election drops it (kept R-B10)", len(findings))
	}

	dateOnly := odu
	var rewritten int
	dateOnly.Facts, rewritten = withDateOnlyStamp(odu.Facts, vulnWindow2Scope, cveExactWidget)
	if rewritten != 1 {
		t.Fatalf("rewrote %d cve facts, want exactly the second window's copy of %s", rewritten, cveExactWidget)
	}
	control := findingFor(t, impactFindings(dateOnly), cveExactWidget)
	if control.AdvisoryRetraction != nil {
		t.Errorf("finding.AdvisoryRetraction = %+v with the Rejected copy's stamp date-only, want nil: "+
			"an unreadable stamp ranks oldest, so the Analyzed copy wins", *control.AdvisoryRetraction)
	}
	if state := suppressionState(control); state != core.SupplyChainSuppressionStateActive {
		t.Errorf("suppression state with the Rejected copy's stamp date-only = %q, want %q",
			state, core.SupplyChainSuppressionStateActive)
	}
}
