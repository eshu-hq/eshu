// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestContractShapesLane5Status pins the lane-5 shape-completeness fixes for
// the status leaf outside the slices-2 files (Routes, IndexLegacy, Semantic,
// SharedShapes, Operations): every 200-response object schema names its
// properties (no bare {"type": "object"}), every property carries a
// description, and fixed key sets are pinned exact. Each entry names the Go
// owner that emits the keys the contract asserts.
func TestContractShapesLane5Status(t *testing.T) {
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

	// requireDescribed enforces the lane-5 bar that every property on a
	// 200-response schema carries a description naming what the owner
	// emits. It recurses into object properties and array items.
	var requireDescribed func(t *testing.T, node map[string]any, where string)
	requireDescribed = func(t *testing.T, node map[string]any, where string) {
		t.Helper()
		if node["type"] == "object" {
			_, hasProps := node["properties"]
			_, hasAdditional := node["additionalProperties"]
			if !hasProps && !hasAdditional && len(node) > 1 {
				t.Errorf("%s: bare object with no properties", where)
			}
		}
		props, ok := node["properties"].(map[string]any)
		if !ok {
			return
		}
		for key, raw := range props {
			sub, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if _, isRef := sub["$ref"]; isRef {
				continue
			}
			if desc, _ := sub["description"].(string); desc == "" {
				t.Errorf("%s: property %q has no description", where, key)
			}
			if sub["type"] == "object" {
				requireDescribed(t, sub, where+"/"+key)
			}
			if sub["type"] == "array" {
				if items, ok := sub["items"].(map[string]any); ok {
					if _, isRef := items["$ref"]; !isRef && items["type"] == "object" {
						requireDescribed(t, items, where+"/"+key+"[]")
					}
				}
			}
		}
	}

	t.Run("collector alias matches main block", func(t *testing.T) {
		t.Parallel()
		// Owner: StatusHandler.getCollectorReadiness serves both routes
		// (go/internal/query/status.go:116-117), so the alias contract
		// must carry the same schema as the canonical route.
		doc := fragment(t, CollectorReadiness, "CollectorReadiness")
		main := schemaAt(t, doc, "/api/v0/status/collector-readiness", "get")
		alias := schemaAt(t, doc, "/api/v0/collector-readiness", "get")
		if !reflect.DeepEqual(main, alias) {
			t.Errorf("alias /api/v0/collector-readiness 200 schema differs from the canonical route")
		}
	})

	t.Run("collector main block names owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: collectorReadinessEntry struct plus the payload assembly
		// in go/internal/query/status_collector_readiness.go:56-88,171-176.
		doc := fragment(t, CollectorReadiness, "CollectorReadiness")
		schema := schemaAt(t, doc, "/api/v0/status/collector-readiness", "get")
		props := propsOf(t, schema, "/api/v0/status/collector-readiness")
		requireExactKeys(t, props, "collector-readiness",
			"version", "generated_at", "count", "readiness")
		requireScalarTypes(t, props, "collector-readiness", map[string]string{
			"version": "string", "generated_at": "string",
			"count": "integer", "readiness": "array",
		})
		items := itemsOf(t, child(t, props, "readiness", "collector-readiness"), "readiness")
		entry := propsOf(t, items, "readiness[]")
		requireExactKeys(t, entry, "readiness[]",
			"collector_kind", "instance_id", "display_name", "promotion_state",
			"runtime_category", "health", "claim_driven", "claim_state",
			"source_scope", "fixture_only", "evidence_sources", "source_systems",
			"observation_count", "reducer_readback", "telemetry_handles", "blockers",
			"recommended_next_action", "last_proof_at", "updated_at",
			"last_failure_class", "last_failure_operation", "last_failure_at",
			"acknowledged_failure")
		ack := propsOf(t, child(t, entry, "acknowledged_failure", "readiness[]"), "acknowledged_failure")
		requireExactKeys(t, ack, "acknowledged_failure",
			"failure_class", "failure_operation", "failed_at", "acknowledged_at")
		requireDescribed(t, schema, "/api/v0/status/collector-readiness")
	})

	t.Run("governance sections name owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: buildGovernanceStatus and its section mappers in
		// go/internal/query/status_governance.go:125-288.
		doc := fragment(t, Governance, "Governance")
		schema := schemaAt(t, doc, "/api/v0/status/governance", "get")
		props := propsOf(t, schema, "/api/v0/status/governance")
		requireExactKeys(t, props, "governance",
			"mode", "state", "source_kind", "policy_revision_hash",
			"readiness", "identity", "tenancy", "egress", "semantic",
			"extensions", "redaction", "retention", "audit", "aggregates",
			"reasons", "all_scope_route_policy",
			"supported_modes", "supported_states")
		requireExactKeys(t,
			propsOf(t, child(t, props, "readiness", "governance"), "readiness"),
			"governance.readiness",
			"identity", "tenant", "egress", "semantic",
			"extensions", "redaction", "retention", "audit")
		requireScalarTypes(t,
			propsOf(t, child(t, props, "readiness", "governance"), "readiness"),
			"governance.readiness", map[string]string{
				"identity": "boolean", "tenant": "boolean", "egress": "boolean",
				"semantic": "boolean", "extensions": "boolean",
				"redaction": "boolean", "retention": "boolean", "audit": "boolean",
			})
		requireExactKeys(t,
			propsOf(t, child(t, props, "identity", "governance"), "identity"),
			"governance.identity",
			"auth_mode", "configured", "shared_token_limitation")
		requireExactKeys(t,
			propsOf(t, child(t, props, "tenancy", "governance"), "tenancy"),
			"governance.tenancy", "tenant_mode", "workspace_mode")
		requireExactKeys(t,
			propsOf(t, child(t, props, "egress", "governance"), "egress"),
			"governance.egress", "mode")
		requireExactKeys(t,
			propsOf(t, child(t, props, "semantic", "governance"), "semantic"),
			"governance.semantic",
			"state", "reason", "provider_configured",
			"provider_profile_count", "policy_denied_count",
			"audit_actor_class_count")
		requireExactKeys(t,
			propsOf(t, child(t, props, "extensions", "governance"), "extensions"),
			"governance.extensions", "mode")
		requireExactKeys(t,
			propsOf(t, child(t, props, "redaction", "governance"), "redaction"),
			"governance.redaction", "state")
		requireExactKeys(t,
			propsOf(t, child(t, props, "retention", "governance"), "retention"),
			"governance.retention", "mode")
		requireExactKeys(t,
			propsOf(t, child(t, props, "audit", "governance"), "audit"),
			"governance.audit",
			"state", "event_count", "denied_decision_count",
			"unavailable_decision_count", "event_type_count",
			"actor_class_count", "scope_class_count", "reason_count",
			"acl_state_count")
		requireExactKeys(t,
			propsOf(t, child(t, props, "aggregates", "governance"), "aggregates"),
			"governance.aggregates",
			"policy_section_count", "denied_decision_count",
			"stale_section_count")
		requireDescribed(t, schema, "/api/v0/status/governance")
	})

	t.Run("probes names owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: the /readyz probe handler serving service, probe,
		// status, and error on failure.
		doc := fragment(t, Probes, "Probes")
		schema := schemaAt(t, doc, "/readyz", "get")
		props := propsOf(t, schema, "/readyz")
		requireExactKeys(t, props, "readyz", "service", "probe", "status")
		requireDescribed(t, schema, "/readyz")
	})

	t.Run("metrics series names owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: the metrics time-series handler projecting one named
		// metric window into ordered {t, v} points.
		doc := fragment(t, Metrics, "Metrics")
		schema := schemaAt(t, doc, "/api/v0/metrics/timeseries", "get")
		props := propsOf(t, schema, "/api/v0/metrics/timeseries")
		requireExactKeys(t, props, "metrics",
			"metric", "unit", "window", "step", "points")
		points := itemsOf(t, child(t, props, "points", "metrics"), "points")
		requireExactKeys(t, propsOf(t, points, "points[]"), "points[]", "t", "v")
		requireScalarTypes(t, propsOf(t, points, "points[]"), "points[]",
			map[string]string{"t": "string", "v": "number"})
		requireDescribed(t, schema, "/api/v0/metrics/timeseries")
	})

	t.Run("fact schema versions names owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: the fact-schema-version handlers reading the static
		// in-binary registry from go/internal/facts.
		doc := fragment(t, FactSchemaVersion, "FactSchemaVersion")
		list := schemaAt(t, doc, "/api/v0/fact-schema-versions", "get")
		listProps := propsOf(t, list, "/api/v0/fact-schema-versions")
		requireExactKeys(t, listProps, "fact-schema-versions",
			"schema_version", "status", "fact_schema_versions",
			"count", "total_count", "limit", "truncated")
		rows := itemsOf(t, child(t, listProps, "fact_schema_versions", "list"), "fact_schema_versions")
		requireExactKeys(t, propsOf(t, rows, "fact_schema_versions[]"),
			"fact_schema_versions[]", "fact_kind", "schema_version")
		requireDescribed(t, list, "/api/v0/fact-schema-versions")
		one := schemaAt(t, doc, "/api/v0/fact-schema-versions/{fact_kind}", "get")
		oneProps := propsOf(t, one, "/api/v0/fact-schema-versions/{fact_kind}")
		requireExactKeys(t, oneProps, "fact-schema-version",
			"schema_version", "status", "fact_kind",
			"supported_version", "candidate", "compatibility")
		requireDescribed(t, one, "/api/v0/fact-schema-versions/{fact_kind}")
	})

	t.Run("answer narration names owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: the answer-narration status handler serving the
		// governed narration posture read model.
		doc := fragment(t, AnswerNarration, "AnswerNarration")
		schema := schemaAt(t, doc, "/api/v0/status/answer-narration", "get")
		props := propsOf(t, schema, "/api/v0/status/answer-narration")
		requireExactKeys(t, props, "answer-narration",
			"state", "reason", "provider_configured", "provider_traffic_enabled",
			"policy_allowed", "budget_available", "publish_safety_enabled",
			"deterministic_fallback_available", "canonical_truth_affected",
			"retention_posture", "policy_hash",
			"supported_states", "supported_reasons", "validator_reason_codes",
			"detail", "updated_at")
		requireDescribed(t, schema, "/api/v0/status/answer-narration")
	})

	t.Run("reindex progress names owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: the reindex-progress handler comparing each named
		// scope's newest activated full against its effective watermark.
		doc := fragment(t, ReindexProgress, "ReindexProgress")
		schema := schemaAt(t, doc, "/api/v0/admin/reindex/progress", "get")
		props := propsOf(t, schema, "/api/v0/admin/reindex/progress")
		requireExactKeys(t, props, "reindex-progress",
			"status", "fleet_requested_at", "counts", "scopes", "truncated")
		counts := propsOf(t, child(t, props, "counts", "reindex-progress"), "counts")
		requireExactKeys(t, counts, "counts", "satisfied", "pending", "never_seen")
		scopes := itemsOf(t, child(t, props, "scopes", "reindex-progress"), "scopes")
		requireExactKeys(t, propsOf(t, scopes, "scopes[]"), "scopes[]",
			"scope_id", "state", "requested_at",
			"last_stored_full_at", "active_generation_id")
		requireDescribed(t, schema, "/api/v0/admin/reindex/progress")
	})

	t.Run("terminal failure acknowledgement names owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: the collector-failures handlers in admin/acknowledgement
		// serving the stored acknowledgement read model (#8830).
		doc := fragment(t, CollectorTerminalFailureAcknowledgement,
			"CollectorTerminalFailureAcknowledgement")
		ack := schemaAt(t, doc, "/api/v0/admin/collector-failures/acknowledge", "post")
		ackProps := propsOf(t, ack, "acknowledge")
		requireExactKeys(t, ackProps, "acknowledge",
			"status", "idempotency_key", "acknowledgement")
		entry := propsOf(t,
			child(t, ackProps, "acknowledgement", "acknowledge"), "acknowledgement")
		requireExactKeys(t, entry, "acknowledgement",
			"collector_kind", "instance_id", "scope_id", "work_item_id",
			"failure_class", "failure_operation", "failed_at", "acknowledged_at")
		requireDescribed(t, ack, "acknowledge")
		list := schemaAt(t, doc,
			"/api/v0/admin/collector-failures/acknowledgements/query", "post")
		listProps := propsOf(t, list, "acknowledgements/query")
		requireExactKeys(t, listProps, "acknowledgements/query",
			"count", "acknowledgements")
		rows := itemsOf(t,
			child(t, listProps, "acknowledgements", "acknowledgements/query"),
			"acknowledgements")
		requireExactKeys(t, propsOf(t, rows, "acknowledgements[]"), "acknowledgements[]",
			"collector_kind", "instance_id", "scope_id", "failure_class",
			"failed_at", "work_item_id", "reason", "actor_class",
			"correlation_id", "acknowledged_at")
		requireDescribed(t, list, "acknowledgements/query")
		query := schemaAt(t, doc, "/api/v0/admin/collector-failures/query", "post")
		queryProps := propsOf(t, query, "collector-failures/query")
		requireExactKeys(t, queryProps, "collector-failures/query",
			"count", "truncated", "next_cursor", "items")
		failed := itemsOf(t,
			child(t, queryProps, "items", "collector-failures/query"), "items")
		requireExactKeys(t, propsOf(t, failed, "items[]"), "items[]",
			"scope_id", "failure_class", "failure_operation", "failed_at",
			"work_item_id", "acknowledged", "acknowledged_at")
		requireDescribed(t, query, "collector-failures/query")
	})
}
