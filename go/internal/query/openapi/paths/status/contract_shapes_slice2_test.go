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

	keySet := func(t *testing.T, props map[string]any, where string) map[string]bool {
		t.Helper()
		set := make(map[string]bool, len(props))
		for key := range props {
			set[key] = true
		}
		return set
	}

	equalSets := func(a, b map[string]bool) bool {
		if len(a) != len(b) {
			return false
		}
		for key := range a {
			if !b[key] {
				return false
			}
		}
		return true
	}

	// requireScalarTypes pins the JSON scalar type of hand-written schema
	// properties. Presence-only checks pass a boolean where the server
	// emits an integer, so every hand-typed scalar needs its type pinned.
	requireKeys := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		for _, key := range keys {
			if _, ok := props[key].(map[string]any); !ok {
				t.Errorf("%s: missing property %q", where, key)
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

	// requireExactKeys pins the closed property set of an object whose
	// owner emits a fixed key list: missing keys and phantom additions
	// both fail.
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
		// Owner emits exactly these 9 keys; oldest_age is seconds as a number.
		requireExactKeys(t, items, "aws_materialization.domains[] items",
			"domain", "outstanding", "pending", "in_flight", "blocked",
			"retrying", "dead_letter", "failed", "oldest_age")
		requireScalarTypes(t, items, "aws_materialization.domains[] items", map[string]string{
			"domain": "string", "outstanding": "integer", "pending": "integer",
			"in_flight": "integer", "blocked": "integer", "retrying": "integer",
			"dead_letter": "integer", "failed": "integer", "oldest_age": "number",
		})
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
		requireExactKeys(t, items, "legacy aws_materialization.domains[] items",
			"domain", "outstanding", "pending", "in_flight", "blocked",
			"retrying", "dead_letter", "failed", "oldest_age")
		requireScalarTypes(t, items, "legacy aws_materialization.domains[] items", map[string]string{
			"domain": "string", "outstanding": "integer", "pending": "integer",
			"in_flight": "integer", "blocked": "integer", "retrying": "integer",
			"dead_letter": "integer", "failed": "integer", "oldest_age": "number",
		})
	})

	t.Run("legacy alias lives outside Routes", func(t *testing.T) {
		t.Parallel()
		// routes.go stays under the 500-line cap: the legacy alias moved to
		// index_legacy.go (IndexLegacy, concatenated into the
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

	t.Run("legacy alias semantic extraction matches main block", func(t *testing.T) {
		t.Parallel()
		// Both routes share getIndexStatus, so their semantic_extraction
		// key sets must be identical.
		mainDoc := fragment(t, Routes, "Routes")
		mainSchema := schemaAt(t, mainDoc, "/api/v0/status/index", "get")
		mainProps := propsOf(t, mainSchema, "/api/v0/status/index")
		mainSem := child(t, mainProps, "semantic_extraction", "index")
		mainKeys := keySet(t, propsOf(t, mainSem, "index semantic_extraction"), "index semantic_extraction")
		legacyDoc := fragment(t, IndexLegacy, "IndexLegacy")
		legacySchema := schemaAt(t, legacyDoc, "/api/v0/index-status", "get")
		legacyProps := propsOf(t, legacySchema, "/api/v0/index-status")
		legacySem := child(t, legacyProps, "semantic_extraction", "legacy")
		legacyKeys := keySet(t, propsOf(t, legacySem, "legacy semantic_extraction"), "legacy semantic_extraction")
		if !equalSets(mainKeys, legacyKeys) {
			t.Errorf("semantic_extraction key sets differ: index=%v legacy=%v", mainKeys, legacyKeys)
		}
		// Owner semanticExtractionStatusToMap emits queue, budget, and
		// audit whenever populated: both blocks must contract them, since
		// parity alone passes mutual omission.
		for _, key := range []string{"queue", "budget", "audit"} {
			if _, ok := mainKeys[key]; !ok {
				t.Errorf("index semantic_extraction: missing conditionally-served property %q", key)
			}
			if _, ok := legacyKeys[key]; !ok {
				t.Errorf("legacy semantic_extraction: missing conditionally-served property %q", key)
			}
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
			// Wire shape is exactly {name, count} (shared.NamedCountJSON).
			requireExactKeys(t, items, "audit."+key+"[] items", "name", "count")
			requireScalarTypes(t, items, "audit."+key+"[] items", map[string]string{
				"name": "string", "count": "integer",
			})
		}
	})

	t.Run("semantic queue and budget nested counts", func(t *testing.T) {
		t.Parallel()
		// Owner: extraction_observability_json.go extractionQueueJSON.
		// The route owner only sets the count arrays when non-empty, so
		// every nested count is asserted present-or-absent by shape, not
		// as always-present: status/source-class/failure-class counts
		// arrive via always-non-nil NamedCountsJSON but omitempty still
		// drops them when empty; provider-profile/policy/guard counts and
		// updated_at/decision_counts are omitted when empty.
		doc := fragment(t, Semantic, "Semantic")
		schema := schemaAt(t, doc, "/api/v0/status/semantic-extraction", "get")
		props := propsOf(t, schema, "/api/v0/status/semantic-extraction")
		queue := child(t, props, "queue", "semantic_extraction")
		queueProps := propsOf(t, queue, "semantic queue")
		for _, key := range []string{
			"status_counts", "source_class_counts", "failure_class_counts",
			"provider_profile_counts", "policy_decision_counts", "guard_decision_counts",
		} {
			if _, ok := queueProps[key].(map[string]any); !ok {
				t.Errorf("semantic queue: missing conditionally-served property %q", key)
			}
		}
		budget := child(t, props, "budget", "semantic_extraction")
		budgetProps := propsOf(t, budget, "semantic budget")
		if _, ok := budgetProps["decision_counts"].(map[string]any); !ok {
			t.Error("semantic budget: missing conditionally-served property \"decision_counts\"")
		}
		// Nested item shapes are closed structs: NamedCountJSON is exactly
		// {name, count}; decision rows carry optional string classifiers
		// with a required integer count.
		for _, key := range []string{"status_counts", "source_class_counts", "failure_class_counts"} {
			arr := child(t, queueProps, key, "semantic queue")
			requireExactKeys(t, propsOf(t, itemsOf(t, arr, key), key+" items"), "semantic queue."+key+"[]",
				"name", "count")
		}
		budgetDecisions := child(t, budgetProps, "decision_counts", "semantic budget")
		budgetDecisionProps := propsOf(t, itemsOf(t, budgetDecisions, "decision_counts"), "budget decision_counts[]")
		requireKeys(t, budgetDecisionProps, "semantic budget.decision_counts[]", "count")
		requireScalarTypes(t, budgetDecisionProps, "semantic budget.decision_counts[]", map[string]string{
			"count": "integer",
		})
		for _, key := range []string{"provider_profile_counts", "policy_decision_counts", "guard_decision_counts"} {
			arr := child(t, queueProps, key, "semantic queue")
			arrProps := propsOf(t, itemsOf(t, arr, key), key+" items")
			requireKeys(t, arrProps, "semantic queue."+key+"[]", "count")
			requireScalarTypes(t, arrProps, "semantic queue."+key+"[]", map[string]string{
				"count": "integer",
			})
		}
	})
}
