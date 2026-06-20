package query

import "testing"

func driftTruth() *TruthEnvelope {
	return &TruthEnvelope{
		Level:     TruthLevelExact,
		Basis:     TruthBasisRuntimeState,
		Profile:   ProfileProduction,
		Backend:   GraphBackendNornicDB,
		Freshness: TruthFreshness{State: FreshnessFresh},
	}
}

func driftFindings() []CloudRuntimeDriftFindingView {
	return []CloudRuntimeDriftFindingView{
		{
			FactID: "fact-drift-1", Provider: "aws", ScopeID: "acct-1", GenerationID: "gen-9",
			SourceSystem: "aws_runtime", CloudResourceUID: "aws:s3:bucket-a", FindingKind: "orphaned_cloud_resource",
			ManagementStatus: "terraform_state_only", SourceState: "orphaned",
			MatchedTerraformStateAddress: "aws_s3_bucket.a",
		},
		{
			FactID: "fact-drift-2", Provider: "aws", ScopeID: "acct-1", GenerationID: "gen-9",
			CloudResourceUID: "aws:s3:bucket-b", FindingKind: "ambiguous_cloud_resource",
			ManagementStatus: "ambiguous_management",
			MissingEvidence:  []string{"no terraform state addresses this resource"},
			SafetyGate:       IaCManagementSafetyGate{Outcome: "rejected", ReviewRequired: true, Warnings: []string{"ambiguous_ownership"}},
		},
		{
			FactID: "fact-drift-3", Provider: "aws", ScopeID: "acct-1", GenerationID: "gen-9",
			CloudResourceUID: "aws:s3:bucket-c", FindingKind: "unknown_cloud_resource",
		},
	}
}

func TestBuildDriftPacketRepresentsDriftStates(t *testing.T) {
	scope := map[string]string{"scope_id": "acct-1", "provider": "aws"}
	packet, err := BuildDriftPacket(driftFindings(), scope, driftTruth(), nil)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	if packet.Identity.Family != InvestigationFamilyDrift {
		t.Errorf("family = %q", packet.Identity.Family)
	}
	if len(packet.SourceFacts) != 3 {
		t.Errorf("source facts = %d, want 3", len(packet.SourceFacts))
	}
	states := map[string]int{}
	for _, d := range packet.ReducerDecisions {
		states[d.State]++
	}
	if states["rejected"] != 1 {
		t.Errorf("orphaned → rejected count = %d, want 1", states["rejected"])
	}
	if states["ambiguous"] != 1 {
		t.Errorf("ambiguous count = %d, want 1", states["ambiguous"])
	}
	if states["missing_evidence"] != 1 {
		t.Errorf("unknown → missing_evidence count = %d, want 1", states["missing_evidence"])
	}
	// The orphaned resource matched a Terraform address → one present management edge.
	if len(packet.GraphAnswers) != 1 || packet.GraphAnswers[0].Relationship != "MANAGED_BY_TERRAFORM" {
		t.Errorf("expected one MANAGED_BY_TERRAFORM edge, got %+v", packet.GraphAnswers)
	}
	if len(packet.MissingEvidence) != 1 {
		t.Errorf("expected one missing hop, got %+v", packet.MissingEvidence)
	}
	if !containsStr(packet.Limitations, "safety: ambiguous_ownership") {
		t.Errorf("expected safety-gate warning limitation, got %+v", packet.Limitations)
	}
	if packet.Validation.Status != "passed" {
		t.Errorf("validation = %q", packet.Validation.Status)
	}
}

func TestBuildDriftPacketSkipsEmptyFactID(t *testing.T) {
	// A finding without a durable fact id must not produce an unreferenceable
	// source-fact entry; its decision still represents the drift.
	findings := []CloudRuntimeDriftFindingView{
		{FactID: "", Provider: "aws", ScopeID: "acct-1", CloudResourceUID: "aws:s3:b", FindingKind: "orphaned_cloud_resource"},
		{FactID: "fact-keep", Provider: "aws", ScopeID: "acct-1", CloudResourceUID: "aws:s3:c", FindingKind: "ambiguous_cloud_resource"},
	}
	packet, err := BuildDriftPacket(findings, map[string]string{"scope_id": "acct-1"}, driftTruth(), nil)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	if len(packet.SourceFacts) != 1 || packet.SourceFacts[0].FactID != "fact-keep" {
		t.Errorf("source facts = %+v, want only fact-keep", packet.SourceFacts)
	}
	if len(packet.ReducerDecisions) != 2 {
		t.Errorf("reducer decisions = %d, want 2 (both findings represented)", len(packet.ReducerDecisions))
	}
}

func TestBuildDriftPacketUnknownKindIsMissingEvidence(t *testing.T) {
	findings := []CloudRuntimeDriftFindingView{
		{FactID: "fact-x", Provider: "aws", ScopeID: "acct-1", CloudResourceUID: "aws:s3:x", FindingKind: "some_future_kind"},
	}
	packet, err := BuildDriftPacket(findings, map[string]string{"scope_id": "acct-1"}, driftTruth(), nil)
	if err != nil {
		t.Fatalf("build packet: %v", err)
	}
	if len(packet.ReducerDecisions) != 1 || packet.ReducerDecisions[0].State != "missing_evidence" {
		t.Errorf("unknown kind state = %+v, want missing_evidence", packet.ReducerDecisions)
	}
}

func TestBuildDriftPacketReproducible(t *testing.T) {
	scope := map[string]string{"scope_id": "acct-1"}
	first, err := BuildDriftPacket(driftFindings(), scope, driftTruth(), nil)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := BuildDriftPacket(driftFindings(), scope, driftTruth(), nil)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.PacketID != second.PacketID {
		t.Errorf("packet ids differ")
	}
}

func containsStr(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
