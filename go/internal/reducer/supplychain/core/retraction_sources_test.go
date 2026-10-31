// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// retractionPackageFact builds one affected_package fact for the shared test
// package, with its own fact id so several sources can supply one.
func retractionPackageFact(factID, cveID, source, advisoryID, fixedVersion string) facts.Envelope {
	return vulnerabilityAffectedPackageFactWithSource(
		factID, cveID, source, advisoryID, retractionPackageID, "npm", "widget", "1.0.0", fixedVersion,
	)
}

func retractionConsumption() facts.Envelope {
	return packageConsumptionFactWithRange("consume-1", retractionPackageID, testImpactRepositoryID, "1.0.0")
}

// quarantinedCVEFact is a vulnerability.cve fact whose status has the wrong JSON
// type, so the typed decode quarantines it and it never reaches the index.
func quarantinedCVEFact(factID, cveID, source, advisoryID string) facts.Envelope {
	envelope := retractionCVEFact(factID, cveID, source, advisoryID, "")
	envelope.Payload["status"] = float64(7)
	return envelope
}

// TestMissingLiveCVEFactDoesNotLetARejectionRetract is the F1 repro: a live
// source's affected_package is present but its own vulnerability.cve fact is
// missing. The package is a live source, so the finding must stay active.
func TestMissingLiveCVEFactDoesNotLetARejectionRetract(t *testing.T) {
	t.Parallel()

	finding := buildRetractionFinding(t,
		retractionCVEFact("nvd-cve", "CVE-2026-4101", "nvd", "CVE-2026-4101", "Rejected", "CVE-2026-4102"),
		retractionPackageFact("ghsa-pkg", "CVE-2026-4101", "osv", "GHSA-live-aaaa-bbbb", "1.0.1"),
		retractionConsumption(),
	)
	if got := finding.Suppression.State; got != SupplyChainSuppressionStateActive {
		t.Fatalf("state = %q, want active: a live source still asserts the vulnerability through its package", got)
	}
	if finding.AdvisoryRetraction != nil {
		t.Fatalf("AdvisoryRetraction = %#v, want nil", finding.AdvisoryRetraction)
	}
}

// TestQuarantinedLiveCVEFactDoesNotLetARejectionRetract covers the same
// hazard when the live source's CVE fact exists but fails to decode.
func TestQuarantinedLiveCVEFactDoesNotLetARejectionRetract(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		retractionCVEFact("nvd-cve", "CVE-2026-4111", "nvd", "CVE-2026-4111", "Rejected"),
		quarantinedCVEFact("ghsa-cve", "CVE-2026-4111", "osv", "GHSA-live-cccc-dddd"),
		retractionPackageFact("ghsa-pkg", "CVE-2026-4111", "osv", "GHSA-live-cccc-dddd", "1.0.1"),
		retractionConsumption(),
	}
	findings, quarantined, err := buildSupplyChainImpactFindingsWithQuarantine(envelopes)
	if err != nil {
		t.Fatalf("build error = %v", err)
	}
	if len(quarantined) != 1 {
		t.Fatalf("quarantined = %d, want the one malformed CVE fact", len(quarantined))
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}
	finding := applyRetractionDecision(findings[0])
	if got := finding.Suppression.State; got != SupplyChainSuppressionStateActive {
		t.Fatalf("state = %q, want active while a live source's package is present", got)
	}
}

// TestQuarantinedRejectedFactAndLiveSourceStaysActive is the mirror: the
// rejected record is the quarantined one.
func TestQuarantinedRejectedFactAndLiveSourceStaysActive(t *testing.T) {
	t.Parallel()

	rejected := quarantinedCVEFact("nvd-cve", "CVE-2026-4121", "nvd", "CVE-2026-4121")
	finding := buildRetractionFinding(t,
		rejected,
		retractionCVEFact("ghsa-cve", "CVE-2026-4121", "osv", "GHSA-live-eeee-ffff", ""),
		retractionPackageFact("ghsa-pkg", "CVE-2026-4121", "osv", "GHSA-live-eeee-ffff", "1.0.1"),
		retractionConsumption(),
	)
	if got := finding.Suppression.State; got != SupplyChainSuppressionStateActive {
		t.Fatalf("state = %q, want active", got)
	}
}

// TestEveryCVEFactQuarantinedEmitsNoFindingAndDoesNotPanic pins the remaining
// mix: with no usable CVE fact there is no group, so there is nothing to
// retract and nothing to report as a finding.
func TestEveryCVEFactQuarantinedEmitsNoFindingAndDoesNotPanic(t *testing.T) {
	t.Parallel()

	findings, quarantined, err := buildSupplyChainImpactFindingsWithQuarantine([]facts.Envelope{
		quarantinedCVEFact("nvd-cve", "CVE-2026-4131", "nvd", "CVE-2026-4131"),
		quarantinedCVEFact("ghsa-cve", "CVE-2026-4131", "osv", "GHSA-gone-aaaa-bbbb"),
		retractionPackageFact("ghsa-pkg", "CVE-2026-4131", "osv", "GHSA-gone-aaaa-bbbb", "1.0.1"),
		retractionConsumption(),
	})
	if err != nil {
		t.Fatalf("build error = %v", err)
	}
	if len(findings) != 0 || len(quarantined) != 2 {
		t.Fatalf("findings = %d quarantined = %d, want 0 and 2", len(findings), len(quarantined))
	}
}

// TestOnePackageFromAnUnmatchedSourceKeepsTheGroupActive: two sources supply a
// package; one has a withdrawn CVE fact, the other has no CVE fact at all.
func TestOnePackageFromAnUnmatchedSourceKeepsTheGroupActive(t *testing.T) {
	t.Parallel()

	withdrawn := vulnerabilityCVEFactWithdrawn(
		"gh-cve", "CVE-2026-4141", "osv", "GHSA-wdrn-aaaa-bbbb", 7.5, "HIGH",
		"2026-05-18T10:00:00Z", "2026-05-22T08:00:00Z",
	)
	finding := buildRetractionFinding(t,
		withdrawn,
		retractionPackageFact("gh-pkg", "CVE-2026-4141", "osv", "GHSA-wdrn-aaaa-bbbb", "1.0.1"),
		retractionPackageFact("other-pkg", "CVE-2026-4141", "gitlab", "GLSA-live-1", "1.0.2"),
		retractionConsumption(),
	)
	if got := finding.Suppression.State; got != SupplyChainSuppressionStateActive {
		t.Fatalf("state = %q, want active: the gitlab package has no retracting record", got)
	}
}

// TestAliasToARejectedCVEWithoutAnNVDPackageStaysActive: the GHSA record names
// the CVE as an alias and is live; NVD rejects the CVE and names no package.
func TestAliasToARejectedCVEWithoutAnNVDPackageStaysActive(t *testing.T) {
	t.Parallel()

	finding := buildRetractionFinding(t,
		retractionCVEFact("nvd-cve", "CVE-2026-4151", "nvd", "CVE-2026-4151", "Rejected"),
		retractionCVEFact("ghsa-cve", "CVE-2026-4151", "osv", "GHSA-live-gggg-hhhh", ""),
		retractionPackageFact("ghsa-pkg", "CVE-2026-4151", "osv", "GHSA-live-gggg-hhhh", "1.0.1"),
		retractionConsumption(),
	)
	if got := finding.Suppression.State; got != SupplyChainSuppressionStateActive {
		t.Fatalf("state = %q, want active", got)
	}
}

// TestRejectedStatusDoesNotMoveFindingIdentityInAMultiSourceGroup is the F4
// repro. NVD scores a CVE and a vector-only GHSA record shares it; NVD is the
// identity source. Rejecting NVD must not move advisory_id or finding_id, and
// an operator suppression scoped to that advisory must keep matching.
func TestRejectedStatusDoesNotMoveFindingIdentityInAMultiSourceGroup(t *testing.T) {
	t.Parallel()

	build := func(status string) SupplyChainImpactFinding {
		nvd := retractionCVEFact("nvd-cve", "CVE-2026-3001", "nvd", "CVE-2026-3001", status)
		nvd.Payload["cvss_score"] = 9.8
		ghsa := retractionCVEFact("ghsa-cve", "CVE-2026-3001", "osv", "GHSA-aaaa-bbbb-cccc", "")
		delete(ghsa.Payload, "cvss_score")
		ghsa.Payload["cvss_vector"] = "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"
		delete(ghsa.Payload, "severity_label")
		return buildRetractionFinding(t,
			nvd, ghsa,
			retractionPackageFact("ghsa-pkg", "CVE-2026-3001", "osv", "GHSA-aaaa-bbbb-cccc", "1.0.1"),
			retractionConsumption(),
		)
	}
	rejected, analyzed := build("Rejected"), build("Analyzed")

	if rejected.AdvisoryID != analyzed.AdvisoryID {
		t.Fatalf("advisory_id moved from %q to %q when NVD was rejected", analyzed.AdvisoryID, rejected.AdvisoryID)
	}
	if supplyChainImpactFindingID(rejected) != supplyChainImpactFindingID(analyzed) {
		t.Fatalf("finding_id moved when NVD was rejected")
	}
	suppression := []vulnerabilitySuppression{{
		SuppressionID: "op-1",
		Source:        chain.VulnerabilitySuppressionSourcePolicy,
		Justification: chain.VulnerabilitySuppressionJustificationAcceptedRisk,
		AuthoredAt:    time.Unix(10, 0).UTC(),
		Scope: vulnerabilitySuppressionScope{
			AdvisoryID: analyzed.AdvisoryID,
			PackageID:  retractionPackageID,
		},
	}}
	for name, finding := range map[string]SupplyChainImpactFinding{"rejected": rejected, "analyzed": analyzed} {
		decision := EvaluateSupplyChainSuppression(finding, suppression, time.Unix(20, 0).UTC())
		if decision.State != SupplyChainSuppressionStateAcceptedRisk {
			t.Fatalf("%s: suppression state = %q, want accepted_risk: the scope's advisory id no longer matches", name, decision.State)
		}
	}
	// The retracted source still must not supply severity: the live record does.
	if rejected.SeveritySource == "nvd" {
		t.Fatalf("severity source = %q, want the live record, not the rejected one", rejected.SeveritySource)
	}
}

// TestCVEListShapedRejectedRecordRetractsBySourceNameAlone proves the rule is
// source-agnostic: a CVE Record Format shaped fact (state REJECTED, a
// replacement) retracts like an NVD one (the item 4.1 collector shape).
func TestCVEListShapedRejectedRecordRetractsBySourceNameAlone(t *testing.T) {
	t.Parallel()

	finding := buildRetractionFinding(t,
		retractionCVEFact("cna-cve", "CVE-2026-4161", "cve_list", "CVE-2026-4161", "REJECTED", "CVE-2026-4162"),
		retractionPackageFact("cna-pkg", "CVE-2026-4161", "cve_list", "CVE-2026-4161", "1.0.1"),
		retractionConsumption(),
	)
	if got := finding.Suppression.State; got != SupplyChainSuppressionStateAdvisoryRetracted {
		t.Fatalf("state = %q, want advisory_retracted", got)
	}
	if want := []string{"CVE-2026-4162"}; !reflect.DeepEqual(finding.AdvisoryRetraction.ReplacedBy, want) {
		t.Fatalf("ReplacedBy = %v, want %v", finding.AdvisoryRetraction.ReplacedBy, want)
	}
}

// TestRejectedRecordDoesNotSupplyTheFallbackLabel kills the label-fallback
// mutant: no record has a score, a rejected record ranks first, and its label
// must not be used while a live record has one.
func TestRejectedRecordDoesNotSupplyTheFallbackLabel(t *testing.T) {
	t.Parallel()

	rejected := retractionCVEFact("ghsa-cve", "CVE-2026-4171", "osv", "GHSA-rej-aaaa-bbbb", "Rejected")
	delete(rejected.Payload, "cvss_score")
	rejected.Payload["severity_label"] = "CRITICAL"
	live := retractionCVEFact("nvd-cve", "CVE-2026-4171", "nvd", "CVE-2026-4171", "")
	delete(live.Payload, "cvss_score")
	live.Payload["severity_label"] = "LOW"
	finding := buildRetractionFinding(t,
		rejected, live,
		retractionPackageFact("pkg", "CVE-2026-4171", "nvd", "CVE-2026-4171", "1.0.1"),
		retractionConsumption(),
	)
	if finding.SeverityLabel != "LOW" {
		t.Fatalf("SeverityLabel = %q, want LOW from the live record, not CRITICAL from the rejected one", finding.SeverityLabel)
	}
}

// TestRejectedRecordDoesNotSupplyTheFixedVersion: the rejected record sorts
// first and names fix 1.0.5; the live record names 1.0.9 and must win.
func TestRejectedRecordDoesNotSupplyTheFixedVersion(t *testing.T) {
	t.Parallel()

	finding := buildRetractionFinding(t,
		retractionCVEFact("rej-cve", "CVE-2026-4181", "osv", "GHSA-aaaa-rej0-0000", "Rejected"),
		retractionCVEFact("live-cve", "CVE-2026-4181", "osv", "GHSA-zzzz-live-0000", ""),
		retractionPackageFact("rej-pkg", "CVE-2026-4181", "osv", "GHSA-aaaa-rej0-0000", "1.0.5"),
		retractionPackageFact("live-pkg", "CVE-2026-4181", "osv", "GHSA-zzzz-live-0000", "1.0.9"),
		retractionConsumption(),
	)
	if finding.FixedVersion != "1.0.9" {
		t.Fatalf("FixedVersion = %q, want 1.0.9 from the live record", finding.FixedVersion)
	}
	if len(finding.FixedVersionBranches) != 1 || finding.FixedVersionBranches[0].Version != "1.0.9" {
		t.Fatalf("FixedVersionBranches = %#v, want only the live record's 1.0.9", finding.FixedVersionBranches)
	}
}

// TestPartlyRetractedGroupKeepsTheRejectionAsSerializedProvenance proves the
// writer carries the rejecting source's status and replacement under
// provenance.advisory_sources[] when the group stays active.
func TestPartlyRetractedGroupKeepsTheRejectionAsSerializedProvenance(t *testing.T) {
	t.Parallel()

	finding := buildRetractionFinding(t,
		retractionCVEFact("nvd-cve", "CVE-2026-4191", "nvd", "CVE-2026-4191", "Rejected", "CVE-2026-4192"),
		retractionCVEFact("ghsa-cve", "CVE-2026-4191", "osv", "GHSA-live-iiii-jjjj", ""),
		retractionPackageFact("ghsa-pkg", "CVE-2026-4191", "osv", "GHSA-live-iiii-jjjj", "1.0.1"),
		retractionConsumption(),
	)
	payload := supplyChainImpactTypedPayload(SupplyChainImpactWrite{IntentID: "i", ScopeID: "s", GenerationID: "g"}, finding)
	rows, _ := payload.Provenance["advisory_sources"].([]map[string]any)
	var nvd map[string]any
	for _, row := range rows {
		if row["source"] == "nvd" {
			nvd = row
		}
	}
	if nvd == nil {
		t.Fatalf("no nvd advisory source in %#v", rows)
	}
	if nvd["status"] != "Rejected" || !reflect.DeepEqual(nvd["replaced_by"], []string{"CVE-2026-4192"}) {
		t.Fatalf("nvd advisory source = %#v, want status Rejected and replaced_by [CVE-2026-4192]", nvd)
	}
	if _, present := payload.Provenance["advisory_retraction"]; present {
		t.Fatalf("a partly retracted group must carry no advisory_retraction block")
	}
}

// TestRetractionSourcesBlockIsSerializedAndOrdered proves the writer emits the
// documented sources[] block, ordered by source then advisory id whatever the
// input order, with a reason that does not depend on that order.
func TestRetractionSourcesBlockIsSerializedAndOrdered(t *testing.T) {
	t.Parallel()

	nvd := retractionCVEFact("nvd-cve", "CVE-2026-4201", "nvd", "CVE-2026-4201", "Rejected", "CVE-2026-4202")
	ghsa := vulnerabilityCVEFactWithdrawn(
		"gh-cve", "CVE-2026-4201", "osv", "GHSA-wdrn-kkkk-llll", 7.5, "HIGH",
		"2026-05-18T10:00:00Z", "2026-05-22T08:00:00Z",
	)
	tail := []facts.Envelope{
		retractionPackageFact("gh-pkg", "CVE-2026-4201", "osv", "GHSA-wdrn-kkkk-llll", "1.0.1"),
		retractionConsumption(),
	}
	var reasons []string
	for name, order := range map[string][]facts.Envelope{
		"nvd first":  {nvd, ghsa},
		"ghsa first": {ghsa, nvd},
	} {
		finding := buildRetractionFinding(t, append(append([]facts.Envelope{}, order...), tail...)...)
		sources := finding.AdvisoryRetraction.Sources
		if len(sources) != 2 || sources[0].Source != "ghsa" || sources[1].Source != "nvd" {
			t.Fatalf("%s: sources = %#v, want ordered ghsa then nvd", name, sources)
		}
		reasons = append(reasons, finding.AdvisoryRetraction.Reason)
		payload := supplyChainImpactTypedPayload(SupplyChainImpactWrite{IntentID: "i", ScopeID: "s", GenerationID: "g"}, finding)
		block, _ := payload.Provenance["advisory_retraction"].(map[string]any)
		rows, _ := block["sources"].([]map[string]any)
		if len(rows) != 2 || rows[0]["source"] != "ghsa" || rows[1]["source"] != "nvd" {
			t.Fatalf("%s: serialized sources = %#v, want two rows ordered ghsa then nvd", name, block["sources"])
		}
		if rows[1]["reason_code"] != AdvisoryRetractionRejected || rows[1]["status"] != "Rejected" ||
			!reflect.DeepEqual(rows[1]["replaced_by"], []string{"CVE-2026-4202"}) {
			t.Fatalf("%s: nvd source row = %#v", name, rows[1])
		}
	}
	if reasons[0] != reasons[1] || !strings.Contains(reasons[0], "ghsa withdrawn") {
		t.Fatalf("reasons differ by input order or miss the ghsa source: %q vs %q", reasons[0], reasons[1])
	}
}

// TestDuplicateRetractingObservationsAppearOnce: two NVD facts for one CVE from
// two scopes both reject it; the reason and sources list it once.
func TestDuplicateRetractingObservationsAppearOnce(t *testing.T) {
	t.Parallel()

	finding := buildRetractionFinding(t,
		retractionCVEFact("nvd-cve-a", "CVE-2026-4211", "nvd", "CVE-2026-4211", "Rejected"),
		retractionCVEFact("nvd-cve-b", "CVE-2026-4211", "nvd", "CVE-2026-4211", "Rejected"),
		retractionPackageFact("pkg", "CVE-2026-4211", "nvd", "CVE-2026-4211", "1.0.1"),
		retractionConsumption(),
	)
	if got := len(finding.AdvisoryRetraction.Sources); got != 1 {
		t.Fatalf("sources = %d, want 1 after de-duplication", got)
	}
	if got := strings.Count(finding.AdvisoryRetraction.Reason, "nvd rejected"); got != 1 {
		t.Fatalf("reason repeats the source %d times: %q", got, finding.AdvisoryRetraction.Reason)
	}
}

// TestAlertSeededFindingStaysActiveWhenItsCVEIsFullyRetracted pins a known
// boundary rather than endorsing it: a provider alert seeds its own finding
// from the alert's repository evidence and does not read the CVE group, so a
// fully rejected CVE does not retract an alert-seeded finding. The provider's
// own alert state still governs that finding.
func TestAlertSeededFindingStaysActiveWhenItsCVEIsFullyRetracted(t *testing.T) {
	t.Parallel()

	repoID := "repo://github/example/api"
	packageID := "pypi://pypi.org/simple/requests"
	alert := securityAlertEnvelope("alert-rejected-cve", repoID, map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(77),
		"provider_state":        "open",
		"package_id":            packageID,
		"ecosystem":             "pypi",
		"package_name":          "requests",
		"manifest_path":         "requirements.txt",
		"ghsa_ids":              []string{"GHSA-alrt-aaaa-bbbb"},
		"cve_ids":               []string{"CVE-2026-4301"},
		"vulnerable_range":      "< 2.0.0",
		"patched_version":       "2.0.0",
	})
	consumption := packageConsumptionCorrelationEnvelope("consume-alert", repoID, packageID, "requirements.txt")
	consumption.Payload["dependency_range"] = "1.0.0"
	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		retractionCVEFact("nvd-cve", "CVE-2026-4301", "nvd", "CVE-2026-4301", "Rejected"),
		alert,
		consumption,
	})
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want the one alert-seeded finding: %#v", len(findings), findings)
	}
	finding := applyRetractionDecision(findings[0])
	if finding.AdvisoryRetraction != nil || finding.Suppression.State != SupplyChainSuppressionStateActive {
		t.Fatalf("alert-seeded finding = %q retraction=%v, want active (the alert path does not read the CVE group)",
			finding.Suppression.State, finding.AdvisoryRetraction)
	}
}

// TestProductPathIdentityIgnoresARejectedStatus is the product-path twin of the
// package-path identity test: the CPE finding's advisory id comes from the group
// representative, and a rejected status must not move it. The rejected record
// ranks above the live one here, so a representative that skipped it would name
// the finding by the live record instead.
func TestProductPathIdentityIgnoresARejectedStatus(t *testing.T) {
	t.Parallel()

	build := func(status string) SupplyChainImpactFinding {
		findings := BuildSupplyChainImpactFindings([]facts.Envelope{
			retractionCVEFact("ghsa-cve", "CVE-2026-4401", "osv", "GHSA-rank-aaaa-bbbb", status),
			retractionCVEFact("nvd-cve", "CVE-2026-4401", "nvd", "CVE-2026-4401", ""),
			vulnerabilityAffectedProductFact("product-1", "CVE-2026-4401", testImpactProductCriteria, testImpactMatchCriteriaID, true),
			sbomComponentCPEImpactFact("component-1", "doc-1", testImpactProductCriteria),
			sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
			containerImageIdentityImpactFact("image-1", testImpactSubjectDigest, testImpactRepositoryID),
		})
		if len(findings) != 1 {
			t.Fatalf("findings = %d, want 1", len(findings))
		}
		return findings[0]
	}
	rejected, live := build("Rejected"), build("")
	if rejected.AdvisoryID != live.AdvisoryID {
		t.Fatalf("advisory_id moved from %q to %q when one record was rejected", live.AdvisoryID, rejected.AdvisoryID)
	}
	if supplyChainImpactFindingID(rejected) != supplyChainImpactFindingID(live) {
		t.Fatalf("finding_id moved when one record was rejected")
	}
}
