// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package compparity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/cli/evidpacket"
	"github.com/eshu-hq/eshu/go/internal/cli/opdigest"
	"github.com/eshu-hq/eshu/go/internal/packetdogfood"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// exerciseOperatorDigestArtifact proves the operator digest and artifact
// paths in internal/cli/opdigest stay wired end to end.
func exerciseOperatorDigestArtifact() error {
	options, err := opdigest.OptionsFromFlags("repo:demo/service", opdigest.DefaultProfile, 2)
	if err != nil {
		return err
	}
	_, err = opdigest.BuildArtifact(opdigest.BuildDigest(options))
	return err
}

// exerciseInvestigationEvidencePacketArtifact renders the fixture supply-chain
// packet and checks it is a supported, complete v2 packet.
func exerciseInvestigationEvidencePacketArtifact() error {
	packet, err := SupportedSupplyChainPacket()
	if err != nil {
		return err
	}
	raw, err := query.RenderInvestigationPacket(packet, query.InvestigationPacketFormatJSON)
	if err != nil {
		return err
	}
	if !packet.Answer.Supported || packet.Answer.Partial {
		return fmt.Errorf("investigation packet exercise did not produce a supported complete packet")
	}
	if !bytes.Contains(raw, []byte(`investigation_evidence_packet.v2`)) {
		return fmt.Errorf("investigation packet artifact missing schema marker")
	}
	return nil
}

// SupportedSupplyChainPacket builds the fixed supply-chain explanation used
// by the investigation exercise: a fully supported finding with source-fact,
// reducer, and graph evidence layers. It is exported so the package test can
// pin that the fixture stays a supported, complete packet — if it drifts to
// partial, the exercise proves nothing.
func SupportedSupplyChainPacket() (query.InvestigationEvidencePacket, error) {
	directDependency := true
	result := query.SupplyChainImpactExplanationResult{
		Outcome: "finding_explained",
		Input: query.SupplyChainImpactExplanationFilter{
			AdvisoryID:   "GHSA-aaaa-bbbb-cccc",
			PackageID:    "pkg:golang/example.com/vuln",
			RepositoryID: "repo-1",
		},
		Finding: &query.SupplyChainImpactFindingResult{
			FindingID:        "finding-1",
			AdvisoryID:       "GHSA-aaaa-bbbb-cccc",
			PackageID:        "pkg:golang/example.com/vuln",
			PackageName:      "example.com/vuln",
			ImpactStatus:     "affected",
			WorkloadIDs:      []string{"workload:checkout"},
			ServiceIDs:       []string{"service:checkout"},
			EvidenceFactIDs:  []string{"fact-advisory", "fact-sbom"},
			DirectDependency: &directDependency,
		},
		Anchors: query.SupplyChainImpactExplanationAnchors{
			RepositoryID:  "repo-1",
			ImageDigests:  []string{"sha256:abc"},
			Workloads:     []string{"workload:checkout"},
			Services:      []string{"service:checkout"},
			SBOMDocuments: []string{"sbom:checkout"},
		},
		ImpactPath: []query.SupplyChainImpactPathHop{
			{Hop: "advisory", Status: "present", EvidenceFactIDs: []string{"fact-advisory"}},
			{Hop: "sbom", Status: "present", EvidenceFactIDs: []string{"fact-sbom"}},
			{Hop: "image", Status: "present"},
			{Hop: "workload", Status: "present"},
			{Hop: "service", Status: "present"},
		},
		Evidence: []query.SupplyChainImpactEvidenceFactSummary{
			{FactID: "fact-advisory", FactKind: "vulnerability_advisory", SourceSystem: "osv", ObservedAt: "2026-06-18T00:00:00Z"},
			{FactID: "fact-sbom", FactKind: "sbom_component", SourceSystem: "sbom_document", ObservedAt: "2026-06-18T00:00:00Z"},
		},
		Readiness: query.SupplyChainImpactReadinessEnvelope{State: query.ReadinessStateReadyWithFindings},
		Freshness: query.SupplyChainImpactExplanationFreshness{
			State:             "fresh",
			LatestObservedAt:  "2026-06-18T00:00:00Z",
			EvidenceFactCount: 2,
		},
	}
	truth := &query.TruthEnvelope{
		Level:      query.TruthLevelExact,
		Capability: "supply_chain.impact_explain",
		Profile:    query.ProfileLocalAuthoritative,
		Basis:      query.TruthBasisAuthoritativeGraph,
		Backend:    query.GraphBackendNornicDB,
		Freshness:  query.TruthFreshness{State: query.FreshnessFresh},
	}
	return query.BuildSupplyChainImpactPacket(result, truth, nil)
}

// exerciseEvidencePacketDogfoodFixture scores the committed dogfood benchmark
// fixture under repoRoot and fails when the fixture is missing or no longer
// passes.
func exerciseEvidencePacketDogfoodFixture(repoRoot string) error {
	raw, err := os.ReadFile(filepath.Join(repoRoot, "go/internal/packetdogfood/testdata/fixture_benchmark.json")) // #nosec G304 -- path suffix is a fixed literal; repoRoot is the operator-supplied repo root, not an HTTP request param //nolint:gosec
	if err != nil {
		return fmt.Errorf("read dogfood fixture: %w", err)
	}
	benchmark, err := packetdogfood.ParseBenchmark(raw)
	if err != nil {
		return err
	}
	verdict := packetdogfood.Score(benchmark)
	if !verdict.Pass {
		return fmt.Errorf("dogfood fixture failed: %s", evidpacket.FailureSummary(verdict))
	}
	return nil
}

// exerciseCapabilityCatalogArtifacts loads the embedded capability catalog and
// surface inventory and proves both are non-empty and JSON-renderable.
func exerciseCapabilityCatalogArtifacts() error {
	catalog, err := capabilitycatalog.Load()
	if err != nil {
		return err
	}
	if len(catalog.Entries) == 0 {
		return fmt.Errorf("capability catalog is empty")
	}
	inventory, err := capabilitycatalog.LoadSurfaceInventory()
	if err != nil {
		return err
	}
	if len(inventory.Surfaces) == 0 {
		return fmt.Errorf("surface inventory is empty")
	}
	if _, err := json.Marshal(catalog); err != nil {
		return fmt.Errorf("marshal capability catalog: %w", err)
	}
	return nil
}
