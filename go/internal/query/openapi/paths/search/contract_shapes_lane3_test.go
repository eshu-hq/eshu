// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package search

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractShapesLane3GraphEntities pins the lane-3 shape completion for
// GET /api/v0/graph/entities: every 200-response property names its keys in
// the fragment instead of a bare {"type": "object"}, and every property
// carries a description naming its emitting owner.
func TestContractShapesLane3GraphEntities(t *testing.T) {
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

	requireAllDescribed := func(t *testing.T, props map[string]any, where string) {
		t.Helper()
		for key, raw := range props {
			node, ok := raw.(map[string]any)
			if !ok {
				t.Errorf("%s: property %q is not an object schema", where, key)
				continue
			}
			if desc, _ := node["description"].(string); desc == "" {
				t.Errorf("%s: property %q has no description", where, key)
			}
		}
	}

	t.Run("graph entity inventory names owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: go/internal/query/graph_entity_inventory.go listEntities
		// builds body{kinds,total,entities,count,limit,offset,truncated}
		// plus kind only when a kind filter is applied; kinds[] rows come
		// from decodeGraphEntityKindCounts
		// (graph_entity_inventory_counts.go) and entities[] rows from the
		// kind-anchored list query.
		doc := fragment(t, GraphEntities, "GraphEntities")
		schema := schemaAt(t, doc, "/api/v0/graph/entities", "get")
		props := propsOf(t, schema, "/api/v0/graph/entities")
		requireKeys(t, props, "graph inventory",
			"kinds", "total", "kind", "entities", "count", "limit", "offset", "truncated")
		requireAllDescribed(t, props, "graph inventory")
		requireScalarTypes(t, props, "graph inventory", map[string]string{
			"total": "integer", "kind": "string", "count": "integer",
			"limit": "integer", "offset": "integer", "truncated": "boolean",
		})

		// Owner: decodeGraphEntityKindCounts emits exactly kind, label,
		// count per facet in graphEntityKinds display order.
		kinds := child(t, props, "kinds", "graph inventory")
		kindItems := propsOf(t, itemsOf(t, kinds, "kinds"), "kinds items")
		requireExactKeys(t, kindItems, "kinds[] items", "kind", "label", "count")
		requireDescribed(t, kindItems, "kinds[] items", "kind", "label", "count")
		requireScalarTypes(t, kindItems, "kinds[] items", map[string]string{
			"kind": "string", "label": "string", "count": "integer",
		})

		// Owner: listEntities emits exactly id, name, kind, account per
		// entity row; account is "" when the facet label carries no
		// account-like field.
		entities := child(t, props, "entities", "graph inventory")
		entityItems := propsOf(t, itemsOf(t, entities, "entities"), "entities items")
		requireExactKeys(t, entityItems, "entities[] items", "id", "name", "kind", "account")
		requireDescribed(t, entityItems, "entities[] items", "id", "name", "kind", "account")
		requireScalarTypes(t, entityItems, "entities[] items", map[string]string{
			"id": "string", "name": "string", "kind": "string", "account": "string",
		})
	})
}
