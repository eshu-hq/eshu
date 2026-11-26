// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"testing"
)

// TestContractShapesAreComplete pins the slice-1 shape-completeness fixes:
// every 200-response object the console renders must name its properties in
// the served spec instead of a bare {"type": "object"}. Each entry names a
// JSON pointer into the assembled spec and the property keys the Go owner
// actually emits.
func TestContractShapesAreComplete(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("spec has no paths object")
	}

	schemaAt := func(t *testing.T, path, method string) map[string]any {
		t.Helper()
		route, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatalf("spec paths missing %s", path)
		}
		op, ok := route[method].(map[string]any)
		if !ok {
			t.Fatalf("%s missing %s operation", path, method)
		}
		responses, ok := op["responses"].(map[string]any)
		if !ok {
			t.Fatalf("%s %s missing responses", path, method)
		}
		okResp, ok := responses["200"].(map[string]any)
		if !ok {
			t.Fatalf("%s %s missing 200 response", path, method)
		}
		content, ok := okResp["content"].(map[string]any)
		if !ok {
			t.Fatalf("%s %s 200 response has no content (only a description)", path, method)
		}
		body, ok := content["application/json"].(map[string]any)
		if !ok {
			t.Fatalf("%s %s 200 response has no application/json content", path, method)
		}
		schema, ok := body["schema"].(map[string]any)
		if !ok {
			t.Fatalf("%s %s 200 response has no schema", path, method)
		}
		return schema
	}

	propsOf := func(t *testing.T, node map[string]any, where string) map[string]any {
		t.Helper()
		props, ok := node["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s: object schema has no properties", where)
		}
		return props
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
				if _, hasProps := node["properties"]; !hasProps {
					t.Errorf("%s: property %q is a bare object with no properties", where, key)
				}
			}
		}
	}

	t.Run("operations queue and aggregates", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/status/operations", "get"), "operations")
		queue, ok := props["queue"].(map[string]any)
		if !ok {
			t.Fatal("operations: missing queue")
		}
		requireKeys(t, propsOf(t, queue, "operations.queue"), "operations.queue",
			"total", "outstanding", "pending", "in_flight", "retrying",
			"succeeded", "dead_letter", "failed",
			"oldest_outstanding_age", "oldest_outstanding_age_ms", "overdue_claims")
		requireKeys(t, propsOf(t, props["health"].(map[string]any), "operations.health"),
			"operations.health", "state", "reasons")
		stages := props["stage_summaries"].(map[string]any)
		requireKeys(t, propsOf(t, stages["items"].(map[string]any), "operations.stage_summaries[]"),
			"operations.stage_summaries[]", "stage", "pending", "dead_letter", "failed")
		backlogs := props["domain_backlogs"].(map[string]any)
		requireKeys(t, propsOf(t, backlogs["items"].(map[string]any), "operations.domain_backlogs[]"),
			"operations.domain_backlogs[]", "domain", "outstanding", "in_flight", "dead_letter", "oldest_age")
	})

	t.Run("index status queue family", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/status/index", "get"), "index")
		requireKeys(t, propsOf(t, props["queue"].(map[string]any), "index.queue"),
			"index.queue", "total", "dead_letter", "overdue_claims")
		requireKeys(t, propsOf(t, props["scope_activity"].(map[string]any), "index.scope_activity"),
			"index.scope_activity", "active", "changed", "unchanged")
		blockages := props["queue_blockages"].(map[string]any)
		requireKeys(t, propsOf(t, blockages["items"].(map[string]any), "index.queue_blockages[]"),
			"index.queue_blockages[]", "stage", "domain", "conflict_key", "blocked", "oldest_age")
	})

	t.Run("hosted readiness coordinator and diagnostics", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/status/hosted-readiness", "get"), "hosted-readiness")
		requireKeys(t, propsOf(t, props["coordinator"].(map[string]any), "hosted-readiness.coordinator"),
			"hosted-readiness.coordinator", "collector_instances", "collector_backpressure",
			"source_pacing", "active_claims", "overdue_claims")
		paths := props["diagnostic_paths"].(map[string]any)
		requireKeys(t, propsOf(t, paths["items"].(map[string]any), "hosted-readiness.diagnostic_paths[]"),
			"hosted-readiness.diagnostic_paths[]", "name", "path")
	})

	t.Run("retire unclaimed response body", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/admin/workflow-retire-unclaimed", "post"), "retire-unclaimed")
		requireKeys(t, props, "retire-unclaimed", "count", "items")
		items := props["items"].(map[string]any)
		requireKeys(t, propsOf(t, items["items"].(map[string]any), "retire-unclaimed.items[]"),
			"retire-unclaimed.items[]", "work_item_id", "scope_id")
	})

	t.Run("work item evidence rows", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/work-items/evidence", "get"), "work-item")
		items := props["evidence"].(map[string]any)
		requireKeys(t, propsOf(t, items["items"].(map[string]any), "work-item.evidence[]"),
			"work-item.evidence[]", "issue_type_id", "issue_type_name", "status_id",
			"status_name", "created_at", "field", "relationship", "application_name")
	})

	t.Run("impact count envelope documented", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/supply-chain/impact/findings/count", "get"), "impact-count")
		for _, key := range []string{"total_findings", "by_severity", "by_priority_bucket", "scope"} {
			node, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("impact-count: missing property %q", key)
				continue
			}
			if desc, _ := node["description"].(string); desc == "" {
				t.Errorf("impact-count: property %q has no description", key)
			}
		}
	})
}
