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

const retractionPackageID = "npm://registry.npmjs.org/widget"

// retractionCVEFact builds a vulnerability.cve envelope that carries the #9695
// status and replaced_by keys the way the NVD and OSV collectors write them.
func retractionCVEFact(
	factID string,
	cveID string,
	source string,
	advisoryID string,
	status string,
	replacedBy ...string,
) facts.Envelope {
	envelope := vulnerabilityCVEFactWithProvenance(
		factID, cveID, source, advisoryID, 7.5, "", "HIGH", "2026-05-18T10:00:00Z",
	)
	if status != "" {
		envelope.Payload["status"] = status
	}
	if len(replacedBy) > 0 {
		values := make([]any, 0, len(replacedBy))
		for _, id := range replacedBy {
			values = append(values, id)
		}
		envelope.Payload["replaced_by"] = values
	}
	return envelope
}

// retractionPackageEvidence is the package and consumption evidence that gives
// every retraction test an owned anchor, so a finding is emitted whatever the
// advisory status says.
func retractionPackageEvidence(cveID string, source string, advisoryID string) []facts.Envelope {
	return []facts.Envelope{
		vulnerabilityAffectedPackageFactWithSource(
			"affected-"+source, cveID, source, advisoryID, retractionPackageID, "npm", "widget", "1.0.0", "1.0.1",
		),
		packageConsumptionFactWithRange("consume-1", retractionPackageID, testImpactRepositoryID, "1.0.0"),
	}
}

func buildRetractionFinding(t *testing.T, envelopes ...facts.Envelope) SupplyChainImpactFinding {
	t.Helper()
	findings := BuildSupplyChainImpactFindings(envelopes)
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want exactly 1: %#v", len(findings), findings)
	}
	return applyRetractionDecision(findings[0])
}

// applyRetractionDecision evaluates suppression the way Handle does, with no
// operator suppressions, so the decision a reader sees is the one asserted.
func applyRetractionDecision(finding SupplyChainImpactFinding) SupplyChainImpactFinding {
	finding.Suppression = EvaluateSupplyChainSuppression(finding, nil, time.Unix(0, 0).UTC())
	return finding
}

func TestRejectedAdvisoryWithReplacementRetractsItsFindingAndExplainsWhy(t *testing.T) {
	t.Parallel()

	envelopes := append(
		[]facts.Envelope{retractionCVEFact("nvd-cve", "CVE-2026-2001", "nvd", "CVE-2026-2001", "Rejected", "CVE-2026-2002")},
		retractionPackageEvidence("CVE-2026-2001", "nvd", "CVE-2026-2001")...,
	)
	finding := buildRetractionFinding(t, envelopes...)

	if got, want := finding.Suppression.State, SupplyChainSuppressionStateAdvisoryRetracted; got != want {
		t.Fatalf("Suppression.State = %q, want %q", got, want)
	}
	if !SupplyChainSuppressionStateIsHidden(finding.Suppression.State) {
		t.Fatalf("state %q must hide the finding from the default view", finding.Suppression.State)
	}
	retraction := finding.AdvisoryRetraction
	if retraction == nil {
		t.Fatalf("AdvisoryRetraction = nil, want the retraction carried on the finding")
	}
	if got, want := retraction.ReasonCode, AdvisoryRetractionRejected; got != want {
		t.Fatalf("ReasonCode = %q, want %q", got, want)
	}
	if want := []string{"CVE-2026-2002"}; !reflect.DeepEqual(retraction.ReplacedBy, want) {
		t.Fatalf("ReplacedBy = %v, want %v", retraction.ReplacedBy, want)
	}
	for _, want := range []string{"rejected", "nvd", "CVE-2026-2002"} {
		if !strings.Contains(finding.Suppression.Reason, want) {
			t.Fatalf("Suppression.Reason = %q, want it to name %q", finding.Suppression.Reason, want)
		}
	}
}

func TestWithdrawnOSVAdvisoryRetractsThroughTheSamePath(t *testing.T) {
	t.Parallel()

	withdrawnByStatus := retractionCVEFact("osv-cve", "CVE-2026-2010", "osv", "GHSA-wdrn-aaaa-bbbb", "withdrawn")
	withdrawnByStatus.Payload["withdrawn_at"] = "2026-05-22T08:00:00Z"
	// A fact written before the collector emitted status still carries the OSV
	// timestamp; it must retract the same way.
	withdrawnByTimestamp := vulnerabilityCVEFactWithdrawn(
		"osv-cve", "CVE-2026-2010", "osv", "GHSA-wdrn-aaaa-bbbb", 7.5, "HIGH",
		"2026-05-18T10:00:00Z", "2026-05-22T08:00:00Z",
	)

	for name, cve := range map[string]facts.Envelope{
		"status and timestamp": withdrawnByStatus,
		"timestamp only":       withdrawnByTimestamp,
	} {
		t.Run(name, func(t *testing.T) {
			finding := buildRetractionFinding(t, append(
				[]facts.Envelope{cve},
				retractionPackageEvidence("CVE-2026-2010", "osv", "GHSA-wdrn-aaaa-bbbb")...,
			)...)
			if got, want := finding.Suppression.State, SupplyChainSuppressionStateAdvisoryRetracted; got != want {
				t.Fatalf("Suppression.State = %q, want %q", got, want)
			}
			if got, want := finding.AdvisoryRetraction.ReasonCode, AdvisoryRetractionWithdrawn; got != want {
				t.Fatalf("ReasonCode = %q, want %q", got, want)
			}
			if len(finding.AdvisoryRetraction.ReplacedBy) != 0 {
				t.Fatalf("ReplacedBy = %v, want none: OSV states no replacement", finding.AdvisoryRetraction.ReplacedBy)
			}
		})
	}
}

func TestWithdrawnStatusAloneRetractsWithoutATimestamp(t *testing.T) {
	t.Parallel()

	envelopes := append(
		[]facts.Envelope{retractionCVEFact("osv-cve", "CVE-2026-2015", "osv", "GHSA-stat-aaaa-bbbb", " Withdrawn ")},
		retractionPackageEvidence("CVE-2026-2015", "osv", "GHSA-stat-aaaa-bbbb")...,
	)
	finding := buildRetractionFinding(t, envelopes...)
	if got, want := finding.Suppression.State, SupplyChainSuppressionStateAdvisoryRetracted; got != want {
		t.Fatalf("Suppression.State = %q, want %q for a withdrawn status with no timestamp", got, want)
	}
}

func TestRejectedObservationIsSkippedWhenSelectingSeverity(t *testing.T) {
	t.Parallel()

	// A rejected NVD record scores 9.8 and a live GHSA record scores 4.0. The
	// rejected record must not supply the finding's severity, exactly as a
	// withdrawn record never has.
	rejected := retractionCVEFact("nvd-cve", "CVE-2026-2016", "nvd", "CVE-2026-2016", "Rejected")
	rejected.Payload["cvss_score"] = 9.8
	live := retractionCVEFact("ghsa-cve", "CVE-2026-2016", "osv", "GHSA-live-cccc-dddd", "")
	live.Payload["cvss_score"] = 4.0
	finding := buildRetractionFinding(t, append(
		[]facts.Envelope{rejected, live},
		retractionPackageEvidence("CVE-2026-2016", "osv", "GHSA-live-cccc-dddd")...,
	)...)
	if finding.CVSSScore != 4.0 {
		t.Fatalf("CVSSScore = %v, want 4.0 from the live record, not 9.8 from the rejected one", finding.CVSSScore)
	}
	if finding.Suppression.State != SupplyChainSuppressionStateActive {
		t.Fatalf("Suppression.State = %q, want active", finding.Suppression.State)
	}
}

func TestRejectedObservationIsSkippedByTheSeverityFallback(t *testing.T) {
	t.Parallel()

	// The live record reports only a label, so selection reaches the fallback
	// tiers. The rejected record's score must still not be used.
	rejected := retractionCVEFact("nvd-cve", "CVE-2026-2017", "nvd", "CVE-2026-2017", "Rejected")
	rejected.Payload["cvss_score"] = 9.8
	live := retractionCVEFact("ghsa-cve", "CVE-2026-2017", "osv", "GHSA-live-eeee-ffff", "")
	delete(live.Payload, "cvss_score")
	live.Payload["severity_label"] = "MEDIUM"
	finding := buildRetractionFinding(t, append(
		[]facts.Envelope{rejected, live},
		retractionPackageEvidence("CVE-2026-2017", "osv", "GHSA-live-eeee-ffff")...,
	)...)
	if finding.CVSSScore != 0 || finding.SeverityLabel != "MEDIUM" {
		t.Fatalf("severity = (%v, %q), want the live record's MEDIUM label and no score", finding.CVSSScore, finding.SeverityLabel)
	}
}

func TestRetractionNeedsEverySourceToRetract(t *testing.T) {
	t.Parallel()

	// NVD rejects the CVE but a GHSA record for the same CVE is still live. The
	// finding must stay active: hiding a real vulnerability because one source
	// retracted is the worse error. The rejection stays visible as provenance.
	envelopes := append([]facts.Envelope{
		retractionCVEFact("nvd-cve", "CVE-2026-2020", "nvd", "CVE-2026-2020", "Rejected", "CVE-2026-2021"),
		retractionCVEFact("ghsa-cve", "CVE-2026-2020", "osv", "GHSA-live-aaaa-bbbb", ""),
	}, retractionPackageEvidence("CVE-2026-2020", "osv", "GHSA-live-aaaa-bbbb")...)

	finding := buildRetractionFinding(t, envelopes...)
	if got := finding.Suppression.State; got != SupplyChainSuppressionStateActive {
		t.Fatalf("Suppression.State = %q, want active while one source still asserts the advisory", got)
	}
	if finding.AdvisoryRetraction != nil {
		t.Fatalf("AdvisoryRetraction = %#v, want nil for a partly retracted group", finding.AdvisoryRetraction)
	}
	var nvd *AdvisorySourceObservation
	for i := range finding.AdvisorySources {
		if finding.AdvisorySources[i].Source == "nvd" {
			nvd = &finding.AdvisorySources[i]
		}
	}
	if nvd == nil || nvd.Status != "Rejected" || !reflect.DeepEqual(nvd.ReplacedBy, []string{"CVE-2026-2021"}) {
		t.Fatalf("nvd advisory source = %#v, want the rejection and its replacement kept as provenance", nvd)
	}
}

func TestEveryRetractedSourceRetractsTheGroupWithBothReasons(t *testing.T) {
	t.Parallel()

	withdrawn := vulnerabilityCVEFactWithdrawn(
		"osv-cve", "CVE-2026-2030", "osv", "GHSA-wdrn-cccc-dddd", 7.5, "HIGH",
		"2026-05-18T10:00:00Z", "2026-05-22T08:00:00Z",
	)
	envelopes := append([]facts.Envelope{
		retractionCVEFact("nvd-cve", "CVE-2026-2030", "nvd", "CVE-2026-2030", "REJECTED", " CVE-2026-2032 ", "CVE-2026-2031", "CVE-2026-2031"),
		withdrawn,
	}, retractionPackageEvidence("CVE-2026-2030", "osv", "GHSA-wdrn-cccc-dddd")...)

	finding := buildRetractionFinding(t, envelopes...)
	if got, want := finding.Suppression.State, SupplyChainSuppressionStateAdvisoryRetracted; got != want {
		t.Fatalf("Suppression.State = %q, want %q", got, want)
	}
	if got, want := finding.AdvisoryRetraction.ReasonCode, AdvisoryRetractionRejectedAndWithdrawn; got != want {
		t.Fatalf("ReasonCode = %q, want %q", got, want)
	}
	if want := []string{"CVE-2026-2031", "CVE-2026-2032"}; !reflect.DeepEqual(finding.AdvisoryRetraction.ReplacedBy, want) {
		t.Fatalf("ReplacedBy = %v, want trimmed, de-duplicated and sorted %v", finding.AdvisoryRetraction.ReplacedBy, want)
	}
	for _, source := range finding.AdvisoryRetraction.Sources {
		if source.Source == "nvd" && !reflect.DeepEqual(source.ReplacedBy, []string{"CVE-2026-2032", "CVE-2026-2031"}) {
			t.Fatalf("nvd source ReplacedBy = %v, want trimmed and de-duplicated in source order", source.ReplacedBy)
		}
	}
}

func TestOnlyRejectedAndWithdrawnStatusesRetract(t *testing.T) {
	t.Parallel()

	for _, status := range []string{
		"", "Analyzed", "Modified", "Received", "Awaiting Analysis", "Undergoing Analysis",
		"Deferred", "Disputed", "rejected-pending", "Unknown New Status",
	} {
		t.Run("status="+status, func(t *testing.T) {
			envelopes := append(
				[]facts.Envelope{retractionCVEFact("nvd-cve", "CVE-2026-2040", "nvd", "CVE-2026-2040", status)},
				retractionPackageEvidence("CVE-2026-2040", "nvd", "CVE-2026-2040")...,
			)
			finding := buildRetractionFinding(t, envelopes...)
			if got := finding.Suppression.State; got != SupplyChainSuppressionStateActive {
				t.Fatalf("status %q gave Suppression.State = %q, want active: only rejected and withdrawn retract", status, got)
			}
		})
	}
}

func TestRetractionIsCaseInsensitiveAndTrimmed(t *testing.T) {
	t.Parallel()

	for _, status := range []string{"Rejected", "REJECTED", "rejected", "  Rejected\t"} {
		envelopes := append(
			[]facts.Envelope{retractionCVEFact("nvd-cve", "CVE-2026-2050", "nvd", "CVE-2026-2050", status)},
			retractionPackageEvidence("CVE-2026-2050", "nvd", "CVE-2026-2050")...,
		)
		if got := buildRetractionFinding(t, envelopes...).Suppression.State; got != SupplyChainSuppressionStateAdvisoryRetracted {
			t.Fatalf("status %q gave state %q, want advisory_retracted", status, got)
		}
	}
}

func TestUnrejectedAdvisoryKeepsTheSameFindingIdentity(t *testing.T) {
	t.Parallel()

	build := func(status string) SupplyChainImpactFinding {
		return buildRetractionFinding(t, append(
			[]facts.Envelope{retractionCVEFact("nvd-cve", "CVE-2026-2060", "nvd", "CVE-2026-2060", status, "CVE-2026-2061")},
			retractionPackageEvidence("CVE-2026-2060", "nvd", "CVE-2026-2060")...,
		)...)
	}
	rejected, restored := build("Rejected"), build("Analyzed")

	if restored.Suppression.State != SupplyChainSuppressionStateActive {
		t.Fatalf("restored state = %q, want active once the record is no longer rejected", restored.Suppression.State)
	}
	if restored.AdvisoryRetraction != nil {
		t.Fatalf("restored finding still carries a retraction: %#v", restored.AdvisoryRetraction)
	}
	write := SupplyChainImpactWrite{IntentID: "intent", ScopeID: "scope", GenerationID: "gen"}
	for name, pair := range map[string][2]string{
		"finding_id": {supplyChainImpactFindingID(rejected), supplyChainImpactFindingID(restored)},
		"stable key": {supplyChainImpactStableFactKey(write, rejected), supplyChainImpactStableFactKey(write, restored)},
	} {
		if pair[0] != pair[1] {
			t.Fatalf("%s moved when the advisory was retracted: %q vs %q", name, pair[0], pair[1])
		}
	}
	if rejected.AdvisoryID != restored.AdvisoryID || rejected.CVEID != restored.CVEID {
		t.Fatalf("identity moved: rejected=(%s,%s) restored=(%s,%s)", rejected.CVEID, rejected.AdvisoryID, restored.CVEID, restored.AdvisoryID)
	}
}

func TestRetractedProductFindingIsRetractedToo(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		retractionCVEFact("nvd-cve", "CVE-2026-2070", "nvd", "CVE-2026-2070", "Rejected", "CVE-2026-2071"),
		vulnerabilityAffectedProductFact("product-1", "CVE-2026-2070", testImpactProductCriteria, testImpactMatchCriteriaID, true),
		sbomComponentCPEImpactFact("component-1", "doc-1", testImpactProductCriteria),
		sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		containerImageIdentityImpactFact("image-1", testImpactSubjectDigest, testImpactRepositoryID),
	})
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}
	finding := applyRetractionDecision(findings[0])
	if got, want := finding.Suppression.State, SupplyChainSuppressionStateAdvisoryRetracted; got != want {
		t.Fatalf("product finding state = %q, want %q", got, want)
	}
	if want := []string{"CVE-2026-2071"}; !reflect.DeepEqual(finding.AdvisoryRetraction.ReplacedBy, want) {
		t.Fatalf("ReplacedBy = %v, want %v", finding.AdvisoryRetraction.ReplacedBy, want)
	}
}

func TestRetractionOutranksAnOperatorSuppression(t *testing.T) {
	t.Parallel()

	envelopes := append(
		[]facts.Envelope{retractionCVEFact("nvd-cve", "CVE-2026-2080", "nvd", "CVE-2026-2080", "Rejected")},
		retractionPackageEvidence("CVE-2026-2080", "nvd", "CVE-2026-2080")...,
	)
	findings := BuildSupplyChainImpactFindings(envelopes)
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}
	operator := []vulnerabilitySuppression{{
		SuppressionID: "op-1",
		Source:        chain.VulnerabilitySuppressionSourcePolicy,
		Justification: chain.VulnerabilitySuppressionJustificationAcceptedRisk,
		AuthoredAt:    time.Unix(10, 0).UTC(),
		Scope:         vulnerabilitySuppressionScope{CVEID: "CVE-2026-2080"},
	}}
	decision := EvaluateSupplyChainSuppression(findings[0], operator, time.Unix(20, 0).UTC())
	if decision.State != SupplyChainSuppressionStateAdvisoryRetracted {
		t.Fatalf("state = %q, want the source retraction to outrank an accepted-risk suppression", decision.State)
	}
}

func TestRetractedFindingPayloadCarriesReasonAndReplacement(t *testing.T) {
	t.Parallel()

	envelopes := append(
		[]facts.Envelope{retractionCVEFact("nvd-cve", "CVE-2026-2090", "nvd", "CVE-2026-2090", "Rejected", "CVE-2026-2091")},
		retractionPackageEvidence("CVE-2026-2090", "nvd", "CVE-2026-2090")...,
	)
	finding := buildRetractionFinding(t, envelopes...)
	payload := supplyChainImpactTypedPayload(SupplyChainImpactWrite{IntentID: "i", ScopeID: "s", GenerationID: "g"}, finding)

	if payload.SuppressionState == nil || *payload.SuppressionState != string(SupplyChainSuppressionStateAdvisoryRetracted) {
		t.Fatalf("SuppressionState = %v, want advisory_retracted as the top-level key the read model filters on", payload.SuppressionState)
	}
	if reason, _ := payload.Suppression["reason"].(string); !strings.Contains(reason, "CVE-2026-2091") {
		t.Fatalf("suppression.reason = %q, want it to name the replacement", reason)
	}
	block, ok := payload.Provenance["advisory_retraction"].(map[string]any)
	if !ok {
		t.Fatalf("provenance.advisory_retraction missing: %#v", payload.Provenance)
	}
	if block["reason_code"] != AdvisoryRetractionRejected {
		t.Fatalf("reason_code = %v, want %q", block["reason_code"], AdvisoryRetractionRejected)
	}
	if !reflect.DeepEqual(block["replaced_by"], []string{"CVE-2026-2091"}) {
		t.Fatalf("replaced_by = %#v, want [CVE-2026-2091]", block["replaced_by"])
	}
}

func TestAdvisoryRetractedStateIsPartOfTheStateContract(t *testing.T) {
	t.Parallel()

	var listed, hidden bool
	for _, state := range SupplyChainSuppressionStates() {
		listed = listed || state == SupplyChainSuppressionStateAdvisoryRetracted
	}
	for _, state := range SupplyChainSuppressionHiddenStates() {
		hidden = hidden || state == SupplyChainSuppressionStateAdvisoryRetracted
	}
	if !listed || !hidden {
		t.Fatalf("advisory_retracted listed=%v hidden=%v, want both", listed, hidden)
	}
}
