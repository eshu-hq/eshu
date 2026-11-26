// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"testing"
)

// TestContractShapesLaneb10Status pins the lane-B10 shape-completeness fixes
// for the status leaf fragments outside lane-5: every 200-response property
// carries a description naming its emitting owner, and fixed-key objects are
// pinned exact. Each entry names the Go owner that emits the keys.
func TestContractShapesLaneb10Status(t *testing.T) {
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

	t.Run("collector extraction readiness", func(t *testing.T) {
		t.Parallel()
		// Owner: query/collector_extraction_readiness.go
		// CollectorExtractionReadinessListResponse plus the limit/truncation
		// logic in list; family rows are extraction.Readiness and checklist
		// rows are extraction.CriterionResult.
		props := propsOf(t, schemaAt(t, "/api/v0/collector-extraction-readiness", "get", "200"), "extraction-readiness")
		requireExactKeys(t, props, "extraction-readiness",
			"schema_version", "status", "families", "count", "total_count", "limit", "truncated")
		requireDescribed(t, props, "extraction-readiness",
			"schema_version", "status", "families", "count", "total_count", "limit", "truncated")
		requireScalarTypes(t, props, "extraction-readiness", map[string]string{
			"schema_version": "string", "status": "string", "families": "array",
			"count": "integer", "total_count": "integer", "limit": "integer", "truncated": "boolean",
		})
		family := itemsOf(t, child(t, props, "families", "extraction-readiness"), "extraction-readiness.families")
		familyProps := propsOf(t, family, "extraction-readiness.families[]")
		requireKeys(t, familyProps, "extraction-readiness.families[]",
			"family", "display_name", "classification", "criteria", "rationale")
		requireDescribed(t, familyProps, "extraction-readiness.families[]",
			"family", "display_name", "classification", "criteria", "blockers", "rationale")
		criterion := itemsOf(t, child(t, familyProps, "criteria", "extraction-readiness.families[]"), "extraction-readiness.criteria")
		criterionProps := propsOf(t, criterion, "extraction-readiness.criteria[]")
		requireKeys(t, criterionProps, "extraction-readiness.criteria[]", "criterion", "state")
		requireDescribed(t, criterionProps, "extraction-readiness.criteria[]", "criterion", "state", "detail")

		// Owner: CollectorExtractionReadinessFamilyResponse.
		drillProps := propsOf(t, schemaAt(t, "/api/v0/collector-extraction-readiness/{family}", "get", "200"), "extraction-readiness-family")
		requireExactKeys(t, drillProps, "extraction-readiness-family", "schema_version", "status", "family")
		requireDescribed(t, drillProps, "extraction-readiness-family", "schema_version", "status", "family")
		drillFamilyProps := propsOf(t, child(t, drillProps, "family", "extraction-readiness-family"), "extraction-readiness-family.family")
		requireKeys(t, drillFamilyProps, "extraction-readiness-family.family",
			"family", "display_name", "classification", "criteria", "rationale")
	})

	t.Run("delta union completeness", func(t *testing.T) {
		t.Parallel()
		// Owner: recovery.DeltaUnionCompletenessReport served by
		// admin.deltaUnionCompleteness. Base/active generations serialize
		// with encoding/json defaults (capitalized keys) because
		// recovery.DeltaUnionGeneration carries no json tags.
		props := propsOf(t, schemaAt(t, "/api/v0/admin/delta-union/completeness", "get", "200"), "delta-union")
		requireExactKeys(t, props, "delta-union", "repos", "mismatches")
		requireDescribed(t, props, "delta-union", "repos", "mismatches")
		requireScalarTypes(t, props, "delta-union", map[string]string{
			"repos": "array", "mismatches": "integer",
		})
		repo := itemsOf(t, child(t, props, "repos", "delta-union"), "delta-union.repos")
		repoProps := propsOf(t, repo, "delta-union.repos[]")
		requireKeys(t, repoProps, "delta-union.repos[]",
			"repo_id", "scope_id", "outcome", "base", "active", "graph_files", "active_files", "annotation")
		requireDescribed(t, repoProps, "delta-union.repos[]",
			"repo_id", "scope_id", "outcome", "base", "active", "graph_files", "active_files",
			"missing", "extra", "drifted", "annotation")
		for _, gen := range []string{"base", "active"} {
			genProps := propsOf(t, child(t, repoProps, gen, "delta-union.repos[]"), "delta-union.repos[]."+gen)
			requireExactKeys(t, genProps, "delta-union.repos[]."+gen, "GenerationID", "Status", "IsDelta")
		}
	})

	t.Run("hosted readiness", func(t *testing.T) {
		t.Parallel()
		// Owner: query.hostedReadinessReport built by
		// buildHostedReadinessReport. active_work_source and
		// coordinator_source are omitempty and absent when the reader
		// reports none, so they are presence-optional.
		props := propsOf(t, schemaAt(t, "/api/v0/status/hosted-readiness", "get", "200"), "hosted-readiness")
		requireKeys(t, props, "hosted-readiness",
			"version", "state", "ready", "summary", "generated_at", "failure_classes",
			"repository_count", "queue", "coordinator", "checks", "diagnostic_paths")
		requireDescribed(t, props, "hosted-readiness",
			"version", "state", "ready", "summary", "generated_at", "failure_classes",
			"repository_count", "queue", "coordinator", "checks", "diagnostic_paths")
		requireScalarTypes(t, props, "hosted-readiness", map[string]string{
			"version": "string", "state": "string", "ready": "boolean", "summary": "string",
			"generated_at": "string", "failure_classes": "array", "repository_count": "integer",
			"queue": "object", "coordinator": "object", "checks": "array", "diagnostic_paths": "array",
		})
		check := itemsOf(t, child(t, props, "checks", "hosted-readiness"), "hosted-readiness.checks")
		checkProps := propsOf(t, check, "hosted-readiness.checks[]")
		requireKeys(t, checkProps, "hosted-readiness.checks[]", "name", "state", "detail")
		requireDescribed(t, checkProps, "hosted-readiness.checks[]",
			"name", "state", "failure_class", "detail", "next_diagnostic")
	})

	t.Run("operator control plane", func(t *testing.T) {
		t.Parallel()
		// Owner: query.operatorControlPlaneToMap and its mappers.
		// active_work_source is conditional (withActiveWorkSource), the
		// collector-family display_name/blockers and the latest_failure
		// identifiers are withheld from scoped callers, so those are
		// presence-optional.
		props := propsOf(t, schemaAt(t, "/api/v0/status/operator-control-plane", "get", "200"), "control-plane")
		requireKeys(t, props, "control-plane",
			"version", "as_of", "scoped", "health", "queue", "reducer_domains",
			"collector_families", "dead_letters", "retry_policies")
		requireDescribed(t, props, "control-plane",
			"version", "as_of", "scoped", "health", "queue", "reducer_domains",
			"collector_families", "dead_letters", "retry_policies")
		queueProps := propsOf(t, child(t, props, "queue", "control-plane"), "control-plane.queue")
		requireExactKeys(t, queueProps, "control-plane.queue",
			"total", "outstanding", "pending", "in_flight", "retrying", "dead_letter",
			"claim_latency", "stuck")
		latencyProps := propsOf(t, child(t, queueProps, "claim_latency", "control-plane.queue"), "control-plane.queue.claim_latency")
		requireExactKeys(t, latencyProps, "control-plane.queue.claim_latency",
			"overdue_claims", "oldest_outstanding_age", "oldest_outstanding_age_ms", "coordinator_oldest_pending")
		stuckProps := propsOf(t, child(t, queueProps, "stuck", "control-plane.queue"), "control-plane.queue.stuck")
		requireExactKeys(t, stuckProps, "control-plane.queue.stuck", "oldest_outstanding_age", "blocked_conflicts")
		domainProps := propsOf(t, itemsOf(t, child(t, props, "reducer_domains", "control-plane"), "control-plane.reducer_domains"), "control-plane.reducer_domains[]")
		requireExactKeys(t, domainProps, "control-plane.reducer_domains[]",
			"domain", "outstanding", "in_flight", "retrying", "dead_letter", "failed", "oldest_age")
		familyProps := propsOf(t, itemsOf(t, child(t, props, "collector_families", "control-plane"), "control-plane.collector_families"), "control-plane.collector_families[]")
		requireKeys(t, familyProps, "control-plane.collector_families[]",
			"collector_kind", "promotion_state", "health", "claim_state", "reducer_readback",
			"last_observed_at", "telemetry")
		requireDescribed(t, familyProps, "control-plane.collector_families[]",
			"collector_kind", "promotion_state", "health", "claim_state", "reducer_readback",
			"last_observed_at", "telemetry", "display_name", "blockers")
		deadProps := propsOf(t, child(t, props, "dead_letters", "control-plane"), "control-plane.dead_letters")
		requireExactKeys(t, deadProps, "control-plane.dead_letters",
			"queue_dead_letter", "by_domain", "collector_generation", "latest_failure")
		genProps := propsOf(t, child(t, deadProps, "collector_generation", "control-plane.dead_letters"), "control-plane.dead_letters.collector_generation")
		requireExactKeys(t, genProps, "control-plane.dead_letters.collector_generation",
			"dead_letter", "replay_requested", "replay_attempts", "oldest_dead_letter_age", "oldest_dead_letter_age_ms")
		requireDescribed(t, genProps, "control-plane.dead_letters.collector_generation",
			"dead_letter", "replay_requested", "replay_attempts", "oldest_dead_letter_age", "oldest_dead_letter_age_ms")
		policyProps := propsOf(t, itemsOf(t, child(t, props, "retry_policies", "control-plane"), "control-plane.retry_policies"), "control-plane.retry_policies[]")
		requireExactKeys(t, policyProps, "control-plane.retry_policies[]",
			"stage", "max_attempts", "retry_delay", "retry_delay_ms", "retry_delay_seconds")
		requireDescribed(t, policyProps, "control-plane.retry_policies[]",
			"stage", "max_attempts", "retry_delay", "retry_delay_ms", "retry_delay_seconds")
	})
}
