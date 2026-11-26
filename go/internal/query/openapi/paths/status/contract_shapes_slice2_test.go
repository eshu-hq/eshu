// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractShapesSlice2Status pins the slice-2 shape-completeness fixes
// for the index-status family: every 200-response object the console renders
// must name its properties in the fragment instead of a bare
// {"type": "object"}. Each entry navigates the fragment JSON and requires
// the property keys the Go owner actually emits.
func TestContractShapesSlice2Status(t *testing.T) {
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

	t.Run("index domains items name owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: go/internal/query/aws_materialization_status.go:144
		// domainBacklogToMap emits domain, outstanding, pending, in_flight,
		// blocked, retrying, dead_letter, failed, oldest_age; blockage-only
		// rows (:61-70) omit oldest_age.
		doc := fragment(t, Routes, "Routes")
		schema := schemaAt(t, doc, "/api/v0/status/index", "get")
		props := propsOf(t, schema, "/api/v0/status/index")
		aws := child(t, props, "aws_materialization", "/api/v0/status/index")
		awsProps := propsOf(t, aws, "aws_materialization")
		domains := child(t, awsProps, "domains", "aws_materialization")
		items := propsOf(t, itemsOf(t, domains, "domains"), "domains items")
		requireKeys(t, items, "aws_materialization.domains[] items",
			"domain", "outstanding", "pending", "in_flight", "blocked",
			"retrying", "dead_letter", "failed", "oldest_age")
	})

	t.Run("legacy alias domains items name owner keys", func(t *testing.T) {
		t.Parallel()
		// Same owner as the main index block:
		// go/internal/query/aws_materialization_status.go:144.
		doc := fragment(t, IndexLegacy, "IndexLegacy")
		schema := schemaAt(t, doc, "/api/v0/index-status", "get")
		props := propsOf(t, schema, "/api/v0/index-status")
		aws := child(t, props, "aws_materialization", "/api/v0/index-status")
		awsProps := propsOf(t, aws, "aws_materialization")
		domains := child(t, awsProps, "domains", "aws_materialization")
		items := propsOf(t, itemsOf(t, domains, "domains"), "domains items")
		requireKeys(t, items, "aws_materialization.domains[] items",
			"domain", "outstanding", "pending", "in_flight", "blocked",
			"retrying", "dead_letter", "failed", "oldest_age")
	})

	t.Run("legacy alias lives outside Routes", func(t *testing.T) {
		t.Parallel()
		// routes.go stays under the 500-line cap: the legacy alias moved to
		// index_status_legacy.go (IndexLegacy, concatenated into the
		// spec right after Routes). Either side regaining the other's path
		// is a split regression.
		routesDoc := fragment(t, Routes, "Routes")
		if _, ok := routesDoc["/api/v0/index-status"]; ok {
			t.Error("Routes still documents /api/v0/index-status; it moved to IndexLegacy")
		}
		if _, ok := routesDoc["/api/v0/status/index"]; !ok {
			t.Error("Routes no longer documents /api/v0/status/index")
		}
		legacyDoc := fragment(t, IndexLegacy, "IndexLegacy")
		if _, ok := legacyDoc["/api/v0/index-status"]; !ok {
			t.Error("IndexLegacy does not document /api/v0/index-status")
		}
		if _, ok := legacyDoc["/api/v0/status/index"]; ok {
			t.Error("IndexLegacy documents /api/v0/status/index; it belongs to Routes")
		}
	})

	t.Run("semantic audit counts name owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: go/internal/status/semantic/extraction_observability_json.go:69
		// extractionAuditJSON projects both arrays through
		// shared.NamedCountsJSON (go/internal/status/shared/wire.go:11),
		// whose wire shape is {name, count}.
		doc := fragment(t, Semantic, "Semantic")
		schema := schemaAt(t, doc, "/api/v0/status/semantic-extraction", "get")
		props := propsOf(t, schema, "/api/v0/status/semantic-extraction")
		audit := child(t, props, "audit", "semantic_extraction")
		auditProps := propsOf(t, audit, "audit")
		for _, key := range []string{"actor_class_counts", "acl_state_counts"} {
			arr := child(t, auditProps, key, "audit")
			items := propsOf(t, itemsOf(t, arr, key), key+" items")
			requireKeys(t, items, "audit."+key+"[] items", "name", "count")
		}
	})
}
