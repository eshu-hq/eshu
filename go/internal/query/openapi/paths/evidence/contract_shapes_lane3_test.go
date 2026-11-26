// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractShapesLane3Evidence pins the lane-3 shape completion for the
// investigation-workflow catalog/resolver and the incident-context packet:
// every 200-response property names its keys in the fragment instead of a
// bare {"type": "object"}, and every property carries a description naming
// its emitting owner.
func TestContractShapesLane3Evidence(t *testing.T) {
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
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if desc, _ := node["description"].(string); desc == "" {
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

	requireAllDescribed := func(t *testing.T, props map[string]any, where string) {
		t.Helper()
		for key, raw := range props {
			node, ok := raw.(map[string]any)
			if !ok {
				t.Errorf("%s: property %q is not an object schema", where, key)
				continue
			}
			if desc, _ := node["description"].(string); desc == "" {
				t.Errorf("%s: property %q has no description", where, key)
			}
		}
	}

	t.Run("investigation workflow catalog names owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: go/internal/query/investigation_workflow_handler.go list
		// serves investigationWorkflowListResponse: schema_version,
		// workflows (compact InvestigationWorkflowSummary by default, full
		// InvestigationWorkflow on view=full), versions, count, total,
		// limit, offset, truncated, next_offset.
		doc := fragment(t, InvestigationWorkflows, "InvestigationWorkflows")
		schema := schemaAt(t, doc, "/api/v0/investigation-workflows", "get")
		props := propsOf(t, schema, "workflows catalog")
		requireKeys(t, props, "workflows catalog",
			"schema_version", "workflows", "versions", "count", "total",
			"limit", "offset", "truncated", "next_offset")
		requireAllDescribed(t, props, "workflows catalog")
		requireScalarTypes(t, props, "workflows catalog", map[string]string{
			"schema_version": "string", "count": "integer", "total": "integer",
			"limit": "integer", "offset": "integer", "truncated": "boolean",
		})

		// Owner: InvestigationWorkflowSummary (investigation_workflow.go
		// ToSummary) emits exactly id, name, version, domain, description
		// on the compact branch; the full branch (view=full) carries those
		// plus the complete InvestigationWorkflow shape. The items node is
		// a view-keyed anyOf (Lane H gap-5 fix for the prose-only shape).
		workflows := child(t, props, "workflows", "workflows catalog")
		items := itemsOf(t, workflows, "workflows")
		anyOf, ok := items["anyOf"].([]any)
		if !ok || len(anyOf) != 2 {
			t.Fatalf("workflows[] items: want a two-branch anyOf keyed on the view param (compact vs full)")
		}
		byTitle := map[string]map[string]any{}
		for _, branch := range anyOf {
			bm, ok := branch.(map[string]any)
			if !ok {
				t.Fatalf("workflows[] items: anyOf branch is not an object")
			}
			title, _ := bm["title"].(string)
			byTitle[title] = bm
		}
		compact, ok := byTitle["compact"]
		if !ok {
			t.Fatalf("workflows[] items: anyOf has no compact branch")
		}
		full, ok := byTitle["full"]
		if !ok {
			t.Fatalf("workflows[] items: anyOf has no full branch")
		}
		workflowItems := propsOf(t, compact, "workflows items compact")
		requireExactKeys(t, workflowItems, "workflows[] compact items",
			"id", "name", "version", "domain", "description")
		requireDescribed(t, workflowItems, "workflows[] compact items",
			"id", "name", "version", "domain", "description")
		fullItems := propsOf(t, full, "workflows items full")
		requireKeys(t, fullItems, "workflows[] full items",
			"id", "name", "version", "domain", "description",
			"required_inputs", "required_evidence", "optional_evidence",
			"output_packet", "tool_groups", "starter_prompts",
			"failure_modes", "missing_evidence_routes")
		requireDescribed(t, fullItems, "workflows[] full items",
			"id", "name", "version", "domain", "description",
			"required_inputs", "required_evidence", "optional_evidence",
			"output_packet", "tool_groups", "starter_prompts",
			"failure_modes", "missing_evidence_routes")

		// Owner: InvestigationWorkflowCatalogVersions emits exactly id,
		// version per ref.
		versions := child(t, props, "versions", "workflows catalog")
		versionItems := propsOf(t, itemsOf(t, versions, "versions"), "versions items")
		requireExactKeys(t, versionItems, "versions[] items", "id", "version")
		requireDescribed(t, versionItems, "versions[] items", "id", "version")
		requireScalarTypes(t, versionItems, "versions[] items", map[string]string{
			"id": "string", "version": "string",
		})
	})

	t.Run("investigation workflow resolve names owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: go/internal/query/investigation_workflow_handler.go
		// resolve serves investigationWorkflowResolveResponse with
		// ResolvedInvestigationWorkflow (investigation_workflow.go):
		// workflow_id, version, domain, output_packet,
		// recommended_next_calls, plus blocked_next_calls and
		// unmatched_missing_evidence only when non-empty.
		doc := fragment(t, InvestigationWorkflows, "InvestigationWorkflows")
		schema := schemaAt(t, doc, "/api/v0/investigation-workflows/resolve", "post")
		props := propsOf(t, schema, "workflow resolve")
		requireKeys(t, props, "workflow resolve", "schema_version", "resolved")
		requireAllDescribed(t, props, "workflow resolve")

		resolved := child(t, props, "resolved", "workflow resolve")
		resolvedProps := propsOf(t, resolved, "workflow resolve.resolved")
		requireKeys(t, resolvedProps, "workflow resolve.resolved",
			"workflow_id", "version", "domain", "output_packet",
			"recommended_next_calls", "blocked_next_calls", "unmatched_missing_evidence")
		requireAllDescribed(t, resolvedProps, "workflow resolve.resolved")

		// Owner: WorkflowOutputPacket emits exactly schema, truth_labels,
		// sections.
		packet := child(t, resolvedProps, "output_packet", "resolved")
		packetProps := propsOf(t, packet, "resolved.output_packet")
		requireExactKeys(t, packetProps, "resolved.output_packet",
			"schema", "truth_labels", "sections")
		requireDescribed(t, packetProps, "resolved.output_packet",
			"schema", "truth_labels", "sections")

		// Owner: ResolvedWorkflowCall emits exactly id, tool, reason,
		// missing_evidence, arguments, expected_evidence. arguments is a
		// genuine open map keyed by tool argument name, so it carries
		// additionalProperties with the key semantics described.
		calls := child(t, resolvedProps, "recommended_next_calls", "resolved")
		callItems := propsOf(t, itemsOf(t, calls, "recommended_next_calls"), "recommended_next_calls items")
		requireExactKeys(t, callItems, "recommended_next_calls[]",
			"id", "tool", "reason", "missing_evidence", "arguments", "expected_evidence")
		requireDescribed(t, callItems, "recommended_next_calls[]",
			"id", "tool", "reason", "missing_evidence", "arguments", "expected_evidence")
		arguments := child(t, callItems, "arguments", "recommended_next_calls[]")
		if _, ok := arguments["additionalProperties"]; !ok {
			t.Error("recommended_next_calls[].arguments: open tool-argument map needs additionalProperties")
		}

		// Owner: BlockedWorkflowCall emits exactly id, tool, reason,
		// missing_evidence, required_inputs_any.
		blocked := child(t, resolvedProps, "blocked_next_calls", "resolved")
		blockedItems := propsOf(t, itemsOf(t, blocked, "blocked_next_calls"), "blocked_next_calls items")
		requireExactKeys(t, blockedItems, "blocked_next_calls[]",
			"id", "tool", "reason", "missing_evidence", "required_inputs_any")
		requireDescribed(t, blockedItems, "blocked_next_calls[]",
			"id", "tool", "reason", "missing_evidence", "required_inputs_any")
	})

	t.Run("incident context packet names owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: go/internal/query/incident/model/response.go
		// BuildIncidentContextResponse assembles IncidentContextResponse
		// (incident/model/types.go): exactly query, incident, timeline,
		// related_changes, evidence_path, missing_evidence,
		// ambiguous_evidence, truncated, answer_metadata, all always
		// present (empty slices, never absent).
		doc := fragment(t, IncidentContext, "IncidentContext")
		schema := schemaAt(t, doc, "/api/v0/incidents/{incident_id}/context", "get")
		props := propsOf(t, schema, "incident context")
		requireExactKeys(t, props, "incident context",
			"query", "incident", "timeline", "related_changes", "evidence_path",
			"missing_evidence", "ambiguous_evidence", "truncated", "answer_metadata")
		requireAllDescribed(t, props, "incident context")
		requireScalarTypes(t, props, "incident context", map[string]string{
			"truncated": "boolean",
		})

		// Owner: IncidentContextQuery echoes the normalized filter:
		// provider and provider_incident_id plus the limit are always
		// set; scope_id, service_id, since, until only when supplied.
		query := child(t, props, "query", "incident context")
		queryProps := propsOf(t, query, "incident query")
		requireExactKeys(t, queryProps, "incident query",
			"provider", "provider_incident_id", "scope_id", "service_id",
			"since", "until", "limit")
		requireAllDescribed(t, queryProps, "incident query")
		requireScalarTypes(t, queryProps, "incident query", map[string]string{
			"provider": "string", "provider_incident_id": "string",
			"scope_id": "string", "service_id": "string", "since": "string",
			"until": "string", "limit": "integer",
		})

		// Owner: IncidentContextIncident is the provider-reported anchor:
		// provider and provider_incident_id always present, the rest only
		// when the provider record carries them. Nested references share
		// the IncidentContextReference shape.
		incident := child(t, props, "incident", "incident context")
		incidentProps := propsOf(t, incident, "incident")
		requireKeys(t, incidentProps, "incident",
			"provider", "provider_incident_id", "scope_id", "incident_number",
			"title", "status", "urgency", "priority", "service",
			"escalation_policy", "teams", "assignments", "created_at",
			"updated_at", "resolved_at", "source_url", "evidence_fact_id",
			"source_confidence", "observed_at")
		requireAllDescribed(t, incidentProps, "incident")
		reference := child(t, incidentProps, "priority", "incident")
		requireExactKeys(t, propsOf(t, reference, "incident reference"),
			"incident reference", "id", "type", "summary", "url")
		requireAllDescribed(t, propsOf(t, reference, "incident reference"), "incident reference")

		// Owner: IncidentContextTimelineEvent carries event_id always; the
		// remaining keys only when the provider event reports them.
		timeline := child(t, props, "timeline", "incident context")
		timelineItems := propsOf(t, itemsOf(t, timeline, "timeline"), "timeline items")
		requireKeys(t, timelineItems, "timeline[]",
			"event_id", "event_type", "actor", "channel", "summary",
			"created_at", "source_url", "evidence_fact_id",
			"source_confidence", "observed_at")
		requireAllDescribed(t, timelineItems, "timeline[]")
		actor := child(t, timelineItems, "actor", "timeline[]")
		requireExactKeys(t, propsOf(t, actor, "timeline actor"),
			"timeline actor", "id", "type", "summary", "url")
		requireAllDescribed(t, propsOf(t, actor, "timeline actor"), "timeline actor")

		// Owner: BuildIncidentContextResponse defaults an empty
		// truth_label to fallback and an empty explanation to the
		// PagerDuty service/time-window candidate note, so the response
		// always carries change_id, truth_label, and explanation.
		changes := child(t, props, "related_changes", "incident context")
		changeItems := propsOf(t, itemsOf(t, changes, "related_changes"), "related_changes items")
		requireKeys(t, changeItems, "related_changes[]",
			"change_id", "summary", "source", "services", "links",
			"timestamp", "source_url", "truth_label", "explanation",
			"evidence_fact_id", "source_confidence", "observed_at")
		requireAllDescribed(t, changeItems, "related_changes[]")
		requireDescribed(t, changeItems, "related_changes[]",
			"change_id", "truth_label", "explanation")
		links := child(t, changeItems, "links", "related_changes[]")
		linkItems := propsOf(t, itemsOf(t, links, "links"), "related_changes links")
		requireExactKeys(t, linkItems, "related_changes links[]", "href", "text")
		requireAllDescribed(t, linkItems, "related_changes links[]")

		// Owner: IncidentContextEvidenceEdge carries slot, truth_label,
		// and explanation always (normalize fills blanks); value is a
		// genuine open map of slot-scoped attribute names to strings
		// (for example provider/provider_incident_id on the incident
		// slot, service_id/service_name/service_url on the service
		// slot), so it carries additionalProperties with the key
		// semantics described.
		path := child(t, props, "evidence_path", "incident context")
		edgeItems := propsOf(t, itemsOf(t, path, "evidence_path"), "evidence_path items")
		requireKeys(t, edgeItems, "evidence_path[]",
			"slot", "truth_label", "explanation", "value", "evidence", "candidates")
		requireAllDescribed(t, edgeItems, "evidence_path[]")
		value := child(t, edgeItems, "value", "evidence_path[]")
		if _, ok := value["additionalProperties"]; !ok {
			t.Error("evidence_path[].value: open slot-attribute map needs additionalProperties")
		}
		refs := child(t, edgeItems, "evidence", "evidence_path[]")
		refItems := propsOf(t, itemsOf(t, refs, "evidence"), "evidence_path evidence")
		requireExactKeys(t, refItems, "evidence_path evidence[]",
			"fact_id", "record_id", "source", "kind", "url", "confidence", "observed_at")
		requireAllDescribed(t, refItems, "evidence_path evidence[]")
		candidates := child(t, edgeItems, "candidates", "evidence_path[]")
		candidateItems := propsOf(t, itemsOf(t, candidates, "candidates"), "evidence_path candidates")
		requireExactKeys(t, candidateItems, "evidence_path candidates[]",
			"id", "label", "url", "reason")
		requireAllDescribed(t, candidateItems, "evidence_path candidates[]")

		// Owner: IncidentMissingEvidence emits exactly slot and reason;
		// the ambiguous list reuses the evidence-edge shape above.
		missing := child(t, props, "missing_evidence", "incident context")
		missingItems := propsOf(t, itemsOf(t, missing, "missing_evidence"), "missing_evidence items")
		requireExactKeys(t, missingItems, "missing_evidence[]", "slot", "reason")
		requireDescribed(t, missingItems, "missing_evidence[]", "slot", "reason")
		ambiguous := child(t, props, "ambiguous_evidence", "incident context")
		ambiguousItems := propsOf(t, itemsOf(t, ambiguous, "ambiguous_evidence"), "ambiguous_evidence items")
		requireKeys(t, ambiguousItems, "ambiguous_evidence[]",
			"slot", "truth_label", "explanation", "value", "evidence", "candidates")
		requireAllDescribed(t, ambiguousItems, "ambiguous_evidence[]")

		// Owner: incidentContextAnswerMetadata (response.go) derives the
		// normalized companion: always the eight AnswerMetadata keys
		// with route coverage query_shape incident_context_evidence_path
		// counting evidence_path_slots, missing_evidence,
		// ambiguous_evidence, related_change_count, and truncated.
		meta := child(t, props, "answer_metadata", "incident context")
		metaProps := propsOf(t, meta, "answer_metadata")
		requireExactKeys(t, metaProps, "answer_metadata",
			"schema_version", "evidence_handles", "missing_evidence",
			"limitations", "truncated", "coverage",
			"partial_reasons", "recommended_next_calls")
		requireAllDescribed(t, metaProps, "answer_metadata")
		coverage := child(t, metaProps, "coverage", "answer_metadata")
		coverageProps := propsOf(t, coverage, "answer_metadata.coverage")
		requireKeys(t, coverageProps, "answer_metadata.coverage",
			"query_shape", "evidence_path_slots", "missing_evidence",
			"ambiguous_evidence", "related_change_count", "truncated")
		requireAllDescribed(t, coverageProps, "answer_metadata.coverage")
		// Owner: query/querycontract/answer metadata.go — Limitations and
		// RecommendedNextCalls are []map[string]any, genuine open maps, so
		// the items carry additionalProperties with the key semantics
		// described rather than a bare object.
		for _, key := range []string{"limitations", "recommended_next_calls"} {
			arr := child(t, metaProps, key, "answer_metadata")
			items, ok := arr["items"].(map[string]any)
			if !ok {
				t.Fatalf("answer_metadata.%s: array schema has no items", key)
			}
			if _, ok := items["additionalProperties"]; !ok {
				t.Errorf("answer_metadata.%s[]: open attribute map needs additionalProperties", key)
			}
			if desc, _ := items["description"].(string); desc == "" {
				t.Errorf("answer_metadata.%s[]: open attribute map needs its key semantics described", key)
			}
		}
	})
}
