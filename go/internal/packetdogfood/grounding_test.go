package packetdogfood_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/packetdogfood"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestFixturePacketBudgetsAreAchievable grounds the dogfood benchmark's lower
// bound: it builds a real evidence packet for each implemented family through the
// production emitters, estimates its token budget the same way the benchmark does
// (bytes/4), and asserts the fixture's claimed evidence_packet token budget is at
// least the real packet's cost. It proves the fixture is not understated relative
// to the emitters (the floor), not that the fixture scenarios are the largest a
// realistic input would produce; a richer scenario would only make the packet's
// token advantage over the baselines larger.
func TestFixturePacketBudgetsAreAchievable(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "fixture_benchmark.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	benchmark, err := packetdogfood.ParseBenchmark(raw)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	realTokens := map[string]int{
		"supply_chain_impact": tokenBudget(t, buildSupplyChainPacket(t)),
		"deployable_unit":     tokenBudget(t, buildDeployableUnitPacket(t)),
		"drift":               tokenBudget(t, buildDriftPacket(t)),
	}

	for _, task := range benchmark.Tasks {
		real, ok := realTokens[task.Family]
		if !ok {
			continue // service_context has no production emitter yet
		}
		claimed := packetTokenBudget(t, task)
		if claimed < real {
			t.Errorf("task %q: fixture claims %d packet tokens but the real %s packet costs %d (understated)",
				task.Name, claimed, task.Family, real)
		}
	}
}

func packetTokenBudget(t *testing.T, task packetdogfood.Task) int {
	t.Helper()
	for _, approach := range task.Approaches {
		if approach.Approach == packetdogfood.ApproachEvidencePacket {
			return approach.TokenBudget
		}
	}
	t.Fatalf("task %q has no evidence_packet approach", task.Name)
	return 0
}

func tokenBudget(t *testing.T, packet query.InvestigationEvidencePacket) int {
	t.Helper()
	raw, err := json.Marshal(packet)
	if err != nil {
		t.Fatalf("marshal packet: %v", err)
	}
	return len(raw) / 4
}

func freshGraphTruth() *query.TruthEnvelope {
	return &query.TruthEnvelope{
		Level:     query.TruthLevelExact,
		Basis:     query.TruthBasisAuthoritativeGraph,
		Profile:   query.ProfileLocalAuthoritative,
		Backend:   query.GraphBackendNornicDB,
		Freshness: query.TruthFreshness{State: query.FreshnessFresh},
	}
}

func buildSupplyChainPacket(t *testing.T) query.InvestigationEvidencePacket {
	t.Helper()
	result := query.SupplyChainImpactExplanationResult{
		Outcome: "finding_explained",
		Input:   query.SupplyChainImpactExplanationFilter{AdvisoryID: "GHSA-aaaa-bbbb-cccc", PackageID: "pkg:golang/example.com/vuln"},
		Finding: &query.SupplyChainImpactFindingResult{
			FindingID: "finding-1", AdvisoryID: "GHSA-aaaa-bbbb-cccc", PackageName: "example.com/vuln",
			ImpactStatus: "affected_exact", WorkloadIDs: []string{"workload:checkout"}, ServiceIDs: []string{"service:checkout"},
			EvidenceFactIDs: []string{"fact-advisory"},
		},
		ImpactPath: []query.SupplyChainImpactPathHop{
			{Hop: "advisory", Status: "present", EvidenceFactIDs: []string{"fact-advisory"}},
			{Hop: "service", Status: "present"},
		},
		Evidence:  []query.SupplyChainImpactEvidenceFactSummary{{FactID: "fact-advisory", FactKind: "vulnerability_advisory", SourceSystem: "osv"}},
		Freshness: query.SupplyChainImpactExplanationFreshness{State: "fresh", LatestObservedAt: "2026-06-18T00:00:00Z"},
	}
	packet, err := query.BuildSupplyChainImpactPacket(result, freshGraphTruth(), nil)
	if err != nil {
		t.Fatalf("build supply-chain packet: %v", err)
	}
	return packet
}

func buildDeployableUnitPacket(t *testing.T) query.InvestigationEvidencePacket {
	t.Helper()
	decisions := []query.AdmissionDecisionResult{
		{
			DecisionID: "d1", Domain: "deployable_unit_correlation", State: "admitted", ScopeID: "s1", GenerationID: "g1",
			AnchorKind: "repository", AnchorID: "repo-1", CandidateKind: "deployable_unit", CandidateID: "workload:checkout",
			SourceHandles:  []query.AdmissionDecisionSourceHandle{{Kind: "deployment_config", ID: "fact-deploy-1", ScopeID: "s1"}},
			CanonicalWrite: query.AdmissionDecisionCanonicalWrite{Eligible: true, Written: true, TargetKind: "CORRELATES_DEPLOYABLE_UNIT", TargetID: "workload:checkout"},
		},
	}
	packet, err := query.BuildDeployableUnitPacket(decisions, map[string]string{"scope_id": "s1", "generation_id": "g1"}, freshGraphTruth(), nil)
	if err != nil {
		t.Fatalf("build deployable-unit packet: %v", err)
	}
	return packet
}

func buildDriftPacket(t *testing.T) query.InvestigationEvidencePacket {
	t.Helper()
	findings := []query.CloudRuntimeDriftFindingView{
		{
			FactID: "fact-drift-1", Provider: "aws", ScopeID: "acct-1", GenerationID: "g9", CloudResourceUID: "aws:s3:bucket-a",
			FindingKind: "orphaned_cloud_resource", ManagementStatus: "terraform_state_only", SourceState: "orphaned",
			MatchedTerraformStateAddress: "aws_s3_bucket.a",
		},
	}
	truth := freshGraphTruth()
	truth.Basis = query.TruthBasisRuntimeState
	packet, err := query.BuildDriftPacket(findings, map[string]string{"scope_id": "acct-1", "provider": "aws"}, truth, nil)
	if err != nil {
		t.Fatalf("build drift packet: %v", err)
	}
	return packet
}
