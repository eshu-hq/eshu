// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/competitiveparity"
	"github.com/eshu-hq/eshu/go/internal/packetdogfood"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func init() {
	rootCmd.AddCommand(newCompetitiveParityCommand())
}

func newCompetitiveParityCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "competitive-parity",
		Short:         "Validate shipped competitive-parity surfaces",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(newCompetitiveParityValidateCommand())
	return cmd
}

func newCompetitiveParityValidateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "validate",
		Short:         "Run the offline competitive parity gate",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runCompetitiveParityValidate,
	}
	cmd.Flags().String("repo-root", ".", "Repository root used to read public contract docs")
	cmd.Flags().Bool("json", false, "Emit the parity artifact as JSON instead of Markdown")
	cmd.Flags().String("out", "", "Optional path to write the parity artifact")
	return cmd
}

func runCompetitiveParityValidate(cmd *cobra.Command, _ []string) error {
	repoRoot, err := cmd.Flags().GetString("repo-root")
	if err != nil {
		return err
	}
	jsonOut, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	outPath, err := cmd.Flags().GetString("out")
	if err != nil {
		return err
	}
	inventory, err := competitiveParityInventory(repoRoot)
	if err != nil {
		return err
	}
	report := competitiveparity.Validate(inventory, competitiveparity.DefaultExpectations())
	artifact, err := competitiveParityArtifact(report, jsonOut)
	if err != nil {
		return err
	}
	if strings.TrimSpace(outPath) != "" {
		if err := os.WriteFile(outPath, artifact, 0o600); err != nil {
			return fmt.Errorf("write competitive parity artifact: %w", err)
		}
	} else if _, err := cmd.OutOrStdout().Write(artifact); err != nil {
		return fmt.Errorf("write competitive parity artifact: %w", err)
	}
	if !report.Pass {
		return commandExitError{message: "competitive parity gate failed", code: 1}
	}
	return nil
}

func competitiveParityInventory(repoRoot string) (competitiveparity.Inventory, error) {
	surfaces, err := capabilitycatalog.LoadSurfaceInventory()
	if err != nil {
		return competitiveparity.Inventory{}, err
	}
	inv := competitiveparity.Inventory{
		Commands:  commandPaths(rootCmd),
		Docs:      map[string]string{},
		Exercises: competitiveParityExerciseResults(repoRoot),
	}
	for _, surface := range surfaces.Surfaces {
		switch surface.Category {
		case capabilitycatalog.SurfaceAPIRoute:
			inv.APIRoutes = append(inv.APIRoutes, surface.Name)
		case capabilitycatalog.SurfaceMCPTool:
			inv.MCPTools = append(inv.MCPTools, surface.Name)
		case capabilitycatalog.SurfaceConsolePage:
			inv.ConsolePages = append(inv.ConsolePages, surface.Name)
		}
	}
	sort.Strings(inv.APIRoutes)
	sort.Strings(inv.MCPTools)
	sort.Strings(inv.ConsolePages)
	for _, path := range competitiveParityDocPaths() {
		raw, err := os.ReadFile(filepath.Join(repoRoot, path)) //nolint:gosec // repo-local public docs path from static expectations
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return competitiveparity.Inventory{}, fmt.Errorf("read parity doc %s: %w", path, err)
		}
		inv.Docs[path] = string(raw)
	}
	return inv, nil
}

func competitiveParityExerciseResults(repoRoot string) []competitiveparity.ExerciseResult {
	checks := []struct {
		id string
		fn func() error
	}{
		{id: "first_run_report_artifact", fn: exerciseFirstRunReportArtifact},
		{id: "operator_digest_artifact", fn: exerciseOperatorDigestArtifact},
		{id: "investigation_evidence_packet_artifact", fn: exerciseInvestigationEvidencePacketArtifact},
		{id: "evidence_packet_dogfood_fixture", fn: func() error { return exerciseEvidencePacketDogfoodFixture(repoRoot) }},
		{id: "capability_catalog_artifacts", fn: exerciseCapabilityCatalogArtifacts},
	}
	results := make([]competitiveparity.ExerciseResult, 0, len(checks))
	for _, check := range checks {
		result := competitiveparity.ExerciseResult{ID: check.id, OK: true, Detail: "exercised"}
		if err := check.fn(); err != nil {
			result.OK = false
			result.Detail = competitiveParityExerciseFailureDetail(check.id)
		}
		results = append(results, result)
	}
	return results
}

func competitiveParityExerciseFailureDetail(id string) string {
	switch id {
	case "first_run_report_artifact":
		return "first-run evidence exercise failed"
	case "operator_digest_artifact":
		return "operator digest artifact exercise failed"
	case "investigation_evidence_packet_artifact":
		return "investigation evidence packet exercise failed"
	case "evidence_packet_dogfood_fixture":
		return "dogfood fixture unavailable"
	case "capability_catalog_artifacts":
		return "capability catalog artifact exercise failed"
	default:
		return "exercise failed"
	}
}

func exerciseFirstRunReportArtifact() error {
	result := newFirstRunResult("http://localhost:8080")
	result.RuntimeShape = firstRunShapeExistingAPI
	result.RepoIndexed = "demo/repo"
	result.RepoTarget = "demo/repo"
	result.Readiness = "complete"
	result.QueryAnswered = true
	result.QuerySummary = "bounded query returned one repository"
	result.Truth = map[string]any{"freshness": "current", "completeness": "complete"}
	report := buildFirstRunEvidence(result, nil)
	raw, err := renderEvidenceArtifact(report, evidenceFormatJSON)
	if err != nil {
		return err
	}
	if !bytes.Contains(raw, []byte(`"command": "first-run-evidence"`)) {
		return fmt.Errorf("first-run evidence artifact missing command field")
	}
	return nil
}

func exerciseOperatorDigestArtifact() error {
	options, err := operatorDigestOptionsFromFlags("repo:demo/service", operatorDigestDefaultProfile, 2)
	if err != nil {
		return err
	}
	_, err = buildOperatorDigestArtifact(buildOperatorDigest(options))
	return err
}

func exerciseInvestigationEvidencePacketArtifact() error {
	packet, err := buildCompetitiveParitySupportedSupplyChainPacket()
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

func buildCompetitiveParitySupportedSupplyChainPacket() (query.InvestigationEvidencePacket, error) {
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

func exerciseEvidencePacketDogfoodFixture(repoRoot string) error {
	raw, err := os.ReadFile(filepath.Join(repoRoot, "go/internal/packetdogfood/testdata/fixture_benchmark.json")) //nolint:gosec // repo-local fixture path
	if err != nil {
		return fmt.Errorf("read dogfood fixture: %w", err)
	}
	benchmark, err := packetdogfood.ParseBenchmark(raw)
	if err != nil {
		return err
	}
	verdict := packetdogfood.Score(benchmark)
	if !verdict.Pass {
		return fmt.Errorf("dogfood fixture failed: %s", dogfoodFailureSummary(verdict))
	}
	return nil
}

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

func competitiveParityArtifact(report competitiveparity.Report, jsonOut bool) ([]byte, error) {
	if jsonOut {
		return competitiveparity.RenderJSON(report)
	}
	return []byte(competitiveparity.RenderMarkdown(report)), nil
}

func competitiveParityDocPaths() []string {
	seen := map[string]struct{}{}
	for _, expectation := range competitiveparity.DefaultExpectations() {
		for _, doc := range expectation.Docs {
			seen[doc.Path] = struct{}{}
		}
	}
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func commandPaths(root *cobra.Command) []string {
	var paths []string
	var walk func(cmd *cobra.Command, prefix []string)
	walk = func(cmd *cobra.Command, prefix []string) {
		children := cmd.Commands()
		sort.SliceStable(children, func(i, j int) bool {
			return commandName(children[i]) < commandName(children[j])
		})
		for _, child := range children {
			if !child.Runnable() && !child.HasSubCommands() {
				continue
			}
			next := append(append([]string{}, prefix...), commandName(child))
			paths = append(paths, strings.Join(next, " "))
			walk(child, next)
		}
	}
	walk(root, nil)
	sort.Strings(paths)
	return paths
}

func commandName(cmd *cobra.Command) string {
	return strings.Fields(cmd.Use)[0]
}
