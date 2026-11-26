// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"testing"
)

// TestContractShapesLaneB9RepoNarrative pins the lane-B9
// shape-completeness fixes for the repository context, story, and branches
// reads.
// fixes for the repository family: the repository list, by-language,
// language-inventory, catalog, context, story, tree, and content reads, the
// branches, freshness, stats, and coverage reads, the forward/reverse
// package dependency inventory, the manifest dependency inventory, the five
// package-registry reads plus the two registry aggregates, and the runtime
// declaration query. Bare {"type": "object"} shapes gain properties (or a
// described open-map shape), and every 200-response property carries a
// description naming its Go owner.
func TestContractShapesLaneB9RepoNarrative(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}

	child := func(t *testing.T, node map[string]any, key, where string) map[string]any {
		t.Helper()
		next, ok := node[key].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing or non-object property %q", where, key)
		}
		return next
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
			node, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if node["type"] == "object" {
				_, hasProps := node["properties"]
				_, hasAdditional := node["additionalProperties"]
				if !hasProps && !hasAdditional {
					t.Errorf("%s: property %q is a bare object with no properties", where, key)
				}
			}
			if node["type"] == "array" {
				items, ok := node["items"].(map[string]any)
				if !ok {
					t.Errorf("%s: property %q is an array with no items", where, key)
					continue
				}
				if items["type"] == "object" {
					_, hasProps := items["properties"]
					_, hasAdditional := items["additionalProperties"]
					if !hasProps && !hasAdditional {
						t.Errorf("%s: property %q has bare object items", where, key)
					}
				}
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

	schema200 := func(t *testing.T, path, method string) map[string]any {
		t.Helper()
		paths, ok := spec["paths"].(map[string]any)
		if !ok {
			t.Fatal("spec has no paths object")
		}
		route, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatalf("spec paths missing %s", path)
		}
		op := child(t, route, method, path)
		responses := child(t, op, "responses", path+" "+method)
		okResp := child(t, responses, "200", path+" "+method)
		content := child(t, okResp, "content", path+" "+method+" 200")
		body := child(t, content, "application/json", path+" "+method+" 200")
		return child(t, body, "schema", path+" "+method+" 200")
	}

	// Relationship row keys shared by the context relationships/consumers
	// lists and the relationship_overview partitions: fixed identity keys
	// plus the conditional evidence-type and edge-evidence metadata the
	// repository read copies from the graph edge.
	relRowKeys := []string{
		"type", "target_name", "target_id", "direction",
		"source_name", "source_id", "evidence_type", "resolved_id",
		"generation_id", "confidence", "evidence_count", "evidence_kinds",
		"resolution_source", "confidence_basis", "rationale",
	}

	t.Run("repository context", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/repositories/{repo_id}/context"
		top := propsOf(t, schema200(t, "/api/v0/repositories/{repo_id}/context", "get"), where)
		requireExactKeys(t, top, where,
			"repository", "file_count", "workload_count", "platform_count",
			"dependency_count", "relationships", "relationship_overview",
			"api_surface", "deployment_evidence", "consumers",
			"language_breakdown", "source_tool_breakdown", "partial_reasons")
		requireDescribed(t, top, where,
			"repository", "file_count", "workload_count", "platform_count",
			"dependency_count", "relationships", "relationship_overview",
			"api_surface", "deployment_evidence", "consumers",
			"language_breakdown", "source_tool_breakdown", "partial_reasons")
		for _, key := range []string{"relationships", "consumers"} {
			items := itemsOf(t, top[key].(map[string]any), where+" "+key)
			rprops := propsOf(t, items, where+" "+key+"[]")
			// Owner: repository.QueryRepoDependencies / queryRepoConsumers
			// rows plus copyRelationshipEvidenceMetadata.
			requireExactKeys(t, rprops, where+" "+key+"[]", relRowKeys...)
			requireDescribed(t, rprops, where+" "+key+"[]", relRowKeys...)
		}
		ov := propsOf(t, top["relationship_overview"].(map[string]any), where+" relationship_overview")
		// Owner: repository.BuildRepositoryRelationshipOverview.
		requireExactKeys(t, ov, where+" relationship_overview",
			"relationship_count", "relationships", "relationship_types",
			"evidence_types", "controller_driven", "workflow_driven",
			"iac_driven", "other_relationships", "story")
		requireDescribed(t, ov, where+" relationship_overview",
			"relationship_count", "relationships", "relationship_types",
			"evidence_types", "controller_driven", "workflow_driven",
			"iac_driven", "other_relationships", "story")
		ovItems := itemsOf(t, ov["relationships"].(map[string]any), where+" relationship_overview.relationships")
		ovprops := propsOf(t, ovItems, where+" relationship_overview.relationships[]")
		requireExactKeys(t, ovprops, where+" relationship_overview.relationships[]", relRowKeys...)
		requireDescribed(t, ovprops, where+" relationship_overview.relationships[]", relRowKeys...)
		for _, key := range []string{"controller_driven", "workflow_driven", "iac_driven", "other_relationships"} {
			partItems := itemsOf(t, ov[key].(map[string]any), where+" relationship_overview."+key)
			if _, hasProps := partItems["properties"]; !hasProps {
				if _, hasAdditional := partItems["additionalProperties"]; !hasAdditional {
					t.Errorf("%s: relationship_overview.%s items must carry properties or additionalProperties", where, key)
				}
			}
		}
		api := propsOf(t, top["api_surface"].(map[string]any), where+" api_surface")
		// Owner: repository.buildGraphAPISurface.
		requireExactKeys(t, api, where+" api_surface",
			"truth_basis", "endpoint_count", "method_count", "operation_id_count",
			"endpoints", "source_paths", "spec_paths", "spec_versions",
			"api_versions", "detail_limit", "detail_truncated", "frameworks",
			"framework_route_count")
		requireDescribed(t, api, where+" api_surface",
			"truth_basis", "endpoint_count", "method_count", "operation_id_count",
			"endpoints", "source_paths", "spec_paths", "spec_versions",
			"api_versions", "detail_limit", "detail_truncated", "frameworks",
			"framework_route_count")
		epItems := itemsOf(t, api["endpoints"].(map[string]any), where+" api_surface.endpoints")
		epprops := propsOf(t, epItems, where+" api_surface.endpoints[]")
		requireExactKeys(t, epprops, where+" api_surface.endpoints[]",
			"id", "path", "methods", "operation_ids", "source",
			"source_kinds", "source_paths", "evidence_source",
			"workload_id", "workload_name")
		requireDescribed(t, epprops, where+" api_surface.endpoints[]",
			"id", "path", "methods", "operation_ids", "source",
			"source_kinds", "source_paths", "evidence_source",
			"workload_id", "workload_name")
		// deployment_evidence is a bounded graph-evidence map: the contract
		// pins the described open-map shape plus key semantics.
		de := top["deployment_evidence"].(map[string]any)
		if _, ok := de["additionalProperties"]; !ok {
			if _, hasProps := de["properties"]; !hasProps {
				t.Errorf("%s: deployment_evidence must carry properties or additionalProperties", where)
			}
		}
		requireDescribed(t, top, where, "deployment_evidence")
	})

	t.Run("repository story", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/repositories/{repo_id}/story"
		top := propsOf(t, schema200(t, "/api/v0/repositories/{repo_id}/story", "get"), where)
		requireExactKeys(t, top, where,
			"repository", "subject", "story", "story_sections",
			"semantic_overview", "ci_cd_evidence", "deployment_overview",
			"gitops_overview", "documentation_overview", "support_overview",
			"coverage_summary", "limitations", "truncated", "drilldowns",
			"answer_metadata", "evidence_boundaries",
			"relationship_overview", "infrastructure_overview")
		requireDescribed(t, top, where,
			"repository", "subject", "story", "story_sections",
			"semantic_overview", "ci_cd_evidence", "deployment_overview",
			"gitops_overview", "documentation_overview", "support_overview",
			"coverage_summary", "limitations", "truncated", "drilldowns",
			"answer_metadata", "evidence_boundaries",
			"relationship_overview", "infrastructure_overview")
		// Owner: repository.BuildRepositoryRelationshipOverview (conditional).
		sov := propsOf(t, top["relationship_overview"].(map[string]any), where+" relationship_overview")
		requireExactKeys(t, sov, where+" relationship_overview",
			"relationship_count", "relationships", "relationship_types",
			"evidence_types", "controller_driven", "workflow_driven",
			"iac_driven", "other_relationships", "story")
		requireDescribed(t, sov, where+" relationship_overview",
			"relationship_count", "relationships", "relationship_types",
			"evidence_types", "controller_driven", "workflow_driven",
			"iac_driven", "other_relationships", "story")
		// Owner: repository.BuildRepositoryInfrastructureOverview (conditional).
		iov := propsOf(t, top["infrastructure_overview"].(map[string]any), where+" infrastructure_overview")
		requireExactKeys(t, iov, where+" infrastructure_overview",
			"families", "entity_family_counts", "entity_type_counts",
			"artifact_family_counts")
		requireDescribed(t, iov, where+" infrastructure_overview",
			"families", "entity_family_counts", "entity_type_counts",
			"artifact_family_counts")
		subj := propsOf(t, top["subject"].(map[string]any), where+" subject")
		// Owner: repository.BuildRepositoryStoryResponseWithCoverage subject map.
		requireExactKeys(t, subj, where+" subject", "type", "id", "name")
		requireDescribed(t, subj, where+" subject", "type", "id", "name")
		secItems := itemsOf(t, top["story_sections"].(map[string]any), where+" story_sections")
		secprops := propsOf(t, secItems, where+" story_sections[]")
		requireExactKeys(t, secprops, where+" story_sections[]", "title", "summary")
		requireDescribed(t, secprops, where+" story_sections[]", "title", "summary")
		gitops := propsOf(t, top["gitops_overview"].(map[string]any), where+" gitops_overview")
		// Owner: repository story gitops_overview map.
		requireExactKeys(t, gitops, where+" gitops_overview",
			"enabled", "tool_families", "observed_targets")
		requireDescribed(t, gitops, where+" gitops_overview",
			"enabled", "tool_families", "observed_targets")
		doc := propsOf(t, top["documentation_overview"].(map[string]any), where+" documentation_overview")
		requireExactKeys(t, doc, where+" documentation_overview",
			"repo_slug", "remote_url", "has_remote", "local_path_present",
			"portable_identifier")
		requireDescribed(t, doc, where+" documentation_overview",
			"repo_slug", "remote_url", "has_remote", "local_path_present",
			"portable_identifier")
		supp := propsOf(t, top["support_overview"].(map[string]any), where+" support_overview")
		requireExactKeys(t, supp, where+" support_overview",
			"dependency_count", "language_count", "languages")
		requireDescribed(t, supp, where+" support_overview",
			"dependency_count", "language_count", "languages")
		drill := propsOf(t, top["drilldowns"].(map[string]any), where+" drilldowns")
		requireExactKeys(t, drill, where+" drilldowns",
			"context_path", "stats_path", "coverage_path")
		requireDescribed(t, drill, where+" drilldowns",
			"context_path", "stats_path", "coverage_path")
		// semantic_overview, deployment_overview, and coverage_summary are
		// bounded graph-derived maps: the contract pins described shapes.
		for _, key := range []string{"semantic_overview", "deployment_overview", "coverage_summary"} {
			requireKeys(t, top, where, key)
		}
		deploy := propsOf(t, top["deployment_overview"].(map[string]any), where+" deployment_overview")
		// Owner: repository.buildRepositoryDeploymentOverview.
		requireExactKeys(t, deploy, where+" deployment_overview",
			"workload_count", "platform_count", "workloads", "platforms",
			"infrastructure_families", "deployment_artifacts", "shared_config_paths",
			"delivery_paths", "delivery_workflows", "delivery_family_paths",
			"delivery_family_story", "topology_story", "direct_story",
			"trace_limitations")
		requireDescribed(t, deploy, where+" deployment_overview",
			"workload_count", "platform_count", "workloads", "platforms",
			"infrastructure_families", "deployment_artifacts", "shared_config_paths",
			"delivery_paths", "delivery_workflows", "delivery_family_paths",
			"delivery_family_story", "topology_story", "direct_story",
			"trace_limitations")
		for _, key := range []string{"delivery_paths", "delivery_workflows", "delivery_family_paths"} {
			arrItems := itemsOf(t, deploy[key].(map[string]any), where+" deployment_overview."+key)
			if _, ok := arrItems["properties"]; !ok {
				t.Errorf("%s: deployment_overview.%s items must carry properties", where, key)
			}
		}
		meta := propsOf(t, top["answer_metadata"].(map[string]any), where+" answer_metadata")
		// Owner: querycontract.AnswerMetadata via AttachAnswerMetadata.
		requireExactKeys(t, meta, where+" answer_metadata",
			"schema_version", "evidence_handles", "missing_evidence",
			"limitations", "truncated", "coverage", "partial_reasons",
			"recommended_next_calls")
		requireDescribed(t, meta, where+" answer_metadata",
			"schema_version", "evidence_handles", "missing_evidence",
			"limitations", "truncated", "coverage", "partial_reasons",
			"recommended_next_calls")
	})

	t.Run("repository branches", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/repositories/{repo_id}/branches"
		top := propsOf(t, schema200(t, "/api/v0/repositories/{repo_id}/branches", "get"), where)
		requireExactKeys(t, top, where,
			"default_branch", "branches", "tags", "truncated", "next_cursor",
			"tags_truncated")
		requireDescribed(t, top, where,
			"default_branch", "branches", "tags", "truncated", "next_cursor",
			"tags_truncated")
		bitems := itemsOf(t, top["branches"].(map[string]any), where+" branches")
		bprops := propsOf(t, bitems, where+" branches[]")
		// Owner: readmodel.RefWindowEntries branch rows.
		requireExactKeys(t, bprops, where+" branches[]",
			"name", "kind", "head_sha", "is_default", "observed_at",
			"last_indexed_at")
		requireDescribed(t, bprops, where+" branches[]",
			"name", "kind", "head_sha", "is_default", "observed_at",
			"last_indexed_at")
		titems := itemsOf(t, top["tags"].(map[string]any), where+" tags")
		tprops := propsOf(t, titems, where+" tags[]")
		// Owner: readmodel.RefWindowEntries tag rows.
		requireExactKeys(t, tprops, where+" tags[]",
			"name", "kind", "head_sha", "observed_at", "last_indexed_at")
		requireDescribed(t, tprops, where+" tags[]",
			"name", "kind", "head_sha", "observed_at", "last_indexed_at")
	})
}
