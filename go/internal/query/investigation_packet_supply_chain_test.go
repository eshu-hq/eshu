package query

import (
	"encoding/json"
	"strings"
	"testing"
)

func scPacketFreshTruth() *TruthEnvelope {
	return &TruthEnvelope{
		Level:      TruthLevelExact,
		Capability: "supply_chain.impact_explain",
		Profile:    ProfileLocalAuthoritative,
		Basis:      TruthBasisAuthoritativeGraph,
		Backend:    GraphBackendNornicDB,
		Freshness:  TruthFreshness{State: FreshnessFresh},
	}
}

// completeSupplyChainResult is a fully resolved advisory→service explanation.
func completeSupplyChainResult() SupplyChainImpactExplanationResult {
	directDep := true
	return SupplyChainImpactExplanationResult{
		Outcome: "finding_explained",
		Input: SupplyChainImpactExplanationFilter{
			AdvisoryID:   "GHSA-aaaa-bbbb-cccc",
			PackageID:    "pkg:golang/example.com/vuln",
			RepositoryID: "repo-1",
		},
		Finding: &SupplyChainImpactFindingResult{
			FindingID:        "finding-1",
			AdvisoryID:       "GHSA-aaaa-bbbb-cccc",
			PackageID:        "pkg:golang/example.com/vuln",
			PackageName:      "example.com/vuln",
			ImpactStatus:     "affected",
			WorkloadIDs:      []string{"workload:checkout"},
			ServiceIDs:       []string{"service:checkout"},
			EvidenceFactIDs:  []string{"fact-advisory", "fact-sbom", "fact-missing-from-evidence"},
			DirectDependency: &directDep,
		},
		Anchors: SupplyChainImpactExplanationAnchors{
			RepositoryID: "repo-1",
			ImageDigests: []string{"sha256:abc"},
			Workloads:    []string{"workload:checkout"},
			Services:     []string{"service:checkout"},
		},
		ImpactPath: []SupplyChainImpactPathHop{
			{Hop: "advisory", Status: "present", EvidenceFactIDs: []string{"fact-advisory"}},
			{Hop: "sbom", Status: "present", EvidenceFactIDs: []string{"fact-sbom"}},
			{Hop: "image", Status: "present"},
			{Hop: "workload", Status: "present"},
			{Hop: "service", Status: "present"},
		},
		Evidence: []SupplyChainImpactEvidenceFactSummary{
			{FactID: "fact-advisory", FactKind: "vulnerability_advisory", SourceSystem: "osv", ObservedAt: "2026-06-18T00:00:00Z"},
			{FactID: "fact-sbom", FactKind: "sbom_component", SourceSystem: "sbom_document", ObservedAt: "2026-06-18T00:00:00Z"},
		},
		Readiness: SupplyChainImpactReadinessEnvelope{State: ReadinessStateReadyWithFindings},
		Freshness: SupplyChainImpactExplanationFreshness{State: "fresh", LatestObservedAt: "2026-06-18T00:00:00Z", EvidenceFactCount: 2},
	}
}

func TestBuildSupplyChainImpactPacketCompletePath(t *testing.T) {
	packet, err := BuildSupplyChainImpactPacket(completeSupplyChainResult(), scPacketFreshTruth(), nil)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	if packet.Identity.Family != InvestigationFamilySupplyChainImpact {
		t.Errorf("family = %q", packet.Identity.Family)
	}
	if !packet.Answer.Supported || packet.Answer.Partial {
		t.Errorf("complete path should be supported and not partial: supported=%t partial=%t", packet.Answer.Supported, packet.Answer.Partial)
	}
	if packet.Answer.Summary == "" {
		t.Error("expected a summary for a complete finding")
	}
	if len(packet.SourceFacts) != 2 {
		t.Errorf("source facts = %d, want 2", len(packet.SourceFacts))
	}
	if len(packet.ReducerDecisions) != 1 || packet.ReducerDecisions[0].State != "admitted" {
		t.Errorf("expected one admitted reducer decision, got %+v", packet.ReducerDecisions)
	}
	// The decision must only reference facts present in the source layer; the
	// dangling "fact-missing-from-evidence" id must be filtered out.
	for _, ref := range packet.ReducerDecisions[0].SourceFactIDs {
		if ref == "fact-missing-from-evidence" {
			t.Error("decision referenced a fact absent from the source layer")
		}
	}
	if len(packet.MissingEvidence) != 0 {
		t.Errorf("complete path should have no missing hops, got %+v", packet.MissingEvidence)
	}
	if packet.Validation.Status != "passed" {
		t.Errorf("validation = %q", packet.Validation.Status)
	}
	// The advisory hop must preserve its backing fact for traceability; the
	// service hop has no backing fact and must carry none.
	advisoryHop := graphAnswerForHop(packet.GraphAnswers, "advisory")
	if advisoryHop == nil || len(advisoryHop.SourceFactIDs) != 1 || advisoryHop.SourceFactIDs[0] != "fact-advisory" {
		t.Errorf("advisory hop source facts = %+v, want [fact-advisory]", advisoryHop)
	}
}

func graphAnswerForHop(answers []PacketGraphAnswer, hop string) *PacketGraphAnswer {
	for i := range answers {
		if answers[i].Hop == hop {
			return &answers[i]
		}
	}
	return nil
}

func TestBuildSupplyChainImpactPacketMissingSBOM(t *testing.T) {
	result := completeSupplyChainResult()
	result.ImpactPath = []SupplyChainImpactPathHop{
		{Hop: "advisory", Status: "present", EvidenceFactIDs: []string{"fact-advisory"}},
		{Hop: "sbom", Status: "missing_evidence", MissingEvidence: []string{"no SBOM document links the advisory to an image"}},
	}
	result.Evidence = []SupplyChainImpactEvidenceFactSummary{
		{FactID: "fact-advisory", FactKind: "vulnerability_advisory", SourceSystem: "osv"},
	}
	packet, err := BuildSupplyChainImpactPacket(result, scPacketFreshTruth(), nil)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	if !packet.Answer.Partial {
		t.Error("a missing SBOM hop should make the answer partial")
	}
	if !missingHopPresent(packet.MissingEvidence, "sbom") {
		t.Errorf("expected a missing sbom hop, got %+v", packet.MissingEvidence)
	}
}

func TestBuildSupplyChainImpactPacketMissingWorkload(t *testing.T) {
	result := completeSupplyChainResult()
	result.ImpactPath = []SupplyChainImpactPathHop{
		{Hop: "advisory", Status: "present", EvidenceFactIDs: []string{"fact-advisory"}},
		{Hop: "sbom", Status: "present", EvidenceFactIDs: []string{"fact-sbom"}},
		{Hop: "image", Status: "present"},
		{Hop: "workload", Status: "missing_evidence", MissingEvidence: []string{"no workload runs this image"}},
	}
	result.Finding.WorkloadIDs = nil
	result.Finding.ServiceIDs = nil
	result.MissingEvidence = []string{serviceCatalogAnchorMissingReason}
	packet, err := BuildSupplyChainImpactPacket(result, scPacketFreshTruth(), nil)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	if !packet.Answer.Partial {
		t.Error("a missing workload hop should make the answer partial")
	}
	if !missingHopPresent(packet.MissingEvidence, "workload") {
		t.Errorf("expected a missing workload hop, got %+v", packet.MissingEvidence)
	}
	if !missingHopPresent(packet.MissingEvidence, "correlation") {
		t.Errorf("expected the top-level correlation missing reason, got %+v", packet.MissingEvidence)
	}
}

func TestBuildSupplyChainImpactPacketStaleGeneration(t *testing.T) {
	result := completeSupplyChainResult()
	result.Freshness.State = "stale"
	packet, err := BuildSupplyChainImpactPacket(result, scPacketFreshTruth(), nil)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	if packet.Freshness.State != FreshnessStale {
		t.Errorf("freshness = %q, want stale", packet.Freshness.State)
	}
	if !packet.Answer.Partial {
		t.Error("stale evidence should make the answer partial")
	}
}

func TestBuildSupplyChainImpactPacketNoFinding(t *testing.T) {
	result := SupplyChainImpactExplanationResult{
		Outcome: "no_finding",
		Input:   SupplyChainImpactExplanationFilter{AdvisoryID: "GHSA-none"},
		Readiness: SupplyChainImpactReadinessEnvelope{
			State:             ReadinessStateEvidenceIncomplete,
			IncompleteReasons: []string{"sbom evidence not yet collected"},
		},
		Freshness: SupplyChainImpactExplanationFreshness{State: "fresh"},
	}
	packet, err := BuildSupplyChainImpactPacket(result, scPacketFreshTruth(), nil)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	if packet.Answer.Summary != "" {
		t.Error("a no-finding result must not carry a confident summary")
	}
	if !packet.Answer.Partial {
		t.Error("a no-finding result should be partial (no evidence resolved)")
	}
	if len(packet.Limitations) == 0 {
		t.Error("expected readiness incomplete reasons to surface as limitations")
	}
}

func TestImpactStatusToDecisionStateRealValues(t *testing.T) {
	cases := map[string]string{
		"affected_exact":           "admitted",
		"affected_derived":         "admitted",
		"possibly_affected":        "ambiguous",
		"not_affected_known_fixed": "rejected",
		"unknown_impact":           "missing_evidence",
		"":                         "ambiguous",
	}
	for status, want := range cases {
		if got := impactStatusToDecisionState(status); got != want {
			t.Errorf("impactStatusToDecisionState(%q) = %q, want %q", status, got, want)
		}
	}
}

func TestBuildSupplyChainImpactPacketNotAffectedRejected(t *testing.T) {
	result := completeSupplyChainResult()
	result.Finding.ImpactStatus = "not_affected_known_fixed"
	packet, err := BuildSupplyChainImpactPacket(result, scPacketFreshTruth(), nil)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	if len(packet.ReducerDecisions) != 1 || packet.ReducerDecisions[0].State != "rejected" {
		t.Errorf("decision = %+v, want one rejected", packet.ReducerDecisions)
	}
}

func TestBuildSupplyChainImpactPacketReproducible(t *testing.T) {
	first, err := BuildSupplyChainImpactPacket(completeSupplyChainResult(), scPacketFreshTruth(), nil)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := BuildSupplyChainImpactPacket(completeSupplyChainResult(), scPacketFreshTruth(), nil)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.PacketID != second.PacketID {
		t.Errorf("packet ids differ: %q vs %q", first.PacketID, second.PacketID)
	}
}

func TestRenderInvestigationPacketFormats(t *testing.T) {
	packet, err := BuildSupplyChainImpactPacket(completeSupplyChainResult(), scPacketFreshTruth(), nil)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}

	jsonBytes, err := RenderInvestigationPacket(packet, InvestigationPacketFormatJSON)
	if err != nil {
		t.Fatalf("render json: %v", err)
	}
	var decoded InvestigationEvidencePacket
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatalf("json round-trip: %v", err)
	}
	if decoded.PacketID != packet.PacketID {
		t.Error("json render lost the packet id")
	}

	md, err := RenderInvestigationPacket(packet, InvestigationPacketFormatMarkdown)
	if err != nil {
		t.Fatalf("render md: %v", err)
	}
	for _, want := range []string{"# Investigation Evidence Packet", "## Source facts", "## Reducer decisions", "## Graph answers", "## Missing evidence"} {
		if !strings.Contains(string(md), want) {
			t.Errorf("markdown missing section %q", want)
		}
	}

	htmlBytes, err := RenderInvestigationPacket(packet, InvestigationPacketFormatHTML)
	if err != nil {
		t.Fatalf("render html: %v", err)
	}
	if !strings.HasPrefix(string(htmlBytes), "<!DOCTYPE html>") {
		t.Error("html render missing doctype")
	}
}

func TestRenderInvestigationPacketHTMLEscapes(t *testing.T) {
	result := completeSupplyChainResult()
	result.Finding.PackageName = "<script>alert(1)</script>"
	packet, err := BuildSupplyChainImpactPacket(result, scPacketFreshTruth(), nil)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	htmlBytes, err := RenderInvestigationPacket(packet, InvestigationPacketFormatHTML)
	if err != nil {
		t.Fatalf("render html: %v", err)
	}
	if strings.Contains(string(htmlBytes), "<script>alert(1)</script>") {
		t.Error("html render did not escape evidence text")
	}
}

func TestParseInvestigationPacketFormat(t *testing.T) {
	cases := map[string]InvestigationPacketFormat{
		"":         InvestigationPacketFormatJSON,
		"json":     InvestigationPacketFormatJSON,
		"md":       InvestigationPacketFormatMarkdown,
		"markdown": InvestigationPacketFormatMarkdown,
		"HTML":     InvestigationPacketFormatHTML,
	}
	for raw, want := range cases {
		got, err := ParseInvestigationPacketFormat(raw)
		if err != nil {
			t.Errorf("parse %q: %v", raw, err)
		}
		if got != want {
			t.Errorf("parse %q = %q, want %q", raw, got, want)
		}
	}
	if _, err := ParseInvestigationPacketFormat("pdf"); err == nil {
		t.Error("expected error for unsupported format")
	}
}

func missingHopPresent(missing []PacketMissingHop, hop string) bool {
	for _, m := range missing {
		if m.Hop == hop {
			return true
		}
	}
	return false
}
