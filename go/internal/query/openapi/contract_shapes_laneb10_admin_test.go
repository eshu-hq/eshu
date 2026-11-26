// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"testing"
)

// TestContractShapesLaneb10Admin pins the lane-B10 shape-completeness fixes
// for the admin fragments: every 200/202-response property carries a
// description naming its emitting owner, and fixed-key objects are pinned
// exact. Each entry names the Go owner that emits the keys.
func TestContractShapesLaneb10Admin(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("spec has no paths object")
	}

	child := func(t *testing.T, node map[string]any, key, where string) map[string]any {
		t.Helper()
		next, ok := node[key].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing or non-object property %q", where, key)
		}
		return next
	}

	schemaAt := func(t *testing.T, path, method, status string) map[string]any {
		t.Helper()
		route, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatalf("spec paths missing %s", path)
		}
		op := child(t, route, method, path)
		responses := child(t, op, "responses", path+" "+method)
		okResp := child(t, responses, status, path+" "+method)
		content, ok := okResp["content"].(map[string]any)
		if !ok {
			t.Fatalf("%s %s %s response has no content (only a description)", path, method, status)
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
				continue
			}
			if desc, _ := node["description"].(string); desc == "" {
				t.Errorf("%s: property %q has no description", where, key)
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

	t.Run("refinalize", func(t *testing.T) {
		t.Parallel()
		// Owner: admin.refinalize serving recovery.RefinalizeResult.
		props := propsOf(t, schemaAt(t, "/api/v0/admin/refinalize", "post", "200"), "refinalize")
		requireExactKeys(t, props, "refinalize",
			"status", "enqueued", "scope_ids", "reducer_work_deleted", "shared_intents_reopened",
			"readiness_phases_cleared", "generations_retired", "shared_projection_acceptance_cleared",
			"skipped_scopes", "delta_active_scopes", "reindex_requests_written")
		// skipped_scopes, delta_active_scopes, and reindex_requests_written
		// are $refs whose targets (components_recovery.go) carry the
		// description, matching the no-sibling-description precedent for
		// $refs elsewhere in this file.
		requireDescribed(t, props, "refinalize",
			"status", "enqueued", "scope_ids", "reducer_work_deleted", "shared_intents_reopened",
			"readiness_phases_cleared", "generations_retired", "shared_projection_acceptance_cleared")
		requireScalarTypes(t, props, "refinalize", map[string]string{
			"status": "string", "enqueued": "integer", "scope_ids": "array",
			"reducer_work_deleted": "integer", "shared_intents_reopened": "integer",
			"readiness_phases_cleared": "integer", "generations_retired": "integer",
			"shared_projection_acceptance_cleared": "integer",
		})
	})

	t.Run("reindex accepted", func(t *testing.T) {
		t.Parallel()
		// Owner: admin.reindex (workspace) and admin.reindexRepositories.
		schema := schemaAt(t, "/api/v0/admin/reindex", "post", "202")
		props := propsOf(t, schema, "reindex")
		requireKeys(t, props, "reindex", "status", "ingester", "scope", "force", "detail")
		requireDescribed(t, props, "reindex",
			"status", "ingester", "scope", "force", "requested_at", "repositories", "detail")
		oneOf, ok := schema["oneOf"].([]any)
		if !ok || len(oneOf) != 2 {
			t.Fatalf("reindex: 202 schema has no two-variant oneOf")
		}
		for i, variant := range oneOf {
			vm, ok := variant.(map[string]any)
			if !ok {
				t.Fatalf("reindex: variant %d is not an object", i)
			}
			vprops := propsOf(t, vm, "reindex")
			requireDescribed(t, vprops, "reindex",
				"status", "ingester", "scope", "force", "requested_at", "repositories", "detail")
		}
	})

	t.Run("recover generations", func(t *testing.T) {
		t.Parallel()
		// Owner: admin.recoverGenerations (performed) and
		// admin.respondDuplicateRecoverGenerations (idempotent replay).
		schema := schemaAt(t, "/api/v0/admin/recover-generations", "post", "200")
		variants, ok := schema["oneOf"].([]any)
		if !ok || len(variants) != 2 {
			t.Fatalf("recover-generations: 200 schema has no two-variant oneOf")
		}
		performed, ok := variants[0].(map[string]any)
		if !ok {
			t.Fatal("recover-generations: first oneOf variant is not an object")
		}
		performedProps := propsOf(t, performed, "recover-generations.performed")
		requireExactKeys(t, performedProps, "recover-generations.performed",
			"status", "enqueued", "scope_ids", "reducer_work_deleted", "shared_intents_reopened",
			"readiness_phases_cleared", "generations_retired", "shared_projection_acceptance_cleared",
			"skipped_scopes", "delta_active_scopes", "reindex_requests_written",
			"idempotency_key", "duplicate")
		requireDescribed(t, performedProps, "recover-generations.performed",
			"status", "enqueued", "scope_ids", "reducer_work_deleted", "shared_intents_reopened",
			"readiness_phases_cleared", "generations_retired", "shared_projection_acceptance_cleared",
			"idempotency_key", "duplicate")
		replay, ok := variants[1].(map[string]any)
		if !ok {
			t.Fatal("recover-generations: second oneOf variant is not an object")
		}
		replayProps := propsOf(t, replay, "recover-generations.replay")
		requireExactKeys(t, replayProps, "recover-generations.replay",
			"status", "enqueued", "scope_ids", "idempotency_key", "duplicate")
		requireDescribed(t, replayProps, "recover-generations.replay",
			"status", "enqueued", "scope_ids", "idempotency_key", "duplicate")
	})

	t.Run("work items and decisions", func(t *testing.T) {
		t.Parallel()
		// Owner: admin.listWorkItems via admin.workItemsToSlice. The mapper
		// emits nine fixed keys; lease/failure/note/prior/visible keys are
		// conditional. replay_attempt_baseline lives on the WorkItem struct
		// but the mapper does not project it.
		props := propsOf(t, schemaAt(t, "/api/v0/admin/work-items/query", "post", "200"), "work-items")
		requireExactKeys(t, props, "work-items", "count", "items")
		requireDescribed(t, props, "work-items", "count", "items")
		itemProps := propsOf(t, itemsOf(t, child(t, props, "items", "work-items"), "work-items.items"), "work-items.items[]")
		requireKeys(t, itemProps, "work-items.items[]",
			"work_item_id", "scope_id", "generation_id", "stage", "domain", "status",
			"attempt_count", "created_at", "updated_at")
		requireDescribed(t, itemProps, "work-items.items[]",
			"work_item_id", "scope_id", "generation_id", "stage", "domain", "status",
			"attempt_count", "lease_owner", "failure_class", "failure_message", "operator_note",
			"prior_failure", "created_at", "updated_at", "visible_at")

		// Owner: admin.listDecisions via admin.decisionRowToMap; the evidence
		// member appears only with include_evidence (admin.evidenceRowsToSlice).
		decProps := propsOf(t, schemaAt(t, "/api/v0/admin/decisions/query", "post", "200"), "decisions")
		requireExactKeys(t, decProps, "decisions", "count", "decisions")
		requireDescribed(t, decProps, "decisions", "count", "decisions")
		decItemProps := propsOf(t, itemsOf(t, child(t, decProps, "decisions", "decisions"), "decisions.decisions"), "decisions.decisions[]")
		requireExactKeys(t, decItemProps, "decisions.decisions[]",
			"decision_id", "decision_type", "repository_id", "source_run_id", "work_item_id",
			"subject", "confidence_score", "confidence_reason", "provenance_summary", "created_at", "evidence")
		requireDescribed(t, decItemProps, "decisions.decisions[]",
			"decision_id", "decision_type", "repository_id", "source_run_id", "work_item_id",
			"subject", "confidence_score", "confidence_reason", "provenance_summary", "created_at", "evidence")
		requireScalarTypes(t, decItemProps, "decisions.decisions[]", map[string]string{
			"confidence_score": "number",
		})
	})

	t.Run("bounded reads", func(t *testing.T) {
		t.Parallel()
		// Owner: admin.listDeadLetters via admin.deadLetterItemsToSlice;
		// failure_class and visible_at are conditional.
		deadProps := propsOf(t, schemaAt(t, "/api/v0/admin/dead-letters/query", "post", "200"), "dead-letters")
		requireExactKeys(t, deadProps, "dead-letters",
			"schema_version", "limit", "count", "truncated", "items")
		requireDescribed(t, deadProps, "dead-letters",
			"schema_version", "limit", "count", "truncated", "items")
		deadItemProps := propsOf(t, itemsOf(t, child(t, deadProps, "items", "dead-letters"), "dead-letters.items"), "dead-letters.items[]")
		requireKeys(t, deadItemProps, "dead-letters.items[]",
			"work_item_id", "scope_id", "generation_id", "stage", "domain",
			"collector_kind", "attempt_count", "created_at", "updated_at")
		requireDescribed(t, deadItemProps, "dead-letters.items[]",
			"work_item_id", "scope_id", "generation_id", "stage", "domain",
			"collector_kind", "attempt_count", "failure_class", "created_at", "updated_at", "visible_at")

		// Owner: admin.listInputInvalidFacts; InputInvalidFact has no
		// omitempty so every key is fixed.
		invalidProps := propsOf(t, schemaAt(t, "/api/v0/admin/input-invalid-facts/query", "post", "200"), "input-invalid")
		requireExactKeys(t, invalidProps, "input-invalid",
			"schema_version", "limit", "count", "truncated", "items")
		requireDescribed(t, invalidProps, "input-invalid",
			"schema_version", "limit", "count", "truncated", "items")
		invalidItemProps := propsOf(t, itemsOf(t, child(t, invalidProps, "items", "input-invalid"), "input-invalid.items"), "input-invalid.items[]")
		requireExactKeys(t, invalidItemProps, "input-invalid.items[]",
			"fact_id", "fact_kind", "missing_field", "failure_class", "domain",
			"scope_id", "generation_id", "decided_at")
		requireDescribed(t, invalidItemProps, "input-invalid.items[]",
			"fact_id", "fact_kind", "missing_field", "failure_class", "domain",
			"scope_id", "generation_id", "decided_at")

		// Owner: admin.listChangedSincePoisonedLinks;
		// ChangedSincePoisonedLink carries three conditional time/class
		// keys, and next_cursor appears only on a truncated page.
		linkProps := propsOf(t, schemaAt(t, "/api/v0/admin/changed-since/poisoned-links/query", "post", "200"), "poisoned-links")
		requireKeys(t, linkProps, "poisoned-links",
			"schema_version", "limit", "count", "truncated", "items")
		requireDescribed(t, linkProps, "poisoned-links",
			"schema_version", "limit", "count", "truncated", "next_cursor", "items")
		linkItemProps := propsOf(t, itemsOf(t, child(t, linkProps, "items", "poisoned-links"), "poisoned-links.items"), "poisoned-links.items[]")
		requireKeys(t, linkItemProps, "poisoned-links.items[]",
			"scope_id", "status", "activation_seq", "attempt_count", "updated_at")
		requireDescribed(t, linkItemProps, "poisoned-links.items[]",
			"scope_id", "status", "activation_seq", "attempt_count",
			"last_failure_class", "next_attempt_at", "poisoned_at", "updated_at")
		requireScalarTypes(t, linkItemProps, "poisoned-links.items[]", map[string]string{
			"activation_seq": "integer", "attempt_count": "integer",
		})
	})

	t.Run("mutations", func(t *testing.T) {
		t.Parallel()
		// Owner: admin.tuningReport serves the pinned
		// SharedProjectionTuningReportStatus/Detail constants.
		tuningProps := propsOf(t, schemaAt(t, "/api/v0/admin/shared-projection/tuning-report", "get", "200"), "tuning-report")
		requireExactKeys(t, tuningProps, "tuning-report", "status", "detail")
		requireDescribed(t, tuningProps, "tuning-report", "status", "detail")

		// Owner: admin.deadLetter and admin.skip via admin.workItemsToSlice.
		for _, route := range []string{"/api/v0/admin/dead-letter", "/api/v0/admin/skip"} {
			mutProps := propsOf(t, schemaAt(t, route, "post", "200"), route)
			requireExactKeys(t, mutProps, route, "count", "items")
			requireDescribed(t, mutProps, route, "count", "items")
		}

		// Owner: admin.backfill; scope_id, generation_id, and
		// operator_note are conditional on the request that created the row.
		backfillProps := propsOf(t, schemaAt(t, "/api/v0/admin/backfill", "post", "200"), "backfill")
		requireExactKeys(t, backfillProps, "backfill", "status", "backfill_request")
		requireDescribed(t, backfillProps, "backfill", "status", "backfill_request")
		reqProps := propsOf(t, child(t, backfillProps, "backfill_request", "backfill"), "backfill.backfill_request")
		requireKeys(t, reqProps, "backfill.backfill_request", "backfill_request_id", "created_at")
		requireDescribed(t, reqProps, "backfill.backfill_request",
			"backfill_request_id", "created_at", "scope_id", "generation_id", "operator_note")

		// Owner: admin.listReplayEvents; failure_class and operator_note
		// are conditional. The route description also names
		// replay_attempt_baseline and prior_admission_wait_attempts, which
		// the mapper does not project.
		eventProps := propsOf(t, schemaAt(t, "/api/v0/admin/replay-events/query", "post", "200"), "replay-events")
		requireExactKeys(t, eventProps, "replay-events", "count", "events")
		requireDescribed(t, eventProps, "replay-events", "count", "events")
		eventItemProps := propsOf(t, itemsOf(t, child(t, eventProps, "events", "replay-events"), "replay-events.events"), "replay-events.events[]")
		requireKeys(t, eventItemProps, "replay-events.events[]",
			"replay_event_id", "work_item_id", "scope_id", "generation_id", "created_at")
		requireDescribed(t, eventItemProps, "replay-events.events[]",
			"replay_event_id", "work_item_id", "scope_id", "generation_id",
			"failure_class", "operator_note", "created_at")
	})

	t.Run("replay", func(t *testing.T) {
		t.Parallel()
		// Owner: admin.replay (performed) and admin.respondDuplicateReplay
		// (idempotent replay); the replayed rows use admin.workItemsToSlice.
		schema := schemaAt(t, "/api/v0/admin/replay", "post", "200")
		variants, ok := schema["oneOf"].([]any)
		if !ok || len(variants) != 2 {
			t.Fatalf("replay: 200 schema has no two-variant oneOf")
		}
		performed, ok := variants[0].(map[string]any)
		if !ok {
			t.Fatal("replay: first oneOf variant is not an object")
		}
		performedProps := propsOf(t, performed, "replay.performed")
		requireExactKeys(t, performedProps, "replay.performed",
			"status", "replayed_count", "work_item_ids", "replayed", "idempotency_key", "duplicate")
		requireDescribed(t, performedProps, "replay.performed",
			"status", "replayed_count", "work_item_ids", "replayed", "idempotency_key", "duplicate")
		replay, ok := variants[1].(map[string]any)
		if !ok {
			t.Fatal("replay: second oneOf variant is not an object")
		}
		replayProps := propsOf(t, replay, "replay.replay")
		requireExactKeys(t, replayProps, "replay.replay",
			"status", "replayed_count", "work_item_ids", "idempotency_key", "duplicate")
		requireDescribed(t, replayProps, "replay.replay",
			"status", "replayed_count", "work_item_ids", "idempotency_key", "duplicate")

		// Owner: admin.replay selector refusal plus
		// admin.refuseUnsafeExplicitReplay and
		// admin.refuseSupersededExplicitReplay. generation_id appears
		// only on superseded-generation refusals.
		route, ok := paths["/api/v0/admin/replay"].(map[string]any)
		if !ok {
			t.Fatal("spec paths missing /api/v0/admin/replay")
		}
		op := child(t, route, "post", "/api/v0/admin/replay")
		resp422 := child(t, child(t, op, "responses", "replay post"), "422", "replay post")
		content422, ok := resp422["content"].(map[string]any)
		if !ok {
			t.Fatal("replay 422 has no content")
		}
		refusedProps := propsOf(t, child(t, child(t, content422, "application/json", "replay 422"), "schema", "replay 422"), "replay.422")
		requireKeys(t, refusedProps, "replay.422", "status", "reason", "detail")
		requireDescribed(t, refusedProps, "replay.422",
			"status", "reason", "detail", "failure_class", "refused_work_items")
	})

	t.Run("reopen", func(t *testing.T) {
		t.Parallel()
		// Owner: admin.reopen (performed) and admin.respondDuplicateReopen
		// (totals without the reducer/intent split); reopened_units rows
		// come from admin.reopenedUnitsToSlice.
		schema := schemaAt(t, "/api/v0/admin/reopen", "post", "200")
		variants, ok := schema["oneOf"].([]any)
		if !ok || len(variants) != 2 {
			t.Fatalf("reopen: 200 schema has no two-variant oneOf")
		}
		performed, ok := variants[0].(map[string]any)
		if !ok {
			t.Fatal("reopen: first oneOf variant is not an object")
		}
		performedProps := propsOf(t, performed, "reopen.performed")
		requireExactKeys(t, performedProps, "reopen.performed",
			"status", "domain", "scope_id", "generation_id", "reopened_reducer_count",
			"reopened_reducer_ids", "reopened_intent_count", "reopened_intent_ids",
			"reopened_units", "reopened_total_count", "idempotency_key", "duplicate")
		requireDescribed(t, performedProps, "reopen.performed",
			"status", "domain", "scope_id", "generation_id", "reopened_reducer_count",
			"reopened_reducer_ids", "reopened_intent_count", "reopened_intent_ids",
			"reopened_units", "reopened_total_count", "idempotency_key", "duplicate")
		unitProps := propsOf(t, itemsOf(t, child(t, performedProps, "reopened_units", "reopen.performed"), "reopen.reopened_units"), "reopen.reopened_units[]")
		requireExactKeys(t, unitProps, "reopen.reopened_units[]",
			"acceptance_unit_id", "source_run_id", "intent_id")
		requireDescribed(t, unitProps, "reopen.reopened_units[]",
			"acceptance_unit_id", "source_run_id", "intent_id")
		duplicate, ok := variants[1].(map[string]any)
		if !ok {
			t.Fatal("reopen: second oneOf variant is not an object")
		}
		duplicateProps := propsOf(t, duplicate, "reopen.replay")
		requireExactKeys(t, duplicateProps, "reopen.replay",
			"status", "reopened_total_count", "reopened_ids", "idempotency_key", "duplicate")
		requireDescribed(t, duplicateProps, "reopen.replay",
			"status", "reopened_total_count", "reopened_ids", "idempotency_key", "duplicate")
	})
}
