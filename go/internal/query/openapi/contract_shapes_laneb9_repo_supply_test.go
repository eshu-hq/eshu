// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"testing"
)

// TestContractShapesLaneB9RepoSupply pins the lane-B9 shape-completeness
// fixes for the package dependency and manifest inventories, the
// package-registry reads and aggregates, and the runtime declaration query.
// fixes for the repository family: the repository list, by-language,
// language-inventory, catalog, context, story, tree, and content reads, the
// branches, freshness, stats, and coverage reads, the forward/reverse
// package dependency inventory, the manifest dependency inventory, the five
// package-registry reads plus the two registry aggregates, and the runtime
// declaration query. Bare {"type": "object"} shapes gain properties (or a
// described open-map shape), and every 200-response property carries a
// description naming its Go owner.
func TestContractShapesLaneB9RepoSupply(t *testing.T) {
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

	t.Run("package dependencies", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/dependencies"
		top := propsOf(t, schema200(t, "/api/v0/dependencies", "get"), where)
		requireExactKeys(t, top, where,
			"dependencies", "direction", "count", "limit", "truncated", "next_cursor")
		requireDescribed(t, top, where,
			"dependencies", "direction", "count", "limit", "truncated", "next_cursor")
		items := itemsOf(t, top["dependencies"].(map[string]any), where+" dependencies")
		dprops := propsOf(t, items, where+" dependencies[]")
		// Owner: dependency.Row.
		requireExactKeys(t, dprops, where+" dependencies[]",
			"direction", "anchor_package_id", "anchor_package", "anchor_ecosystem",
			"declaring_version", "related_package_id", "related_package",
			"related_ecosystem", "dependency_range", "dependency_type",
			"optional", "edge_id")
		requireDescribed(t, dprops, where+" dependencies[]",
			"direction", "anchor_package_id", "anchor_package", "anchor_ecosystem",
			"declaring_version", "related_package_id", "related_package",
			"related_ecosystem", "dependency_range", "dependency_type",
			"optional", "edge_id")
		cursor := propsOf(t, top["next_cursor"].(map[string]any), where+" next_cursor")
		requireExactKeys(t, cursor, where+" next_cursor", "after_name", "after_edge")
		requireDescribed(t, cursor, where+" next_cursor", "after_name", "after_edge")
	})

	t.Run("repository manifest dependencies", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/repository/manifest-dependencies"
		top := propsOf(t, schema200(t, "/api/v0/repository/manifest-dependencies", "get"), where)
		requireExactKeys(t, top, where,
			"repository_id", "dependencies", "count", "limit", "truncated",
			"include_dev_dependencies", "next_cursor", "freshness")
		requireDescribed(t, top, where,
			"repository_id", "dependencies", "count", "limit", "truncated",
			"include_dev_dependencies", "next_cursor", "freshness")
		items := itemsOf(t, top["dependencies"].(map[string]any), where+" dependencies")
		dprops := propsOf(t, items, where+" dependencies[]")
		// Owner: manifest.ManifestDependencyRow.
		requireExactKeys(t, dprops, where+" dependencies[]",
			"package_name", "declared_range", "manifest_file", "ecosystem",
			"package_manager", "section", "scope", "fact_id", "generation_id",
			"observed_at")
		requireDescribed(t, dprops, where+" dependencies[]",
			"package_name", "declared_range", "manifest_file", "ecosystem",
			"package_manager", "section", "scope", "fact_id", "generation_id",
			"observed_at")
		cursor := propsOf(t, top["next_cursor"].(map[string]any), where+" next_cursor")
		requireExactKeys(t, cursor, where+" next_cursor", "after_observed_at", "after_fact_id")
		requireDescribed(t, cursor, where+" next_cursor", "after_observed_at", "after_fact_id")
		fresh := propsOf(t, top["freshness"].(map[string]any), where+" freshness")
		// Owner: manifest.ManifestDependencyFreshness.
		requireExactKeys(t, fresh, where+" freshness",
			"scope_found", "active_generation_id", "generation_status",
			"observed_at", "freshness_state")
		requireDescribed(t, fresh, where+" freshness",
			"scope_found", "active_generation_id", "generation_status",
			"observed_at", "freshness_state")
	})

	t.Run("package registry packages", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/package-registry/packages"
		top := propsOf(t, schema200(t, "/api/v0/package-registry/packages", "get"), where)
		requireKeys(t, top, where, "packages", "identity_issues", "count", "limit", "truncated")
		requireDescribed(t, top, where, "packages", "identity_issues", "count", "limit", "truncated")
		items := itemsOf(t, top["packages"].(map[string]any), where+" packages")
		pprops := propsOf(t, items, where+" packages[]")
		// Owner: registry package result rows; identity_issues rows share the
		// identity fields plus reason/missing_evidence.
		pkgKeys := []string{
			"package_id", "ecosystem", "registry", "namespace",
			"normalized_name", "purl", "bom_ref", "package_manager",
			"source_path", "source_specific_id", "visibility",
			"source_confidence", "version_count",
		}
		requireExactKeys(t, pprops, where+" packages[]", pkgKeys...)
		requireDescribed(t, pprops, where+" packages[]", pkgKeys...)
		iitems := itemsOf(t, top["identity_issues"].(map[string]any), where+" identity_issues")
		iprops := propsOf(t, iitems, where+" identity_issues[]")
		requireExactKeys(t, iprops, where+" identity_issues[]",
			append([]string{"reason", "missing_evidence"}, pkgKeys[1:]...)...)
		requireDescribed(t, iprops, where+" identity_issues[]",
			append([]string{"reason", "missing_evidence"}, pkgKeys[1:]...)...)
	})

	t.Run("package registry versions", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/package-registry/versions"
		top := propsOf(t, schema200(t, "/api/v0/package-registry/versions", "get"), where)
		requireKeys(t, top, where, "versions", "count", "limit", "truncated")
		requireDescribed(t, top, where, "versions", "count", "limit", "truncated")
		items := itemsOf(t, top["versions"].(map[string]any), where+" versions")
		vprops := propsOf(t, items, where+" versions[]")
		// Owner: registry version result rows.
		requireExactKeys(t, vprops, where+" versions[]",
			"version_id", "package_id", "version", "purl", "bom_ref",
			"package_manager", "published_at", "is_yanked", "is_unlisted",
			"is_deprecated", "is_retracted")
		requireDescribed(t, vprops, where+" versions[]",
			"version_id", "package_id", "version", "purl", "bom_ref",
			"package_manager", "published_at", "is_yanked", "is_unlisted",
			"is_deprecated", "is_retracted")
	})

	t.Run("package registry dependencies", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/package-registry/dependencies"
		top := propsOf(t, schema200(t, "/api/v0/package-registry/dependencies", "get"), where)
		requireKeys(t, top, where, "dependencies", "count", "limit", "truncated", "next_cursor")
		requireDescribed(t, top, where, "dependencies", "count", "limit", "truncated", "next_cursor")
		items := itemsOf(t, top["dependencies"].(map[string]any), where+" dependencies")
		dprops := propsOf(t, items, where+" dependencies[]")
		// Owner: registry dependency result rows.
		depKeys := []string{
			"dependency_id", "source_package_id", "source_version_id",
			"version", "dependency_package_id", "dependency_ecosystem",
			"dependency_registry", "dependency_namespace", "dependency_normalized",
			"dependency_purl", "dependency_bom_ref", "dependency_manager",
			"dependency_range", "dependency_type", "target_framework", "marker",
			"optional", "excluded", "source_confidence", "collector_kind",
			"collector_instance_id", "correlation_anchors",
		}
		requireExactKeys(t, dprops, where+" dependencies[]", depKeys...)
		requireDescribed(t, dprops, where+" dependencies[]", depKeys...)
		cursor := propsOf(t, top["next_cursor"].(map[string]any), where+" next_cursor")
		requireExactKeys(t, cursor, where+" next_cursor", "after_version_id", "after_dependency_id")
		requireDescribed(t, cursor, where+" next_cursor", "after_version_id", "after_dependency_id")
	})

	t.Run("package registry correlations", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/package-registry/correlations"
		top := propsOf(t, schema200(t, "/api/v0/package-registry/correlations", "get"), where)
		requireKeys(t, top, where, "correlations", "count", "limit", "truncated", "next_cursor")
		requireDescribed(t, top, where, "correlations", "count", "limit", "truncated", "next_cursor")
		items := itemsOf(t, top["correlations"].(map[string]any), where+" correlations")
		cprops := propsOf(t, items, where+" correlations[]")
		// Owner: registry.CorrelationResult.
		corrKeys := []string{
			"correlation_id", "relationship_kind", "package_id",
			"version_id", "version", "published_at", "ecosystem", "package_name",
			"repository_id", "repository_name", "source_url",
			"candidate_repository_ids", "relative_path", "manifest_section",
			"dependency_range", "outcome", "reason", "provenance_only",
			"canonical_writes", "evidence_fact_ids",
		}
		requireExactKeys(t, cprops, where+" correlations[]", corrKeys...)
		requireDescribed(t, cprops, where+" correlations[]", corrKeys...)
		cursor := propsOf(t, top["next_cursor"].(map[string]any), where+" next_cursor")
		requireExactKeys(t, cursor, where+" next_cursor", "after_correlation_id")
		requireDescribed(t, cursor, where+" next_cursor", "after_correlation_id")
	})

	t.Run("package registry dependency chains", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/package-registry/dependency-chains"
		top := propsOf(t, schema200(t, "/api/v0/package-registry/dependency-chains", "get"), where)
		requireKeys(t, top, where, "chains", "repository_id", "count", "limit", "truncated", "next_cursor")
		requireDescribed(t, top, where, "chains", "repository_id", "count", "limit", "truncated", "next_cursor")
		items := itemsOf(t, top["chains"].(map[string]any), where+" chains")
		cprops := propsOf(t, items, where+" chains[]")
		// Owner: registry.PackageDependencyChainResult.
		requireExactKeys(t, cprops, where+" chains[]",
			"consumer_repository_id", "consumer_repository_name", "package_id",
			"package_name", "ecosystem", "dependency_range",
			"consumption_correlation_id", "consumption_provenance_only",
			"consumption_canonical_writes", "ambiguous", "publishers")
		requireDescribed(t, cprops, where+" chains[]",
			"consumer_repository_id", "consumer_repository_name", "package_id",
			"package_name", "ecosystem", "dependency_range",
			"consumption_correlation_id", "consumption_provenance_only",
			"consumption_canonical_writes", "ambiguous", "publishers")
		pitems := itemsOf(t, cprops["publishers"].(map[string]any), where+" chains[].publishers")
		pprops := propsOf(t, pitems, where+" chains[].publishers[]")
		// Owner: registry chain publisher results.
		requireExactKeys(t, pprops, where+" chains[].publishers[]",
			"correlation_id", "relationship_kind", "repository_id",
			"repository_name", "source_url", "outcome", "reason",
			"provenance_only", "canonical_writes")
		requireDescribed(t, pprops, where+" chains[].publishers[]",
			"correlation_id", "relationship_kind", "repository_id",
			"repository_name", "source_url", "outcome", "reason",
			"provenance_only", "canonical_writes")
		cursor := propsOf(t, top["next_cursor"].(map[string]any), where+" next_cursor")
		requireExactKeys(t, cursor, where+" next_cursor", "after_correlation_id")
		requireDescribed(t, cursor, where+" next_cursor", "after_correlation_id")
	})

	t.Run("package registry aggregates", func(t *testing.T) {
		t.Parallel()
		count := propsOf(t, schema200(t, "/api/v0/package-registry/packages/count", "get"), "GET /api/v0/package-registry/packages/count")
		requireKeys(t, count, "GET /api/v0/package-registry/packages/count",
			"total_packages", "by_ecosystem", "scope")
		requireDescribed(t, count, "GET /api/v0/package-registry/packages/count",
			"total_packages", "by_ecosystem", "scope")
		inv := propsOf(t, schema200(t, "/api/v0/package-registry/packages/inventory", "get"), "GET /api/v0/package-registry/packages/inventory")
		requireKeys(t, inv, "GET /api/v0/package-registry/packages/inventory",
			"buckets", "count", "limit", "offset", "group_by", "truncated",
			"next_offset", "scope")
		requireDescribed(t, inv, "GET /api/v0/package-registry/packages/inventory",
			"buckets", "count", "limit", "offset", "group_by", "truncated",
			"next_offset", "scope")
		bitems := itemsOf(t, inv["buckets"].(map[string]any), "GET /api/v0/package-registry/packages/inventory buckets")
		bprops := propsOf(t, bitems, "GET /api/v0/package-registry/packages/inventory buckets[]")
		// Owner: registry.InventoryRow.
		requireExactKeys(t, bprops, "GET /api/v0/package-registry/packages/inventory buckets[]",
			"dimension", "value", "count")
		requireDescribed(t, bprops, "GET /api/v0/package-registry/packages/inventory buckets[]",
			"dimension", "value", "count")
	})

	t.Run("runtime declarations query", func(t *testing.T) {
		t.Parallel()
		where := "POST /api/v0/runtime-declarations/query"
		top := propsOf(t, schema200(t, "/api/v0/runtime-declarations/query", "post"), where)
		requireKeys(t, top, where,
			"declarations", "count", "limit", "truncated", "next_cursor",
			"filters", "excluded_from_comparison", "partial_reasons")
		requireDescribed(t, top, where,
			"declarations", "count", "limit", "truncated", "next_cursor",
			"filters", "excluded_from_comparison", "partial_reasons")
		items := itemsOf(t, top["declarations"].(map[string]any), where+" declarations")
		dprops := propsOf(t, items, where+" declarations[]")
		// Owner: declaration row structs.
		declKeys := []string{
			"repo_id", "repo_name", "relative_path", "scope_dir",
			"line_number", "order_index", "runtime", "source_kind", "tool_name",
			"raw_spec", "spec_kind", "version_major", "version_minor",
			"version_patch", "distribution", "range_lower", "range_upper",
			"range_lower_major", "range_upper_major", "range_lower_inclusive",
			"range_upper_inclusive",
		}
		requireExactKeys(t, dprops, where+" declarations[]", declKeys...)
		requireDescribed(t, dprops, where+" declarations[]", declKeys...)
		excl := propsOf(t, top["excluded_from_comparison"].(map[string]any), where+" excluded_from_comparison")
		// Owner: declaration.excludedSummary.
		requireExactKeys(t, excl, where+" excluded_from_comparison", "total", "by_spec_kind")
		requireDescribed(t, excl, where+" excluded_from_comparison", "total", "by_spec_kind")
	})
}
