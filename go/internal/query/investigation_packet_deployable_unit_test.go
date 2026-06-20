package query

import "testing"

func deployableUnitTruth() *TruthEnvelope {
	return &TruthEnvelope{
		Level:     TruthLevelExact,
		Basis:     TruthBasisAuthoritativeGraph,
		Profile:   ProfileLocalAuthoritative,
		Backend:   GraphBackendNornicDB,
		Freshness: TruthFreshness{State: FreshnessFresh},
	}
}

func deployableUnitDecisions() []AdmissionDecisionResult {
	return []AdmissionDecisionResult{
		{
			DecisionID: "dec-admitted", Domain: "deployable_unit", State: "admitted",
			ScopeID: "scope-1", GenerationID: "gen-1", AnchorKind: "repository", AnchorID: "repo-1",
			CandidateKind: "workload", CandidateID: "workload:checkout",
			SourceHandles:  []AdmissionDecisionSourceHandle{{Kind: "deployment_config", ID: "fact-deploy-1", ScopeID: "scope-1"}},
			CanonicalWrite: AdmissionDecisionCanonicalWrite{Eligible: true, Written: true, TargetKind: "CORRELATES_DEPLOYABLE_UNIT", TargetID: "workload:checkout"},
		},
		{
			DecisionID: "dec-ambiguous", Domain: "deployable_unit", State: "ambiguous",
			ScopeID: "scope-1", GenerationID: "gen-1", AnchorKind: "repository", AnchorID: "repo-1",
			CandidateKind:     "workload",
			RecommendedAction: AdmissionDecisionNextAction{Action: "review", Reason: "two manifests claim the same workload"},
			SourceHandles:     []AdmissionDecisionSourceHandle{{Kind: "deployment_config", ID: "fact-deploy-2"}},
		},
		{
			DecisionID: "dec-stale", Domain: "deployable_unit", State: "stale",
			ScopeID: "scope-1", GenerationID: "gen-0", AnchorKind: "repository", AnchorID: "repo-1",
			CandidateKind: "workload",
		},
	}
}

func TestBuildDeployableUnitPacketRepresentsAllStates(t *testing.T) {
	scope := map[string]string{"workload_id": "workload:checkout", "scope_id": "scope-1"}
	packet, err := BuildDeployableUnitPacket(deployableUnitDecisions(), scope, deployableUnitTruth(), nil)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	if packet.Identity.Family != InvestigationFamilyDeployableUnit {
		t.Errorf("family = %q", packet.Identity.Family)
	}
	if len(packet.ReducerDecisions) != 3 {
		t.Fatalf("reducer decisions = %d, want 3", len(packet.ReducerDecisions))
	}
	states := map[string]int{}
	for _, d := range packet.ReducerDecisions {
		states[d.State]++
	}
	for _, want := range []string{"admitted", "ambiguous", "stale"} {
		if states[want] != 1 {
			t.Errorf("expected one %q decision, got %d", want, states[want])
		}
	}
	// The admitted decision wrote canonical truth → one present graph edge.
	if len(packet.GraphAnswers) != 1 || !packet.GraphAnswers[0].Present {
		t.Errorf("expected one present graph answer, got %+v", packet.GraphAnswers)
	}
	// Ambiguous and stale candidates must appear explicitly as missing hops.
	if len(packet.MissingEvidence) != 2 {
		t.Errorf("expected 2 missing hops (ambiguous + stale), got %+v", packet.MissingEvidence)
	}
	if !packet.Answer.Supported {
		t.Error("expected supported packet")
	}
	if len(packet.Reproduce) == 0 {
		t.Error("expected reproduce steps")
	}
	if packet.Validation.Status != "passed" {
		t.Errorf("validation = %q", packet.Validation.Status)
	}
}

func TestBuildDeployableUnitPacketReferentialIntegrity(t *testing.T) {
	// The admitted decision's source handle must resolve to a source fact.
	packet, err := BuildDeployableUnitPacket(deployableUnitDecisions(), map[string]string{"scope_id": "scope-1"}, deployableUnitTruth(), nil)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	var admitted *PacketReducerDecision
	for i := range packet.ReducerDecisions {
		if packet.ReducerDecisions[i].State == "admitted" {
			admitted = &packet.ReducerDecisions[i]
		}
	}
	if admitted == nil || len(admitted.SourceFactIDs) != 1 || admitted.SourceFactIDs[0] != "fact-deploy-1" {
		t.Errorf("admitted decision source facts = %+v, want [fact-deploy-1]", admitted)
	}
}

func TestBuildDeployableUnitPacketReproducible(t *testing.T) {
	first, err := BuildDeployableUnitPacket(deployableUnitDecisions(), map[string]string{"scope_id": "scope-1"}, deployableUnitTruth(), nil)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := BuildDeployableUnitPacket(deployableUnitDecisions(), map[string]string{"scope_id": "scope-1"}, deployableUnitTruth(), nil)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.PacketID != second.PacketID {
		t.Errorf("packet ids differ: %q vs %q", first.PacketID, second.PacketID)
	}
}
