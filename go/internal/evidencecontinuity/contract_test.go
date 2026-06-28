// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidencecontinuity

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidateRealEvidenceContinuityContract(t *testing.T) {
	root := repoRoot(t)

	findings, err := ValidateRepository(root)
	if err != nil {
		t.Fatalf("ValidateRepository() error = %v", err)
	}
	if len(findings) > 0 {
		t.Fatalf("ValidateRepository() findings:\n%s", FormatFindings(findings))
	}
}

func TestValidatorRejectsMissingDomainAndProofs(t *testing.T) {
	contract := Contract{
		Version:              "v1",
		RequiredDomains:      []string{"code_to_cloud", "supply_chain"},
		RequiredCapabilities: []string{"platform_impact.deployment_chain", "supply_chain.impact_findings.list"},
		Rows: []Row{{
			ID:         "row-with-gaps",
			Domain:     "code_to_cloud",
			Capability: "platform_impact.deployment_chain",
		}},
	}
	surfaces := SurfaceIndex{
		Capabilities: map[string]struct{}{"platform_impact.deployment_chain": {}},
		APIRoutes:    map[string]struct{}{"POST /api/v0/impact/trace-deployment-chain": {}},
		MCPTools:     map[string]struct{}{"trace_deployment_chain": {}},
	}

	findings := Validate(contract, surfaces)
	mustContainFinding(t, findings, "missing_required_domain", "supply_chain")
	mustContainFinding(t, findings, "missing_required_capability", "supply_chain.impact_findings.list")
	mustContainFinding(t, findings, "missing_api_route", "row-with-gaps")
	mustContainFinding(t, findings, "missing_mcp_tool", "row-with-gaps")
	mustContainFinding(t, findings, "missing_source_fact_proof", "row-with-gaps")
	mustContainFinding(t, findings, "missing_continuity_stage", "row-with-gaps")
	mustContainFinding(t, findings, "missing_empty_state_proof", "row-with-gaps")
	mustContainFinding(t, findings, "missing_negative_proof", "empty_evidence")
	mustContainFinding(t, findings, "missing_negative_proof", "inaccessible_evidence")
}

func TestValidatorChecksReferencedSurfaces(t *testing.T) {
	contract := Contract{
		Version: "v1",
		Rows: []Row{{
			ID:           "unknown-surface",
			Domain:       "code_to_cloud",
			Capability:   "missing.capability",
			APIRoutes:    []string{"POST /api/v0/missing"},
			MCPTools:     []string{"missing_tool"},
			SourceFact:   ProofRef{Ref: "go test ./internal/query"},
			Continuity:   ContinuityProofs{Projection: ProofRef{Ref: "go test ./internal/query"}, API: ProofRef{Ref: "go test ./internal/query"}, MCP: ProofRef{Ref: "go test ./internal/mcp"}},
			EmptyStates:  EmptyStateProofs{NoProvider: ProofRef{Ref: "go test ./internal/query"}, NoCollector: ProofRef{Ref: "go test ./internal/query"}, Empty: ProofRef{Ref: "go test ./internal/query"}},
			NegativeCase: []NegativeProof{{Case: "empty_evidence", Ref: "go test ./internal/query"}, {Case: "missing_evidence", Ref: "go test ./internal/query"}, {Case: "stale_evidence", Ref: "go test ./internal/query"}, {Case: "truncated_evidence", Ref: "go test ./internal/query"}, {Case: "inaccessible_evidence", Ref: "go test ./internal/query"}},
		}},
	}

	findings := Validate(contract, SurfaceIndex{})
	mustContainFinding(t, findings, "unknown_capability", "missing.capability")
	mustContainFinding(t, findings, "unknown_api_route", "POST /api/v0/missing")
	mustContainFinding(t, findings, "unknown_mcp_tool", "missing_tool")
}

func TestValidatorRejectsMCPToolOutsideCapability(t *testing.T) {
	contract := Contract{
		Version: "v1",
		Rows: []Row{{
			ID:         "wrong-capability-tool",
			Domain:     "code_to_cloud",
			Capability: "platform_impact.deployment_chain",
			APIRoutes:  []string{"POST /api/v0/impact/trace-deployment-chain"},
			MCPTools:   []string{"list_supply_chain_impact_findings"},
			SourceFact: ProofRef{Ref: "go test ./internal/query"},
			Continuity: ContinuityProofs{
				Projection: ProofRef{Ref: "go test ./internal/query"},
				API:        ProofRef{Ref: "go test ./internal/query"},
				MCP:        ProofRef{Ref: "go test ./internal/mcp"},
			},
			EmptyStates: EmptyStateProofs{
				NoProvider:  ProofRef{Ref: "go test ./internal/query"},
				NoCollector: ProofRef{Ref: "go test ./internal/query"},
				Empty:       ProofRef{Ref: "go test ./internal/query"},
			},
			NegativeCase: []NegativeProof{
				{Case: "empty_evidence", Ref: "go test ./internal/query"},
				{Case: "missing_evidence", Ref: "go test ./internal/query"},
				{Case: "stale_evidence", Ref: "go test ./internal/query"},
				{Case: "truncated_evidence", Ref: "go test ./internal/query"},
				{Case: "inaccessible_evidence", Ref: "go test ./internal/query"},
			},
			Properties: map[string]string{"provider_key_independent": "true", "deterministic_truth": "required"},
		}},
	}
	surfaces := SurfaceIndex{
		Capabilities: map[string]struct{}{"platform_impact.deployment_chain": {}},
		CapabilityMCPTools: map[string]map[string]struct{}{
			"platform_impact.deployment_chain": {"trace_deployment_chain": {}},
		},
		APIRoutes: map[string]struct{}{"POST /api/v0/impact/trace-deployment-chain": {}},
		MCPTools:  map[string]struct{}{"trace_deployment_chain": {}, "list_supply_chain_impact_findings": {}},
	}

	findings := Validate(contract, surfaces)
	mustContainFinding(t, findings, "mcp_tool_not_in_capability", "list_supply_chain_impact_findings")
}

func TestValidatorRejectsAPIRouteOutsideCapability(t *testing.T) {
	contract := Contract{
		Version: "v1",
		Rows: []Row{{
			ID:         "wrong-capability-api-route",
			Domain:     "code_to_cloud",
			Capability: "platform_impact.deployment_chain",
			APIRoutes:  []string{"GET /api/v0/supply-chain/impact/findings"},
			MCPTools:   []string{"trace_deployment_chain"},
			SourceFact: ProofRef{Ref: "go test ./internal/query -run '^(TestBuildDeploymentTraceResponsePromotesWorkflowAndControllerProvenance)$' -count=1"},
			Continuity: ContinuityProofs{
				Projection: ProofRef{Ref: "go test ./internal/query -run '^(TestBuildDeploymentTraceArtifactLineageSkipsImplicitOnlyEvidence)$' -count=1"},
				API:        ProofRef{Ref: "go test ./internal/query -run '^(TestTraceDeploymentChain_LocalLightweightReturnsStructuredUnsupportedCapability)$' -count=1"},
				MCP:        ProofRef{Ref: "go test ./internal/mcp -run '^(TestResolveRouteMapsTraceDeploymentChain)$' -count=1"},
			},
			EmptyStates: EmptyStateProofs{
				NoProvider:  ProofRef{Ref: "go test ./internal/query -run '^(TestTraceDeploymentChain_LocalLightweightReturnsStructuredUnsupportedCapability)$' -count=1"},
				NoCollector: ProofRef{Ref: "go test ./internal/query -run '^(TestTraceDeploymentChainSkipsIndirectEvidenceWhenDirectOnly)$' -count=1"},
				Empty:       ProofRef{Ref: "go test ./internal/query -run '^(TestBuildDeploymentTraceResponseDoesNotEmitStructurallyEmptyDeliveryPaths)$' -count=1"},
			},
			NegativeCase: []NegativeProof{
				{Case: "empty_evidence", Ref: "go test ./internal/query -run '^(TestBuildDeploymentTraceResponseDoesNotEmitStructurallyEmptyDeliveryPaths)$' -count=1"},
				{Case: "missing_evidence", Ref: "go test ./internal/query -run '^(TestBuildDeploymentTraceResponseExplainsUncorrelatedCloudCandidates)$' -count=1"},
				{Case: "stale_evidence", Ref: "go test ./internal/query -run '^(TestTraceDeploymentChain_LocalLightweightReturnsStructuredUnsupportedCapability)$' -count=1"},
				{Case: "truncated_evidence", Ref: "go test ./internal/query -run '^(TestBuildDeploymentTraceResponseIncludesArtifactBackedDeliveryPaths)$' -count=1"},
				{Case: "inaccessible_evidence", Ref: "go test ./internal/query -run '^(TestTraceDeploymentChain_LocalLightweightReturnsStructuredUnsupportedCapability)$' -count=1"},
			},
			Properties: map[string]string{"provider_key_independent": "true", "deterministic_truth": "required"},
		}},
	}
	surfaces := SurfaceIndex{
		Capabilities: map[string]struct{}{"platform_impact.deployment_chain": {}},
		CapabilityAPIRoutes: map[string]map[string]struct{}{
			"platform_impact.deployment_chain": {"POST /api/v0/impact/trace-deployment-chain": {}},
		},
		CapabilityMCPTools: map[string]map[string]struct{}{
			"platform_impact.deployment_chain": {"trace_deployment_chain": {}},
		},
		APIRoutes: map[string]struct{}{"POST /api/v0/impact/trace-deployment-chain": {}, "GET /api/v0/supply-chain/impact/findings": {}},
		MCPTools:  map[string]struct{}{"trace_deployment_chain": {}},
	}

	findings := Validate(contract, surfaces)
	mustContainFinding(t, findings, "api_route_not_in_capability", "GET /api/v0/supply-chain/impact/findings")
}

func TestValidatorRejectsProseProofRefs(t *testing.T) {
	contract := Contract{
		Version: "v1",
		Rows: []Row{{
			ID:         "prose-proof",
			Domain:     "code_to_cloud",
			Capability: "platform_impact.deployment_chain",
			APIRoutes:  []string{"POST /api/v0/impact/trace-deployment-chain"},
			MCPTools:   []string{"trace_deployment_chain"},
			SourceFact: ProofRef{Ref: "operator should check the source fact"},
			Continuity: ContinuityProofs{
				Projection: ProofRef{Ref: "projection preserves evidence"},
				API:        ProofRef{Ref: "api preserves evidence"},
				MCP:        ProofRef{Ref: "mcp preserves evidence"},
			},
			EmptyStates: EmptyStateProofs{
				NoProvider:  ProofRef{Ref: "no provider still works"},
				NoCollector: ProofRef{Ref: "no collector is explicit"},
				Empty:       ProofRef{Ref: "empty is explicit"},
			},
			NegativeCase: []NegativeProof{{Case: "empty_evidence", Ref: "empty"}, {Case: "missing_evidence", Ref: "missing"}, {Case: "stale_evidence", Ref: "stale"}, {Case: "truncated_evidence", Ref: "truncated"}, {Case: "inaccessible_evidence", Ref: "inaccessible"}},
			Properties:   map[string]string{"provider_key_independent": "true", "deterministic_truth": "required"},
		}},
	}
	surfaces := SurfaceIndex{
		Capabilities: map[string]struct{}{"platform_impact.deployment_chain": {}},
		APIRoutes:    map[string]struct{}{"POST /api/v0/impact/trace-deployment-chain": {}},
		MCPTools:     map[string]struct{}{"trace_deployment_chain": {}},
	}

	findings := Validate(contract, surfaces)
	mustContainFinding(t, findings, "invalid_proof_ref", "source fact")
	mustContainFinding(t, findings, "invalid_proof_ref", "empty_evidence")
}

func TestValidatorRejectsBroadProofRefs(t *testing.T) {
	contract := Contract{
		Version: "v1",
		Rows: []Row{{
			ID:         "broad-proof",
			Domain:     "code_to_cloud",
			Capability: "platform_impact.deployment_chain",
			APIRoutes:  []string{"POST /api/v0/impact/trace-deployment-chain"},
			MCPTools:   []string{"trace_deployment_chain"},
			SourceFact: ProofRef{Ref: "go test ./internal/query -run 'Test.*Empty' -count=1"},
			Continuity: ContinuityProofs{
				Projection: ProofRef{Ref: "go test ./internal/query -run '^(TestBuildDeploymentTraceArtifactLineageSkipsImplicitOnlyEvidence)$' -count=1"},
				API:        ProofRef{Ref: "go test ./internal/query -run '^(TestTraceDeploymentChain_LocalLightweightReturnsStructuredUnsupportedCapability)$' -count=1"},
				MCP:        ProofRef{Ref: "go test ./internal/mcp -run '^(TestResolveRouteMapsTraceDeploymentChain)$' -count=1"},
			},
			EmptyStates: EmptyStateProofs{
				NoProvider:  ProofRef{Ref: "go test ./internal/query -run '^(TestTraceDeploymentChain_LocalLightweightReturnsStructuredUnsupportedCapability)$' -count=1"},
				NoCollector: ProofRef{Ref: "go test ./internal/query -run '^(TestTraceDeploymentChainSkipsIndirectEvidenceWhenDirectOnly)$' -count=1"},
				Empty:       ProofRef{Ref: "go test ./internal/query -run '^(TestBuildDeploymentTraceResponseDoesNotEmitStructurallyEmptyDeliveryPaths)$' -count=1"},
			},
			NegativeCase: []NegativeProof{
				{Case: "empty_evidence", Ref: "go test ./internal/query -run '^(TestBuildDeploymentTraceResponseDoesNotEmitStructurallyEmptyDeliveryPaths)$' -count=1"},
				{Case: "missing_evidence", Ref: "go test ./internal/query -run '^(TestBuildDeploymentTraceResponseExplainsUncorrelatedCloudCandidates)$' -count=1"},
				{Case: "stale_evidence", Ref: "go test ./internal/query -run '^(TestTraceDeploymentChain_LocalLightweightReturnsStructuredUnsupportedCapability)$' -count=1"},
				{Case: "truncated_evidence", Ref: "go test ./internal/query -run '^(TestBuildDeploymentTraceResponseIncludesArtifactBackedDeliveryPaths)$' -count=1"},
				{Case: "inaccessible_evidence", Ref: "go test ./internal/query -run '^(TestTraceDeploymentChain_LocalLightweightReturnsStructuredUnsupportedCapability)$' -count=1"},
			},
			Properties: map[string]string{"provider_key_independent": "true", "deterministic_truth": "required"},
		}},
	}
	surfaces := SurfaceIndex{
		Capabilities: map[string]struct{}{"platform_impact.deployment_chain": {}},
		APIRoutes:    map[string]struct{}{"POST /api/v0/impact/trace-deployment-chain": {}},
		MCPTools:     map[string]struct{}{"trace_deployment_chain": {}},
	}

	findings := Validate(contract, surfaces)
	mustContainFinding(t, findings, "invalid_proof_ref", "source fact")
}

func TestValidatorRejectsWrongPropertyValues(t *testing.T) {
	contract := Contract{
		Version: "v1",
		Rows: []Row{{
			ID:         "wrong-properties",
			Domain:     "code_to_cloud",
			Capability: "platform_impact.deployment_chain",
			APIRoutes:  []string{"POST /api/v0/impact/trace-deployment-chain"},
			MCPTools:   []string{"trace_deployment_chain"},
			SourceFact: ProofRef{Ref: "go test ./internal/query -run '^(TestBuildDeploymentTraceResponsePromotesWorkflowAndControllerProvenance)$' -count=1"},
			Continuity: ContinuityProofs{
				Projection: ProofRef{Ref: "go test ./internal/query -run '^(TestBuildDeploymentTraceArtifactLineageSkipsImplicitOnlyEvidence)$' -count=1"},
				API:        ProofRef{Ref: "go test ./internal/query -run '^(TestTraceDeploymentChain_LocalLightweightReturnsStructuredUnsupportedCapability)$' -count=1"},
				MCP:        ProofRef{Ref: "go test ./internal/mcp -run '^(TestResolveRouteMapsTraceDeploymentChain)$' -count=1"},
			},
			EmptyStates: EmptyStateProofs{
				NoProvider:  ProofRef{Ref: "go test ./internal/query -run '^(TestTraceDeploymentChain_LocalLightweightReturnsStructuredUnsupportedCapability)$' -count=1"},
				NoCollector: ProofRef{Ref: "go test ./internal/query -run '^(TestTraceDeploymentChainSkipsIndirectEvidenceWhenDirectOnly)$' -count=1"},
				Empty:       ProofRef{Ref: "go test ./internal/query -run '^(TestBuildDeploymentTraceResponseDoesNotEmitStructurallyEmptyDeliveryPaths)$' -count=1"},
			},
			NegativeCase: []NegativeProof{
				{Case: "empty_evidence", Ref: "go test ./internal/query -run '^(TestBuildDeploymentTraceResponseDoesNotEmitStructurallyEmptyDeliveryPaths)$' -count=1"},
				{Case: "missing_evidence", Ref: "go test ./internal/query -run '^(TestBuildDeploymentTraceResponseExplainsUncorrelatedCloudCandidates)$' -count=1"},
				{Case: "stale_evidence", Ref: "go test ./internal/query -run '^(TestTraceDeploymentChain_LocalLightweightReturnsStructuredUnsupportedCapability)$' -count=1"},
				{Case: "truncated_evidence", Ref: "go test ./internal/query -run '^(TestBuildDeploymentTraceResponseIncludesArtifactBackedDeliveryPaths)$' -count=1"},
				{Case: "inaccessible_evidence", Ref: "go test ./internal/query -run '^(TestTraceDeploymentChain_LocalLightweightReturnsStructuredUnsupportedCapability)$' -count=1"},
			},
			Properties: map[string]string{"provider_key_independent": "false", "deterministic_truth": "optional"},
		}},
	}
	surfaces := SurfaceIndex{
		Capabilities: map[string]struct{}{"platform_impact.deployment_chain": {}},
		APIRoutes:    map[string]struct{}{"POST /api/v0/impact/trace-deployment-chain": {}},
		MCPTools:     map[string]struct{}{"trace_deployment_chain": {}},
	}

	findings := Validate(contract, surfaces)
	mustContainFinding(t, findings, "invalid_property", "provider_key_independent")
	mustContainFinding(t, findings, "invalid_property", "deterministic_truth")
}

func mustContainFinding(t *testing.T, findings []Finding, kind FindingKind, needle string) {
	t.Helper()

	for _, finding := range findings {
		if finding.Kind == kind && (strings.Contains(finding.RowID, needle) || strings.Contains(finding.Message, needle)) {
			return
		}
	}
	t.Fatalf("missing finding %q containing %q in:\n%s", kind, needle, FormatFindings(findings))
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
}
