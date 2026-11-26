// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"strings"
	"testing"
)

// Lane-4 shape-completeness pins: every bare {"type": "object"} on the code,
// dead-code, CLI-support, auth, CI/CD, and freshness 200-response schemas
// must name its properties (or carry a keyed additionalProperties map with a
// description naming the key semantics), every 200-response property carries
// a description naming its emitting owner, and owner-verified fixed key sets
// are pinned closed. Owner references name the Go handler, mapper, or struct
// that emits the keys each subtest asserts. Shared helpers live here; the
// lane-4 subtests are split across the contract_shapes_lane4_* files so each
// stays under the 500-line cap.

func lane4Child(t *testing.T, node map[string]any, key, where string) map[string]any {
	t.Helper()
	next, ok := node[key].(map[string]any)
	if !ok {
		t.Fatalf("%s: missing or non-object property %q", where, key)
	}
	return next
}

func lane4SchemaAt(t *testing.T, paths map[string]any, path, method, code string) map[string]any {
	t.Helper()
	route, ok := paths[path].(map[string]any)
	if !ok {
		t.Fatalf("spec paths missing %s", path)
	}
	op := lane4Child(t, route, method, path)
	responses := lane4Child(t, op, "responses", path+" "+method)
	okResp := lane4Child(t, responses, code, path+" "+method)
	content, ok := okResp["content"].(map[string]any)
	if !ok {
		t.Fatalf("%s %s %s response has no content (only a description)", path, method, code)
	}
	body := lane4Child(t, content, "application/json", path+" "+method)
	return lane4Child(t, body, "schema", path+" "+method)
}

func lane4PropsOf(t *testing.T, node map[string]any, where string) map[string]any {
	t.Helper()
	props, ok := node["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s: object schema has no properties", where)
	}
	return props
}

func lane4ItemsOf(t *testing.T, node map[string]any, where string) map[string]any {
	t.Helper()
	items, ok := node["items"].(map[string]any)
	if !ok {
		t.Fatalf("%s: array schema has no items", where)
	}
	return items
}

func lane4RequireKeys(t *testing.T, props map[string]any, where string, keys ...string) {
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
	}
}

func lane4RequireDescribed(t *testing.T, props map[string]any, where string, keys ...string) {
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

func lane4RequireScalarTypes(t *testing.T, props map[string]any, where string, want map[string]string) {
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

func lane4RequireExactKeys(t *testing.T, props map[string]any, where string, keys ...string) {
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

// lane4RequireOpenMap asserts a genuine open map: additionalProperties plus
// a description naming the key semantics (or named properties).
func lane4RequireOpenMap(t *testing.T, props map[string]any, where, key string) {
	t.Helper()
	node, ok := props[key].(map[string]any)
	if !ok {
		t.Fatalf("%s: missing property %q", where, key)
	}
	if _, hasProps := node["properties"]; hasProps {
		return
	}
	if _, ok := node["additionalProperties"]; !ok {
		t.Errorf("%s: property %q is a bare object with no properties", where, key)
		return
	}
	if desc, _ := node["description"].(string); desc == "" {
		t.Errorf("%s: property %q has no description naming the key semantics", where, key)
	}
}

func lane4SpecPaths(t *testing.T) map[string]any {
	t.Helper()
	var spec map[string]any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("spec has no paths object")
	}
	return paths
}

// lane4RequireFilterEchoScope asserts the aggregate scope idiom: a
// string-valued map echo of applied filters with the console chips note.
func lane4RequireFilterEchoScope(t *testing.T, props map[string]any, where string) {
	t.Helper()
	node, ok := props["scope"].(map[string]any)
	if !ok {
		t.Fatalf("%s: missing scope property", where)
	}
	additional, ok := node["additionalProperties"].(map[string]any)
	if !ok || additional["type"] != "string" {
		t.Errorf("%s: scope has no string additionalProperties (not a filter-echo map)", where)
	}
	desc, _ := node["description"].(string)
	if !strings.Contains(desc, "string pairs") || !strings.Contains(desc, "filter chips") {
		t.Errorf("%s: scope description lacks the filter-echo idiom: %q", where, desc)
	}
}

func TestContractShapesLane4Dead(t *testing.T) {
	t.Parallel()
	paths := lane4SpecPaths(t)

	t.Run("cross-repo evidence items name owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: deadcode/cross_repo_boundary.go crossRepoDeadCodeEvidenceMaps
		// emits exactly these 14 fixed keys plus conditional observed_at and
		// reason. Buckets carry full candidate rows, so items stay open maps
		// with the evidence keys pinned present.
		props := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/dead-code/cross-repo", "post", "200"), "cross-repo")
		buckets := lane4PropsOf(t, lane4Child(t, props, "candidate_buckets", "cross-repo"), "candidate_buckets")
		lane4RequireExactKeys(t, buckets, "candidate_buckets", "dead", "live_by_consumer", "unknown", "suppressed")
		evidenceKeys := []string{
			"consumer_repo_id", "consumer_repo_name", "consumer_entity_id",
			"relationship_type", "evidence_family", "citation", "confidence",
			"confidence_label", "resolution_method", "depth", "generation_id",
			"generation_status", "ambiguous", "needs_evidence",
		}
		rowKeys := []string{
			"consumer_evidence", "consumer_evidence_source", "consumer_evidence_count",
			"consumer_evidence_group_count", "consumer_evidence_handles_truncated",
		}
		for _, bucket := range []string{"dead", "live_by_consumer", "unknown"} {
			rows := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, buckets, bucket, "bucket"), bucket), bucket+"[]")
			lane4RequireKeys(t, rows, "cross-repo."+bucket+"[]", rowKeys...)
			lane4RequireDescribed(t, rows, "cross-repo."+bucket+"[]", rowKeys...)
			items := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, rows, "consumer_evidence", "row"), "evidence"), "evidence[]")
			lane4RequireKeys(t, items, "cross-repo."+bucket+"[].consumer_evidence[]", evidenceKeys...)
			lane4RequireScalarTypes(t, items, "cross-repo."+bucket+"[].consumer_evidence[]", map[string]string{
				"consumer_repo_id": "string", "confidence": "number",
				"depth": "integer", "ambiguous": "boolean", "needs_evidence": "boolean",
			})
			lane4RequireDescribed(t, items, "cross-repo."+bucket+"[].consumer_evidence[]", "consumer_repo_id", "evidence_family", "confidence_label")
		}
		suppressedItems, ok := lane4Child(t, buckets, "suppressed", "buckets")["items"].(map[string]any)
		if !ok {
			t.Fatal("cross-repo.suppressed: array schema has no items")
		}
		if _, hasProps := suppressedItems["properties"]; !hasProps {
			if _, ok := suppressedItems["additionalProperties"]; !ok {
				t.Error("cross-repo.suppressed[]: bare object items with no properties")
			}
		}
		if desc, _ := suppressedItems["description"].(string); desc == "" {
			t.Error("cross-repo.suppressed[]: items have no description")
		}
		boundary := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, props, "boundary_consumer_evidence", "cross-repo"), "boundary"), "boundary[]")
		lane4RequireKeys(t, boundary, "cross-repo.boundary[]", evidenceKeys...)
		lane4RequireDescribed(t, props, "cross-repo",
			"repo_id", "language", "limit", "consumer_repo_ids", "query_shape",
			"truncated", "display_truncated", "candidate_scan_pages", "candidate_scan_rows",
			"bucket_counts", "analysis")
	})

	t.Run("investigation next calls name owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: deadcode/next_calls.go InvestigationNextCalls emits
		// {tool, arguments, reason} per call; arguments is an open map.
		props := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/dead-code/investigate", "post", "200"), "investigate")
		calls := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, props, "recommended_next_calls", "investigate"), "calls"), "calls[]")
		lane4RequireExactKeys(t, calls, "investigate.calls[]", "tool", "arguments", "reason")
		lane4RequireScalarTypes(t, calls, "investigate.calls[]", map[string]string{
			"tool": "string", "arguments": "object", "reason": "string",
		})
		lane4RequireOpenMap(t, calls, "investigate.calls[]", "arguments")
		lane4RequireDescribed(t, props, "investigate",
			"repo_id", "language", "limit", "offset", "truncated", "coverage",
			"bucket_counts", "root_policy", "language_maturity",
			"exactness_blockers", "observed_exactness_blockers",
			"recommended_next_calls", "analysis")
		scan := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/dead-code", "post", "200"), "scan")
		lane4RequireDescribed(t, scan, "scan",
			"candidate_kind", "repo_id", "language", "limit", "results", "analysis")
		lane4RequireScalarTypes(t, scan, "scan", map[string]string{
			"candidate_kind": "string", "repo_id": "string",
			"language": "string", "limit": "integer",
		})
		analysis := lane4PropsOf(t, lane4Child(t, scan, "analysis", "scan"), "scan.analysis")
		lane4RequireDescribed(t, analysis, "scan.analysis",
			"root_categories_used", "frameworks_recognized", "tests_excluded",
			"generated_code_excluded", "user_overrides_applied",
			"dead_code_language_maturity", "modeled_entrypoints",
			"modeled_public_api", "notes")
	})
}
