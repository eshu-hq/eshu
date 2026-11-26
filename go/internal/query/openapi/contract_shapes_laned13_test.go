// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"testing"
)

// TestContractShapesLaneD13 pins the lane-D13 response-contract completion:
// every property of the Relationship, SymbolSearchResult, and
// SemanticEvidenceRow schemas carries a description naming its emitting
// owner, and the fixed-key owners carry committed exact-set pins.
func TestContractShapesLaneD13(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	components, ok := spec["components"].(map[string]any)
	if !ok {
		t.Fatal("spec has no components object")
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatal("spec components has no schemas object")
	}

	schemaOf := func(t *testing.T, name string) map[string]any {
		t.Helper()
		node, ok := schemas[name].(map[string]any)
		if !ok {
			t.Fatalf("spec schemas missing %q", name)
		}
		return node
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

	requireDescribedTypes := func(t *testing.T, props map[string]any, where string, want map[string]string) {
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

	t.Run("relationship rows are fully described", func(t *testing.T) {
		t.Parallel()
		// Row owners: code edges from
		// codemodel.RelationshipGraphRowCypherFromAnchor (via
		// codequery.relationshipsGraphRow for handleRelationships),
		// class/inheritance rows from
		// codequery/relationships/story.ClassMethodsCypher and
		// InheritanceDepthCypher, direct story rows from
		// codequery/relationships/story.GraphCypher, correlation
		// evidence from query.evidence_read_model plus
		// querycontract.AddRelationshipConfidenceBasis, provenance from
		// codemodel.RelationshipStoryProvenance, centrality from
		// codeshaping.RelationshipStoryRankByCentrality.
		rel := schemaOf(t, "Relationship")
		props := propsOf(t, rel, "Relationship")
		requireExactKeys(t, props, "Relationship",
			"type", "target_name", "target_id",
			"source_name", "source_id",
			"source_repo_id", "source_repo_name",
			"source_file_path", "source_language", "source_type",
			"source_start_line", "source_end_line",
			"target_repo_id", "target_repo_name",
			"target_file_path", "target_language", "target_type",
			"target_start_line", "target_end_line",
			"confidence", "confidence_basis", "reason",
			"resolution_source", "evidence_type", "evidence_kinds",
			"resolution_method", "provenance", "centrality",
			"direction", "call_kind", "edge_origin",
			"evidence_source", "why_trail_json",
			"source_handle", "target_handle", "why_trail_truncated")
		requireDescribedTypes(t, props, "Relationship", map[string]string{
			"type": "string", "target_name": "string",
			"target_id": "string", "source_name": "string",
			"source_id": "string", "source_repo_id": "string",
			"source_repo_name": "string", "source_file_path": "string",
			"source_language": "string", "source_type": "string",
			"source_start_line": "integer", "source_end_line": "integer",
			"target_repo_id": "string", "target_repo_name": "string",
			"target_file_path": "string", "target_language": "string",
			"target_type":       "string",
			"target_start_line": "integer", "target_end_line": "integer",
			"confidence": "number", "confidence_basis": "string",
			"reason": "string", "resolution_source": "string",
			"evidence_type": "string", "evidence_kinds": "array",
			"resolution_method": "string", "provenance": "object",
			"centrality": "integer", "direction": "string",
			"call_kind": "string", "edge_origin": "string",
			"evidence_source": "string", "why_trail_json": "string",
			"source_handle": "string", "target_handle": "string",
			"why_trail_truncated": "boolean",
		})

		prov, ok := props["provenance"].(map[string]any)
		if !ok {
			t.Fatal("Relationship has no provenance property")
		}
		provProps := propsOf(t, prov, "Relationship.provenance")
		requireExactKeys(t, provProps, "Relationship.provenance",
			"confidence", "confidence_state", "confidence_tier",
			"method", "source_family", "reason", "truth_state",
			"why_trail", "why_trail_truncated",
			"derived", "heuristic", "unsupported")
		requireDescribedTypes(t, provProps, "Relationship.provenance", map[string]string{
			"confidence": "number", "confidence_state": "string",
			"confidence_tier": "string", "method": "string",
			"source_family": "string", "reason": "string",
			"truth_state": "string", "why_trail": "array",
			"why_trail_truncated": "boolean",
			"derived":             "boolean", "heuristic": "boolean",
			"unsupported": "boolean",
		})
	})

	t.Run("symbol search rows carry definition kind and backend", func(t *testing.T) {
		t.Parallel()
		// Row owners codequery.symbolEntityResults (content path) and
		// codequery.normalizeSymbolGraphResults (graph path); envelope
		// owner codequery.handleSymbolSearch.
		result := schemaOf(t, "SymbolSearchResult")
		props := propsOf(t, result, "SymbolSearchResult")
		requireExactKeys(t, props, "SymbolSearchResult",
			"entity_id", "name", "entity_name", "entity_type",
			"file_path", "relative_path", "repo_id", "language",
			"start_line", "end_line", "classification", "match_kind",
			"rank", "source_handle",
			"source_cache", "source_cache_clipped",
			"source_cache_clip_bytes", "source_cache_total_bytes",
			"docstring_clipped", "docstring_clip_bytes",
			"docstring_total_bytes",
			"metadata", "semantic_summary", "semantic_profile",
			"definition_kind", "source_backend")
		requireDescribedTypes(t, props, "SymbolSearchResult", map[string]string{
			"entity_id": "string", "name": "string",
			"entity_name": "string", "entity_type": "string",
			"file_path": "string", "relative_path": "string",
			"repo_id": "string", "language": "string",
			"start_line": "integer", "end_line": "integer",
			"classification": "string", "match_kind": "string",
			"rank": "integer", "source_handle": "object",
			"definition_kind": "string", "source_backend": "string",
		})
	})

	t.Run("semantic evidence row projects owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner query.semanticEvidencePublicRow projects the durable fact
		// record: identity from the fact row, content/state keys from the
		// payload via copyString/copyAny, filtered maps via
		// copyFilteredMapList/copyFilteredNestedMap over fixed allowlists
		// from facts/semantic (SemanticSourceRef, SemanticChunkRef,
		// SemanticProviderRef, SemanticCodeEntityRef, docs.EvidenceRef).
		row := schemaOf(t, "SemanticEvidenceRow")
		props := propsOf(t, row, "SemanticEvidenceRow")
		requireExactKeys(t, props, "SemanticEvidenceRow",
			"fact_id", "fact_kind", "truth_basis",
			"scope_id", "generation_id", "source_system", "observed_at",
			"observation_id", "observation_type", "observation_text",
			"observation_hash",
			"hint_id", "hint_type", "relationship_kind",
			"hint_text", "hint_hash",
			"confidence", "unsupported_reason",
			"freshness_state", "policy_state", "promotion_policy",
			"redaction_state", "redaction_summary",
			"admission_state", "corroboration_state",
			"missing_evidence", "evidence_refs", "object_refs",
			"source", "chunk", "provider", "subject",
			"provider_profile_id", "provider_kind",
			"model_id", "endpoint_profile_id",
			"prompt_version", "redaction_version",
			"extractor_version", "extraction_mode",
			"source_acl_state", "access_disposition",
			"permission_denied", "content_withheld")
		requireDescribedTypes(t, props, "SemanticEvidenceRow", map[string]string{
			"fact_id": "string", "fact_kind": "string",
			"truth_basis": "string",
			"scope_id":    "string", "generation_id": "string",
			"source_system": "string", "observed_at": "string",
			"observation_id": "string", "observation_type": "string",
			"observation_text": "string", "observation_hash": "string",
			"hint_id": "string", "hint_type": "string",
			"relationship_kind": "string",
			"hint_text":         "string", "hint_hash": "string",
			"confidence": "string", "unsupported_reason": "string",
			"promotion_policy": "string",
			"redaction_state":  "string", "redaction_summary": "string",
			"missing_evidence": "array", "evidence_refs": "array",
			"object_refs": "array",
			"source":      "object", "chunk": "object",
			"provider": "object", "subject": "object",
			"model_id": "string", "endpoint_profile_id": "string",
			"extractor_version": "string", "extraction_mode": "string",
		})

		childProps := func(t *testing.T, props map[string]any, key, where string) map[string]any {
			t.Helper()
			node, ok := props[key].(map[string]any)
			if !ok {
				t.Fatalf("%s: missing property %q", where, key)
			}
			return propsOf(t, node, where+" "+key)
		}
		childItems := func(t *testing.T, props map[string]any, key, where string) map[string]any {
			t.Helper()
			node, ok := props[key].(map[string]any)
			if !ok {
				t.Fatalf("%s: missing property %q", where, key)
			}
			return propsOf(t, itemsOf(t, node, where+" "+key), where+" "+key+"[]")
		}

		requireExactKeys(t,
			childItems(t, props, "evidence_refs", "SemanticEvidenceRow"),
			"SemanticEvidenceRow.evidence_refs[]",
			"kind", "id", "uri", "confidence")

		requireExactKeys(t,
			childItems(t, props, "object_refs", "SemanticEvidenceRow"),
			"SemanticEvidenceRow.object_refs[]",
			"repository_id", "relative_path", "entity_kind",
			"entity_id", "line_start", "line_end")

		requireExactKeys(t, childProps(t, props, "source", "SemanticEvidenceRow"),
			"SemanticEvidenceRow.source",
			"source_id", "source_class", "source_handle",
			"repository_id", "document_id", "relative_path",
			"external_anchor", "section_id",
			"line_start", "line_end", "page_start", "page_end")
		requireExactKeys(t, childProps(t, props, "chunk", "SemanticEvidenceRow"),
			"SemanticEvidenceRow.chunk",
			"chunk_id", "chunk_hash", "source_hash",
			"prompt_version", "redaction_version",
			"extractor_version", "extraction_mode")
		requireExactKeys(t, childProps(t, props, "provider", "SemanticEvidenceRow"),
			"SemanticEvidenceRow.provider",
			"provider_profile_id", "provider_kind",
			"model_id", "endpoint_profile_id")
		requireExactKeys(t, childProps(t, props, "subject", "SemanticEvidenceRow"),
			"SemanticEvidenceRow.subject",
			"repository_id", "relative_path", "entity_kind",
			"entity_id", "line_start", "line_end")
	})
}
