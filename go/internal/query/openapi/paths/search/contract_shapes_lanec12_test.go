// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package search

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractShapesLaneC12Search pins the lane-C12 completeness proof for
// the in-scope search routes (resolve, entity-context, workload-story,
// api-endpoints, downstream-consumers; the service-story fragment is
// slices-owned and untouched here). The lane-B8 suite pins the exact key
// sets against each emitting handler; this suite proves the completion bar
// on top: no bare {"type": "object"} anywhere in the in-scope 200 schemas
// (a shape is complete via properties, additionalProperties, or $ref
// composition), and every 200-response property at every depth carries a
// description naming its emitting owner.
func TestContractShapesLaneC12Search(t *testing.T) {
	t.Parallel()

	fragment := func(t *testing.T, raw, where string) map[string]any {
		t.Helper()
		trimmed := strings.TrimSpace(raw)
		trimmed = strings.TrimSuffix(trimmed, ",")
		// EvidenceBoundaries is a Go constant concatenated into the
		// workload-story and service-story fragments; stub it so the
		// fragment parses as JSON.
		trimmed = strings.ReplaceAll(trimmed, "` + schema.EvidenceBoundaries + `", `"__evidence_boundaries__"`)
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

	// requireNoBare walks a 200 schema recursively: every object node
	// completes via properties, additionalProperties, or $ref/allOf/oneOf
	// composition; every array node carries items.
	requireNoBare := func(t *testing.T, node map[string]any, where string) {
		t.Helper()
		var walk func(n map[string]any, at string)
		walk = func(n map[string]any, at string) {
			if len(n) == 1 {
				if _, ok := n["$ref"]; ok {
					return
				}
			}
			if n["type"] == "object" {
				_, hasProps := n["properties"]
				_, hasAdditional := n["additionalProperties"]
				_, hasAllOf := n["allOf"]
				_, hasOneOf := n["oneOf"]
				if !hasProps && !hasAdditional && !hasAllOf && !hasOneOf {
					t.Errorf("%s: bare object at %q (no properties, additionalProperties, or composition)", where, at)
				}
			}
			if n["type"] == "array" {
				items, ok := n["items"].(map[string]any)
				if !ok {
					t.Errorf("%s: array at %q has no items", where, at)
				} else {
					walk(items, at+"[]")
				}
			}
			if props, ok := n["properties"].(map[string]any); ok {
				for k, v := range props {
					if vm, ok := v.(map[string]any); ok {
						walk(vm, at+"."+k)
					}
				}
			}
		}
		walk(node, where)
	}

	// requireAllDescribed walks every properties map at every depth: each
	// $ref-free property carries a description naming its emitting owner.
	requireAllDescribed := func(t *testing.T, node map[string]any, where string) {
		t.Helper()
		var walk func(n map[string]any, at string)
		walk = func(n map[string]any, at string) {
			if props, ok := n["properties"].(map[string]any); ok {
				for k, v := range props {
					vm, ok := v.(map[string]any)
					if !ok {
						t.Errorf("%s: property %q is not an object schema", where, at+"."+k)
						continue
					}
					if _, ok := vm["$ref"]; !ok {
						if desc, _ := vm["description"].(string); desc == "" {
							t.Errorf("%s: property %q has no owner-naming description", where, at+"."+k)
						}
					}
					walk(vm, at+"."+k)
				}
			}
			if items, ok := n["items"].(map[string]any); ok {
				walk(items, at+"[]")
			}
			if nesting, ok := n["additionalProperties"].(map[string]any); ok {
				walk(nesting, at+"<additional>")
			}
		}
		walk(node, where)
	}

	inScope := map[string]string{
		"/api/v0/entities/resolve":                              "post",
		"/api/v0/entities/{entity_id}/context":                  "get",
		"/api/v0/workloads/{workload_id}/story":                 "get",
		"/api/v0/services/{service_name}/api-surface/endpoints": "get",
		"/api/v0/services/{service_name}/downstream-consumers":  "get",
	}

	t.Run("in-scope 200 schemas are complete", func(t *testing.T) {
		t.Parallel()
		doc := fragment(t, Entities, "Entities")
		for path, method := range inScope {
			schema := schemaAt(t, doc, path, method)
			where := method + " " + path
			requireNoBare(t, schema, where)
			requireAllDescribed(t, schema, where)
		}
	})

	t.Run("downstream consumers rows stay an open enrichment map", func(t *testing.T) {
		t.Parallel()
		// Owner: service.ConsumerListPage.Consumers is []map[string]any:
		// rows always carry repository, repo_id, and consumer_kinds, plus
		// whichever enrichment keys fired (graph_relationship_types,
		// graph_relationship_reasons, evidence_kinds, matched_values,
		// sample_paths). The contract keeps the open map and names the
		// key semantics instead of freezing a key set the enrichment
		// does not guarantee.
		doc := fragment(t, Entities, "Entities")
		schema := schemaAt(t, doc, "/api/v0/services/{service_name}/downstream-consumers", "get")
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatal("downstream consumers: 200 schema has no properties")
		}
		consumers, ok := props["consumers"].(map[string]any)
		if !ok {
			t.Fatal("downstream consumers: missing consumers property")
		}
		items, ok := consumers["items"].(map[string]any)
		if !ok {
			t.Fatal("downstream consumers.consumers: array has no items")
		}
		if _, ok := items["additionalProperties"]; !ok {
			t.Error("downstream consumers.consumers[]: open enrichment map lost its additionalProperties")
		}
		if desc, _ := items["description"].(string); desc == "" {
			t.Error("downstream consumers.consumers[]: missing description naming the row key semantics")
		}
	})
}
