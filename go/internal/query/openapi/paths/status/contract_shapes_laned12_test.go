// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractShapesLaneD12IngesterDetail pins the lane-D12 ingester-detail
// completion: both getIngesterStatus routes (the canonical
// /api/v0/status/ingesters/{ingester} and its legacy
// /api/v0/ingesters/{ingester} alias) serve the same query.status
// getIngesterStatus payload, so both 200 schemas must name every key the
// owner emits -- version, ingester, runtime_family, health, queue,
// coordinator, scope_activity, stage_summaries, and domain_backlogs -- with
// owner-backed nested shapes instead of bare objects.
func TestContractShapesLaneD12IngesterDetail(t *testing.T) {
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
			if typ, _ := obj["type"].(string); typ == "object" || (typ == "" && obj["properties"] != nil) {
				_, hasProps := obj["properties"]
				_, hasAdditional := obj["additionalProperties"]
				if !hasProps && !hasAdditional {
					t.Errorf("%s: bare object at %q (no properties/additionalProperties)", where, path)
					return
				}
				if props, ok := obj["properties"].(map[string]any); ok {
					for key, sub := range props {
						walk(sub, path+"."+key)
					}
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
		}
		walk(node, where)
	}

	for _, path := range []string{"/api/v0/status/ingesters/{ingester}", "/api/v0/ingesters/{ingester}"} {
		t.Run("ingester detail on "+path, func(t *testing.T) {
			t.Parallel()
			doc := fragment(t, Routes, "status routes")
			props := propsOf(t, schemaAt(t, doc, path, "get"), path)
			// Owner: query.status getIngesterStatus always emits version,
			// ingester, runtime_family, health, queue, coordinator,
			// scope_activity, stage_summaries, and domain_backlogs;
			// active_work_source and coordinator_source join when the
			// status reader reports a source.
			requireExactKeys(t, props, path,
				"version", "ingester", "runtime_family", "health", "queue",
				"coordinator", "scope_activity", "stage_summaries",
				"domain_backlogs", "active_work_source", "coordinator_source")
			requireDescribed(t, props, path,
				"version", "ingester", "runtime_family", "health", "queue",
				"coordinator", "scope_activity", "stage_summaries",
				"domain_backlogs")
			requireScalarTypes(t, props, path, map[string]string{
				"version": "string", "ingester": "string", "runtime_family": "string",
			})
			// Owner: healthToMap emits state plus reasons; queueToMap the
			// queue snapshot counters; scopeActivityToMap the generation
			// activity counters.
			requireExactKeys(t, propsOf(t, child(t, props, "health", path), path+".health"),
				path+".health", "state", "reasons")
			requireExactKeys(t, propsOf(t, child(t, props, "queue", path), path+".queue"),
				path+".queue", "total", "outstanding", "pending", "in_flight",
				"retrying", "succeeded", "dead_letter", "failed",
				"oldest_outstanding_age", "oldest_outstanding_age_ms", "overdue_claims")
			requireScalarTypes(t, propsOf(t, child(t, props, "queue", path), path+".queue"),
				path+".queue", map[string]string{
					"total": "integer", "outstanding": "integer",
					"oldest_outstanding_age":    "number",
					"oldest_outstanding_age_ms": "integer",
				})
			requireExactKeys(t, propsOf(t, child(t, props, "scope_activity", path), path+".scope_activity"),
				path+".scope_activity", "active", "changed", "unchanged")
			// Owner: coordinatorToMap the coordinator snapshot (scoped
			// callers receive the scopedCoordinatorToMap reduced variant
			// documented on the coordinator shape); stageSummariesToSlice
			// and domainBacklogsToSlice the stage and backlog rows.
			coordinatorProps := propsOf(t, child(t, props, "coordinator", path), path+".coordinator")
			for _, key := range []string{
				"collector_instances", "collector_backpressure", "source_pacing",
				"run_status_counts", "work_item_status_counts", "completeness_counts",
				"active_claims", "overdue_claims", "oldest_pending_age",
			} {
				if _, ok := coordinatorProps[key].(map[string]any); !ok {
					t.Errorf("%s.coordinator: missing property %q", path, key)
				}
			}
			stages := child(t, props, "stage_summaries", path)
			requireExactKeys(t, propsOf(t, itemsOf(t, stages, path+".stage_summaries"), path+".stage_summaries[]"),
				path+".stage_summaries[]", "stage", "pending", "claimed",
				"running", "retrying", "succeeded", "dead_letter", "failed")
			backlogs := child(t, props, "domain_backlogs", path)
			requireExactKeys(t, propsOf(t, itemsOf(t, backlogs, path+".domain_backlogs"), path+".domain_backlogs[]"),
				path+".domain_backlogs[]", "domain", "outstanding", "pending",
				"in_flight", "blocked", "retrying", "dead_letter", "failed", "oldest_age")
			requireNoBare(t, props, path)
		})
	}
}
