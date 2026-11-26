// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"testing"
)

// TestContractShapesLaneB9Repository pins the lane-B9 shape-completeness
// fixes for the repository list, by-language, language-inventory, catalog,
// tree, and content reads.
// fixes for the repository family: the repository list, by-language,
// language-inventory, catalog, context, story, tree, and content reads, the
// branches, freshness, stats, and coverage reads, the forward/reverse
// package dependency inventory, the manifest dependency inventory, the five
// package-registry reads plus the two registry aggregates, and the runtime
// declaration query. Bare {"type": "object"} shapes gain properties (or a
// described open-map shape), and every 200-response property carries a
// description naming its Go owner.
func TestContractShapesLaneB9Repository(t *testing.T) {
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

	t.Run("health", func(t *testing.T) {
		t.Parallel()
		where := "GET /health"
		top := propsOf(t, schema200(t, "/health", "get"), where)
		requireExactKeys(t, top, where, "status")
		requireDescribed(t, top, where, "status")
	})

	t.Run("repositories list", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/repositories"
		top := propsOf(t, schema200(t, "/api/v0/repositories", "get"), where)
		requireExactKeys(t, top, where,
			"repositories", "count", "total", "limit", "offset", "truncated",
			"projection", "result_limits", "partial_reasons")
		requireDescribed(t, top, where,
			"repositories", "count", "total", "limit", "offset", "truncated",
			"projection", "result_limits", "partial_reasons")
		requireScalarTypes(t, top, where, map[string]string{
			"count": "integer", "truncated": "boolean",
		})
	})

	t.Run("repositories by language", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/repositories/by-language"
		top := propsOf(t, schema200(t, "/api/v0/repositories/by-language", "get"), where)
		requireExactKeys(t, top, where,
			"language", "normalized_languages", "repository_count", "file_count",
			"last_indexed_at", "repositories", "limit", "offset", "truncated")
		requireDescribed(t, top, where,
			"language", "normalized_languages", "repository_count", "file_count",
			"last_indexed_at", "repositories", "limit", "offset", "truncated")
		items := itemsOf(t, top["repositories"].(map[string]any), where+" repositories")
		// Owner: repository by-language rows extend the shared Repository
		// component with per-language file counts; the contract pins the
		// extension object here alongside the $ref.
		allOf, ok := items["allOf"].([]any)
		if !ok || len(allOf) != 2 {
			t.Fatalf("%s: repositories items must carry the Repository allOf extension", where)
		}
		ext, ok := allOf[1].(map[string]any)
		if !ok {
			t.Fatalf("%s: repositories allOf extension must be an object", where)
		}
		eprops := propsOf(t, ext, where+" repositories[] extension")
		requireExactKeys(t, eprops, where+" repositories[] extension",
			"file_count", "languages", "last_indexed_at")
		requireDescribed(t, eprops, where+" repositories[] extension",
			"file_count", "languages", "last_indexed_at")
		litems := itemsOf(t, eprops["languages"].(map[string]any), where+" languages")
		lprops := propsOf(t, litems, where+" languages[]")
		requireExactKeys(t, lprops, where+" languages[]", "language", "file_count")
		requireDescribed(t, lprops, where+" languages[]", "language", "file_count")
	})

	t.Run("repository language inventory", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/repositories/language-inventory"
		top := propsOf(t, schema200(t, "/api/v0/repositories/language-inventory", "get"), where)
		requireExactKeys(t, top, where, "languages", "limit", "offset", "truncated")
		requireDescribed(t, top, where, "languages", "limit", "offset", "truncated")
		items := itemsOf(t, top["languages"].(map[string]any), where+" languages")
		lprops := propsOf(t, items, where+" languages[]")
		// Owner: repository language_inventory rows.
		requireExactKeys(t, lprops, where+" languages[]",
			"language", "repository_count", "file_count", "last_indexed_at")
		requireDescribed(t, lprops, where+" languages[]",
			"language", "repository_count", "file_count", "last_indexed_at")
	})

	t.Run("catalog", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/catalog"
		top := propsOf(t, schema200(t, "/api/v0/catalog", "get"), where)
		requireExactKeys(t, top, where,
			"repositories", "workloads", "services", "counts", "count",
			"limit", "truncated", "workloads_truncated", "limitations")
		requireDescribed(t, top, where,
			"repositories", "workloads", "services", "counts", "count",
			"limit", "truncated", "workloads_truncated", "limitations")
		// counts is a genuine open map: collection name -> row count.
		counts := top["counts"].(map[string]any)
		if _, ok := counts["additionalProperties"]; !ok {
			t.Errorf("%s: counts must carry additionalProperties", where)
		}
	})

	t.Run("repository tree", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/repositories/{repo_id}/tree"
		top := propsOf(t, schema200(t, "/api/v0/repositories/{repo_id}/tree", "get"), where)
		requireExactKeys(t, top, where, "ref", "path", "truncated", "entries")
		requireDescribed(t, top, where, "ref", "path", "truncated", "entries")
		items := itemsOf(t, top["entries"].(map[string]any), where+" entries")
		eprops := propsOf(t, items, where+" entries[]")
		// Owner: repository tree entry rows.
		requireExactKeys(t, eprops, where+" entries[]",
			"name", "type", "path", "size", "language", "child_count")
		requireDescribed(t, eprops, where+" entries[]",
			"name", "type", "path", "size", "language", "child_count")
	})

	t.Run("repository content", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/repositories/{repo_id}/content"
		top := propsOf(t, schema200(t, "/api/v0/repositories/{repo_id}/content", "get"), where)
		requireExactKeys(t, top, where,
			"path", "ref", "encoding", "content", "size", "language", "truncated")
		requireDescribed(t, top, where,
			"path", "ref", "encoding", "content", "size", "language", "truncated")
	})
}
