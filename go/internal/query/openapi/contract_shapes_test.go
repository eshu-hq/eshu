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

	// child navigates one map level, failing cleanly when the key is absent
	// or not an object instead of panicking on a type assertion.
	child := func(t *testing.T, node map[string]any, key, where string) map[string]any {
		t.Helper()
		next, ok := node[key].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing or non-object property %q", where, key)
		}
		return next
	}

	schemaAt := func(t *testing.T, path, method string) map[string]any {
		t.Helper()
		route, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatalf("spec paths missing %s", path)
		}
		op := child(t, route, method, path)
		responses := child(t, op, "responses", path+" "+method)
		okResp := child(t, responses, "200", path+" "+method)
		content, ok := okResp["content"].(map[string]any)
		if !ok {
			t.Fatalf("%s %s 200 response has no content (only a description)", path, method)
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
				if _, hasProps := node["properties"]; !hasProps {
					t.Errorf("%s: property %q is a bare object with no properties", where, key)
				}
			}
		}
	}

	// requireScalarTypes pins the JSON scalar type of hand-written schema
	// properties. Presence-only checks pass a boolean where the server
	// emits an integer, so every hand-typed scalar needs its type pinned.
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

	requireArrayOfStrings := func(t *testing.T, props map[string]any, where, key string) {
		t.Helper()
		node, ok := props[key].(map[string]any)
		if !ok {
			t.Errorf("%s: missing property %q", where, key)
			return
		}
		if node["type"] != "array" {
			t.Errorf("%s: property %q is not an array", where, key)
			return
		}
		items, ok := node["items"].(map[string]any)
		if !ok || items["type"] != "string" {
			t.Errorf("%s: property %q is not an array of strings", where, key)
		}
	}

	t.Run("operations queue and aggregates", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/status/operations", "get"), "operations")
		queue := child(t, props, "queue", "operations")
		requireKeys(t, propsOf(t, queue, "operations.queue"), "operations.queue",
			"total", "outstanding", "pending", "in_flight", "retrying",
			"succeeded", "dead_letter", "failed",
			"oldest_outstanding_age", "oldest_outstanding_age_ms", "overdue_claims")
		health := child(t, props, "health", "operations")
		requireKeys(t, propsOf(t, health, "operations.health"),
			"operations.health", "state", "reasons")
		stages := child(t, props, "stage_summaries", "operations")
		requireKeys(t, propsOf(t, itemsOf(t, stages, "operations.stage_summaries"), "operations.stage_summaries[]"),
			"operations.stage_summaries[]", "stage", "pending", "dead_letter", "failed")
		backlogs := child(t, props, "domain_backlogs", "operations")
		requireKeys(t, propsOf(t, itemsOf(t, backlogs, "operations.domain_backlogs"), "operations.domain_backlogs[]"),
			"operations.domain_backlogs[]", "domain", "outstanding", "in_flight", "dead_letter", "oldest_age")
	})

	t.Run("index status queue family", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/status/index", "get"), "index")
		queue := child(t, props, "queue", "index")
		requireKeys(t, propsOf(t, queue, "index.queue"),
			"index.queue", "total", "dead_letter", "overdue_claims")
		scopeActivity := child(t, props, "scope_activity", "index")
		requireKeys(t, propsOf(t, scopeActivity, "index.scope_activity"),
			"index.scope_activity", "active", "changed", "unchanged")
		blockages := child(t, props, "queue_blockages", "index")
		requireKeys(t, propsOf(t, itemsOf(t, blockages, "index.queue_blockages"), "index.queue_blockages[]"),
			"index.queue_blockages[]", "stage", "domain", "conflict_key", "blocked", "oldest_age")
	})

	t.Run("operator control plane read model", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/status/operator-control-plane", "get"), "control-plane")
		queue := child(t, props, "queue", "control-plane")
		queueProps := propsOf(t, queue, "control-plane.queue")
		requireKeys(t, queueProps, "control-plane.queue",
			"total", "outstanding", "pending", "in_flight", "retrying", "dead_letter")
		latency := child(t, queueProps, "claim_latency", "control-plane.queue")
		requireKeys(t, propsOf(t, latency, "control-plane.queue.claim_latency"), "control-plane.queue.claim_latency",
			"overdue_claims", "oldest_outstanding_age", "oldest_outstanding_age_ms", "coordinator_oldest_pending")
		stuck := child(t, queueProps, "stuck", "control-plane.queue")
		requireKeys(t, propsOf(t, stuck, "control-plane.queue.stuck"), "control-plane.queue.stuck",
			"oldest_outstanding_age", "blocked_conflicts")
		families := child(t, props, "collector_families", "control-plane")
		familyProps := propsOf(t, itemsOf(t, families, "control-plane.collector_families"), "control-plane.collector_families[]")
		requireKeys(t, familyProps, "control-plane.collector_families[]",
			"collector_kind", "promotion_state", "health", "telemetry", "display_name", "blockers")
		requireArrayOfStrings(t, familyProps, "control-plane.collector_families[]", "telemetry")
		dead := child(t, props, "dead_letters", "control-plane")
		deadProps := propsOf(t, dead, "control-plane.dead_letters")
		requireKeys(t, deadProps, "control-plane.dead_letters",
			"queue_dead_letter", "by_domain", "collector_generation", "latest_failure")
		byDomain := child(t, deadProps, "by_domain", "control-plane.dead_letters")
		requireKeys(t, propsOf(t, itemsOf(t, byDomain, "control-plane.dead_letters.by_domain"), "control-plane.dead_letters.by_domain[]"),
			"control-plane.dead_letters.by_domain[]", "domain", "dead_letter", "oldest_age")
		generation := child(t, deadProps, "collector_generation", "control-plane.dead_letters")
		generationProps := propsOf(t, generation, "control-plane.dead_letters.collector_generation")
		requireKeys(t, generationProps, "control-plane.dead_letters.collector_generation",
			"dead_letter", "replay_requested", "replay_attempts")
		requireScalarTypes(t, generationProps, "control-plane.dead_letters.collector_generation", map[string]string{
			"dead_letter": "integer", "replay_requested": "integer", "replay_attempts": "integer",
			"oldest_dead_letter_age": "number", "oldest_dead_letter_age_ms": "integer",
		})
		latest := child(t, deadProps, "latest_failure", "control-plane.dead_letters")
		requireKeys(t, propsOf(t, latest, "control-plane.dead_letters.latest_failure"), "control-plane.dead_letters.latest_failure",
			"failure_class", "domain")
		policies := child(t, props, "retry_policies", "control-plane")
		requireKeys(t, propsOf(t, itemsOf(t, policies, "control-plane.retry_policies"), "control-plane.retry_policies[]"),
			"control-plane.retry_policies[]", "stage", "max_attempts", "retry_delay")
	})

	t.Run("hosted readiness coordinator and diagnostics", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/status/hosted-readiness", "get"), "hosted-readiness")
		coordinator := child(t, props, "coordinator", "hosted-readiness")
		requireKeys(t, propsOf(t, coordinator, "hosted-readiness.coordinator"),
			"hosted-readiness.coordinator", "collector_instances", "collector_backpressure",
			"source_pacing", "active_claims", "overdue_claims")
		paths := child(t, props, "diagnostic_paths", "hosted-readiness")
		requireKeys(t, propsOf(t, itemsOf(t, paths, "hosted-readiness.diagnostic_paths"), "hosted-readiness.diagnostic_paths[]"),
			"hosted-readiness.diagnostic_paths[]", "name", "path")
	})

	t.Run("retire unclaimed response body", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/admin/workflow-retire-unclaimed", "post"), "retire-unclaimed")
		requireKeys(t, props, "retire-unclaimed", "count", "items")
		items := child(t, props, "items", "retire-unclaimed")
		requireKeys(t, propsOf(t, itemsOf(t, items, "retire-unclaimed.items"), "retire-unclaimed.items[]"),
			"retire-unclaimed.items[]", "work_item_id", "scope_id")
	})

	t.Run("work item evidence rows", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/work-items/evidence", "get"), "work-item")
		evidence := child(t, props, "evidence", "work-item")
		requireKeys(t, propsOf(t, itemsOf(t, evidence, "work-item.evidence"), "work-item.evidence[]"),
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
