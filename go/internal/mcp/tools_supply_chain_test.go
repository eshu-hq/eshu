// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestSupplyChainToolsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"get_vulnerability_scanner_read_contract",
		"list_container_image_identities",
		"list_supply_chain_impact_findings",
		"list_advisory_evidence",
		"explain_supply_chain_impact",
		"list_security_alert_reconciliations",
		"list_sbom_attestation_attachments",
	} {
		_ = requireToolDefinition(t, name)
	}
}

func TestSupplyChainGetVulnerabilityScannerReadContractSchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "get_vulnerability_scanner_read_contract")
	schema, _ := tool.InputSchema.(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	if _, ok := properties["route"]; !ok {
		t.Fatalf("get_vulnerability_scanner_read_contract schema missing route")
	}
}

func TestSupplyChainListContainerImageIdentitiesSchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "list_container_image_identities")
	schema, _ := tool.InputSchema.(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	for _, field := range []string{"digest", "image_ref", "repository_id", "source_repository_id", "outcome", "limit"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("list_container_image_identities schema missing %q", field)
		}
	}
}

func TestSupplyChainListSupplyChainImpactFindingsSchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "list_supply_chain_impact_findings")
	schema, _ := tool.InputSchema.(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	for _, field := range []string{"cve_id", "advisory_id", "package_id", "repository_id", "impact_status", "limit"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("list_supply_chain_impact_findings schema missing %q", field)
		}
	}
}

func TestSupplyChainExplainSupplyChainImpactSchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "explain_supply_chain_impact")
	schema, _ := tool.InputSchema.(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	if _, ok := properties["finding_id"]; !ok {
		t.Fatalf("explain_supply_chain_impact schema missing finding_id")
	}
}

func TestSupplyChainResolveRouteGetVulnerabilityScannerReadContract(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_vulnerability_scanner_read_contract", map[string]any{
		"contract_version": "v1",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/supply-chain/vulnerability-scanner/contract"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestSupplyChainResolveRouteListContainerImageIdentities(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_container_image_identities", map[string]any{
		"repository_id": "repo-1",
		"limit":         float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/supply-chain/container-images/identities"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestSupplyChainResolveRouteListSupplyChainImpactFindings(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_supply_chain_impact_findings", map[string]any{
		"repository_id": "repo-1",
		"limit":         float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/supply-chain/impact/findings"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestSupplyChainResolveRouteExplainSupplyChainImpact(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("explain_supply_chain_impact", map[string]any{
		"finding_id": "finding-1",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/supply-chain/impact/explain"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestSupplyChainResolveRouteListSecurityAlertReconciliations(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_security_alert_reconciliations", map[string]any{
		"repository_id": "repo-1",
		"limit":         float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/supply-chain/security-alerts/reconciliations"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestSupplyChainResolveRouteListSBOMAttestationAttachments(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_sbom_attestation_attachments", map[string]any{
		"repository_id": "repo-1",
		"limit":         float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/supply-chain/sbom-attestations/attachments"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
