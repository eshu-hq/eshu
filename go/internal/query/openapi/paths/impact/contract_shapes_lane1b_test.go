// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractShapesLane1Impact pins the lane-1 shape-completeness fixes for
// the impact path family: every 200-response object the console renders must
// name its properties in the fragment instead of a bare {"type": "object"},
// every 200-response property must carry a description naming its emitting
// owner, and string-map scopes must use the filter-echo idiom. Each entry
// navigates the fragment JSON and requires the property keys the Go owner
// actually emits; invented shapes fail review.
func TestContractShapesLane1ImpactB(t *testing.T) {
	t.Parallel()

	fragment := func(t *testing.T, raw, where string) map[string]any {
		t.Helper()
		trimmed := strings.TrimSpace(raw)
		trimmed = strings.TrimSuffix(trimmed, ",")
		var doc map[string]any
		if err := json.Unmarshal([]byte("{"+trimmed+"}"), &doc); err != nil {
			t.Fatalf("%s: fragment is not valid JSON: %v", where, err)
		}
		return doc
	}

	child := func(t *testing.T, node map[string]any, key, where string) map[string]any {
		t.Helper()
		next, ok := node[key].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing or non-object property %q", where, key)
		}
		return next
	}

	schemaAt := func(t *testing.T, doc map[string]any, path, method string) map[string]any {
		t.Helper()
		route, ok := doc[path].(map[string]any)
		if !ok {
			t.Fatalf("fragment paths missing %s", path)
		}
		op := child(t, route, method, path)
		responses := child(t, op, "responses", path+" "+method)
		okResp := child(t, responses, "200", path+" "+method)
		content, ok := okResp["content"].(map[string]any)
		if !ok {
			t.Fatalf("%s %s 200 response has no content", path, method)
		}
		body := child(t, content, "application/json", path+" "+method)
		return child(t, body, "schema", path+" "+method)
	}

	propsOf := func(t *testing.T, node map[string]any, where string) map[string]any {
		t.Helper()
		props, ok := node["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s: object schema has no properties", where)
		}
		return props
	}

	itemsOf := func(t *testing.T, node map[string]any, where string) map[string]any {
		t.Helper()
		items, ok := node["items"].(map[string]any)
		if !ok {
			t.Fatalf("%s: array schema has no items", where)
		}
		return items
	}

	requireKeys := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		for _, key := range keys {
			if _, ok := props[key].(map[string]any); !ok {
				t.Errorf("%s: missing property %q", where, key)
			}
		}
	}

	requireExactKeys := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		want := make(map[string]bool, len(keys))
		for _, key := range keys {
			want[key] = true
			if _, ok := props[key].(map[string]any); !ok {
				t.Errorf("%s: missing property %q", where, key)
			}
		}
		for key := range props {
			if !want[key] {
				t.Errorf("%s: phantom property %q not emitted by the owner", where, key)
			}
		}
	}

	requireScalarTypes := func(t *testing.T, props map[string]any, where string, want map[string]string) {
		t.Helper()
		for key, typ := range want {
			node, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if got, _ := node["type"].(string); got != typ {
				t.Errorf("%s: property %q has type %q, want %q", where, key, got, typ)
			}
		}
	}

	requireDescribed := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		for _, key := range keys {
			node, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if desc, _ := node["description"].(string); desc == "" {
				t.Errorf("%s: property %q has no description", where, key)
			}
		}
	}

	// requireNoBareObjects fails on any {"type": "object"} without
	// properties or additionalProperties, and any array items object
	// without properties or additionalProperties. An object with an
	// explicitly empty properties map documents an always-empty shape
	// (e.g. consumers, which the owner never populates); it passes.
	var requireNoBareObjects func(t *testing.T, node any, where string)
	requireNoBareObjects = func(t *testing.T, node any, where string) {
		t.Helper()
		obj, ok := node.(map[string]any)
		if !ok {
			return
		}
		if obj["type"] == "object" {
			_, hasProps := obj["properties"]
			_, hasAdditional := obj["additionalProperties"]
			if !hasProps && !hasAdditional {
				t.Errorf("%s: bare object with no properties", where)
			}
			if props, ok := obj["properties"].(map[string]any); ok {
				for key, sub := range props {
					requireNoBareObjects(t, sub, where+"."+key)
				}
			}
			if addl, ok := obj["additionalProperties"].(map[string]any); ok {
				requireNoBareObjects(t, addl, where+"[]")
			}
		}
		if obj["type"] == "array" {
			items, ok := obj["items"].(map[string]any)
			if !ok {
				t.Errorf("%s: array schema has no items", where)
				return
			}
			if items["type"] == "object" {
				_, hasProps := items["properties"]
				_, hasAdditional := items["additionalProperties"]
				if !hasProps && !hasAdditional {
					t.Errorf("%s: array has bare object items", where)
				}
			}
			requireNoBareObjects(t, items, where+"[]")
		}
		for key, sub := range obj {
			if key == "properties" || key == "items" || key == "additionalProperties" {
				continue
			}
			requireNoBareObjects(t, sub, where+"."+key)
		}
	}

	t.Run("developer change plan", func(t *testing.T) {
		t.Parallel()
		// Owner: impact/developer_change_plan.go developerChangePlanData
		// and its row builders.
		doc := fragment(t, Rest, "Rest")
		props := propsOf(t, schemaAt(t, doc, "/api/v0/impact/developer-change-plan", "post"), "dev-plan")
		requireDescribed(t, props, "dev-plan",
			"schema_version", "workflow", "read_only", "developer_intent",
			"change_set", "changed_files", "changed_file_count", "coverage",
			"affected_entities", "missing_evidence", "recommended_tests",
			"bounded_next_calls", "actions", "patch_guidance", "blocked",
			"truncated", "pre_change_summary", "pre_change_impact_ref",
			"answer_metadata", "answer_packet")
		requireScalarTypes(t, props, "dev-plan", map[string]string{
			"schema_version": "string", "workflow": "string", "read_only": "boolean",
			"developer_intent": "string", "changed_file_count": "integer",
			"blocked": "boolean", "truncated": "boolean",
			"pre_change_summary": "string", "pre_change_impact_ref": "string",
		})
		entities := child(t, props, "affected_entities", "dev-plan")
		entityProps := propsOf(t, itemsOf(t, entities, "dev-plan entities"), "dev-plan affected_entities[]")
		requireExactKeys(t, entityProps, "dev-plan affected_entities[]",
			"kind", "name", "relative_path", "language")
		tests := child(t, props, "recommended_tests", "dev-plan")
		testProps := propsOf(t, itemsOf(t, tests, "dev-plan tests"), "dev-plan recommended_tests[]")
		requireExactKeys(t, testProps, "dev-plan recommended_tests[]", "command", "reason")
		calls := child(t, props, "bounded_next_calls", "dev-plan")
		callProps := propsOf(t, itemsOf(t, calls, "dev-plan calls"), "dev-plan bounded_next_calls[]")
		requireExactKeys(t, callProps, "dev-plan bounded_next_calls[]", "kind", "target", "reason")
		guidance := child(t, props, "patch_guidance", "dev-plan")
		guidanceProps := propsOf(t, itemsOf(t, guidance, "dev-plan guidance"), "dev-plan patch_guidance[]")
		requireKeys(t, guidanceProps, "dev-plan patch_guidance[]",
			"status", "relative_path", "guidance", "safe", "old_path")
	})

	t.Run("entity map", func(t *testing.T) {
		t.Parallel()
		// Owner: impact/entity_map_response.go entityMapResponse and
		// entity_map_traversal.go entityMapRelationshipMaps.
		doc := fragment(t, Rest, "Rest")
		props := propsOf(t, schemaAt(t, doc, "/api/v0/impact/entity-map", "post"), "entity-map")
		requireDescribed(t, props, "entity-map",
			"status", "command", "from", "scope", "resolution", "sections",
			"evidence", "coverage", "warnings")
		sectionsProps := propsOf(t, child(t, props, "sections", "entity-map"), "entity-map.sections")
		requireExactKeys(t, sectionsProps, "entity-map.sections",
			"defined_by", "deployed_by", "runs_as", "depends_on", "consumed_by")
		depends := child(t, sectionsProps, "depends_on", "entity-map.sections")
		relProps := propsOf(t, itemsOf(t, depends, "depends_on"), "entity-map sections.depends_on[]")
		requireKeys(t, relProps, "entity-map sections.depends_on[]",
			"entity_id", "entity_name", "direction", "relationship_type",
			"repo_id", "environment", "entity_labels", "relationship_types", "depth")
		evidenceProps := propsOf(t, child(t, props, "evidence", "entity-map"), "entity-map.evidence")
		requireExactKeys(t, evidenceProps, "entity-map.evidence",
			"relationships", "relationship_count", "truncated", "relationship_filter")
		coverageProps := propsOf(t, child(t, props, "coverage", "entity-map"), "entity-map.coverage")
		requireExactKeys(t, coverageProps, "entity-map.coverage",
			"query_shape", "depth", "limit", "truncated")
	})

	t.Run("resource investigation", func(t *testing.T) {
		t.Parallel()
		// Owner: impact/resource_investigation_response.go
		// resourceInvestigationResponse plus the candidate, workload,
		// and path-row builders it calls.
		doc := fragment(t, Rest, "Rest")
		props := propsOf(t, schemaAt(t, doc, "/api/v0/impact/resource-investigation", "post"), "resource")
		requireDescribed(t, props, "resource",
			"scope", "target_resolution", "resource", "story", "workloads",
			"workload_count", "provisioning_paths", "source_handles",
			"recommended_next_calls", "missing_evidence", "limitations",
			"coverage", "limit", "max_depth", "truncated", "source_backend")
		scopeProps := propsOf(t, child(t, props, "scope", "resource"), "resource.scope")
		requireExactKeys(t, scopeProps, "resource.scope",
			"query", "resource_id", "resource_type", "environment")
		workloads := child(t, props, "workloads", "resource")
		workloadProps := propsOf(t, itemsOf(t, workloads, "resource workloads"), "resource workloads[]")
		requireKeys(t, workloadProps, "resource workloads[]",
			"workload_id", "workload_name", "instance_id", "environment",
			"relationship_type", "relationship_reason", "confidence")
		paths := child(t, props, "provisioning_paths", "resource")
		pathProps := propsOf(t, itemsOf(t, paths, "resource paths"), "resource provisioning_paths[]")
		requireExactKeys(t, pathProps, "resource provisioning_paths[]",
			"repo_id", "repo_name", "direction", "depth", "hops")
		coverageProps := propsOf(t, child(t, props, "coverage", "resource"), "resource.coverage")
		requireExactKeys(t, coverageProps, "resource.coverage",
			"query_shape", "max_depth", "limit", "truncated", "workload_count", "path_count")
	})

	t.Run("trace resource to code", func(t *testing.T) {
		t.Parallel()
		// Owner: impact/handler.go traceResourceToCode; hops from
		// deployment.ImpactTraceHops ({type} plus confidence/reason).
		doc := fragment(t, Rest, "Rest")
		props := propsOf(t, schemaAt(t, doc, "/api/v0/impact/trace-resource-to-code", "post"), "trace-code")
		requireDescribed(t, props, "trace-code",
			"start", "paths", "count", "limit", "truncated", "environment")
		paths := child(t, props, "paths", "trace-code")
		pathProps := propsOf(t, itemsOf(t, paths, "trace-code paths"), "trace-code paths[]")
		requireExactKeys(t, pathProps, "trace-code paths[]",
			"repo_id", "repo_name", "depth", "hops")
		hops := child(t, pathProps, "hops", "trace-code paths")
		hopProps := propsOf(t, itemsOf(t, hops, "trace-code hops"), "trace-code paths[].hops[]")
		requireKeys(t, hopProps, "trace-code paths[].hops[]", "type", "confidence", "reason")
	})

	t.Run("explain dependency path", func(t *testing.T) {
		t.Parallel()
		// Owner: impact/handler.go explainDependencyPath; hops from
		// deployment.ImpactDependencyHops.
		doc := fragment(t, Rest, "Rest")
		props := propsOf(t, schemaAt(t, doc, "/api/v0/impact/explain-dependency-path", "post"), "explain")
		requireDescribed(t, props, "explain",
			"source", "target", "path", "confidence", "reason", "environment")
		pathProps := propsOf(t, child(t, props, "path", "explain"), "explain.path")
		requireExactKeys(t, pathProps, "explain.path", "depth", "hops")
		hops := child(t, pathProps, "hops", "explain.path")
		hopProps := propsOf(t, itemsOf(t, hops, "explain hops"), "explain path.hops[]")
		requireKeys(t, hopProps, "explain path.hops[]",
			"type", "from_id", "from_name", "to_id", "to_name", "confidence", "reason")
	})

	t.Run("exposure path", func(t *testing.T) {
		t.Parallel()
		// Owner: impact/exposure_path_mapping.go exposureFindingPayload.
		doc := fragment(t, Exposure, "Exposure")
		props := propsOf(t, schemaAt(t, doc, "/api/v0/impact/trace-exposure-path", "post"), "exposure")
		requireDescribed(t, props, "exposure",
			"source", "source_kind", "exposure_rank", "truth_label", "state", "paths", "coverage")
		sourceProps := propsOf(t, child(t, props, "source", "exposure"), "exposure.source")
		requireExactKeys(t, sourceProps, "exposure.source", "entity_id", "name", "labels")
		paths := child(t, props, "paths", "exposure")
		pathProps := propsOf(t, itemsOf(t, paths, "exposure paths"), "exposure paths[]")
		requireExactKeys(t, pathProps, "exposure paths[]",
			"nodes", "sink", "depth", "state", "severity", "reason")
		sinkProps := propsOf(t, child(t, pathProps, "sink", "exposure path"), "exposure paths[].sink")
		requireExactKeys(t, sinkProps, "exposure paths[].sink", "kind", "display_name", "node")
		coverageProps := propsOf(t, child(t, props, "coverage", "exposure"), "exposure.coverage")
		requireExactKeys(t, coverageProps, "exposure.coverage",
			"max_depth", "paths_found", "truncated", "unresolved_reason")
	})

	t.Run("deployment config influence", func(t *testing.T) {
		t.Parallel()
		// Owner: impact/deployment_config_influence.go
		// buildDeploymentConfigInfluenceResponse and its row builders.
		props := propsOf(t, schemaAt(t, fragment(t, deploymentConfigInfluence, "deploymentConfigInfluence"), "/api/v0/impact/deployment-config-influence", "post"), "config-influence")
		requireDescribed(t, props, "config-influence",
			"service_name", "workload_id", "environment", "subject", "story",
			"influencing_repositories", "values_layers", "image_tag_sources",
			"runtime_setting_sources", "resource_limit_sources", "rendered_targets",
			"read_first_files", "recommended_next_calls", "limitations",
			"deployment_source_limits", "k8s_resource_limits", "coverage")
		subjectProps := propsOf(t, child(t, props, "subject", "config-influence"), "config-influence.subject")
		requireExactKeys(t, subjectProps, "config-influence.subject", "type", "id", "name", "environment")
		repos := child(t, props, "influencing_repositories", "config-influence")
		repoProps := propsOf(t, itemsOf(t, repos, "config-influence repos"), "config-influence influencing_repositories[]")
		requireExactKeys(t, repoProps, "config-influence influencing_repositories[]",
			"repo_id", "repo_name", "roles")
		coverageProps := propsOf(t, child(t, props, "coverage", "config-influence"), "config-influence.coverage")
		requireExactKeys(t, coverageProps, "config-influence.coverage",
			"query_shape", "limit", "truncated", "observed_count_is_lower_bound",
			"artifact_candidate_count", "deployment_source_count", "rendered_target_count",
			"environment", "portable_file_handles", "uses_file_content_payloads")
	})

	t.Run("trace deployment chain core", func(t *testing.T) {
		t.Parallel()
		// Owner: deployment.BuildDeploymentTraceResponse and
		// buildDeploymentTraceFields: subject is fixed {type, id, name};
		// delivery_paths and deployment_facts carry the builder enum rows.
		doc := fragment(t, Routes, "Routes")
		props := propsOf(t, schemaAt(t, doc, "/api/v0/impact/trace-deployment-chain", "post"), "trace-chain")
		subjectProps := propsOf(t, child(t, props, "subject", "trace-chain"), "trace-chain.subject")
		requireExactKeys(t, subjectProps, "trace-chain.subject", "type", "id", "name")
		delivery := child(t, props, "delivery_paths", "trace-chain")
		deliveryProps := propsOf(t, itemsOf(t, delivery, "trace-chain delivery"), "trace-chain delivery_paths[]")
		requireKeys(t, deliveryProps, "trace-chain delivery_paths[]",
			"type", "target", "target_id", "confidence", "kind", "source_name", "reason")
		facts := child(t, props, "deployment_facts", "trace-chain")
		factProps := propsOf(t, itemsOf(t, facts, "trace-chain facts"), "trace-chain deployment_facts[]")
		requireKeys(t, factProps, "trace-chain deployment_facts[]",
			"type", "target", "target_id", "confidence", "reason",
			"source_id", "source_name", "evidence_source", "source_tool", "kind")
		sections := child(t, props, "story_sections", "trace-chain")
		sectionProps := propsOf(t, itemsOf(t, sections, "trace-chain sections"), "trace-chain story_sections[]")
		requireExactKeys(t, sectionProps, "trace-chain story_sections[]", "title", "summary")
		drillProps := propsOf(t, child(t, props, "drilldowns", "trace-chain"), "trace-chain.drilldowns")
		requireExactKeys(t, drillProps, "trace-chain.drilldowns",
			"service_context_path", "service_story_path", "workload_context_path")
	})

	t.Run("no bare objects in impact fragments", func(t *testing.T) {
		t.Parallel()
		fragments := map[string]string{
			"Routes":                    Routes,
			"Contract":                  Contract,
			"Rest":                      Rest,
			"Exposure":                  Exposure,
			"deploymentConfigInfluence": deploymentConfigInfluence,
			"K8sResourceLimits":         K8sResourceLimits,
			"ImageRegistryTruthLimits":  ImageRegistryTruthLimits,
		}
		for name, raw := range fragments {
			requireNoBareObjects(t, fragment(t, raw, name), name)
		}
	})
}
