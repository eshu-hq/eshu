// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractShapesLaneB7Packets pins the lane-b7 shape-completeness fixes
// for the service-investigation packet and the work-item evidence rows: every
// 200-response object the console renders must name its properties in the
// fragment instead of a bare {"type": "object"}, and every 200-response
// property must carry a description naming its emitting owner. Each entry
// navigates the fragment JSON and requires the property keys the Go owner
// actually emits; invented shapes fail review.
func TestContractShapesLaneB7Packets(t *testing.T) {
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

	// requireOpenMap asserts a genuine open map: heterogeneous values the
	// owner cannot fix, documented with additionalProperties and a
	// description naming the key semantics.
	requireOpenMap := func(t *testing.T, props map[string]any, where, key string, keySemantics ...string) {
		t.Helper()
		node, ok := props[key].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing property %q", where, key)
		}
		if node["type"] != "object" {
			t.Errorf("%s: property %q is not an object", where, key)
		}
		if _, ok := node["additionalProperties"]; !ok {
			t.Errorf("%s: property %q is a bare object with no properties", where, key)
		}
		desc, _ := node["description"].(string)
		if desc == "" {
			t.Errorf("%s: property %q has no description", where, key)
		}
		for _, semantics := range keySemantics {
			if !strings.Contains(desc, semantics) {
				t.Errorf("%s: property %q description does not name key semantics %q: %q", where, key, semantics, desc)
			}
		}
	}

	// requireNoBareObjects fails on any {"type": "object"} without
	// properties or additionalProperties, and any array object items
	// without properties or additionalProperties.
	var requireNoBareObjects func(t *testing.T, node any, where string)
	requireNoBareObjects = func(t *testing.T, node any, where string) {
		t.Helper()
		obj, ok := node.(map[string]any)
		if !ok {
			return
		}
		if obj["type"] == "object" {
			_, hasProps := obj["properties"]
			_, hasAdditional := obj["additionalProperties"]
			if !hasProps && !hasAdditional {
				t.Errorf("%s: bare object with no properties", where)
			}
			if props, ok := obj["properties"].(map[string]any); ok {
				for key, sub := range props {
					requireNoBareObjects(t, sub, where+"."+key)
				}
			}
			if addl, ok := obj["additionalProperties"].(map[string]any); ok {
				requireNoBareObjects(t, addl, where+"[]")
			}
		}
		if obj["type"] == "array" {
			items, ok := obj["items"].(map[string]any)
			if !ok {
				t.Errorf("%s: array schema has no items", where)
				return
			}
			if items["type"] == "object" {
				_, hasProps := items["properties"]
				_, hasAdditional := items["additionalProperties"]
				if !hasProps && !hasAdditional {
					t.Errorf("%s: array has bare object items", where)
				}
			}
			requireNoBareObjects(t, items, where+"[]")
		}
		for key, sub := range obj {
			if key == "properties" || key == "items" || key == "additionalProperties" {
				continue
			}
			requireNoBareObjects(t, sub, where+"."+key)
		}
	}

	t.Run("service investigation", func(t *testing.T) {
		t.Parallel()
		// Owner: service/investigation_packet.go BuildServiceInvestigationPacket
		// and its builders: serviceInvestigationRepositories,
		// repositoriesWithEvidence, serviceInvestigationFindingsWithContext,
		// serviceInvestigationNextCalls, and serviceInvestigationCoverage.
		// Served by entity/service_investigation.go InvestigateService.
		doc := fragment(t, Investigations, "Investigations")
		schema := schemaAt(t, doc, "/api/v0/investigations/services/{service_name}", "get")
		requireNoBareObjects(t, schema, "service-investigation")
		props := propsOf(t, schema, "service-investigation")
		requireKeys(t, props, "service-investigation",
			"service_name", "environment", "intent", "question",
			"repositories_considered", "repositories_with_evidence",
			"evidence_families_found", "coverage_summary", "investigation_findings",
			"recommended_next_calls", "service_story_path", "service_context_path")
		requireDescribed(t, props, "service-investigation",
			"service_name", "environment", "intent", "question",
			"repositories_considered", "repositories_with_evidence",
			"evidence_families_found", "coverage_summary", "investigation_findings",
			"recommended_next_calls", "service_story_path", "service_context_path")
		requireScalarTypes(t, props, "service-investigation", map[string]string{
			"service_name": "string", "repositories_considered": "array",
			"repositories_with_evidence": "array", "evidence_families_found": "array",
			"coverage_summary": "object", "investigation_findings": "array",
			"recommended_next_calls": "array",
			"service_story_path":     "string", "service_context_path": "string",
		})
		for _, list := range []string{"repositories_considered", "repositories_with_evidence"} {
			repos := child(t, props, list, "service-investigation")
			repoProps := propsOf(t, itemsOf(t, repos, "service-investigation "+list), "service-investigation "+list+"[]")
			requireKeys(t, repoProps, "service-investigation "+list+"[]",
				"repo_id", "repo_name", "roles", "evidence_families", "evidence_family_count", "note")
			requireDescribed(t, repoProps, "service-investigation "+list+"[]",
				"repo_id", "repo_name", "roles", "evidence_families", "evidence_family_count", "note")
			requireScalarTypes(t, repoProps, "service-investigation "+list+"[]", map[string]string{
				"repo_id": "string", "repo_name": "string",
				"roles": "array", "evidence_families": "array",
				"evidence_family_count": "integer",
			})
		}
		coverageProps := propsOf(t, child(t, props, "coverage_summary", "service-investigation"), "service-investigation.coverage_summary")
		requireExactKeys(t, coverageProps, "service-investigation.coverage_summary",
			"state", "reason", "repository_count", "repositories_truncated",
			"repositories_with_evidence_count", "evidence_family_count",
			"result_limit", "downstream_read_limit", "evidence_file_read_limit", "truncated")
		requireDescribed(t, coverageProps, "service-investigation.coverage_summary",
			"state", "reason", "repository_count", "repositories_truncated",
			"repositories_with_evidence_count", "evidence_family_count",
			"result_limit", "downstream_read_limit", "evidence_file_read_limit", "truncated")
		requireScalarTypes(t, coverageProps, "service-investigation.coverage_summary", map[string]string{
			"state": "string", "reason": "string",
			"repository_count": "integer", "repositories_truncated": "boolean",
			"repositories_with_evidence_count": "integer", "evidence_family_count": "integer",
			"result_limit": "integer", "downstream_read_limit": "integer",
			"evidence_file_read_limit": "integer", "truncated": "boolean",
		})
		findings := child(t, props, "investigation_findings", "service-investigation")
		findingProps := propsOf(t, itemsOf(t, findings, "service-investigation findings"), "service-investigation investigation_findings[]")
		requireExactKeys(t, findingProps, "service-investigation investigation_findings[]",
			"family", "summary", "evidence_path")
		requireDescribed(t, findingProps, "service-investigation investigation_findings[]",
			"family", "summary", "evidence_path")
		nextCalls := child(t, props, "recommended_next_calls", "service-investigation")
		nextCallProps := propsOf(t, itemsOf(t, nextCalls, "service-investigation next calls"), "service-investigation recommended_next_calls[]")
		requireExactKeys(t, nextCallProps, "service-investigation recommended_next_calls[]",
			"tool", "reason", "arguments")
		requireDescribed(t, nextCallProps, "service-investigation recommended_next_calls[]",
			"tool", "reason", "arguments")
		requireOpenMap(t, nextCallProps, "service-investigation recommended_next_calls[]", "arguments", "workload_id")
	})

	t.Run("work item evidence rows", func(t *testing.T) {
		t.Parallel()
		// Owner: query/workitem/handler.go listWorkItemEvidence serves rows
		// from the workitem store; the fragment already names every emitted
		// key (slice-1), so this pin locks the exact envelope, the required
		// row keys, scalar types, and owner descriptions.
		doc := fragment(t, WorkItem, "WorkItem")
		schema := schemaAt(t, doc, "/api/v0/work-items/evidence", "get")
		requireNoBareObjects(t, schema, "work-item")
		props := propsOf(t, schema, "work-item")
		requireExactKeys(t, props, "work-item",
			"evidence", "count", "limit", "truncated", "missing_evidence", "states", "next_cursor")
		requireDescribed(t, props, "work-item",
			"evidence", "count", "limit", "truncated", "missing_evidence", "states", "next_cursor")
		requireScalarTypes(t, props, "work-item", map[string]string{
			"evidence": "array", "count": "integer", "limit": "integer",
			"truncated": "boolean", "missing_evidence": "boolean",
			"states": "array", "next_cursor": "object",
		})
		evidence := child(t, props, "evidence", "work-item")
		rowProps := propsOf(t, itemsOf(t, evidence, "work-item evidence"), "work-item evidence[]")
		requireKeys(t, rowProps, "work-item evidence[]",
			"fact_id", "fact_kind", "scope_id", "generation_id", "provider",
			"source_confidence", "observed_at", "evidence_state", "work_item_key",
			"provider_work_item_id", "project_id", "project_key",
			"issue_type_id", "issue_type_name", "status_id", "status_name",
			"created_at", "updated_at", "resolved_at", "provider_changelog_id",
			"field", "from", "to", "value_redacted", "provider_remote_link_id",
			"global_id", "application_name", "application_type", "relationship",
			"correlation_anchor_class", "linked_repository_id", "url_fingerprint",
			"url_present", "url_redacted", "title_present", "summary_present",
			"provider_support_state", "redaction_policy_version",
			"metadata_type", "warning_reason", "provider_id_fingerprint")
		requireDescribed(t, rowProps, "work-item evidence[]",
			"fact_id", "fact_kind", "scope_id", "generation_id", "provider",
			"source_confidence", "observed_at", "evidence_state", "work_item_key",
			"provider_work_item_id", "project_id", "project_key",
			"issue_type_id", "issue_type_name", "status_id", "status_name",
			"created_at", "updated_at", "resolved_at", "provider_changelog_id",
			"field", "from", "to", "value_redacted", "provider_remote_link_id",
			"global_id", "application_name", "application_type", "relationship",
			"correlation_anchor_class", "linked_repository_id", "url_fingerprint",
			"url_present", "url_redacted", "title_present", "summary_present",
			"provider_support_state", "redaction_policy_version",
			"metadata_type", "warning_reason", "provider_id_fingerprint")
		cursorProps := propsOf(t, child(t, props, "next_cursor", "work-item"), "work-item.next_cursor")
		requireExactKeys(t, cursorProps, "work-item.next_cursor", "after_fact_id")
		requireDescribed(t, cursorProps, "work-item.next_cursor", "after_fact_id")
	})
}
