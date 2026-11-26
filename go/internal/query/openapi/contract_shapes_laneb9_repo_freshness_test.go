// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"testing"
)

// TestContractShapesLaneB9RepoFreshness pins the lane-B9
// shape-completeness fixes for the repository freshness, stats, and
// coverage reads.
// fixes for the repository family: the repository list, by-language,
// language-inventory, catalog, context, story, tree, and content reads, the
// branches, freshness, stats, and coverage reads, the forward/reverse
// package dependency inventory, the manifest dependency inventory, the five
// package-registry reads plus the two registry aggregates, and the runtime
// declaration query. Bare {"type": "object"} shapes gain properties (or a
// described open-map shape), and every 200-response property carries a
// description naming its Go owner.
func TestContractShapesLaneB9RepoFreshness(t *testing.T) {
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

	t.Run("repository freshness", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/repositories/{repo_id}/freshness"
		top := propsOf(t, schema200(t, "/api/v0/repositories/{repo_id}/freshness", "get"), where)
		requireExactKeys(t, top, where,
			"repository", "scope_id", "verdict", "observed_commit", "observed_at",
			"generation", "stages", "outstanding_by_stage", "shared_enrichment",
			"unobserved_push", "retirement", "selection", "as_of", "scoped")
		requireDescribed(t, top, where,
			"repository", "scope_id", "verdict", "observed_commit", "observed_at",
			"generation", "stages", "outstanding_by_stage", "shared_enrichment",
			"unobserved_push", "retirement", "selection", "as_of", "scoped")
		gen := propsOf(t, top["generation"].(map[string]any), where+" generation")
		// Owner: repository.repositoryFreshnessGenerationToMap.
		requireExactKeys(t, gen, where+" generation",
			"id", "status", "trigger_kind", "is_delta", "activated_at")
		requireDescribed(t, gen, where+" generation",
			"id", "status", "trigger_kind", "is_delta", "activated_at")
		stages := propsOf(t, top["stages"].(map[string]any), where+" stages")
		// Owner: repository.repositoryFreshnessStagesToMap.
		requireExactKeys(t, stages, where+" stages",
			"collected", "reduced", "projected", "materialized")
		requireDescribed(t, stages, where+" stages",
			"collected", "reduced", "projected", "materialized")
		oitems := itemsOf(t, top["outstanding_by_stage"].(map[string]any), where+" outstanding_by_stage")
		oprops := propsOf(t, oitems, where+" outstanding_by_stage[]")
		// Owner: repository.repositoryFreshnessOutstandingToSlice rows.
		requireExactKeys(t, oprops, where+" outstanding_by_stage[]",
			"stage", "status", "count")
		requireDescribed(t, oprops, where+" outstanding_by_stage[]",
			"stage", "status", "count")
		shared := propsOf(t, top["shared_enrichment"].(map[string]any), where+" shared_enrichment")
		// Owner: repository.repositoryFreshnessSharedEnrichmentToMap.
		requireExactKeys(t, shared, where+" shared_enrichment",
			"pending", "pending_domains")
		requireDescribed(t, shared, where+" shared_enrichment",
			"pending", "pending_domains")
		ditems := itemsOf(t, shared["pending_domains"].(map[string]any), where+" pending_domains")
		dprops := propsOf(t, ditems, where+" pending_domains[]")
		requireExactKeys(t, dprops, where+" pending_domains[]", "domain", "count")
		requireDescribed(t, dprops, where+" pending_domains[]", "domain", "count")
		push := propsOf(t, top["unobserved_push"].(map[string]any), where+" unobserved_push")
		// Owner: repository.repositoryFreshnessUnobservedPushToMap.
		requireExactKeys(t, push, where+" unobserved_push",
			"target_sha", "ref", "received_at")
		requireDescribed(t, push, where+" unobserved_push",
			"target_sha", "ref", "received_at")
	})

	t.Run("repository stats", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/repositories/{repo_id}/stats"
		top := propsOf(t, schema200(t, "/api/v0/repositories/{repo_id}/stats", "get"), where)
		requireExactKeys(t, top, where,
			"repository", "file_count", "languages", "entity_count",
			"entity_types", "coverage", "result_limits", "partial_reasons")
		requireDescribed(t, top, where,
			"repository", "file_count", "languages", "entity_count",
			"entity_types", "coverage", "result_limits", "partial_reasons")
		cov := propsOf(t, top["coverage"].(map[string]any), where+" coverage")
		// Owner: repository.repositoryStatsCoverageMap.
		requireExactKeys(t, cov, where+" coverage",
			"source_backend", "query_shape", "counts_available",
			"entity_types_available", "whole_graph_traversal", "partial_results",
			"truncated", "timeout", "timeout_budget", "missing_evidence",
			"file_count_source", "entity_count_source", "languages_source",
			"entity_types_source", "content_last_indexed_at", "last_error")
		requireDescribed(t, cov, where+" coverage",
			"source_backend", "query_shape", "counts_available",
			"entity_types_available", "whole_graph_traversal", "partial_results",
			"truncated", "timeout", "timeout_budget", "missing_evidence",
			"file_count_source", "entity_count_source", "languages_source",
			"entity_types_source", "content_last_indexed_at", "last_error")
	})

	t.Run("repository coverage", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/repositories/{repo_id}/coverage"
		top := propsOf(t, schema200(t, "/api/v0/repositories/{repo_id}/coverage", "get"), where)
		requireExactKeys(t, top, where,
			"repo_id", "completeness_state", "graph_available",
			"server_content_available", "graph_gap_count", "content_gap_count",
			"file_count", "entity_count", "content_last_indexed_at",
			"last_error", "languages", "summary")
		requireDescribed(t, top, where,
			"repo_id", "completeness_state", "graph_available",
			"server_content_available", "graph_gap_count", "content_gap_count",
			"file_count", "entity_count", "content_last_indexed_at",
			"last_error", "languages", "summary")
		litems := itemsOf(t, top["languages"].(map[string]any), where+" languages")
		lprops := propsOf(t, litems, where+" languages[]")
		// Owner: repository coverage language rows.
		requireExactKeys(t, lprops, where+" languages[]", "language", "file_count")
		requireDescribed(t, lprops, where+" languages[]", "language", "file_count")
		// summary is the bounded coverage summary map: the contract pins the
		// described shape.
		requireKeys(t, top, where, "summary")
	})
}
