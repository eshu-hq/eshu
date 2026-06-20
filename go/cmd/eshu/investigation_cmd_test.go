package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func runInvestigationExportCmd(t *testing.T, args []string, fetch func(*APIClient, query.SupplyChainImpactExplanationFilter) (supplyChainExplainEnvelope, error)) (string, error) {
	t.Helper()
	prev := investigationExportDepsValue
	investigationExportDepsValue = investigationExportDeps{FetchSupplyChainExplain: fetch}
	t.Cleanup(func() { investigationExportDepsValue = prev })

	cmd := newInvestigationExportCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func explainEnvelopeFromComplete() supplyChainExplainEnvelope {
	return supplyChainExplainEnvelope{
		Data:  completeSupplyChainExplainResult(),
		Truth: explainTruthEnvelope(),
	}
}

func explainTruthEnvelope() *query.TruthEnvelope {
	return &query.TruthEnvelope{
		Level:     query.TruthLevelExact,
		Basis:     query.TruthBasisAuthoritativeGraph,
		Profile:   query.ProfileLocalAuthoritative,
		Backend:   query.GraphBackendNornicDB,
		Freshness: query.TruthFreshness{State: query.FreshnessFresh},
	}
}

func completeSupplyChainExplainResult() query.SupplyChainImpactExplanationResult {
	return query.SupplyChainImpactExplanationResult{
		Outcome: "finding_explained",
		Input: query.SupplyChainImpactExplanationFilter{
			AdvisoryID: "GHSA-aaaa-bbbb-cccc",
			PackageID:  "pkg:golang/example.com/vuln",
		},
		Finding: &query.SupplyChainImpactFindingResult{
			FindingID:       "finding-1",
			AdvisoryID:      "GHSA-aaaa-bbbb-cccc",
			PackageName:     "example.com/vuln",
			ImpactStatus:    "affected",
			WorkloadIDs:     []string{"workload:checkout"},
			ServiceIDs:      []string{"service:checkout"},
			EvidenceFactIDs: []string{"fact-advisory"},
		},
		ImpactPath: []query.SupplyChainImpactPathHop{
			{Hop: "advisory", Status: "present", EvidenceFactIDs: []string{"fact-advisory"}},
			{Hop: "service", Status: "present"},
		},
		Evidence: []query.SupplyChainImpactEvidenceFactSummary{
			{FactID: "fact-advisory", FactKind: "vulnerability_advisory", SourceSystem: "osv"},
		},
		Freshness: query.SupplyChainImpactExplanationFreshness{State: "fresh", LatestObservedAt: "2026-06-18T00:00:00Z"},
	}
}

func TestInvestigationExportSupplyChainJSON(t *testing.T) {
	out, err := runInvestigationExportCmd(t,
		[]string{"--family", "supply_chain_impact", "--subject", "advisory_id=GHSA-aaaa-bbbb-cccc", "--subject", "package_id=pkg:golang/example.com/vuln", "--format", "json"},
		func(*APIClient, query.SupplyChainImpactExplanationFilter) (supplyChainExplainEnvelope, error) {
			return explainEnvelopeFromComplete(), nil
		})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var packet query.InvestigationEvidencePacket
	if err := json.Unmarshal([]byte(out), &packet); err != nil {
		t.Fatalf("decode packet: %v\n%s", err, out)
	}
	if packet.Identity.Family != query.InvestigationFamilySupplyChainImpact {
		t.Errorf("family = %q", packet.Identity.Family)
	}
	if !packet.Answer.Supported {
		t.Error("expected supported packet")
	}
	if packet.Schema != query.InvestigationEvidencePacketSchema {
		t.Errorf("schema = %q", packet.Schema)
	}
}

func TestInvestigationExportPassesSubjectToFilter(t *testing.T) {
	var gotFilter query.SupplyChainImpactExplanationFilter
	_, err := runInvestigationExportCmd(t,
		[]string{"--family", "supply_chain_impact", "--subject", "advisory_id=GHSA-x", "--subject", "package_id=pkg:npm/y"},
		func(_ *APIClient, filter query.SupplyChainImpactExplanationFilter) (supplyChainExplainEnvelope, error) {
			gotFilter = filter
			return explainEnvelopeFromComplete(), nil
		})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if gotFilter.AdvisoryID != "GHSA-x" || gotFilter.PackageID != "pkg:npm/y" {
		t.Errorf("filter = %+v, want advisory_id and package_id set", gotFilter)
	}
}

func TestInvestigationExportUnknownFamilyRefuses(t *testing.T) {
	out, err := runInvestigationExportCmd(t,
		[]string{"--family", "not_a_family", "--subject", "x=y", "--format", "json"},
		func(*APIClient, query.SupplyChainImpactExplanationFilter) (supplyChainExplainEnvelope, error) {
			t.Fatal("fetch should not be called for an unknown family")
			return supplyChainExplainEnvelope{}, nil
		})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var packet query.InvestigationEvidencePacket
	if err := json.Unmarshal([]byte(out), &packet); err != nil {
		t.Fatalf("decode packet: %v", err)
	}
	if packet.Refusal != query.PacketRefusalUnknownFamily {
		t.Errorf("refusal = %q, want unknown_family", packet.Refusal)
	}
}

func TestInvestigationExportMissingScopeRefuses(t *testing.T) {
	out, err := runInvestigationExportCmd(t,
		[]string{"--family", "supply_chain_impact", "--subject", "advisory_id=GHSA-x", "--format", "json"},
		func(*APIClient, query.SupplyChainImpactExplanationFilter) (supplyChainExplainEnvelope, error) {
			t.Fatal("fetch should not be called when scope is incomplete")
			return supplyChainExplainEnvelope{}, nil
		})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var packet query.InvestigationEvidencePacket
	if err := json.Unmarshal([]byte(out), &packet); err != nil {
		t.Fatalf("decode packet: %v", err)
	}
	if packet.Refusal != query.PacketRefusalScopeNotFound {
		t.Errorf("refusal = %q, want scope_not_found", packet.Refusal)
	}
}

func TestInvestigationExportErrorEnvelopeRefuses(t *testing.T) {
	out, err := runInvestigationExportCmd(t,
		[]string{"--family", "supply_chain_impact", "--subject", "advisory_id=GHSA-x", "--subject", "package_id=pkg:npm/y", "--format", "json"},
		func(*APIClient, query.SupplyChainImpactExplanationFilter) (supplyChainExplainEnvelope, error) {
			return supplyChainExplainEnvelope{Error: &query.ErrorEnvelope{Code: query.ErrorCodeNotFound, Message: "no finding"}}, nil
		})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var packet query.InvestigationEvidencePacket
	if err := json.Unmarshal([]byte(out), &packet); err != nil {
		t.Fatalf("decode packet: %v", err)
	}
	if packet.Refusal != query.PacketRefusalScopeNotFound {
		t.Errorf("refusal = %q, want scope_not_found", packet.Refusal)
	}
}

func TestInvestigationExportWritesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "packet.md")
	_, err := runInvestigationExportCmd(t,
		[]string{"--family", "supply_chain_impact", "--subject", "advisory_id=GHSA-x", "--subject", "package_id=pkg:npm/y", "--format", "md", "--out", path},
		func(*APIClient, query.SupplyChainImpactExplanationFilter) (supplyChainExplainEnvelope, error) {
			return explainEnvelopeFromComplete(), nil
		})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat artifact: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("artifact perms = %o, want 600", perm)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if !strings.Contains(string(data), "# Investigation Evidence Packet") {
		t.Error("markdown artifact missing header")
	}
}

func TestInvestigationExportMaxSourceFactsWired(t *testing.T) {
	// The explain result carries 3 evidence facts; --max-source-facts 1 must cap
	// the packet's source-facts layer to 1 and mark it truncated.
	envelope := explainEnvelopeFromComplete()
	envelope.Data.Evidence = []query.SupplyChainImpactEvidenceFactSummary{
		{FactID: "fact-1", FactKind: "k"},
		{FactID: "fact-2", FactKind: "k"},
		{FactID: "fact-3", FactKind: "k"},
	}
	envelope.Data.Finding.EvidenceFactIDs = nil
	out, err := runInvestigationExportCmd(t,
		[]string{"--family", "supply_chain_impact", "--subject", "advisory_id=GHSA-x", "--subject", "package_id=pkg:npm/y", "--format", "json", "--max-source-facts", "1"},
		func(*APIClient, query.SupplyChainImpactExplanationFilter) (supplyChainExplainEnvelope, error) {
			return envelope, nil
		})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var packet query.InvestigationEvidencePacket
	if err := json.Unmarshal([]byte(out), &packet); err != nil {
		t.Fatalf("decode packet: %v", err)
	}
	if len(packet.SourceFacts) != 1 {
		t.Errorf("source facts = %d, want 1 (capped by --max-source-facts)", len(packet.SourceFacts))
	}
	if !packet.Bounds.Truncated {
		t.Error("expected bounds.truncated when --max-source-facts caps the layer")
	}
}

func TestInvestigationExportUnsupportedProfileRefuses(t *testing.T) {
	out, err := runInvestigationExportCmd(t,
		[]string{"--family", "supply_chain_impact", "--subject", "advisory_id=GHSA-x", "--subject", "package_id=pkg:npm/y", "--format", "json"},
		func(*APIClient, query.SupplyChainImpactExplanationFilter) (supplyChainExplainEnvelope, error) {
			return supplyChainExplainEnvelope{}, &apiHTTPError{StatusCode: 501, Body: "unsupported_capability"}
		})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var packet query.InvestigationEvidencePacket
	if err := json.Unmarshal([]byte(out), &packet); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if packet.Refusal != query.PacketRefusalProfileUnsupported {
		t.Errorf("refusal = %q, want profile_unsupported", packet.Refusal)
	}
}

func TestInvestigationExportChmodsExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "packet.json")
	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	_, err := runInvestigationExportCmd(t,
		[]string{"--family", "supply_chain_impact", "--subject", "advisory_id=GHSA-x", "--subject", "package_id=pkg:npm/y", "--format", "json", "--out", path},
		func(*APIClient, query.SupplyChainImpactExplanationFilter) (supplyChainExplainEnvelope, error) {
			return explainEnvelopeFromComplete(), nil
		})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perms = %o, want 600 after regenerating a 0644 file", perm)
	}
}

func TestInvestigationExportAcceptsRemoteFlags(t *testing.T) {
	_, err := runInvestigationExportCmd(t,
		[]string{"--family", "supply_chain_impact", "--subject", "advisory_id=GHSA-x", "--subject", "package_id=pkg:npm/y", "--service-url", "http://example:8080", "--api-key", "k", "--format", "json"},
		func(*APIClient, query.SupplyChainImpactExplanationFilter) (supplyChainExplainEnvelope, error) {
			return explainEnvelopeFromComplete(), nil
		})
	if err != nil {
		t.Fatalf("export with remote flags: %v", err)
	}
}

func TestInvestigationExportRejectsBadSubject(t *testing.T) {
	_, err := runInvestigationExportCmd(t,
		[]string{"--family", "supply_chain_impact", "--subject", "noequals", "--format", "json"},
		func(*APIClient, query.SupplyChainImpactExplanationFilter) (supplyChainExplainEnvelope, error) {
			return explainEnvelopeFromComplete(), nil
		})
	if err == nil {
		t.Fatal("expected an error for a malformed --subject")
	}
}
