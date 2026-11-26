// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractShapesLaneC14Status pins the lane-C14 shape completion for the
// status leaf leftovers: the compare answer_metadata companion must carry
// owner-backed nested shapes (not bare items/coverage), the ingester list
// rows must name the keys listIngesters emits, the legacy collectors alias
// must mirror the canonical list payload, and the openapi document route
// must describe its self-referential schema.
func TestContractShapesLaneC14Status(t *testing.T) {
	t.Parallel()

	parseFragment := func(t *testing.T, raw, where string, pathsCloser bool) map[string]any {
		t.Helper()
		trimmed := strings.TrimSpace(raw)
		if pathsCloser {
			// Compare is the last fragment spliced into spec.go, so it
			// ends with the paths-object closer ("},", not ",").
			if !strings.HasSuffix(trimmed, "},") {
				t.Fatalf("%s: compare fragment lost its paths-object closer", where)
			}
			trimmed = strings.TrimSuffix(trimmed, "},")
		} else {
			trimmed = strings.TrimSuffix(trimmed, ",")
		}
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
			prop, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if desc, _ := prop["description"].(string); desc == "" {
				t.Errorf("%s: property %q has no description", where, key)
			}
		}
	}
	requireExactKeys := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		want := map[string]bool{}
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
			prop, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if got, _ := prop["type"].(string); got != typ {
				t.Errorf("%s: property %q has type %q, want %q", where, key, got, typ)
			}
		}
	}
	// requireNoBare walks a decoded fragment and fails on any object schema
	// without properties, additionalProperties, $ref, or composition, and on
	// any array schema without items.
	requireNoBare := func(t *testing.T, node any, where string) {
		t.Helper()
		var walk func(node any, path string)
		walk = func(node any, path string) {
			obj, ok := node.(map[string]any)
			if !ok {
				return
			}
			if ref, _ := obj["$ref"].(string); ref != "" {
				return
			}
			for _, comp := range []string{"oneOf", "anyOf", "allOf"} {
				if branches, ok := obj[comp].([]any); ok {
					for _, branch := range branches {
						walk(branch, path)
					}
					return
				}
			}
			if typ, _ := obj["type"].(string); typ == "object" || (typ == "" && obj["properties"] != nil) {
				_, hasProps := obj["properties"]
				_, hasAdditional := obj["additionalProperties"]
				hasPropsNonEmpty := false
				if props, ok := obj["properties"].(map[string]any); ok && len(props) > 0 {
					hasPropsNonEmpty = true
				}
				if !hasPropsNonEmpty && !hasAdditional && !hasProps {
					t.Errorf("%s: bare object at %q (no properties/additionalProperties)", where, path)
					return
				}
				if props, ok := obj["properties"].(map[string]any); ok {
					for key, sub := range props {
						walk(sub, path+"."+key)
					}
				}
				if addl, ok := obj["additionalProperties"].(map[string]any); ok {
					walk(addl, path+"[]")
				}
				return
			}
			if typ, _ := obj["type"].(string); typ == "array" {
				items, ok := obj["items"].(map[string]any)
				if !ok {
					t.Errorf("%s: array without items at %q", where, path)
					return
				}
				walk(items, path+"[]")
				return
			}
			for _, key := range []string{"properties", "items"} {
				if sub, ok := obj[key]; ok {
					walk(sub, path)
				}
			}
		}
		walk(node, where)
	}

	t.Run("compare answer_metadata nested shapes", func(t *testing.T) {
		t.Parallel()
		doc := parseFragment(t, Compare, "compare", true)
		route, ok := doc["/api/v0/compare/environments"].(map[string]any)
		if !ok {
			t.Fatal("fragment paths missing /api/v0/compare/environments")
		}
		op := child(t, route, "post", "compare")
		responses := child(t, op, "responses", "compare post")
		okResp := child(t, responses, "200", "compare post")
		content := child(t, okResp, "content", "compare post 200")
		body := child(t, content, "application/json", "compare post 200")
		schema := child(t, body, "schema", "compare post 200")
		props := propsOf(t, schema, "compare")

		// Owner: query/querycontract/answer BuildAnswerMetadata derives the
		// companion from the built response; coverage is the carried copy of
		// the route coverage via answerMetadataCoverage.
		meta := child(t, props, "answer_metadata", "compare")
		metaProps := propsOf(t, meta, "compare.answer_metadata")
		requireExactKeys(t, metaProps, "compare.answer_metadata",
			"schema_version", "evidence_handles", "missing_evidence", "limitations",
			"truncated", "coverage", "partial_reasons", "recommended_next_calls")
		required, ok := meta["required"].([]any)
		if !ok || len(required) != 8 {
			t.Errorf("compare.answer_metadata: want required list of the 8 companion keys, got %v", meta["required"])
		}

		// Owner: answer.normalizeMetadataHandle emits kind, repo_id,
		// relative_path, entity_id, evidence_family, reason, start_line,
		// end_line. The compare payload carries no evidence rows, so the
		// array is empty on this route but the item shape stays exact.
		handles := child(t, metaProps, "evidence_handles", "compare.answer_metadata")
		handleProps := propsOf(t, itemsOf(t, handles, "compare.answer_metadata.evidence_handles"), "compare.answer_metadata.evidence_handles[]")
		requireExactKeys(t, handleProps, "compare.answer_metadata.evidence_handles[]",
			"kind", "repo_id", "relative_path", "entity_id", "evidence_family", "reason", "start_line", "end_line")
		requireScalarTypes(t, handleProps, "compare.answer_metadata.evidence_handles[]", map[string]string{
			"start_line": "integer", "end_line": "integer",
		})

		// Owner: answer.metadataReasonRows copies row maps and falls back
		// the reason from explanation/message/slot/kind; dedupe keys are
		// kind, slot, reason, repo_id, relative_path, entity_id.
		missing := child(t, metaProps, "missing_evidence", "compare.answer_metadata")
		missingProps := propsOf(t, itemsOf(t, missing, "compare.answer_metadata.missing_evidence"), "compare.answer_metadata.missing_evidence[]")
		requireExactKeys(t, missingProps, "compare.answer_metadata.missing_evidence[]",
			"kind", "slot", "reason", "repo_id", "relative_path", "entity_id")

		// Owner: answer.answerMetadataLimitations over row maps plus the
		// result_truncated companion row: kind-dispatched open rows, so the
		// items stay an open map whose description names kind and reason.
		limits := child(t, metaProps, "limitations", "compare.answer_metadata")
		limitItems := itemsOf(t, limits, "compare.answer_metadata.limitations")
		if _, ok := limitItems["additionalProperties"]; !ok {
			t.Error("compare.answer_metadata.limitations[]: kind-dispatched rows need additionalProperties")
		}
		limitDesc, _ := limitItems["description"].(string)
		for _, key := range []string{"kind", "reason"} {
			if !strings.Contains(limitDesc, key) {
				t.Errorf("compare.answer_metadata.limitations[]: description must name key %q", key)
			}
		}

		// Owner: answer.answerMetadataCoverage copies data["coverage"], and
		// the compare route emits environmentCompareCoverage's 9 keys.
		coverage := child(t, metaProps, "coverage", "compare.answer_metadata")
		requireExactKeys(t, propsOf(t, coverage, "compare.answer_metadata.coverage"), "compare.answer_metadata.coverage",
			"query_shape", "comparison_basis", "freshness_state", "left_status",
			"right_status", "left_truncated", "right_truncated", "limit", "truncated")

		partial := child(t, metaProps, "partial_reasons", "compare.answer_metadata")
		if items, ok := partial["items"].(map[string]any); !ok || items["type"] != "string" {
			t.Error("compare.answer_metadata.partial_reasons: items must be strings")
		}
		if desc, _ := partial["description"].(string); desc == "" {
			t.Error("compare.answer_metadata.partial_reasons: needs a description")
		}

		// Owner: answer.answerMetadataNextCalls passes the route's
		// tool/reason/arguments rows through; arguments stay tool-conditional.
		next := child(t, metaProps, "recommended_next_calls", "compare.answer_metadata")
		nextProps := propsOf(t, itemsOf(t, next, "compare.answer_metadata.recommended_next_calls"), "compare.answer_metadata.recommended_next_calls[]")
		requireExactKeys(t, nextProps, "compare.answer_metadata.recommended_next_calls[]",
			"tool", "reason", "arguments")
		args := child(t, nextProps, "arguments", "compare.answer_metadata.recommended_next_calls[]")
		if _, ok := args["additionalProperties"]; !ok {
			t.Error("compare.answer_metadata.recommended_next_calls[].arguments: tool-conditional args need additionalProperties")
		}
		requireNoBare(t, meta, "compare.answer_metadata")
	})

	t.Run("ingester list rows", func(t *testing.T) {
		t.Parallel()
		doc := parseFragment(t, Routes, "status routes", false)
		for _, path := range []string{"/api/v0/status/ingesters", "/api/v0/ingesters"} {
			route, ok := doc[path].(map[string]any)
			if !ok {
				t.Fatalf("fragment paths missing %s", path)
			}
			op := child(t, route, "get", path)
			responses := child(t, op, "responses", path+" get")
			okResp := child(t, responses, "200", path+" get")
			content := child(t, okResp, "content", path+" get 200")
			body := child(t, content, "application/json", path+" get 200")
			schema := child(t, body, "schema", path+" get 200")
			props := propsOf(t, schema, path)
			// Owner: query.status listIngesters always emits version,
			// name, runtime_family, and aliases; health, queue_outstanding,
			// and collector_instances join when a status reader is present.
			requireKeys(t, props, path, "version", "ingesters", "count")
			ingesters := child(t, props, "ingesters", path)
			itemProps := propsOf(t, itemsOf(t, ingesters, path+".ingesters"), path+".ingesters[]")
			requireKeys(t, itemProps, path+".ingesters[]",
				"name", "runtime_family", "aliases", "health", "queue_outstanding", "collector_instances")
			requireDescribed(t, itemProps, path+".ingesters[]",
				"name", "runtime_family", "aliases", "health", "queue_outstanding", "collector_instances")
		}
	})

	t.Run("legacy collectors alias mirrors canonical", func(t *testing.T) {
		t.Parallel()
		doc := parseFragment(t, Routes, "status routes", false)
		canonical, ok := doc["/api/v0/status/collectors"].(map[string]any)
		if !ok {
			t.Fatal("fragment paths missing /api/v0/status/collectors")
		}
		legacy, ok := doc["/api/v0/collectors"].(map[string]any)
		if !ok {
			t.Fatal("fragment paths missing /api/v0/collectors")
		}
		schemaAt := func(node map[string]any, where string) map[string]any {
			op := child(t, node, "get", where)
			responses := child(t, op, "responses", where+" get")
			okResp := child(t, responses, "200", where+" get")
			content := child(t, okResp, "content", where+" get 200")
			body := child(t, content, "application/json", where+" get 200")
			return child(t, body, "schema", where+" get 200")
		}
		legacySchema := schemaAt(legacy, "legacy collectors")
		_ = schemaAt(canonical, "canonical collectors")
		legacyProps := propsOf(t, legacySchema, "legacy collectors")
		// Owner: query.status listCollectors serves both routes, so the
		// legacy alias carries the same payload keys.
		for _, key := range []string{"version", "collectors", "count", "classification_basis", "updated_at"} {
			if _, ok := legacyProps[key].(map[string]any); !ok {
				t.Errorf("legacy collectors: missing canonical property %q", key)
			}
		}
		legacyCollectors := child(t, legacyProps, "collectors", "legacy collectors")
		legacyItemProps := propsOf(t, itemsOf(t, legacyCollectors, "legacy collectors.collectors"), "legacy collectors.collectors[]")
		requireKeys(t, legacyItemProps, "legacy collectors.collectors[]", "instance_id", "collector_kind", "health")
	})

	t.Run("openapi document route describes itself", func(t *testing.T) {
		t.Parallel()
		doc := parseFragment(t, Routes, "status routes", false)
		route, ok := doc["/api/v0/openapi.json"].(map[string]any)
		if !ok {
			t.Fatal("fragment paths missing /api/v0/openapi.json")
		}
		op := child(t, route, "get", "openapi")
		responses := child(t, op, "responses", "openapi get")
		okResp := child(t, responses, "200", "openapi get")
		content := child(t, okResp, "content", "openapi get 200")
		body := child(t, content, "application/json", "openapi get 200")
		schema := child(t, body, "schema", "openapi get 200")
		if desc, _ := schema["description"].(string); desc == "" {
			t.Error("/api/v0/openapi.json: self-referential schema needs a description naming the served document")
		}
		if _, ok := schema["additionalProperties"]; !ok {
			t.Error("/api/v0/openapi.json: self-referential document schema needs additionalProperties")
		}
	})
}
