package main

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func runExportWithDeps(t *testing.T, args []string, deps investigationExportDeps) (string, error) {
	t.Helper()
	prev := investigationExportDepsValue
	investigationExportDepsValue = deps
	t.Cleanup(func() { investigationExportDepsValue = prev })

	cmd := newInvestigationExportCommand()
	out := &strBuf{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

type strBuf struct{ b []byte }

func (s *strBuf) Write(p []byte) (int, error) { s.b = append(s.b, p...); return len(p), nil }
func (s *strBuf) String() string              { return string(s.b) }

func TestInvestigationExportDeployableUnit(t *testing.T) {
	var gotParams url.Values
	deps := investigationExportDeps{
		FetchAdmissionDecisions: func(_ *APIClient, params url.Values) (admissionDecisionsEnvelope, error) {
			gotParams = params
			env := admissionDecisionsEnvelope{Truth: &query.TruthEnvelope{Level: query.TruthLevelExact, Basis: query.TruthBasisAuthoritativeGraph, Freshness: query.TruthFreshness{State: query.FreshnessFresh}}}
			env.Data.Decisions = []query.AdmissionDecisionResult{
				{DecisionID: "d1", Domain: "deployable_unit", State: "admitted", ScopeID: "s1", GenerationID: "g1", AnchorKind: "workload", AnchorID: "w1", CandidateKind: "workload", CandidateID: "w1", CanonicalWrite: query.AdmissionDecisionCanonicalWrite{Written: true, TargetKind: "CORRELATES_DEPLOYABLE_UNIT", TargetID: "w1"}},
			}
			return env, nil
		},
	}
	out, err := runExportWithDeps(t, []string{"--family", "deployable_unit", "--subject", "scope_id=s1", "--subject", "workload_id=w1", "--format", "json"}, deps)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if gotParams.Get("domain") != "deployable_unit" || gotParams.Get("scope_id") != "s1" || gotParams.Get("anchor_id") != "w1" {
		t.Errorf("params = %v, want domain/scope/anchor set", gotParams)
	}
	var packet query.InvestigationEvidencePacket
	if err := json.Unmarshal([]byte(out), &packet); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if packet.Identity.Family != query.InvestigationFamilyDeployableUnit || !packet.Answer.Supported {
		t.Errorf("packet family/supported wrong: %q supported=%t", packet.Identity.Family, packet.Answer.Supported)
	}
}

func TestInvestigationExportDeployableUnitMissingScopeRefuses(t *testing.T) {
	deps := investigationExportDeps{
		FetchAdmissionDecisions: func(*APIClient, url.Values) (admissionDecisionsEnvelope, error) {
			t.Fatal("fetch should not run without scope_id")
			return admissionDecisionsEnvelope{}, nil
		},
	}
	out, err := runExportWithDeps(t, []string{"--family", "deployable_unit", "--subject", "workload_id=w1", "--format", "json"}, deps)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var packet query.InvestigationEvidencePacket
	if err := json.Unmarshal([]byte(out), &packet); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if packet.Refusal != query.PacketRefusalScopeNotFound {
		t.Errorf("refusal = %q, want scope_not_found", packet.Refusal)
	}
}

func TestInvestigationExportDrift(t *testing.T) {
	var gotBody map[string]any
	deps := investigationExportDeps{
		FetchDriftFindings: func(_ *APIClient, body map[string]any) (driftFindingsEnvelope, error) {
			gotBody = body
			env := driftFindingsEnvelope{Truth: &query.TruthEnvelope{Level: query.TruthLevelExact, Basis: query.TruthBasisRuntimeState, Freshness: query.TruthFreshness{State: query.FreshnessFresh}}}
			env.Data.DriftFindings = []query.CloudRuntimeDriftFindingView{
				{FactID: "f1", Provider: "aws", ScopeID: "acct1", CloudResourceUID: "aws:s3:b", FindingKind: "orphaned_cloud_resource", MatchedTerraformStateAddress: "aws_s3_bucket.b"},
			}
			return env, nil
		},
	}
	out, err := runExportWithDeps(t, []string{"--family", "drift", "--subject", "scope_id=acct1", "--subject", "provider=aws", "--format", "md"}, deps)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if gotBody["scope_id"] != "acct1" || gotBody["provider"] != "aws" {
		t.Errorf("body = %v, want scope_id/provider", gotBody)
	}
	if !strings.Contains(out, "Investigation Evidence Packet") || !strings.Contains(out, "drift") {
		t.Errorf("markdown output missing expected content:\n%s", out)
	}
}
