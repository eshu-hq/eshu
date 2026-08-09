// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"reflect"
	"testing"
)

const groupCRelationshipResolvedIDSentinel = "__runtime_group_c_relationship_resolved_id__"

type groupCRemainingExpectation struct {
	slug          string
	surface       string
	key           string
	minimum       int
	maximum       int
	resultsField  string
	arguments     map[string]any
	values        map[string]any
	paths         []string
	objectMatches map[string][]map[string]any
	scalar        bool
}

func TestGoldenSnapshotGroupCRemainingClaimsAreNonVacuous(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	expectations := groupCRemainingExpectations()
	if got, want := len(expectations), 20; got != want {
		t.Fatalf("Group C expectation count = %d, want %d", got, want)
	}

	for _, expectation := range expectations {
		expectation := expectation
		t.Run(expectation.slug, func(t *testing.T) {
			t.Parallel()
			if expectation.scalar && (expectation.minimum != 0 || expectation.maximum != 0 || expectation.resultsField != "") {
				t.Fatalf("scalar expectation configures array bounds: [%d,%d] %q", expectation.minimum, expectation.maximum, expectation.resultsField)
			}
			if expectation.scalar && len(expectation.paths) == 0 && len(expectation.objectMatches) == 0 {
				t.Fatal("scalar expectation must require a non-empty nested collection")
			}
			shape, ok := groupCRemainingShape(snapshot, expectation)
			if !ok {
				t.Fatalf("%s shape %q is missing", expectation.surface, expectation.key)
			}
			if shape.MinimumResults != expectation.minimum || shape.MaximumResults != expectation.maximum {
				t.Errorf("result bounds = [%d,%d], want [%d,%d]", shape.MinimumResults, shape.MaximumResults, expectation.minimum, expectation.maximum)
			}
			if shape.ResultsField != expectation.resultsField {
				t.Errorf("ResultsField = %q, want %q", shape.ResultsField, expectation.resultsField)
			}
			if expectation.surface == "mcp" && !reflect.DeepEqual(shape.Arguments, expectation.arguments) {
				t.Errorf("Arguments = %#v, want %#v", shape.Arguments, expectation.arguments)
			}
			assertSnapshotValues(t, shape.RequiredJSONValues, expectation.values)
			assertSnapshotPaths(t, shape.RequiredJSONPaths, expectation.paths)
			for path, want := range expectation.objectMatches {
				if got := shape.RequiredJSONObjectMatches[path]; !reflect.DeepEqual(got, want) {
					t.Errorf("RequiredJSONObjectMatches[%q] = %#v, want %#v", path, got, want)
				}
			}
			assertSourceBackedShapeBITES(t, expectation.key, shape)
		})
	}
}

func groupCRemainingShape(snapshot Snapshot, expectation groupCRemainingExpectation) (QueryShape, bool) {
	if expectation.surface == "http" {
		shape, ok := snapshot.QueryShapes.HTTP[expectation.key]
		return shape, ok
	}
	shape, ok := snapshot.QueryShapes.MCP[expectation.key]
	return shape, ok
}

func groupCRemainingExpectations() []groupCRemainingExpectation {
	const (
		awsScope        = "aws:123456789012:us-east-1:ec2"
		awsARN          = "arn:aws:ec2:us-east-1:123456789012:instance/i-000000000000000a"
		digest          = "sha256:2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e7f80901a"
		pkgID           = "github.com/acme/lib-common"
		ordersRepoID    = "repository:r_ea78e8bb"
		libCommonRepoID = "repository:r_3eddcea1"
	)
	cypher := "MATCH (source:Repository {id: '" + ordersRepoID + "'})-[rel:DEPENDS_ON]->(target:Repository {id: '" + libCommonRepoID + "'}) RETURN source, rel, target, source.id AS source_id, type(rel) AS relationship, target.id AS target_id LIMIT 1"
	return []groupCRemainingExpectation{
		{slug: "prod-package-registry-aggregate", surface: "mcp", key: "get_package_registry_package_inventory", minimum: 1, maximum: 1, resultsField: "buckets", arguments: map[string]any{"group_by": "ecosystem", "ecosystem": "gomod", "limit": float64(10), "offset": float64(0)}, values: map[string]any{"group_by": "ecosystem", "buckets[].dimension": "ecosystem", "buckets[].value": "gomod"}, objectMatches: map[string][]map[string]any{"buckets[]": {{"dimension": "ecosystem", "value": "gomod"}}}},
		{slug: "prod-package-registry-dependencies", surface: "mcp", key: "list_package_registry_dependencies", minimum: 1, maximum: 1, resultsField: "dependencies", arguments: map[string]any{"package_id": pkgID, "limit": float64(10)}, objectMatches: map[string][]map[string]any{"dependencies[]": {{"source_package_id": pkgID, "dependency_package_id": "github.com/acme/synthetic-dep", "dependency_type": "direct"}}}},
		{slug: "prod-package-registry-dependency-chains", surface: "http", key: "GET /api/v0/package-registry/dependency-chains?repository_id=orders-api&limit=10", minimum: 1, maximum: 1, resultsField: "chains", values: map[string]any{"repository_id": ordersRepoID}, objectMatches: map[string][]map[string]any{"chains[]": {{"consumer_repository_id": ordersRepoID, "package_id": pkgID}}}},
		{slug: "prod-pre-change-impact", surface: "mcp", key: "analyze_pre_change_impact", minimum: 1, maximum: 10, resultsField: "direct_impact", arguments: map[string]any{"repo_id": "orders-api", "changed_paths": []any{"main.go"}, "topic": "lib-common", "limit": float64(10)}, values: map[string]any{"workflow": "pre_change_impact", "changed_file_count": float64(1), "changed_files[].repo_id": "orders-api", "changed_files[].path": "main.go"}, paths: []string{"direct_impact[].source_handle"}},
		{slug: "prod-read-only-cypher", surface: "mcp", key: "execute_cypher_query", minimum: 1, maximum: 1, resultsField: "results", arguments: map[string]any{"cypher_query": cypher, "limit": float64(1)}, objectMatches: map[string][]map[string]any{"results[]": {{"source_id": ordersRepoID, "relationship": "DEPENDS_ON", "target_id": libCommonRepoID}}}},
		{slug: "prod-relationship-evidence", surface: "mcp", key: "get_relationship_evidence", minimum: 1, maximum: 10, resultsField: "evidence_preview", arguments: map[string]any{"resolved_id": groupCRelationshipResolvedIDSentinel}, values: map[string]any{"resolved_id": groupCRelationshipResolvedIDSentinel, "relationship_type": "DEPLOYS_FROM", "source.repo_id": "repository:r_217415d9", "target.repo_id": "repository:r_1f68383d"}, paths: []string{"evidence_preview[].kind"}},
		{slug: "prod-relationship-story", surface: "mcp", key: "get_code_relationship_story", minimum: 1, maximum: 1, resultsField: "relationships", arguments: map[string]any{"entity_id": "orders-api:main.go", "repo_id": "orders-api", "relationship_type": "IMPORTS", "direction": "outgoing", "limit": float64(10)}, values: map[string]any{"target_resolution.entity_id": "orders-api:main.go", "target_resolution.status": "resolved"}, objectMatches: map[string][]map[string]any{"relationships[]": {{"type": "IMPORTS", "source_id": "orders-api:main.go"}}}},
		{slug: "prod-replatforming-ownership", surface: "mcp", key: "find_unmanaged_resource_owners", minimum: 1, maximum: 1, resultsField: "ownership_packets", arguments: map[string]any{"scope_id": awsScope, "limit": float64(10)}, values: map[string]any{"scope_id": awsScope, "ownership_packets[].stable_id": awsARN, "ownership_packets[].finding_kind": "image_version_drift"}},
		{slug: "prod-replatforming-plan-readiness", surface: "mcp", key: "compose_replatforming_plan", minimum: 1, maximum: 10, resultsField: "blast_radius_summaries", arguments: map[string]any{"scope_kind": "resource", "scope_id": awsScope, "arn": awsARN, "limit": float64(10)}, values: map[string]any{"scope_id": awsScope, "plan.scope.resource": awsARN, "plan.items[].stable_id": awsARN, "analysis_status": "replatforming_plan_composition"}},
		{slug: "prod-replatforming-rollups", surface: "mcp", key: "get_replatforming_rollups", arguments: map[string]any{"scope_id": awsScope, "limit": float64(10)}, values: map[string]any{"scope_id": awsScope, "rollup_findings_count": float64(1), "analysis_status": "replatforming_rollups"}, paths: []string{"dimensions.account[]"}, objectMatches: map[string][]map[string]any{"dimensions.account[]": {{"key": "123456789012"}}}, scalar: true},
		{slug: "prod-replatforming-selector-inventory", surface: "http", key: "GET /api/v0/replatforming/selectors?limit=10", minimum: 1, maximum: 10, resultsField: "scopes", values: map[string]any{"readiness.state": "ready"}, objectMatches: map[string][]map[string]any{"scopes[]": {{"scope_id": awsScope, "account_id": "123456789012", "region": "us-east-1", "service": "ec2"}}}},
		{slug: "prod-sbom-attestation-attachment-aggregate", surface: "mcp", key: "get_sbom_attestation_attachment_inventory", minimum: 2, maximum: 2, resultsField: "buckets", arguments: map[string]any{"group_by": "artifact_kind", "subject_digest": digest, "limit": float64(10), "offset": float64(0)}, values: map[string]any{"group_by": "artifact_kind"}, objectMatches: map[string][]map[string]any{"buckets[]": {{"dimension": "artifact_kind", "value": "attestation", "count": float64(1)}, {"dimension": "artifact_kind", "value": "sbom", "count": float64(1)}}}},
		{slug: "prod-security-alert-reconciliation-aggregate", surface: "mcp", key: "get_security_alert_reconciliation_inventory", minimum: 1, maximum: 1, resultsField: "buckets", arguments: map[string]any{"group_by": "provider", "repository_id": "security_alert:supply-chain-demo:supply-chain-demo", "provider": "github_dependabot", "limit": float64(10), "offset": float64(0)}, objectMatches: map[string][]map[string]any{"buckets[]": {{"dimension": "provider", "value": "github_dependabot", "count": float64(1)}}}},
		{slug: "prod-security-alert-reconciliations", surface: "mcp", key: "list_security_alert_reconciliations", minimum: 1, maximum: 1, resultsField: "reconciliations", arguments: map[string]any{"repository_id": "security_alert:supply-chain-demo:supply-chain-demo", "provider": "github_dependabot", "limit": float64(10)}, values: map[string]any{"reconciliations[].provider_alert.provider_alert_id": "github_dependabot:security_alert:supply-chain-demo:supply-chain-demo:42", "reconciliations[].provider_alert.ghsa_ids[]": "GHSA-scd0-0000-demo"}},
		{slug: "prod-structural-inventory", surface: "mcp", key: "inspect_code_inventory", minimum: 1, maximum: 1, resultsField: "results", arguments: map[string]any{"repo_id": "orders-api", "inventory_kind": "entity", "entity_kind": "Function", "symbol": "main", "limit": float64(1)}, objectMatches: map[string][]map[string]any{"results[]": {{"entity_id": "Function:orders-api:main", "name": "main", "repo_id": "orders-api", "relative_path": "main.go"}}}},
		{slug: "prod-symbol-lookup", surface: "mcp", key: "find_symbol", minimum: 1, maximum: 1, resultsField: "results", arguments: map[string]any{"symbol": "main", "repo_id": "orders-api", "limit": float64(1)}, objectMatches: map[string][]map[string]any{"results[]": {{"entity_id": "Function:orders-api:main", "name": "main", "repo_id": "orders-api", "relative_path": "main.go"}}}},
		{slug: "prod-topic-investigation", surface: "mcp", key: "investigate_code_topic", minimum: 1, maximum: 10, resultsField: "evidence_groups", arguments: map[string]any{"topic": "lib-common", "repo_id": "orders-api", "limit": float64(10)}, values: map[string]any{"topic": "lib-common", "evidence_groups[].repo_id": "orders-api", "evidence_groups[].relative_path": "main.go", "evidence_groups[].matched_terms[]": "lib-common"}},
		{slug: "prod-visualization-graph-query", surface: "mcp", key: "visualize_graph_query", arguments: map[string]any{"cypher_query": cypher, "limit": float64(1)}, values: map[string]any{"visualization_packet.view": "graph_query", "visualization_packet.supported": true, "visualization_packet.limits.node_count": float64(2), "visualization_packet.limits.edge_count": float64(1)}, objectMatches: map[string][]map[string]any{"visualization_packet.nodes[]": {{"type": "Repository", "label": "orders-api"}, {"type": "Repository", "label": "lib-common"}}, "visualization_packet.edges[]": {{"relationship": "DEPENDS_ON"}}}, scalar: true},
		{slug: "prod-vulnerability-scanner-contract", surface: "mcp", key: "get_vulnerability_scanner_read_contract", minimum: 1, maximum: 1, resultsField: "routes", arguments: map[string]any{"route": "impact_findings"}, values: map[string]any{"schema_version": "eshu.vulnerability_scanner_read_contract.v1", "remediation_packet.schema_version": "eshu.supply_chain_remediation_packet.v1"}, objectMatches: map[string][]map[string]any{"routes[]": {{"name": "impact_findings", "path": "/api/v0/supply-chain/impact/findings", "tool": "list_supply_chain_impact_findings"}}}},
		{slug: "prod-work-item-evidence", surface: "mcp", key: "list_work_item_evidence", minimum: 1, maximum: 1, resultsField: "evidence", arguments: map[string]any{"scope_id": "jira:supply-chain-demo:SCD", "limit": float64(10)}, values: map[string]any{"evidence[].scope_id": "jira:supply-chain-demo:SCD", "evidence[].work_item_key": "SCD-1", "evidence[].provider_work_item_id": "10001"}},
	}
}
