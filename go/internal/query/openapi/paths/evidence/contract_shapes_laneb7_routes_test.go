// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractShapesLaneB7Routes pins the lane-b7 shape-completeness fixes
// for the relationship drilldown, admission-decisions, and citations routes:
// every 200-response object the console renders must name its properties in
// the fragment instead of a bare {"type": "object"}, every 200-response
// property must carry a description naming its emitting owner, and string
// maps must use the filter-echo idiom. Each entry navigates the fragment
// JSON and requires the property keys the Go owner actually emits; invented
// shapes fail review.
func TestContractShapesLaneB7Routes(t *testing.T) {
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

	// requireFilterEchoArgs asserts the string-map echo idiom: a map echoed
	// back from the request filter with string values and a description
	// naming the echoed keys.
	requireFilterEchoArgs := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		node, ok := props["args"].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing args property", where)
		}
		if node["type"] != "object" {
			t.Errorf("%s: args is not an object", where)
		}
		additional, ok := node["additionalProperties"].(map[string]any)
		if !ok || additional["type"] != "string" {
			t.Errorf("%s: args has no string additionalProperties (not a filter-echo map)", where)
		}
		desc, _ := node["description"].(string)
		for _, key := range keys {
			if !strings.Contains(desc, key) {
				t.Errorf("%s: args description does not name echoed key %q: %q", where, key, desc)
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

	t.Run("relationship drilldown", func(t *testing.T) {
		t.Parallel()
		// Owner: evidence_read_model.go scanRelationshipEvidenceByResolvedID
		// plus relationshipEvidenceEndpoint, relationshipEvidenceGeneration,
		// and AddRelationshipConfidenceBasis; preview rows and detail keys
		// from relationships/resolver.go; kinds/type from
		// repository_relationship_read_model.go.
		doc := fragment(t, Routes, "Routes")
		schema := schemaAt(t, doc, "/api/v0/evidence/relationships/{resolved_id}", "get")
		requireNoBareObjects(t, schema, "drilldown")
		props := propsOf(t, schema, "drilldown")
		requireKeys(t, props, "drilldown",
			"lookup_basis", "resolved_id", "postgres_lookup_id", "generation_id",
			"generation", "source", "target", "relationship_type", "confidence",
			"confidence_basis", "evidence_count", "evidence_kinds", "evidence_type",
			"evidence_preview", "rationale", "resolution_source", "details")
		requireDescribed(t, props, "drilldown",
			"lookup_basis", "resolved_id", "postgres_lookup_id", "generation_id",
			"generation", "source", "target", "relationship_type", "confidence",
			"confidence_basis", "evidence_count", "evidence_kinds", "evidence_type",
			"evidence_preview", "rationale", "resolution_source", "details")
		requireScalarTypes(t, props, "drilldown", map[string]string{
			"lookup_basis": "string", "resolved_id": "string",
			"postgres_lookup_id": "string", "generation_id": "string",
			"generation": "object", "source": "object", "target": "object",
			"relationship_type": "string", "confidence": "number",
			"confidence_basis": "string", "evidence_count": "integer",
			"evidence_kinds": "array", "evidence_type": "string",
			"evidence_preview": "array", "rationale": "string",
			"resolution_source": "string", "details": "object",
		})
		generationProps := propsOf(t, child(t, props, "generation", "drilldown"), "drilldown.generation")
		requireExactKeys(t, generationProps, "drilldown.generation", "id", "scope", "run_id", "status")
		requireDescribed(t, generationProps, "drilldown.generation", "id", "scope", "run_id", "status")
		for _, endpoint := range []string{"source", "target"} {
			endpointProps := propsOf(t, child(t, props, endpoint, "drilldown"), "drilldown."+endpoint)
			requireKeys(t, endpointProps, "drilldown."+endpoint, "repo_id", "repo_name", "entity_id")
			requireDescribed(t, endpointProps, "drilldown."+endpoint, "repo_id", "repo_name", "entity_id")
			requireScalarTypes(t, endpointProps, "drilldown."+endpoint, map[string]string{
				"repo_id": "string", "repo_name": "string", "entity_id": "string",
			})
		}
		preview := child(t, props, "evidence_preview", "drilldown")
		previewProps := propsOf(t, itemsOf(t, preview, "drilldown evidence_preview"), "drilldown evidence_preview[]")
		requireExactKeys(t, previewProps, "drilldown evidence_preview[]", "kind", "confidence", "details")
		requireDescribed(t, previewProps, "drilldown evidence_preview[]", "kind", "confidence", "details")
		requireOpenMap(t, previewProps, "drilldown evidence_preview[]", "details", "EvidenceFact")
		detailsProps := propsOf(t, child(t, props, "details", "drilldown"), "drilldown.details")
		requireKeys(t, detailsProps, "drilldown.details", "evidence_kinds", "evidence_preview", "evidence_type")
		requireDescribed(t, detailsProps, "drilldown.details", "evidence_kinds", "evidence_preview", "evidence_type")
	})

	t.Run("admission decisions", func(t *testing.T) {
		t.Parallel()
		// Owner: evidence/admission_decisions.go listAdmissionDecisions and
		// AdmissionDecisionResultFromRow; row shape packet.AdmissionDecisionResult
		// (querycontract/packet/deployable_unit.go); evidence rows
		// packet.AdmissionDecisionEvidenceRow; next calls
		// admissionDecisionRecommendedNextCalls with RecommendedNextCall
		// (evidence/admission_decision_types.go).
		doc := fragment(t, Routes, "Routes")
		schema := schemaAt(t, doc, "/api/v0/evidence/admission-decisions", "get")
		requireNoBareObjects(t, schema, "admission-decisions")
		props := propsOf(t, schema, "admission-decisions")
		requireExactKeys(t, props, "admission-decisions",
			"decisions", "count", "limit", "truncated", "recommended_next_calls")
		requireDescribed(t, props, "admission-decisions",
			"decisions", "count", "limit", "truncated", "recommended_next_calls")
		requireScalarTypes(t, props, "admission-decisions", map[string]string{
			"decisions": "array", "count": "integer", "limit": "integer",
			"truncated": "boolean", "recommended_next_calls": "array",
		})
		decisions := child(t, props, "decisions", "admission-decisions")
		decisionProps := propsOf(t, itemsOf(t, decisions, "admission-decisions decisions"), "admission-decisions decisions[]")
		requireExactKeys(t, decisionProps, "admission-decisions decisions[]",
			"decision_id", "domain", "state", "domain_state", "scope_id", "generation_id",
			"anchor_kind", "anchor_id", "candidate_kind", "candidate_id",
			"confidence_score", "confidence_bucket", "confidence_basis",
			"freshness_state", "freshness_observed_at", "freshness_cause",
			"source_handles", "redaction_state", "redaction_reason",
			"canonical_write", "recommended_action", "payload_version",
			"decided_at", "updated_at", "evidence", "evidence_limit", "evidence_truncated")
		requireDescribed(t, decisionProps, "admission-decisions decisions[]",
			"decision_id", "domain", "state", "domain_state", "scope_id", "generation_id",
			"anchor_kind", "anchor_id", "candidate_kind", "candidate_id",
			"confidence_score", "confidence_bucket", "confidence_basis",
			"freshness_state", "freshness_observed_at", "freshness_cause",
			"source_handles", "redaction_state", "redaction_reason",
			"canonical_write", "recommended_action", "payload_version",
			"decided_at", "updated_at", "evidence", "evidence_limit", "evidence_truncated")
		requireScalarTypes(t, decisionProps, "admission-decisions decisions[]", map[string]string{
			"decision_id": "string", "domain": "string", "state": "string",
			"scope_id": "string", "generation_id": "string",
			"confidence_score": "number", "decided_at": "string", "updated_at": "string",
			"evidence": "array", "evidence_limit": "integer", "evidence_truncated": "boolean",
		})
		handles := child(t, decisionProps, "source_handles", "admission-decisions decisions")
		handleProps := propsOf(t, itemsOf(t, handles, "admission-decisions source_handles"), "admission-decisions source_handles[]")
		requireExactKeys(t, handleProps, "admission-decisions source_handles[]", "kind", "id", "scope_id")
		requireDescribed(t, handleProps, "admission-decisions source_handles[]", "kind", "id", "scope_id")
		canonicalProps := propsOf(t, child(t, decisionProps, "canonical_write", "admission-decisions decisions"), "admission-decisions canonical_write")
		requireExactKeys(t, canonicalProps, "admission-decisions canonical_write",
			"eligible", "written", "target_kind", "target_id", "skipped_reason")
		requireDescribed(t, canonicalProps, "admission-decisions canonical_write",
			"eligible", "written", "target_kind", "target_id", "skipped_reason")
		actionProps := propsOf(t, child(t, decisionProps, "recommended_action", "admission-decisions decisions"), "admission-decisions recommended_action")
		requireExactKeys(t, actionProps, "admission-decisions recommended_action", "action", "reason", "owner")
		requireDescribed(t, actionProps, "admission-decisions recommended_action", "action", "reason", "owner")
		evidence := child(t, decisionProps, "evidence", "admission-decisions decisions")
		evidenceProps := propsOf(t, itemsOf(t, evidence, "admission-decisions evidence"), "admission-decisions evidence[]")
		requireExactKeys(t, evidenceProps, "admission-decisions evidence[]",
			"evidence_id", "decision_id", "source_handle", "evidence_kind", "detail", "created_at")
		requireDescribed(t, evidenceProps, "admission-decisions evidence[]",
			"evidence_id", "decision_id", "source_handle", "evidence_kind", "detail", "created_at")
		requireOpenMap(t, evidenceProps, "admission-decisions evidence[]", "detail", "evidence kind")
		nextCalls := child(t, props, "recommended_next_calls", "admission-decisions")
		nextCallProps := propsOf(t, itemsOf(t, nextCalls, "admission-decisions next calls"), "admission-decisions recommended_next_calls[]")
		requireExactKeys(t, nextCallProps, "admission-decisions recommended_next_calls[]",
			"tool", "route", "reason", "args")
		requireDescribed(t, nextCallProps, "admission-decisions recommended_next_calls[]",
			"tool", "route", "reason", "args")
		requireFilterEchoArgs(t, nextCallProps, "admission-decisions recommended_next_calls[]",
			"domain", "scope_id", "generation_id")
	})

	t.Run("citations", func(t *testing.T) {
		t.Parallel()
		// Owner: evidence/citation.go buildEvidenceCitations; response shape
		// querycontract/evidence.EvidenceCitationResponse with EvidenceCitation,
		// EvidenceCitationHandle, EvidenceCitationCoverage, and
		// evidenceCitationNextCalls.
		doc := fragment(t, Routes, "Routes")
		schema := schemaAt(t, doc, "/api/v0/evidence/citations", "post")
		requireNoBareObjects(t, schema, "citations")
		props := propsOf(t, schema, "citations")
		requireExactKeys(t, props, "citations",
			"subject", "question", "citations", "missing_handles", "coverage", "recommended_next_calls")
		requireDescribed(t, props, "citations",
			"subject", "question", "citations", "missing_handles", "coverage", "recommended_next_calls")
		requireOpenMap(t, props, "citations", "subject", "caller")
		citations := child(t, props, "citations", "citations")
		citationProps := propsOf(t, itemsOf(t, citations, "citations citations"), "citations citations[]")
		requireExactKeys(t, citationProps, "citations citations[]",
			"citation_id", "rank", "kind", "evidence_family", "reason", "confidence",
			"repo_id", "relative_path", "entity_id", "entity_type", "entity_name",
			"start_line", "end_line", "byte_offset", "byte_length", "language",
			"artifact_type", "content_hash", "commit_sha", "provenance", "excerpt")
		requireDescribed(t, citationProps, "citations citations[]",
			"citation_id", "rank", "kind", "evidence_family", "reason", "confidence",
			"repo_id", "relative_path", "entity_id", "entity_type", "entity_name",
			"start_line", "end_line", "byte_offset", "byte_length", "language",
			"artifact_type", "content_hash", "commit_sha", "provenance", "excerpt")
		requireScalarTypes(t, citationProps, "citations citations[]", map[string]string{
			"citation_id": "string", "rank": "integer", "kind": "string",
			"confidence": "number", "provenance": "object", "excerpt": "string",
		})
		provenanceProps := propsOf(t, child(t, citationProps, "provenance", "citations citations"), "citations provenance")
		requireExactKeys(t, provenanceProps, "citations provenance", "basis", "rationale", "source")
		requireDescribed(t, provenanceProps, "citations provenance", "basis", "rationale", "source")
		missing := child(t, props, "missing_handles", "citations")
		missingProps := propsOf(t, itemsOf(t, missing, "citations missing"), "citations missing_handles[]")
		requireExactKeys(t, missingProps, "citations missing_handles[]",
			"kind", "repo_id", "relative_path", "entity_id", "evidence_family",
			"reason", "start_line", "end_line", "confidence")
		requireDescribed(t, missingProps, "citations missing_handles[]",
			"kind", "repo_id", "relative_path", "entity_id", "evidence_family",
			"reason", "start_line", "end_line", "confidence")
		coverageProps := propsOf(t, child(t, props, "coverage", "citations"), "citations.coverage")
		requireExactKeys(t, coverageProps, "citations.coverage",
			"query_shape", "input_handle_count", "resolved_count", "missing_count",
			"limit", "truncated", "source_backend")
		requireDescribed(t, coverageProps, "citations.coverage",
			"query_shape", "input_handle_count", "resolved_count", "missing_count",
			"limit", "truncated", "source_backend")
		requireScalarTypes(t, coverageProps, "citations.coverage", map[string]string{
			"query_shape": "string", "input_handle_count": "integer",
			"resolved_count": "integer", "missing_count": "integer",
			"limit": "integer", "truncated": "boolean", "source_backend": "string",
		})
		nextCalls := child(t, props, "recommended_next_calls", "citations")
		nextCallProps := propsOf(t, itemsOf(t, nextCalls, "citations next calls"), "citations recommended_next_calls[]")
		requireExactKeys(t, nextCallProps, "citations recommended_next_calls[]", "tool", "reason")
		requireDescribed(t, nextCallProps, "citations recommended_next_calls[]", "tool", "reason")
	})
}
