// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Issue #9693: the severity bucket used to come from cvss_score alone, so a
// finding whose source published only a CVSS vector or only a severity label
// was bucketed "none". These tests pin the derivation on the advisory path
// (OSV-shaped vulnerability.cve facts), the NVD product path, and the
// provider-alert path. Vectors and scores come from the FIRST CVSS examples
// (https://www.first.org/cvss/examples): the v3.1 vector below scores 9.8 and
// the v4.0 vector scores 7.7 there.
const (
	criticalV31Vector = "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"
	highV40Vector     = "CVSS:4.0/AV:N/AC:L/AT:P/PR:N/UI:P/VC:H/VI:H/VA:H/SC:N/SI:N/SA:N"
)

// osvLikeCVEFact builds a vulnerability.cve fact the way the OSV collector
// emits it after item 1.1 (#9692): a vector and a label, never a score.
func osvLikeCVEFact(factID, cveID string, extra map[string]any) facts.Envelope {
	payload := map[string]any{
		"cve_id":      cveID,
		"advisory_id": "GHSA-" + factID,
		"source":      "osv",
		"aliases":     []any{cveID},
	}
	for key, value := range extra {
		payload[key] = value
	}
	return facts.Envelope{FactID: factID, FactKind: "vulnerability.cve", Payload: payload}
}

func advisoryFindingFor(t *testing.T, cve facts.Envelope) SupplyChainImpactFinding {
	t.Helper()
	cveID, _ := cve.Payload["cve_id"].(string)
	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		cve,
		vulnerabilityAffectedPackageFact("affected-"+cve.FactID, cveID, testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithChain("consume-"+cve.FactID, testImpactPackageID, testImpactRepositoryID, "1.2.3", []string{"example"}, 1, true),
	})
	if len(findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1: %#v", len(findings), findings)
	}
	return findings[0]
}

func provenanceOf(t *testing.T, finding SupplyChainImpactFinding) map[string]any {
	t.Helper()
	payload := supplyChainImpactPayload(SupplyChainImpactWrite{}, finding)
	provenance, _ := payload["provenance"].(map[string]any)
	return provenance
}

func TestAdvisoryPathDerivesSeverityFromVector(t *testing.T) {
	t.Parallel()

	finding := advisoryFindingFor(t, osvLikeCVEFact("vec", "CVE-2026-93001", map[string]any{
		"cvss_vector":    criticalV31Vector,
		"severity_label": "MODERATE",
	}))
	if finding.CVSSScore != 9.8 {
		t.Fatalf("CVSSScore = %v, want 9.8 derived from the v3.1 vector", finding.CVSSScore)
	}
	if finding.SeverityScoreSource != "derived_from_vector" {
		t.Fatalf("SeverityScoreSource = %q, want derived_from_vector", finding.SeverityScoreSource)
	}
	provenance := provenanceOf(t, finding)
	if provenance["selected_severity_score_source"] != "derived_from_vector" {
		t.Fatalf("provenance = %#v, want selected_severity_score_source=derived_from_vector", provenance)
	}
	if provenance["selected_severity_vector"] != criticalV31Vector || provenance["selected_severity_score"] != 9.8 {
		t.Fatalf("provenance = %#v, want the vector and the derived score", provenance)
	}
	payload := supplyChainImpactPayload(SupplyChainImpactWrite{}, finding)
	if payload["cvss_score"] != 9.8 {
		t.Fatalf("payload cvss_score = %v, want 9.8 so the SQL bucket sees it", payload["cvss_score"])
	}
}

func TestAdvisoryPathDerivesSeverityFromV4Vector(t *testing.T) {
	t.Parallel()

	finding := advisoryFindingFor(t, osvLikeCVEFact("v4", "CVE-2026-93002", map[string]any{
		"cvss_vector": highV40Vector,
	}))
	if finding.CVSSScore != 7.7 || finding.SeverityScoreSource != "derived_from_vector" {
		t.Fatalf("score = %v source = %q, want 7.7 derived_from_vector", finding.CVSSScore, finding.SeverityScoreSource)
	}
}

func TestAdvisoryPathLabelOnlyKeepsNoScoreAndStoresTheLabel(t *testing.T) {
	t.Parallel()

	finding := advisoryFindingFor(t, osvLikeCVEFact("label", "CVE-2026-93003", map[string]any{
		"severity_label": "MODERATE",
	}))
	if finding.CVSSScore != 0 || finding.SeverityScoreSource != "" {
		t.Fatalf("score = %v source = %q, want no score and no score source for a label-only record", finding.CVSSScore, finding.SeverityScoreSource)
	}
	provenance := provenanceOf(t, finding)
	if provenance["selected_severity_label"] != "MODERATE" {
		t.Fatalf("provenance = %#v, want selected_severity_label=MODERATE so SQL can bucket it", provenance)
	}
	if _, present := provenance["selected_severity_score_source"]; present {
		t.Fatalf("provenance = %#v, a label-only record must not carry a score source", provenance)
	}
	// An OSV record with a GHSA advisory id is attributed to ghsa by the
	// existing source classification, which the label-only path reuses.
	if finding.SeveritySource != "ghsa" {
		t.Fatalf("SeveritySource = %q, want ghsa", finding.SeveritySource)
	}
}

func TestAdvisoryPathScoreOnlyIsUnchangedAndUnmarked(t *testing.T) {
	t.Parallel()

	finding := advisoryFindingFor(t, osvLikeCVEFact("score", "CVE-2026-93004", map[string]any{
		"cvss_score": 5.0,
	}))
	if finding.CVSSScore != 5.0 || finding.SeverityScoreSource != "" {
		t.Fatalf("score = %v source = %q, want 5.0 with no marker for a source-reported score", finding.CVSSScore, finding.SeverityScoreSource)
	}
	if _, present := provenanceOf(t, finding)["selected_severity_score_source"]; present {
		t.Fatal("a source-reported score must not carry selected_severity_score_source")
	}
}

func TestAdvisoryPathReportedScoreOutranksVectorAndLabel(t *testing.T) {
	t.Parallel()

	finding := advisoryFindingFor(t, osvLikeCVEFact("both", "CVE-2026-93005", map[string]any{
		"cvss_score":     5.0,
		"cvss_vector":    criticalV31Vector,
		"severity_label": "CRITICAL",
	}))
	if finding.CVSSScore != 5.0 || finding.SeverityScoreSource != "" {
		t.Fatalf("score = %v source = %q, want the reported 5.0 to win", finding.CVSSScore, finding.SeverityScoreSource)
	}
}

func TestAdvisoryPathNoSeverityStaysEmpty(t *testing.T) {
	t.Parallel()

	finding := advisoryFindingFor(t, osvLikeCVEFact("none", "CVE-2026-93006", nil))
	if finding.CVSSScore != 0 || finding.SeverityScoreSource != "" || finding.SeverityLabel != "" {
		t.Fatalf("finding = %+v, want no severity evidence invented", finding)
	}
}

func TestAdvisoryPathMalformedVectorNeverInventsAScore(t *testing.T) {
	t.Parallel()

	for _, vector := range []string{"CVSS:3.1/AV:N", "garbage", "CVSS:3.1/AV:Z/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", "AV:N/AC:L/Au:N/C:C/I:C/A:C"} {
		finding := advisoryFindingFor(t, osvLikeCVEFact("bad", "CVE-2026-93007", map[string]any{"cvss_vector": vector}))
		if finding.CVSSScore != 0 || finding.SeverityScoreSource != "" {
			t.Errorf("vector %q: score = %v source = %q, want none", vector, finding.CVSSScore, finding.SeverityScoreSource)
		}
	}
	finding := advisoryFindingFor(t, osvLikeCVEFact("bad-label", "CVE-2026-93008", map[string]any{
		"cvss_vector":    "garbage",
		"severity_label": "HIGH",
	}))
	if finding.CVSSScore != 0 || provenanceOf(t, finding)["selected_severity_label"] != "HIGH" {
		t.Fatalf("a malformed vector must fall back to the label, got score %v provenance %#v", finding.CVSSScore, provenanceOf(t, finding))
	}
}

// TestAdvisoryPathScoreTierOutranksLabelOnlySourceAcrossSources keeps the
// existing selection rule: a source with a score (reported or derived) is
// chosen before a higher-priority source that carries only a label.
func TestAdvisoryPathScoreTierOutranksLabelOnlySourceAcrossSources(t *testing.T) {
	t.Parallel()

	const cveID = "CVE-2026-93009"
	labelOnly := osvLikeCVEFact("osv-label", cveID, map[string]any{"severity_label": "LOW"})
	scored := vulnerabilityCVEFact("nvd-score", cveID, 8.1)
	scored.Payload["source"] = "nvd"
	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		labelOnly, scored,
		vulnerabilityAffectedPackageFact("affected-93009", cveID, testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithChain("consume-93009", testImpactPackageID, testImpactRepositoryID, "1.2.3", []string{"example"}, 1, true),
	})
	if len(findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1: %#v", len(findings), findings)
	}
	if findings[0].CVSSScore != 8.1 || findings[0].SeverityScoreSource != "" {
		t.Fatalf("score = %v source = %q, want the NVD-reported 8.1", findings[0].CVSSScore, findings[0].SeverityScoreSource)
	}
}

func TestProductPathDerivesSeverityFromVector(t *testing.T) {
	t.Parallel()

	cve := vulnerabilityCVEFact("cve-product-vec", "CVE-2026-93010", 0)
	delete(cve.Payload, "cvss_score")
	cve.Payload["cvss_vector"] = criticalV31Vector
	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		cve,
		vulnerabilityAffectedProductFact("product-vec", "CVE-2026-93010", testImpactProductCriteria, testImpactMatchCriteriaID, true),
		sbomComponentCPEImpactFact("component-vec", "doc-vec", testImpactProductCriteria),
		sbomAttachmentImpactFact("attachment-vec", "doc-vec", testImpactSubjectDigest),
		containerImageIdentityImpactFact("image-vec", testImpactSubjectDigest, testImpactRepositoryID),
	})
	if len(findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1: %#v", len(findings), findings)
	}
	if findings[0].CVSSScore != 9.8 || findings[0].SeverityScoreSource != "derived_from_vector" {
		t.Fatalf("score = %v source = %q, want 9.8 derived_from_vector", findings[0].CVSSScore, findings[0].SeverityScoreSource)
	}
}

func alertFindingFor(t *testing.T, alertExtra map[string]any) SupplyChainImpactFinding {
	t.Helper()
	repoID := "repo://github/example/api"
	packageID := "pypi://pypi.org/django"
	payload := map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(901),
		"provider_state":        "open",
		"package_id":            packageID,
		"ecosystem":             "pypi",
		"package_name":          "django",
		"manifest_path":         "requirements.txt",
		"ghsa_ids":              []string{"GHSA-aaaa-bbbb-cccc"},
		"cve_ids":               []string{"CVE-2026-93020"},
		"vulnerable_range":      ">= 2.0.0, < 2.0.2",
		"patched_version":       "2.0.2",
	}
	for key, value := range alertExtra {
		payload[key] = value
	}
	alert := securityAlertEnvelope("alert-9693", repoID, payload)
	consumption := packageConsumptionCorrelationEnvelope("consume-9693", repoID, packageID, "requirements.txt")
	consumption.Payload["dependency_range"] = "2.0.1"
	findings := BuildSupplyChainImpactFindings([]facts.Envelope{alert, consumption})
	if len(findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1: %#v", len(findings), findings)
	}
	return findings[0]
}

func TestAlertPathDerivesSeverityFromVector(t *testing.T) {
	t.Parallel()

	finding := alertFindingFor(t, map[string]any{
		"severity": "high",
		"cvss":     map[string]any{"vector": criticalV31Vector},
	})
	if finding.CVSSScore != 9.8 || finding.SeverityScoreSource != "derived_from_vector" {
		t.Fatalf("score = %v source = %q, want 9.8 derived_from_vector", finding.CVSSScore, finding.SeverityScoreSource)
	}
	if provenanceOf(t, finding)["selected_severity_score_source"] != "derived_from_vector" {
		t.Fatalf("provenance = %#v, want the derived marker", provenanceOf(t, finding))
	}
}

func TestAlertPathLabelOnlyKeepsNoScore(t *testing.T) {
	t.Parallel()

	finding := alertFindingFor(t, map[string]any{"severity": "high"})
	if finding.CVSSScore != 0 || finding.SeverityScoreSource != "" {
		t.Fatalf("score = %v source = %q, want no score for a label-only alert", finding.CVSSScore, finding.SeverityScoreSource)
	}
	if provenanceOf(t, finding)["selected_severity_label"] != "high" {
		t.Fatalf("provenance = %#v, want selected_severity_label=high", provenanceOf(t, finding))
	}
}

func TestAlertPathScoreOnlyIsUnchangedAndUnmarked(t *testing.T) {
	t.Parallel()

	finding := alertFindingFor(t, map[string]any{
		"severity": "medium",
		"cvss":     map[string]any{"score": 6.5, "vector": criticalV31Vector},
	})
	if finding.CVSSScore != 6.5 || finding.SeverityScoreSource != "" {
		t.Fatalf("score = %v source = %q, want the provider-reported 6.5 unmarked", finding.CVSSScore, finding.SeverityScoreSource)
	}
}

func TestAlertPathNoSeverityAndMalformedVectorStayEmpty(t *testing.T) {
	t.Parallel()

	finding := alertFindingFor(t, map[string]any{})
	if finding.CVSSScore != 0 || finding.SeverityScoreSource != "" {
		t.Fatalf("score = %v source = %q, want none", finding.CVSSScore, finding.SeverityScoreSource)
	}
	finding = alertFindingFor(t, map[string]any{"cvss": map[string]any{"vector": "CVSS:3.1/AV:N"}})
	if finding.CVSSScore != 0 || finding.SeverityScoreSource != "" {
		t.Fatalf("malformed vector: score = %v source = %q, want none", finding.CVSSScore, finding.SeverityScoreSource)
	}
}

// TestDerivedScoreFeedsPriorityOnce pins the visible side effect of deriving
// a score: the finding now earns the CVSS priority contribution a reported
// score earns, computed from the derived value.
func TestDerivedScoreFeedsPriority(t *testing.T) {
	t.Parallel()

	finding := advisoryFindingFor(t, osvLikeCVEFact("prio", "CVE-2026-93030", map[string]any{
		"cvss_vector": criticalV31Vector,
	}))
	found := false
	for _, code := range finding.PriorityReasonCodes {
		if code == "cvss_v3_critical" {
			found = true
		}
	}
	if !found {
		t.Fatalf("PriorityReasonCodes = %v, want cvss_v3_critical from the derived 9.8", finding.PriorityReasonCodes)
	}
}

// TestAdvisoryPathReportedScoreOutranksDerivedScoreAcrossSources pins the
// selection tiers for the 1.1/1.2 handoff: once the OSV collector puts a
// cvss_vector and severity_label on its facts (#9692), the OSV observation
// carries a derived score of 9.8 and an NVD observation carries a reported 8.1.
// For an npm package GHSA outranks NVD by source priority, so a priority-only
// rule would let the derived 9.8 beat the reported 8.1. The rule is tiered
// instead: every source-reported score ranks ahead of every derived score
// (derived scores only fill gaps), and a derived score ranks ahead of a label.
// The losing observation stays visible as an alternate, still marked.
func TestAdvisoryPathReportedScoreOutranksDerivedScoreAcrossSources(t *testing.T) {
	t.Parallel()

	const cveID = "CVE-2026-93040"
	osv := osvLikeCVEFact("osv-vec", cveID, map[string]any{
		"cvss_vector":    criticalV31Vector,
		"severity_label": "critical",
	})
	nvd := vulnerabilityCVEFact("nvd-score", cveID, 8.1)
	nvd.Payload["source"] = "nvd"
	build := func(cves ...facts.Envelope) SupplyChainImpactFinding {
		envelopes := append([]facts.Envelope{}, cves...)
		envelopes = append(envelopes,
			vulnerabilityAffectedPackageFact("affected-93040", cveID, testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
			packageConsumptionFactWithChain("consume-93040", testImpactPackageID, testImpactRepositoryID, "1.2.3", []string{"example"}, 1, true),
		)
		findings := BuildSupplyChainImpactFindings(envelopes)
		if len(findings) != 1 {
			t.Fatalf("len(findings) = %d, want 1: %#v", len(findings), findings)
		}
		return findings[0]
	}

	withNVD := build(osv, nvd)
	if withNVD.CVSSScore != 8.1 || withNVD.SeverityScoreSource != "" || withNVD.SeveritySource != "nvd" {
		t.Fatalf("score = %v source = %q severity source = %q, want the NVD-reported 8.1 unmarked",
			withNVD.CVSSScore, withNVD.SeverityScoreSource, withNVD.SeveritySource)
	}
	var alternate *AlternateSeverity
	for i := range withNVD.AlternateSeverities {
		if withNVD.AlternateSeverities[i].Score == 9.8 {
			alternate = &withNVD.AlternateSeverities[i]
		}
	}
	if alternate == nil || alternate.ScoreSource != "derived_from_vector" {
		t.Fatalf("AlternateSeverities = %+v, want the OSV 9.8 kept as an alternate marked derived_from_vector", withNVD.AlternateSeverities)
	}

	// The marker must also reach the persisted payload, not only the struct:
	// the derived alternate carries score_source, the reported one does not.
	rows, _ := provenanceOf(t, withNVD)["alternate_severities"].([]map[string]any)
	var derivedRows, reportedRows int
	for _, row := range rows {
		switch row["score"] {
		case 9.8:
			if row["score_source"] != "derived_from_vector" {
				t.Fatalf("alternate row %#v, want score_source=derived_from_vector on the derived 9.8", row)
			}
			derivedRows++
		default:
			if _, present := row["score_source"]; present {
				t.Fatalf("alternate row %#v carries score_source but its score was not derived", row)
			}
			reportedRows++
		}
	}
	if derivedRows != 1 {
		t.Fatalf("payload alternate_severities = %#v, want exactly one derived 9.8 row (reported rows: %d)", rows, reportedRows)
	}

	osvOnly := build(osv)
	if osvOnly.CVSSScore != 9.8 || osvOnly.SeverityScoreSource != "derived_from_vector" || osvOnly.SeveritySource != "ghsa" {
		t.Fatalf("score = %v marker = %q source = %q, want the derived 9.8 to win when no source reports a score",
			osvOnly.CVSSScore, osvOnly.SeverityScoreSource, osvOnly.SeveritySource)
	}
	provenance := provenanceOf(t, osvOnly)
	if provenance["selected_severity_score_source"] != "derived_from_vector" {
		t.Fatalf("provenance = %#v, the derived marker must survive selection", provenance)
	}

	// A derived score from a lower-priority source still beats a label-only
	// observation from a higher-priority source.
	labelOnly := osvLikeCVEFact("osv-label", cveID, map[string]any{"severity_label": "low"})
	nvdVector := vulnerabilityCVEFact("nvd-vec", cveID, 0)
	delete(nvdVector.Payload, "cvss_score")
	nvdVector.Payload["source"] = "nvd"
	nvdVector.Payload["cvss_vector"] = criticalV31Vector
	mixed := build(labelOnly, nvdVector)
	if mixed.CVSSScore != 9.8 || mixed.SeverityScoreSource != "derived_from_vector" || mixed.SeveritySource != "nvd" {
		t.Fatalf("score = %v marker = %q source = %q, want the NVD derived 9.8 ahead of the OSV label",
			mixed.CVSSScore, mixed.SeverityScoreSource, mixed.SeveritySource)
	}
}
