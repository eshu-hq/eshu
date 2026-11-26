// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"encoding/json"
	"strings"
	"testing"
)

// Shared fragment-navigation and shape-pin helpers for the lane-2
// contract tests (contract_shapes_lane2_*_test.go).
func lane2Fragment(t *testing.T, raw, where string) map[string]any {
	t.Helper()
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimSuffix(trimmed, ",")
	var doc map[string]any
	if err := json.Unmarshal([]byte("{"+trimmed+"}"), &doc); err != nil {
		t.Fatalf("%s: fragment is not valid JSON: %v", where, err)
	}
	return doc
}

func lane2Child(t *testing.T, node map[string]any, key, where string) map[string]any {
	t.Helper()
	next, ok := node[key].(map[string]any)
	if !ok {
		t.Fatalf("%s: missing or non-object property %q", where, key)
	}
	return next
}

func lane2SchemaAt(t *testing.T, doc map[string]any, path, method string) map[string]any {
	t.Helper()
	route, ok := doc[path].(map[string]any)
	if !ok {
		t.Fatalf("fragment paths missing %s", path)
	}
	op := lane2Child(t, route, method, path)
	responses := lane2Child(t, op, "responses", path+" "+method)
	okResp := lane2Child(t, responses, "200", path+" "+method)
	content := lane2Child(t, okResp, "content", path+" "+method)
	body := lane2Child(t, content, "application/json", path+" "+method)
	return lane2Child(t, body, "schema", path+" "+method)
}

func lane2PropsOf(t *testing.T, node map[string]any, where string) map[string]any {
	t.Helper()
	props, ok := node["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s: object schema has no properties", where)
	}
	return props
}

func lane2ItemsOf(t *testing.T, node map[string]any, where string) map[string]any {
	t.Helper()
	items, ok := node["items"].(map[string]any)
	if !ok {
		t.Fatalf("%s: array schema has no items", where)
	}
	return items
}

// lane2RequireExactKeys pins the closed property set of an object whose
// owner emits a fixed key list: missing keys and phantom additions
// both fail.
func lane2RequireExactKeys(t *testing.T, props map[string]any, where string, keys ...string) {
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

// lane2RequireScalarTypes pins the JSON scalar type of hand-written
// schema properties.
func lane2RequireScalarTypes(t *testing.T, props map[string]any, where string, want map[string]string) {
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

func lane2RequireDescribed(t *testing.T, props map[string]any, where string, keys ...string) {
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

// lane2RequireNoBareObjects fails on any object schema (or array
// items) without properties or additionalProperties under node.
func lane2RequireNoBareObjects(t *testing.T, node map[string]any, where string) {
	t.Helper()
	var walk func(n map[string]any, at string)
	walk = func(n map[string]any, at string) {
		if n["type"] == "object" {
			_, hasProps := n["properties"]
			_, hasAdd := n["additionalProperties"]
			if !hasProps && !hasAdd {
				t.Errorf("%s: bare object at %q (no properties or additionalProperties)", where, at)
			}
		}
		if n["type"] == "array" {
			if items, ok := n["items"].(map[string]any); ok {
				walk(items, at+"[]")
			} else {
				t.Errorf("%s: array at %q has no items", where, at)
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

// lane2PinReadiness pins the shared packet.ReadinessEnvelope shape under
// the 200 response of path+method: closed key sets from the packet owner
// structs, scalar types, owner-naming descriptions, and no bare objects.
func lane2PinReadiness(t *testing.T, doc map[string]any, path, method, where string) {
	t.Helper()
	schema := lane2SchemaAt(t, doc, path, method)
	props := lane2PropsOf(t, schema, where)
	readiness := lane2Child(t, props, "readiness", where+".readiness")
	readinessProps := lane2PropsOf(t, readiness, where+".readiness")
	lane2RequireExactKeys(t, readinessProps, where+".readiness",
		"readiness_state", "target_scope", "evidence_sources",
		"source_snapshots", "source_states", "unsupported_targets",
		"missing_evidence", "incomplete_reasons", "freshness", "counts")
	lane2RequireDescribed(t, readinessProps, where+".readiness",
		"readiness_state", "target_scope", "evidence_sources",
		"source_snapshots", "source_states", "unsupported_targets",
		"missing_evidence", "incomplete_reasons", "freshness", "counts")

	// Owner: packet.TargetScope echoes the bounded anchors.
	scope := lane2Child(t, readinessProps, "target_scope", where+".scope")
	scopeProps := lane2PropsOf(t, scope, where+".scope")
	lane2RequireExactKeys(t, scopeProps, where+".scope",
		"cve_id", "advisory_id", "package_id", "repository_id",
		"subject_digest", "image_ref", "ecosystem", "workload_id",
		"service_id", "environment", "severity", "impact_status")
	lane2RequireDescribed(t, scopeProps, where+".scope",
		"cve_id", "advisory_id", "package_id", "repository_id",
		"subject_digest", "image_ref", "ecosystem", "workload_id",
		"service_id", "environment", "severity", "impact_status")

	// Owner: packet.EvidenceFamily.
	fam := lane2ItemsOf(t, lane2Child(t, readinessProps, "evidence_sources", where+".fam"), where+".fam[]")
	famProps := lane2PropsOf(t, fam, where+".fam[]")
	lane2RequireExactKeys(t, famProps, where+".fam[]",
		"family", "fact_count", "latest_observed_at", "freshness")
	lane2RequireScalarTypes(t, famProps, where+".fam[]", map[string]string{
		"family": "string", "fact_count": "integer",
	})
	lane2RequireDescribed(t, famProps, where+".fam[]",
		"family", "fact_count", "latest_observed_at", "freshness")

	// Owner: packet.SourceSnapshot.
	snap := lane2ItemsOf(t, lane2Child(t, readinessProps, "source_snapshots", where+".snap"), where+".snap[]")
	snapProps := lane2PropsOf(t, snap, where+".snap[]")
	lane2RequireExactKeys(t, snapProps, where+".snap[]",
		"source", "ecosystem", "cache_artifact_version", "snapshot_digest",
		"last_updated_at", "freshness", "complete", "warning_code",
		"warning_message")
	lane2RequireScalarTypes(t, snapProps, where+".snap[]", map[string]string{
		"complete": "boolean",
	})
	lane2RequireDescribed(t, snapProps, where+".snap[]",
		"source", "ecosystem", "cache_artifact_version", "snapshot_digest",
		"last_updated_at", "freshness", "complete", "warning_code",
		"warning_message")

	// Owner: packet.SourceState, including the derived-aggregate rows.
	st := lane2ItemsOf(t, lane2Child(t, readinessProps, "source_states", where+".st"), where+".st[]")
	stProps := lane2PropsOf(t, st, where+".st[]")
	lane2RequireExactKeys(t, stProps, where+".st[]",
		"collector_instance_id", "scope_id", "source", "ecosystem",
		"collection_window", "last_attempt_at", "last_success_at",
		"next_retry_at", "last_error_class", "freshness_state",
		"terminal_status", "result_count", "warning_count", "updated_at",
		"derived", "scope_count", "package_count", "unmatched_package_count",
		"fresh_count", "stale_count", "failed_count", "rate_limited_count",
		"partial_count", "pending_count", "scope_ids", "multi_cve_target",
		"skipped_package_count", "skip_reasons", "retired_releases")
	lane2RequireScalarTypes(t, stProps, where+".st[]", map[string]string{
		"result_count": "integer", "warning_count": "integer",
		"multi_cve_target": "boolean",
	})
	lane2RequireDescribed(t, stProps, where+".st[]",
		"collector_instance_id", "scope_id", "source", "ecosystem",
		"collection_window", "last_attempt_at", "last_success_at",
		"next_retry_at", "last_error_class", "freshness_state",
		"terminal_status", "result_count", "warning_count", "updated_at",
		"derived", "scope_count", "package_count", "unmatched_package_count",
		"fresh_count", "stale_count", "failed_count", "rate_limited_count",
		"partial_count", "pending_count", "scope_ids", "multi_cve_target",
		"skipped_package_count", "skip_reasons", "retired_releases")
	win := lane2Child(t, stProps, "collection_window", where+".window")
	winProps := lane2PropsOf(t, win, where+".window")
	lane2RequireExactKeys(t, winProps, where+".window", "start", "end")
	lane2RequireDescribed(t, winProps, where+".window", "start", "end")

	// Owner: packet.UnsupportedTarget.
	unsup := lane2ItemsOf(t, lane2Child(t, readinessProps, "unsupported_targets", where+".unsup"), where+".unsup[]")
	unsupProps := lane2PropsOf(t, unsup, where+".unsup[]")
	lane2RequireExactKeys(t, unsupProps, where+".unsup[]",
		"target_kind", "reason", "count", "ecosystem", "lockfile_flavor",
		"feature_token")
	lane2RequireScalarTypes(t, unsupProps, where+".unsup[]", map[string]string{
		"count": "integer",
	})
	lane2RequireDescribed(t, unsupProps, where+".unsup[]",
		"target_kind", "reason", "count", "ecosystem", "lockfile_flavor",
		"feature_token")

	// Owner: packet.ReadinessCounts.
	counts := lane2Child(t, readinessProps, "counts", where+".counts")
	countsProps := lane2PropsOf(t, counts, where+".counts")
	lane2RequireExactKeys(t, countsProps, where+".counts",
		"findings_returned", "findings_truncated", "findings_by_status",
		"evidence_facts_total")
	lane2RequireScalarTypes(t, countsProps, where+".counts", map[string]string{
		"findings_returned": "integer", "findings_truncated": "boolean",
		"findings_by_status": "object", "evidence_facts_total": "integer",
	})
	lane2RequireDescribed(t, countsProps, where+".counts",
		"findings_returned", "findings_truncated", "findings_by_status",
		"evidence_facts_total")
}
