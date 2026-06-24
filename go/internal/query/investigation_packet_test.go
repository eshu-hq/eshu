// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"strings"
	"testing"
)

// baseSupplyChainInput returns a complete, deterministic supply-chain impact
// packet input that passes every contract gate. Tests mutate a copy to exercise
// one failure mode at a time.
func baseSupplyChainInput() InvestigationPacketInput {
	return InvestigationPacketInput{
		Family: InvestigationFamilySupplyChainImpact,
		Subject: map[string]string{
			"advisory_id":  "GHSA-xxxx-yyyy-zzzz",
			"package_purl": "pkg:golang/example.com/vuln@1.2.3",
			"service_id":   "svc-checkout",
		},
		Question:   "What does GHSA-xxxx-yyyy-zzzz reach?",
		Generation: "gen-460ece25",
		Truth: &TruthEnvelope{
			Level:      TruthLevelExact,
			Capability: "supply_chain.impact_explain",
			Profile:    ProfileLocalAuthoritative,
			Basis:      TruthBasisAuthoritativeGraph,
			Backend:    GraphBackendNornicDB,
			Freshness:  TruthFreshness{State: FreshnessFresh},
		},
		Summary: "Advisory reaches svc-checkout through 1 workload.",
		SourceFacts: []PacketSourceFact{
			{FactID: "fact-advisory-1", EvidenceFamily: "vulnerability_advisory", CollectorKind: "vulnerability_intelligence", Subject: "advisory:GHSA-xxxx-yyyy-zzzz", Summary: "advisory affects example.com/vuln < 1.3.0"},
			{StableKey: "sbom-component-1", EvidenceFamily: "sbom_component", CollectorKind: "sbom_document", Subject: "image:sha256:abc", Summary: "image contains example.com/vuln@1.2.3"},
		},
		ReducerDecisions: []PacketReducerDecision{
			{Domain: "supply_chain_impact", Subject: "finding-1", State: "admitted", Target: "AFFECTS", Generation: "gen-460ece25", SourceFactIDs: []string{"fact-advisory-1", "sbom-component-1"}},
		},
		GraphAnswers: []PacketGraphAnswer{
			{Relationship: "RUNS_IN", From: "image:sha256:abc", To: "workload:checkout", Hop: "workload", Present: true, TruthClass: AnswerTruthDeterministic},
			{Relationship: "SERVES", From: "workload:checkout", To: "service:svc-checkout", Hop: "service", Present: true, TruthClass: AnswerTruthDeterministic},
		},
		Citations: []evidenceCitationHandle{
			{Kind: "supply_chain_finding", EvidenceFamily: "vulnerability_advisory", Reason: "advisory evidence"},
		},
	}
}

func TestNewInvestigationEvidencePacketDeterministicSupplyChain(t *testing.T) {
	packet, err := NewInvestigationEvidencePacket(baseSupplyChainInput())
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	if packet.Schema != InvestigationEvidencePacketSchema {
		t.Errorf("schema = %q, want %q", packet.Schema, InvestigationEvidencePacketSchema)
	}
	if packet.Identity.Basis != PacketBasisDeterministic {
		t.Errorf("basis = %q, want deterministic", packet.Identity.Basis)
	}
	if !packet.Answer.Supported {
		t.Error("supported = false, want true")
	}
	if packet.Answer.Partial {
		t.Error("partial = true, want false for a complete answer")
	}
	if packet.Answer.Summary == "" {
		t.Error("summary dropped for a supported complete answer")
	}
	if packet.Answer.TruthClass != AnswerTruthDeterministic {
		t.Errorf("truth_class = %q, want deterministic", packet.Answer.TruthClass)
	}
	if packet.Validation.Status != "passed" {
		t.Errorf("validation status = %q, want passed; checks=%+v", packet.Validation.Status, packet.Validation.Checks)
	}
	if !strings.HasPrefix(packet.PacketID, "investigation-evidence-packet:") {
		t.Errorf("packet id = %q, missing prefix", packet.PacketID)
	}
	if packet.Redaction.Profile != InvestigationEvidencePacketRedactionProfile {
		t.Errorf("redaction profile = %q", packet.Redaction.Profile)
	}
}

func TestNewInvestigationEvidencePacketReproducible(t *testing.T) {
	first, err := NewInvestigationEvidencePacket(baseSupplyChainInput())
	if err != nil {
		t.Fatalf("build first: %v", err)
	}
	second, err := NewInvestigationEvidencePacket(baseSupplyChainInput())
	if err != nil {
		t.Fatalf("build second: %v", err)
	}
	if first.PacketID != second.PacketID {
		t.Errorf("packet ids differ: %q vs %q", first.PacketID, second.PacketID)
	}
	firstBytes, _ := json.Marshal(first)
	secondBytes, _ := json.Marshal(second)
	if string(firstBytes) != string(secondBytes) {
		t.Error("packet marshaled bytes are not reproducible for identical inputs")
	}
}

func TestNewInvestigationEvidencePacketDifferentEvidenceDifferentID(t *testing.T) {
	base, err := NewInvestigationEvidencePacket(baseSupplyChainInput())
	if err != nil {
		t.Fatalf("build base: %v", err)
	}
	mutated := baseSupplyChainInput()
	mutated.SourceFacts = append(mutated.SourceFacts, PacketSourceFact{FactID: "fact-extra", EvidenceFamily: "deployment_config", Summary: "extra evidence"})
	other, err := NewInvestigationEvidencePacket(mutated)
	if err != nil {
		t.Fatalf("build other: %v", err)
	}
	if base.PacketID == other.PacketID {
		t.Error("different evidence under the same identity produced the same packet id")
	}
}

func TestNewInvestigationEvidencePacketBoundsTruncation(t *testing.T) {
	in := baseSupplyChainInput()
	in.SourceFacts = make([]PacketSourceFact, 0, 25)
	for i := 0; i < 25; i++ {
		in.SourceFacts = append(in.SourceFacts, PacketSourceFact{FactID: "fact-" + string(rune('a'+i%26)) + string(rune('0'+i)), EvidenceFamily: "sbom_component", Summary: "bulk"})
	}
	in.ReducerDecisions = nil // avoid dangling refs after truncation
	in.Bounds = &PacketBounds{MaxSourceFacts: 10}
	packet, err := NewInvestigationEvidencePacket(in)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	if len(packet.SourceFacts) != 10 {
		t.Errorf("source facts = %d, want capped at 10", len(packet.SourceFacts))
	}
	if !packet.Bounds.Truncated {
		t.Error("bounds.truncated = false, want true")
	}
	if !packetLayerTruncated(packet.Bounds.TruncatedLayers, "source_facts") {
		t.Errorf("truncated_layers = %v, want source_facts", packet.Bounds.TruncatedLayers)
	}
	if !packet.Answer.Partial {
		t.Error("truncated packet should be partial")
	}
}

func TestNewInvestigationEvidencePacketSemanticRequiresPolicy(t *testing.T) {
	in := baseSupplyChainInput()
	in.SemanticObservations = []PacketSemanticObservation{
		{Label: "semantic_observation", Provider: "test", Observation: "looks reachable"},
	}
	// AllowSemantic deliberately left false.
	if _, err := NewInvestigationEvidencePacket(in); err == nil {
		t.Fatal("expected error when semantic observations are supplied without AllowSemantic")
	}
}

func TestNewInvestigationEvidencePacketSemanticAllowed(t *testing.T) {
	in := baseSupplyChainInput()
	in.AllowSemantic = true
	in.SemanticObservations = []PacketSemanticObservation{
		{Label: "semantic_observation", Provider: "test", Observation: "advisory likely reachable via checkout handler", SourceFactIDs: []string{"fact-advisory-1"}},
	}
	packet, err := NewInvestigationEvidencePacket(in)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	if packet.Identity.Basis != PacketBasisSemanticAugmented {
		t.Errorf("basis = %q, want semantic_augmented", packet.Identity.Basis)
	}
	if len(packet.SemanticObservations) != 1 {
		t.Fatalf("semantic observations = %d, want 1", len(packet.SemanticObservations))
	}
}

func TestNewInvestigationEvidencePacketSemanticMissingLabel(t *testing.T) {
	in := baseSupplyChainInput()
	in.AllowSemantic = true
	in.SemanticObservations = []PacketSemanticObservation{
		{Provider: "test", Observation: "unlabeled"}, // missing the required label
	}
	if _, err := NewInvestigationEvidencePacket(in); err == nil {
		t.Fatal("expected error for an unlabeled semantic observation")
	}
}

func TestNewInvestigationEvidencePacketUnknownFamily(t *testing.T) {
	in := baseSupplyChainInput()
	in.Family = "not_a_family"
	packet, err := NewInvestigationEvidencePacket(in)
	if err != nil {
		t.Fatalf("unknown family should yield a refusal packet, not an error: %v", err)
	}
	if packet.Refusal != PacketRefusalUnknownFamily {
		t.Errorf("refusal = %q, want unknown_family", packet.Refusal)
	}
	if packet.Answer.Supported {
		t.Error("refusal packet must be unsupported")
	}
	if packet.Validation.Status != "passed" {
		t.Errorf("refusal packet validation = %q, want passed", packet.Validation.Status)
	}
	if packet.Answer.Summary != "" {
		t.Error("refusal packet must not carry a confident summary")
	}
}

func TestNewInvestigationEvidencePacketExplicitRefusal(t *testing.T) {
	for _, state := range SupportedPacketRefusalStates() {
		in := baseSupplyChainInput()
		in.Refusal = state
		packet, err := NewInvestigationEvidencePacket(in)
		if err != nil {
			t.Fatalf("refusal %q: unexpected error %v", state, err)
		}
		if packet.Refusal != state {
			t.Errorf("refusal = %q, want %q", packet.Refusal, state)
		}
		if len(packet.Answer.UnsupportedReasons) == 0 {
			t.Errorf("refusal %q: expected an unsupported reason", state)
		}
	}
}

func TestNewInvestigationEvidencePacketDanglingSourceRef(t *testing.T) {
	in := baseSupplyChainInput()
	in.ReducerDecisions = []PacketReducerDecision{
		{Domain: "supply_chain_impact", State: "admitted", SourceFactIDs: []string{"does-not-exist"}},
	}
	if _, err := NewInvestigationEvidencePacket(in); err == nil {
		t.Fatal("expected error for a reducer decision referencing an unknown source fact")
	}
}

func TestNewInvestigationEvidencePacketMissingHopNeedsReason(t *testing.T) {
	in := baseSupplyChainInput()
	in.MissingEvidence = []PacketMissingHop{{Hop: "workload"}} // no reason
	if _, err := NewInvestigationEvidencePacket(in); err == nil {
		t.Fatal("expected error for a missing hop without a reason")
	}
}

func TestNewInvestigationEvidencePacketMissingHopPartial(t *testing.T) {
	in := baseSupplyChainInput()
	in.MissingEvidence = []PacketMissingHop{
		{Hop: "service", Reason: "service catalog correlation evidence missing", NextCheck: map[string]any{"route": "GET /api/v0/supply-chain/impact/findings", "reason": "re-check finding"}},
	}
	packet, err := NewInvestigationEvidencePacket(in)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	if !packet.Answer.Partial {
		t.Error("packet with a missing hop should be partial")
	}
	if len(packet.MissingEvidence) != 1 {
		t.Fatalf("missing evidence = %d, want 1", len(packet.MissingEvidence))
	}
}

func TestNewInvestigationEvidencePacketNoEvidencePartialDropsSummary(t *testing.T) {
	in := baseSupplyChainInput()
	in.SourceFacts = nil
	in.GraphAnswers = nil
	in.Citations = nil
	in.ReducerDecisions = nil
	packet, err := NewInvestigationEvidencePacket(in)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	if !packet.Answer.Partial {
		t.Error("no-evidence supported answer should be partial")
	}
	if packet.Answer.Summary != "" {
		t.Error("a partial no-evidence answer must drop the confident summary")
	}
}

func TestNewInvestigationEvidencePacketStaleFreshnessPartial(t *testing.T) {
	in := baseSupplyChainInput()
	in.Truth.Freshness = TruthFreshness{State: FreshnessStale}
	packet, err := NewInvestigationEvidencePacket(in)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	if !packet.Answer.Partial {
		t.Error("stale freshness should mark the answer partial")
	}
	if packet.Freshness.State != FreshnessStale {
		t.Errorf("top-level freshness = %q, want stale", packet.Freshness.State)
	}
}

func TestNewInvestigationEvidencePacketUnsupportedNoTruth(t *testing.T) {
	in := baseSupplyChainInput()
	in.Truth = nil
	packet, err := NewInvestigationEvidencePacket(in)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	if packet.Answer.Supported {
		t.Error("packet without truth should be unsupported")
	}
	if packet.Answer.TruthClass != AnswerTruthUnsupported {
		t.Errorf("truth_class = %q, want unsupported", packet.Answer.TruthClass)
	}
}

func TestNewInvestigationEvidencePacketEmptyScopeFails(t *testing.T) {
	in := baseSupplyChainInput()
	in.Subject = nil
	if _, err := NewInvestigationEvidencePacket(in); err == nil {
		t.Fatal("expected error when the investigation names no scope")
	}
}

func TestInvestigationPacketJSONLayersPresent(t *testing.T) {
	packet, err := NewInvestigationEvidencePacket(baseSupplyChainInput())
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	raw, err := json.Marshal(packet)
	if err != nil {
		t.Fatalf("marshal packet: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal packet: %v", err)
	}
	for _, key := range []string{
		"schema", "packet_id", "identity", "truth", "freshness", "answer",
		"source_facts", "reducer_decisions", "graph_answers", "citations",
		"missing_evidence", "bounds", "redaction", "validation",
	} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("marshaled packet missing required layer %q", key)
		}
	}
}

func TestInvestigationFamilyHelpers(t *testing.T) {
	if len(SupportedInvestigationFamilies()) != 4 {
		t.Errorf("supported families = %d, want 4", len(SupportedInvestigationFamilies()))
	}
	if !ValidInvestigationFamily(InvestigationFamilySupplyChainImpact) {
		t.Error("supply_chain_impact should be valid")
	}
	if ValidInvestigationFamily("bogus") {
		t.Error("bogus family should be invalid")
	}
}

func packetLayerTruncated(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
