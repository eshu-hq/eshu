// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// factID returns a stable, unique source-fact id for bulk-fact tests.
func factID(i int) string {
	return fmt.Sprintf("fact-bulk-%03d", i)
}

func TestNewInvestigationEvidencePacketTruncationToleratesDroppedRefs(t *testing.T) {
	in := baseSupplyChainInput()
	in.SourceFacts = make([]PacketSourceFact, 0, 25)
	for i := 0; i < 25; i++ {
		in.SourceFacts = append(in.SourceFacts, PacketSourceFact{FactID: factID(i), EvidenceFamily: "sbom_component", Summary: "bulk"})
	}
	// A decision references a fact in the truncated tail (index 20). Integrity is
	// validated against the full input, so the build succeeds; the dropped ref is
	// tolerated because source_facts is explicitly truncated.
	in.ReducerDecisions = []PacketReducerDecision{
		{Domain: "supply_chain_impact", State: "admitted", SourceFactIDs: []string{factID(20)}},
	}
	in.Bounds = &PacketBounds{MaxSourceFacts: 10}
	packet, err := NewInvestigationEvidencePacket(in)
	if err != nil {
		t.Fatalf("truncation with a dropped-but-valid ref should build, got: %v", err)
	}
	if !packet.Bounds.Truncated {
		t.Error("expected truncation")
	}
	if packet.Validation.Status != "passed" {
		t.Errorf("validation = %q, want passed", packet.Validation.Status)
	}
}

func TestNewInvestigationEvidencePacketTrulyUnknownRefStillFails(t *testing.T) {
	in := baseSupplyChainInput()
	in.SourceFacts = []PacketSourceFact{{FactID: "fact-only", EvidenceFamily: "sbom_component"}}
	in.ReducerDecisions = []PacketReducerDecision{
		{Domain: "supply_chain_impact", State: "admitted", SourceFactIDs: []string{"never-existed"}},
	}
	in.Bounds = &PacketBounds{MaxSourceFacts: 10} // truncation inactive (1 < 10)
	if _, err := NewInvestigationEvidencePacket(in); err == nil {
		t.Fatal("a reference to a fact never present in the input must fail even with a bounds override")
	}
}

func TestNewInvestigationEvidencePacketAllowSemanticEmptyStaysDeterministic(t *testing.T) {
	in := baseSupplyChainInput()
	in.AllowSemantic = true // but no semantic observations supplied
	packet, err := NewInvestigationEvidencePacket(in)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	if packet.Identity.Basis != PacketBasisDeterministic {
		t.Errorf("basis = %q, want deterministic when no semantic observations are present", packet.Identity.Basis)
	}
}

func TestNewInvestigationEvidencePacketSemanticAugmentedReproducible(t *testing.T) {
	build := func() InvestigationEvidencePacket {
		in := baseSupplyChainInput()
		in.AllowSemantic = true
		in.SemanticObservations = []PacketSemanticObservation{
			{Label: "semantic_observation", Provider: "test", Observation: "reachable via handler", SourceFactIDs: []string{"fact-advisory-1"}},
		}
		packet, err := NewInvestigationEvidencePacket(in)
		if err != nil {
			t.Fatalf("build packet: %v", err)
		}
		return packet
	}
	first, second := build(), build()
	if first.PacketID != second.PacketID {
		t.Errorf("semantic_augmented packet ids differ: %q vs %q", first.PacketID, second.PacketID)
	}
	firstBytes, _ := json.Marshal(first)
	secondBytes, _ := json.Marshal(second)
	if string(firstBytes) != string(secondBytes) {
		t.Error("semantic_augmented packet bytes are not reproducible")
	}
}

func TestNewInvestigationEvidencePacketMissingEvidenceBounded(t *testing.T) {
	in := baseSupplyChainInput()
	in.MissingEvidence = make([]PacketMissingHop, 0, 5)
	for i := 0; i < 5; i++ {
		in.MissingEvidence = append(in.MissingEvidence, PacketMissingHop{Hop: factID(i), Reason: "gap"})
	}
	in.Bounds = &PacketBounds{MaxMissingEvidence: 2}
	packet, err := NewInvestigationEvidencePacket(in)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	if len(packet.MissingEvidence) != 2 {
		t.Errorf("missing evidence = %d, want capped at 2", len(packet.MissingEvidence))
	}
	if !packetLayerTruncated(packet.Bounds.TruncatedLayers, "missing_evidence") {
		t.Errorf("truncated_layers = %v, want missing_evidence", packet.Bounds.TruncatedLayers)
	}
}

func TestNewInvestigationEvidencePacketBoundsOverrideCannotRaiseCap(t *testing.T) {
	in := baseSupplyChainInput()
	// An override above the default must be ignored: the cap stays at the default.
	in.Bounds = &PacketBounds{MaxSourceFacts: 99999}
	packet, err := NewInvestigationEvidencePacket(in)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	if packet.Bounds.MaxSourceFacts != defaultPacketMaxSourceFacts {
		t.Errorf("MaxSourceFacts = %d, want clamped to default %d", packet.Bounds.MaxSourceFacts, defaultPacketMaxSourceFacts)
	}
}

func TestNewInvestigationEvidencePacketRejectsUnknownDecisionState(t *testing.T) {
	in := baseSupplyChainInput()
	in.ReducerDecisions = []PacketReducerDecision{
		{Domain: "supply_chain_impact", State: "totally_made_up", Reason: "x", SourceFactIDs: []string{"fact-advisory-1"}},
	}
	if _, err := NewInvestigationEvidencePacket(in); err == nil {
		t.Fatal("expected error for an unsupported reducer decision state")
	}
}

func TestNewInvestigationEvidencePacketRequiresReasonForNonAdmitted(t *testing.T) {
	in := baseSupplyChainInput()
	in.ReducerDecisions = []PacketReducerDecision{
		{Domain: "supply_chain_impact", State: "rejected", SourceFactIDs: []string{"fact-advisory-1"}}, // no reason
	}
	if _, err := NewInvestigationEvidencePacket(in); err == nil {
		t.Fatal("expected error for a non-admitted decision with no reason")
	}
}

func TestNewInvestigationEvidencePacketSemanticBoundsTruncation(t *testing.T) {
	in := baseSupplyChainInput()
	in.AllowSemantic = true
	in.SemanticObservations = make([]PacketSemanticObservation, 0, 5)
	for i := 0; i < 5; i++ {
		in.SemanticObservations = append(in.SemanticObservations, PacketSemanticObservation{Label: "semantic_observation", Observation: "obs"})
	}
	in.Bounds = &PacketBounds{MaxSemanticObservations: 2}
	packet, err := NewInvestigationEvidencePacket(in)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	if len(packet.SemanticObservations) != 2 {
		t.Errorf("semantic observations = %d, want capped at 2", len(packet.SemanticObservations))
	}
	if !packetLayerTruncated(packet.Bounds.TruncatedLayers, "semantic_observations") {
		t.Errorf("truncated_layers = %v, want semantic_observations", packet.Bounds.TruncatedLayers)
	}
}

func TestNewInvestigationEvidencePacketNilLayersMarshalAsArrays(t *testing.T) {
	in := baseSupplyChainInput()
	in.SourceFacts = nil
	in.ReducerDecisions = nil
	in.GraphAnswers = nil
	in.Citations = nil
	in.MissingEvidence = nil
	packet, err := NewInvestigationEvidencePacket(in)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	raw, err := json.Marshal(packet)
	if err != nil {
		t.Fatalf("marshal packet: %v", err)
	}
	for _, key := range []string{"source_facts", "reducer_decisions", "graph_answers", "citations", "missing_evidence"} {
		if !strings.Contains(string(raw), `"`+key+`":[`) {
			t.Errorf("layer %q did not marshal as a JSON array (expected []), got null", key)
		}
	}
}
