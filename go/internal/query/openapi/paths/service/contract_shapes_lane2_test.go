// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"encoding/json"
	"strings"
	"testing"
)

// Shared fragment-navigation and shape-pin helpers for the lane-2
// service contract tests.
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

// lane2PinTruth pins a querycontract.TruthEnvelope object. where names the
// truth object itself (e.g. "report.truth").
func lane2PinTruth(t *testing.T, node map[string]any, where string) {
	t.Helper()
	truthProps := lane2PropsOf(t, node, where)
	lane2RequireExactKeys(t, truthProps, where+".truth",
		"level", "capability", "profile", "basis", "backend", "freshness",
		"reason", "truncated", "omissions")
	lane2RequireScalarTypes(t, truthProps, where+".truth", map[string]string{
		"level": "string", "freshness": "object",
	})
	lane2RequireDescribed(t, truthProps, where+".truth",
		"level", "capability", "profile", "basis", "backend", "freshness",
		"reason", "truncated", "omissions")

	// Owner: querycontract.TruthFreshness.
	fresh := lane2Child(t, truthProps, "freshness", where+".freshness")
	freshProps := lane2PropsOf(t, fresh, where+".freshness")
	lane2RequireExactKeys(t, freshProps, where+".freshness",
		"state", "observed_at", "detail", "cause", "next_check")
	lane2RequireDescribed(t, freshProps, where+".freshness",
		"state", "observed_at", "detail", "cause", "next_check")

	// Owner: querycontract.FreshnessNextCheck.
	check := lane2Child(t, freshProps, "next_check", where+".check")
	checkProps := lane2PropsOf(t, check, where+".check")
	lane2RequireExactKeys(t, checkProps, where+".check",
		"tool", "route", "reason", "params")
	lane2RequireDescribed(t, checkProps, where+".check",
		"tool", "route", "reason", "params")
	params := lane2Child(t, checkProps, "params", where+".params")
	if add, ok := params["additionalProperties"].(map[string]any); !ok {
		t.Errorf("%s.params: missing additionalProperties for the bounded string map", where)
	} else if typ, _ := add["type"].(string); typ != "string" {
		t.Errorf("%s.params: additionalProperties type %q, want string", where, typ)
	}
	if desc, _ := params["description"].(string); desc == "" {
		t.Errorf("%s.params: missing description naming key semantics", where)
	}

	// Owner: querycontract.TruthOmission.
	om := lane2ItemsOf(t, lane2Child(t, truthProps, "omissions", where+".om"), where+".om[]")
	omProps := lane2PropsOf(t, om, where+".om[]")
	lane2RequireExactKeys(t, omProps, where+".om[]",
		"section", "detail", "total")
	lane2RequireScalarTypes(t, omProps, where+".om[]", map[string]string{
		"total": "integer",
	})
	lane2RequireDescribed(t, omProps, where+".om[]",
		"section", "detail", "total")
}

// lane2PinAnswerPacket pins an answer.AnswerPacket object.
func lane2PinAnswerPacket(t *testing.T, node map[string]any, where string) {
	t.Helper()
	props := lane2PropsOf(t, node, where)
	lane2RequireExactKeys(t, props, where,
		"prompt_family", "question", "primary_tool", "primary_route",
		"truth_class", "summary", "supported", "partial", "result_ref",
		"result", "truth", "limitations", "truncated", "missing_evidence",
		"evidence_handles", "citation_ref", "recommended_next_calls",
		"unsupported_reasons")
	lane2RequireScalarTypes(t, props, where, map[string]string{
		"supported": "boolean", "partial": "boolean", "truncated": "boolean",
		"truth": "object", "missing_evidence": "array",
		"evidence_handles": "array", "recommended_next_calls": "array",
	})
	lane2RequireDescribed(t, props, where,
		"prompt_family", "question", "primary_tool", "primary_route",
		"truth_class", "summary", "supported", "partial", "result_ref",
		"result", "truth", "limitations", "truncated", "missing_evidence",
		"evidence_handles", "citation_ref", "recommended_next_calls",
		"unsupported_reasons")

	lane2PinTruth(t, lane2Child(t, props, "truth", where+".truth"), where)

	// Owner: evidence.EvidenceCitationHandle.
	for _, key := range []string{"missing_evidence", "evidence_handles"} {
		h := lane2ItemsOf(t, lane2Child(t, props, key, where+"."+key), where+"."+key+"[]")
		hProps := lane2PropsOf(t, h, where+"."+key+"[]")
		lane2RequireExactKeys(t, hProps, where+"."+key+"[]",
			"kind", "repo_id", "relative_path", "entity_id",
			"evidence_family", "reason", "start_line", "end_line",
			"confidence")
		lane2RequireScalarTypes(t, hProps, where+"."+key+"[]", map[string]string{
			"start_line": "integer", "end_line": "integer",
			"confidence": "number",
		})
		lane2RequireDescribed(t, hProps, where+"."+key+"[]",
			"kind", "repo_id", "relative_path", "entity_id",
			"evidence_family", "reason", "start_line", "end_line",
			"confidence")
	}

	calls := lane2Child(t, props, "recommended_next_calls", where+".calls")
	if items, ok := calls["items"].(map[string]any); !ok {
		t.Errorf("%s.calls: array has no items", where)
	} else if _, ok := items["additionalProperties"]; !ok {
		t.Errorf("%s.calls: missing additionalProperties for the open call maps", where)
	} else if desc, _ := calls["description"].(string); desc == "" {
		t.Errorf("%s.calls: missing description", where)
	}
}

// TestContractShapesLane2Service pins the lane-2 shape-completeness fixes
// for the service-catalog correlations and intelligence-report fragments.
func TestContractShapesLane2Service(t *testing.T) {
	t.Parallel()

	t.Run("correlations", func(t *testing.T) {
		t.Parallel()
		// Owners: CatalogHandler.listCorrelations
		// (query/service/catalog.go) emits CatalogCorrelationResult rows
		// plus the CatalogEvidenceSummary; the cursor carries the last
		// correlation id and is built inline.
		doc := lane2Fragment(t, Catalog, "Catalog")
		schema := lane2SchemaAt(t, doc, "/api/v0/service-catalog/correlations", "get")
		props := lane2PropsOf(t, schema, "correlations")
		lane2RequireExactKeys(t, props, "correlations",
			"correlations", "count", "limit", "truncated",
			"missing_evidence", "evidence_summary", "next_cursor")
		lane2RequireScalarTypes(t, props, "correlations", map[string]string{
			"correlations": "array", "count": "integer", "limit": "integer",
			"truncated": "boolean", "missing_evidence": "array",
			"evidence_summary": "object", "next_cursor": "object",
		})
		lane2RequireDescribed(t, props, "correlations",
			"correlations", "count", "limit", "truncated",
			"missing_evidence", "evidence_summary", "next_cursor")

		row := lane2ItemsOf(t, lane2Child(t, props, "correlations", "correlations"), "correlations[]")
		rowProps := lane2PropsOf(t, row, "correlations[]")
		lane2RequireExactKeys(t, rowProps, "correlations[]",
			"correlation_id", "provider", "entity_ref", "entity_type",
			"display_name", "repository_id", "service_id", "workload_id",
			"owner_ref", "lifecycle", "tier", "outcome", "reason",
			"provenance_only", "drift_kind", "drift_status",
			"candidate_repository_ids", "evidence_fact_ids",
			"required_anchor_keys")
		lane2RequireScalarTypes(t, rowProps, "correlations[]", map[string]string{
			"correlation_id": "string", "outcome": "string",
			"provenance_only": "boolean",
		})
		lane2RequireDescribed(t, rowProps, "correlations[]",
			"correlation_id", "provider", "entity_ref", "entity_type",
			"display_name", "repository_id", "service_id", "workload_id",
			"owner_ref", "lifecycle", "tier", "outcome", "reason",
			"provenance_only", "drift_kind", "drift_status",
			"candidate_repository_ids", "evidence_fact_ids",
			"required_anchor_keys")

		// Owner: CatalogMissingEvidence.
		me := lane2ItemsOf(t, lane2Child(t, props, "missing_evidence", "correlations.me"), "correlations.me[]")
		meProps := lane2PropsOf(t, me, "correlations.me[]")
		lane2RequireExactKeys(t, meProps, "correlations.me[]", "class", "reason")
		lane2RequireDescribed(t, meProps, "correlations.me[]", "class", "reason")

		// Owner: CatalogEvidenceSummary.
		summary := lane2Child(t, props, "evidence_summary", "correlations.summary")
		summaryProps := lane2PropsOf(t, summary, "correlations.summary")
		lane2RequireExactKeys(t, summaryProps, "correlations.summary",
			"local_descriptors", "external_catalog_confirmation", "reason")
		lane2RequireDescribed(t, summaryProps, "correlations.summary",
			"local_descriptors", "external_catalog_confirmation", "reason")

		// Owner: CatalogLocalDescriptorEvidence.
		local := lane2Child(t, summaryProps, "local_descriptors", "correlations.local")
		localProps := lane2PropsOf(t, local, "correlations.local")
		lane2RequireExactKeys(t, localProps, "correlations.local",
			"state", "count", "providers", "source_uris", "facts",
			"truncated", "reason")
		lane2RequireScalarTypes(t, localProps, "correlations.local", map[string]string{
			"count": "integer",
		})
		lane2RequireDescribed(t, localProps, "correlations.local",
			"state", "count", "providers", "source_uris", "facts",
			"truncated", "reason")

		// Owner: CatalogLocalDescriptorEvidenceFact.
		fact := lane2ItemsOf(t, lane2Child(t, localProps, "facts", "correlations.fact"), "correlations.fact[]")
		factProps := lane2PropsOf(t, fact, "correlations.fact[]")
		lane2RequireExactKeys(t, factProps, "correlations.fact[]",
			"fact_id", "fact_kind", "provider", "entity_ref", "source_uri")
		lane2RequireDescribed(t, factProps, "correlations.fact[]",
			"fact_id", "fact_kind", "provider", "entity_ref", "source_uri")

		// Owner: CatalogExternalCatalogEvidence.
		external := lane2Child(t, summaryProps, "external_catalog_confirmation", "correlations.external")
		externalProps := lane2PropsOf(t, external, "correlations.external")
		lane2RequireExactKeys(t, externalProps, "correlations.external",
			"state", "count", "truncated", "reason")
		lane2RequireScalarTypes(t, externalProps, "correlations.external", map[string]string{
			"count": "integer",
		})
		lane2RequireDescribed(t, externalProps, "correlations.external",
			"state", "count", "truncated", "reason")

		// Owner: listCorrelations builds next_cursor only when
		// truncated, carrying the last correlation id forward.
		cursor := lane2Child(t, props, "next_cursor", "correlations")
		cursorProps := lane2PropsOf(t, cursor, "correlations.next_cursor")
		lane2RequireExactKeys(t, cursorProps, "correlations.next_cursor",
			"after_correlation_id")
		lane2RequireScalarTypes(t, cursorProps, "correlations.next_cursor",
			map[string]string{"after_correlation_id": "string"})
		lane2RequireDescribed(t, cursorProps, "correlations.next_cursor",
			"after_correlation_id")

		lane2RequireNoBareObjects(t, schema, "correlations")
	})

	t.Run("intelligence report", func(t *testing.T) {
		t.Parallel()
		// Owner: ReportHandler.getServiceIntelligenceReport
		// (serviceintelhttp/handler.go) emits serviceintel.Report composed
		// by serviceintel.Compose: the identity-anchored truth plus one
		// ReportSection per fixed SectionKind, each embedding a canonical
		// answer packet.
		doc := lane2Fragment(t, IntelligenceReport, "IntelligenceReport")
		schema := lane2SchemaAt(t, doc, "/api/v0/services/{service_name}/intelligence-report", "get")
		props := lane2PropsOf(t, schema, "report")
		lane2RequireExactKeys(t, props, "report",
			"schema", "subject", "supported", "partial", "truth_class",
			"truth", "sections", "limitations", "recommended_next_calls",
			"suggested_investigations")
		lane2RequireScalarTypes(t, props, "report", map[string]string{
			"schema": "string", "subject": "object", "supported": "boolean",
			"partial": "boolean", "truth_class": "string", "truth": "object",
			"sections": "array", "limitations": "array",
			"recommended_next_calls":   "array",
			"suggested_investigations": "array",
		})
		lane2RequireDescribed(t, props, "report",
			"schema", "subject", "supported", "partial", "truth_class",
			"truth", "sections", "limitations", "recommended_next_calls",
			"suggested_investigations")

		// Owner: serviceintel.ReportSubject.
		subject := lane2Child(t, props, "subject", "report.subject")
		subjectProps := lane2PropsOf(t, subject, "report.subject")
		lane2RequireExactKeys(t, subjectProps, "report.subject",
			"service_name", "service_id", "repo_id", "repo_name")
		lane2RequireDescribed(t, subjectProps, "report.subject",
			"service_name", "service_id", "repo_id", "repo_name")

		lane2PinTruth(t, lane2Child(t, props, "truth", "report.truth"), "report.truth")

		// Owner: serviceintel.ReportSection.
		section := lane2ItemsOf(t, lane2Child(t, props, "sections", "report.sections"), "report.sections[]")
		sectionProps := lane2PropsOf(t, section, "report.sections[]")
		lane2RequireExactKeys(t, sectionProps, "report.sections[]",
			"kind", "title", "status", "answer")
		lane2RequireDescribed(t, sectionProps, "report.sections[]",
			"kind", "title", "status", "answer")

		lane2PinAnswerPacket(t, lane2Child(t, sectionProps, "answer", "report.answer"), "report.answer")

		// Owner: serviceintel.NextCall.
		call := lane2ItemsOf(t, lane2Child(t, props, "recommended_next_calls", "report.calls"), "report.calls[]")
		callProps := lane2PropsOf(t, call, "report.calls[]")
		lane2RequireExactKeys(t, callProps, "report.calls[]",
			"tool", "route", "playbook", "reason", "arguments")
		lane2RequireDescribed(t, callProps, "report.calls[]",
			"tool", "route", "playbook", "reason", "arguments")
		args := lane2Child(t, callProps, "arguments", "report.args")
		if _, ok := args["additionalProperties"]; !ok {
			t.Errorf("report.args: missing additionalProperties for the bounded argument map")
		} else if desc, _ := args["description"].(string); desc == "" {
			t.Errorf("report.args: missing description naming key semantics")
		}

		// Owner: serviceintel.SuggestedInvestigation.
		inv := lane2ItemsOf(t, lane2Child(t, props, "suggested_investigations", "report.inv"), "report.inv[]")
		invProps := lane2PropsOf(t, inv, "report.inv[]")
		lane2RequireExactKeys(t, invProps, "report.inv[]",
			"id", "section", "basis", "reason", "evidence_basis",
			"next_call", "expected_truth_class")
		lane2RequireDescribed(t, invProps, "report.inv[]",
			"id", "section", "basis", "reason", "evidence_basis",
			"next_call", "expected_truth_class")
		next := lane2Child(t, invProps, "next_call", "report.inv.next")
		nextProps := lane2PropsOf(t, next, "report.inv.next")
		lane2RequireExactKeys(t, nextProps, "report.inv.next",
			"tool", "route", "playbook", "reason", "arguments")
		lane2RequireDescribed(t, nextProps, "report.inv.next",
			"tool", "route", "playbook", "reason", "arguments")

		lane2RequireNoBareObjects(t, schema, "report")
	})
}
