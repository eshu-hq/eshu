// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const (
	awsDriftScope                  = "aws:123456789012:us-east-1:ec2"
	awsDriftARN                    = "arn:aws:ec2:us-east-1:123456789012:instance/i-000000000000000a"
	awsLambdaResourceID            = "dafbedae2b62cb12dc97060debce1884305fd8dfdfb80f1fc8d514b17fae144f"
	libCommon                      = "lib-common"
	dartRepo                       = "dart_comprehensive"
	infraAWSCountSentinel          = "__runtime_infra_aws_count__"
	infraGCPCountSentinel          = "__runtime_infra_gcp_count__"
	ecosystemRepoCountSentinel     = "__runtime_ecosystem_repo_count__"
	ecosystemWorkloadCountSentinel = "__runtime_ecosystem_workload_count__"
)

type sourceBackedShapeSpec struct {
	slug          string
	transport     string
	key           string
	minimum       int
	maximum       int
	resultsField  string
	arguments     map[string]any
	requestBody   map[string]any
	values        map[string]any
	paths         []string
	objectMatches map[string][]map[string]any
}

func TestGoldenSnapshotSourceBackedRemainingRows(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	for _, spec := range sourceBackedRemainingSpecs() {
		spec := spec
		t.Run(spec.slug+"/"+spec.key, func(t *testing.T) {
			t.Parallel()
			shapes := snapshot.QueryShapes.MCP
			if spec.transport == "http" {
				shapes = snapshot.QueryShapes.HTTP
			}
			shape, ok := shapes[spec.key]
			if !ok {
				t.Fatalf("query_shapes.%s missing %q", spec.transport, spec.key)
			}
			if shape.MinimumResults != spec.minimum || shape.MaximumResults != spec.maximum || shape.ResultsField != spec.resultsField {
				t.Errorf("bounds/field = [%d,%d] %q, want [%d,%d] %q", shape.MinimumResults, shape.MaximumResults, shape.ResultsField, spec.minimum, spec.maximum, spec.resultsField)
			}
			if spec.arguments != nil && !reflect.DeepEqual(shape.Arguments, spec.arguments) {
				t.Errorf("Arguments = %#v, want %#v", shape.Arguments, spec.arguments)
			}
			if spec.requestBody != nil && !reflect.DeepEqual(shape.RequestBody, spec.requestBody) {
				t.Errorf("RequestBody = %#v, want %#v", shape.RequestBody, spec.requestBody)
			}
			assertSourceBackedValues(t, shape.RequiredJSONValues, spec.values)
			assertSourceBackedPaths(t, shape.RequiredJSONPaths, spec.paths)
			for path, want := range spec.objectMatches {
				if got := shape.RequiredJSONObjectMatches[path]; !reflect.DeepEqual(got, want) {
					t.Errorf("RequiredJSONObjectMatches[%q] = %#v, want %#v", path, got, want)
				}
			}
			assertSourceBackedShapeBITES(t, spec.key, shape)
		})
	}
}

func sourceBackedRemainingSpecs() []sourceBackedShapeSpec {
	return append(sourceBackedCoreSpecs(), sourceBackedContextAndCallSpecs()...)
}

func sourceBackedCoreSpecs() []sourceBackedShapeSpec {
	return []sourceBackedShapeSpec{
		{slug: "prod-aws-runtime-drift-read-model", transport: "mcp", key: "list_aws_runtime_drift_findings", minimum: 1, maximum: 10, resultsField: "drift_findings", arguments: map[string]any{"scope_id": awsDriftScope, "limit": float64(10)}, values: map[string]any{"scope_id": awsDriftScope, "drift_findings[].arn": awsDriftARN, "drift_findings[].finding_kind": "image_version_drift"}},
		{slug: "prod-aws-runtime-drift-read-model", transport: "mcp", key: "find_unmanaged_resources", minimum: 1, maximum: 10, resultsField: "findings", arguments: map[string]any{"scope_id": awsDriftScope, "finding_kinds": []any{"image_version_drift"}, "limit": float64(10)}, values: map[string]any{"scope_id": awsDriftScope, "findings[].arn": awsDriftARN, "findings[].finding_kind": "image_version_drift"}},
		{slug: "prod-aws-runtime-drift-read-model", transport: "mcp", key: "get_iac_management_status", arguments: map[string]any{"scope_id": awsDriftScope, "resource_id": awsDriftARN, "finding_kinds": []any{"image_version_drift"}}, values: map[string]any{"scope_id": awsDriftScope, "arn": awsDriftARN, "analysis_status": "active_management_finding", "finding.arn": awsDriftARN, "finding.finding_kind": "image_version_drift"}},
		{slug: "prod-aws-runtime-drift-read-model", transport: "mcp", key: "explain_iac_management_status", arguments: map[string]any{"scope_id": awsDriftScope, "resource_id": awsDriftARN, "finding_kinds": []any{"image_version_drift"}}, values: map[string]any{"scope_id": awsDriftScope, "arn": awsDriftARN, "finding.arn": awsDriftARN, "finding.finding_kind": "image_version_drift"}, paths: []string{"evidence_groups[]"}},
		{slug: "prod-aws-runtime-drift-read-model", transport: "mcp", key: "propose_terraform_import_plan", minimum: 1, maximum: 10, resultsField: "candidates", arguments: map[string]any{"scope_id": awsDriftScope, "resource_id": awsDriftARN, "finding_kinds": []any{"image_version_drift"}, "limit": float64(10)}, values: map[string]any{"scope_id": awsDriftScope, "arn": awsDriftARN, "candidates[].arn": awsDriftARN}},
		{slug: "prod-call-graph-metrics", transport: "mcp", key: "inspect_call_graph_metrics", minimum: 2, maximum: 10, resultsField: "functions", arguments: map[string]any{"repo_id": dartRepo, "language": "dart", "metric_type": "recursive_functions", "limit": float64(10)}, values: map[string]any{"metric_type": "recursive_functions", "scope.repo_id": "repository:r_ed3a9bab", "scope.language": "dart", "source_backend": "graph"}, objectMatches: map[string][]map[string]any{"functions[]": {{"function_name": "recursionFib", "recursion_kind": "self_call"}, {"function_name": "recursionFact", "recursion_kind": "self_call"}}}},
		{slug: "prod-change-surface", transport: "mcp", key: "find_change_surface", minimum: 1, maximum: 10, resultsField: "impacted", arguments: map[string]any{"target": "repository:r_3eddcea1", "limit": float64(10)}, values: map[string]any{"target.id": "repository:r_3eddcea1", "target.name": libCommon}, objectMatches: map[string][]map[string]any{"impacted[]": {{"id": "repository:r_ea78e8bb", "name": "orders-api", "rel_type": "DEPENDS_ON", "depth": float64(1)}}}},
		{slug: "prod-change-surface", transport: "mcp", key: "investigate_change_surface", minimum: 1, maximum: 10, resultsField: "direct_impact", arguments: map[string]any{"target": "repository:r_3eddcea1", "target_type": "repository", "limit": float64(10)}, values: map[string]any{"scope.target": "repository:r_3eddcea1", "scope.target_type": "repository", "target_resolution.status": "resolved"}, objectMatches: map[string][]map[string]any{"direct_impact[]": {{"id": "repository:r_ea78e8bb", "name": "orders-api", "depth": float64(1)}}}},
		{slug: "prod-dependency-path", transport: "mcp", key: "explain_dependency_path", values: map[string]any{"source.name": "orders-api", "target.name": libCommon, "path.depth": float64(1)}, arguments: map[string]any{"source": "orders-api", "target": libCommon}, objectMatches: map[string][]map[string]any{"path.hops[]": {{"from_name": "orders-api", "to_name": libCommon, "type": "DEPENDS_ON"}}}},
		{slug: "prod-deployment-config-influence", transport: "mcp", key: "investigate_deployment_config", minimum: 1, maximum: 10, resultsField: "rendered_targets", arguments: map[string]any{"service_name": "deployable-config", "limit": float64(10)}, values: map[string]any{"service_name": "deployable-config", "workload_id": "workload:deployable-config", "coverage.query_shape": "deployment_config_influence_story"}, paths: []string{"values_layers[]", "rendered_targets[]"}},
		{slug: "prod-entity-map", transport: "http", key: "POST /api/v0/impact/entity-map", requestBody: map[string]any{"from": "deployable-config", "from_type": "repository", "relationship": "DEFINES", "depth": float64(1), "limit": float64(10)}, values: map[string]any{"status": "mapped", "scope.from_type": "repository", "coverage.depth": float64(1)}, paths: []string{"evidence.relationships[]"}, objectMatches: map[string][]map[string]any{"evidence.relationships[]": {{"entity_id": "workload:deployable-config", "relationship_type": "DEFINES", "depth": float64(1)}}}},
		{slug: "prod-impact-surface", transport: "mcp", key: "find_blast_radius", minimum: 1, maximum: 10, resultsField: "affected", arguments: map[string]any{"target": libCommon, "target_type": "repository", "limit": float64(10)}, values: map[string]any{"target": libCommon, "target_type": "repository"}, objectMatches: map[string][]map[string]any{"affected[]": {{"repo": "orders-api", "hops": float64(1)}}}},
		{slug: "prod-infra-resource-aggregate", transport: "mcp", key: "count_infra_resources", values: map[string]any{"by_provider.aws": infraAWSCountSentinel, "by_provider.gcp": infraGCPCountSentinel}, paths: []string{"total_resources"}},
		{slug: "prod-infra-resource-aggregate", transport: "mcp", key: "get_infra_resource_inventory", minimum: 2, maximum: 10, resultsField: "buckets", arguments: map[string]any{"group_by": "provider", "limit": float64(10), "offset": float64(0)}, values: map[string]any{"group_by": "provider"}, objectMatches: map[string][]map[string]any{"buckets[]": {{"dimension": "provider", "value": "aws"}, {"dimension": "provider", "value": "gcp"}}}},
		{slug: "prod-observability-coverage-correlations", transport: "mcp", key: "list_observability_coverage_correlations", minimum: 1, maximum: 10, resultsField: "correlations", arguments: map[string]any{"provider": "tempo", "limit": float64(10)}, objectMatches: map[string][]map[string]any{"correlations[]": {{"provider": "tempo", "coverage_signal": "trace_signal", "observability_object_ref": "service-name-tagset", "outcome": "exact", "resource_class": "trace_signal"}}}},
		{slug: "prod-package-registry-versions", transport: "http", key: "GET /api/v0/package-registry/versions?package_id=github.com/acme/lib-common&limit=50", minimum: 1, maximum: 50, resultsField: "versions", values: map[string]any{"versions[].package_id": "github.com/acme/lib-common", "versions[].version_id": "github.com/acme/lib-common@1.0.0", "versions[].version": "1.0.0", "versions[].purl": "pkg:golang/github.com/acme/lib-common@1.0.0", "versions[].package_manager": "gomod"}},
		{slug: "prod-relationships-catalog", transport: "http", key: "POST /api/v0/relationships/catalog", minimum: 1, maximum: 50, resultsField: "verbs", objectMatches: map[string][]map[string]any{"verbs[]": {{"verb": "CALLS", "layer": "code"}, {"verb": "DEPENDS_ON"}}}},
		{slug: "prod-relationships-catalog", transport: "http", key: "POST /api/v0/relationships/edges?assert=deploys-from", minimum: 1, maximum: 10, resultsField: "edges", requestBody: map[string]any{"verb": "DEPLOYS_FROM", "limit": float64(10)}, values: map[string]any{"verb": "DEPLOYS_FROM"}, objectMatches: map[string][]map[string]any{"edges[]": {{"source_id": "repository:r_217415d9", "source_name": "deployable-config", "target_id": "repository:r_1f68383d", "target_name": "deployable-source"}}}},
		{slug: "prod-resource-to-code", transport: "mcp", key: "trace_resource_to_code", minimum: 1, maximum: 10, resultsField: "paths", arguments: map[string]any{"start": awsLambdaResourceID, "max_depth": float64(4), "limit": float64(10)}, values: map[string]any{"start.id": awsLambdaResourceID, "paths[].repo_id": "repository:r_69256c06"}, objectMatches: map[string][]map[string]any{"paths[].hops[]": {{"type": "AWS_lambda_function_uses_image"}, {"type": "BUILT_FROM"}}}},
	}
}

func sourceBackedContextAndCallSpecs() []sourceBackedShapeSpec {
	return []sourceBackedShapeSpec{
		{slug: "prod-context-overview", transport: "mcp", key: "get_repo_context", minimum: 1, maximum: 50, resultsField: "relationships", arguments: map[string]any{"repo_id": "orders-api"}, values: map[string]any{"repository.id": "repository:r_ea78e8bb", "repository.name": "orders-api"}},
		{slug: "prod-context-overview", transport: "mcp", key: "get_repo_summary", arguments: map[string]any{"repo_name": "orders-api"}, values: map[string]any{"repository.name": "orders-api"}, paths: []string{"coverage"}},
		{slug: "prod-context-overview", transport: "mcp", key: "get_repo_story", minimum: 1, maximum: 50, resultsField: "story_sections", arguments: map[string]any{"repo_id": "orders-api"}, values: map[string]any{"subject.id": "repository:r_ea78e8bb", "subject.name": "orders-api"}},
		{slug: "prod-context-overview", transport: "mcp", key: "get_service_context", minimum: 1, maximum: 50, resultsField: "story_sections", arguments: map[string]any{"workload_id": "workload:api-svc"}, values: map[string]any{"id": "workload:api-svc", "name": "api-svc"}},
		{slug: "prod-context-overview", transport: "mcp", key: "get_service_story", minimum: 1, maximum: 50, resultsField: "story_sections", arguments: map[string]any{"workload_id": "workload:api-svc"}, values: map[string]any{"service_identity.service_id": "workload:api-svc"}},
		{slug: "prod-context-overview", transport: "mcp", key: "get_workload_context", minimum: 1, maximum: 50, resultsField: "story_sections", arguments: map[string]any{"workload_id": "workload:api-svc"}, values: map[string]any{"id": "workload:api-svc", "name": "api-svc"}},
		{slug: "prod-context-overview", transport: "mcp", key: "get_workload_story", arguments: map[string]any{"workload_id": "workload:api-svc"}, values: map[string]any{"workload_id": "workload:api-svc", "name": "api-svc"}},
		{slug: "prod-context-overview", transport: "mcp", key: "get_service_intelligence_report", minimum: 1, maximum: 50, resultsField: "sections", arguments: map[string]any{"workload_id": "workload:api-svc"}, values: map[string]any{"schema": "service_intelligence_report.v1", "subject.service_id": "workload:api-svc"}},
		{slug: "prod-context-overview", transport: "mcp", key: "investigate_service", minimum: 1, maximum: 50, resultsField: "repositories_with_evidence", arguments: map[string]any{"service_name": "api-svc", "question": "runtime readiness"}, values: map[string]any{"service_name": "api-svc"}, paths: []string{"service_context_path", "service_story_path"}},
		{slug: "prod-context-overview", transport: "mcp", key: "get_ecosystem_overview", values: map[string]any{"repo_count": ecosystemRepoCountSentinel, "workload_count": ecosystemWorkloadCountSentinel}},
		{slug: "prod-direct-callers", transport: "http", key: "POST /api/v0/code/relationships?assert=direct-callers", minimum: 1, maximum: 1, resultsField: "incoming", requestBody: map[string]any{"name": "mutualPing", "repo_id": dartRepo, "direction": "incoming", "relationship_type": "CALLS"}, values: map[string]any{"name": "mutualPing", "repo_id": "repository:r_ed3a9bab"}, objectMatches: map[string][]map[string]any{"incoming[]": {{"source_name": "mutualPong", "target_name": "mutualPing", "type": "CALLS"}}}},
		{slug: "prod-direct-callees", transport: "http", key: "POST /api/v0/code/relationships?assert=direct-callees", minimum: 1, maximum: 1, resultsField: "outgoing", requestBody: map[string]any{"name": "mutualPing", "repo_id": dartRepo, "direction": "outgoing", "relationship_type": "CALLS"}, values: map[string]any{"name": "mutualPing", "repo_id": "repository:r_ed3a9bab"}, objectMatches: map[string][]map[string]any{"outgoing[]": {{"source_name": "mutualPing", "target_name": "mutualPong", "type": "CALLS"}}}},
		{slug: "prod-transitive-callers", transport: "http", key: "POST /api/v0/code/relationships?assert=transitive-callers", minimum: 1, maximum: 4, resultsField: "incoming", requestBody: map[string]any{"name": "mutualPing", "repo_id": dartRepo, "direction": "incoming", "relationship_type": "CALLS", "transitive": true, "max_depth": float64(4)}, objectMatches: map[string][]map[string]any{"incoming[]": {{"source_name": "mutualPong", "type": "CALLS", "depth": float64(1), "reason": "transitive_call_graph"}}}},
		{slug: "prod-transitive-callees", transport: "http", key: "POST /api/v0/code/relationships?assert=transitive-callees", minimum: 1, maximum: 4, resultsField: "outgoing", requestBody: map[string]any{"name": "mutualPing", "repo_id": dartRepo, "direction": "outgoing", "relationship_type": "CALLS", "transitive": true, "max_depth": float64(4)}, objectMatches: map[string][]map[string]any{"outgoing[]": {{"target_name": "mutualPong", "type": "CALLS", "depth": float64(1), "reason": "transitive_call_graph"}}}},
	}
}

func assertSourceBackedValues(t *testing.T, got, want map[string]any) {
	t.Helper()
	for path, value := range want {
		if actual := got[path]; !reflect.DeepEqual(actual, value) {
			t.Errorf("RequiredJSONValues[%q] = %#v, want %#v", path, actual, value)
		}
	}
}

func assertSourceBackedPaths(t *testing.T, got, want []string) {
	t.Helper()
	for _, path := range want {
		if !containsString(got, path) {
			t.Errorf("RequiredJSONPaths missing %q", path)
		}
	}
}

func assertSourceBackedShapeBITES(t *testing.T, name string, shape QueryShape) {
	t.Helper()
	response := fakeQueryShapeResponse(shape)
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal positive response: %v", err)
	}
	if finding := EvaluateQueryShape(name+"-positive", shape, raw); !finding.OK {
		t.Fatalf("generated positive response failed: %+v", finding)
	}
	if shape.ResultsField != "" && shape.MinimumResults > 0 {
		response[shape.ResultsField] = []any{}
		empty, marshalErr := json.Marshal(response)
		if marshalErr != nil {
			t.Fatalf("marshal empty response: %v", marshalErr)
		}
		if finding := EvaluateQueryShape(name+"-empty", shape, empty); finding.OK {
			t.Fatalf("empty %s response passed: %+v", shape.ResultsField, finding)
		}
	}
	wrong := wrongPinnedSourceBackedResponse(raw, shape)
	if wrong != nil {
		if finding := EvaluateQueryShape(name+"-wrong", shape, wrong); finding.OK {
			t.Fatalf("wrong pinned identity passed: %+v", finding)
		}
	}
}

func wrongPinnedSourceBackedResponse(raw []byte, shape QueryShape) []byte {
	for _, matches := range shape.RequiredJSONObjectMatches {
		for _, match := range matches {
			for _, value := range match {
				if text, ok := value.(string); ok && text != "" {
					encoded, _ := json.Marshal(text)
					return []byte(strings.ReplaceAll(string(raw), string(encoded), `"__wrong_identity__"`))
				}
			}
		}
	}
	for _, value := range shape.RequiredJSONValues {
		if text, ok := value.(string); ok && text != "" {
			encoded, _ := json.Marshal(text)
			return []byte(strings.ReplaceAll(string(raw), string(encoded), `"__wrong_identity__"`))
		}
	}
	return nil
}
