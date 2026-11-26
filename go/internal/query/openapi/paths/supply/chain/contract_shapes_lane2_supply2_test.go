// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"testing"
)

// TestContractShapesLane2Supply2 pins the lane-2 shape-completeness fixes
// for the remaining supply-chain fragments: images, tag history, the
// vulnerability-scanner contract, the suppression mutation, and the
// security-alert aggregates.
func TestContractShapesLane2Supply2(t *testing.T) {
	t.Parallel()

	t.Run("images", func(t *testing.T) {
		t.Parallel()
		// Owners: listImages (query/images.go) emits ImageRow rows; the
		// cursor carries the next offset and is built inline.
		doc := lane2Fragment(t, Images, "Images")
		schema := lane2SchemaAt(t, doc, "/api/v0/images", "get")
		props := lane2PropsOf(t, schema, "images")
		lane2RequireExactKeys(t, props, "images",
			"images", "count", "limit", "offset", "truncated", "next_cursor")
		lane2RequireScalarTypes(t, props, "images", map[string]string{
			"images": "array", "count": "integer", "limit": "integer",
			"offset": "integer", "truncated": "boolean", "next_cursor": "object",
		})
		lane2RequireDescribed(t, props, "images",
			"images", "count", "limit", "offset", "truncated", "next_cursor")

		row := lane2ItemsOf(t, lane2Child(t, props, "images", "images"), "images[]")
		rowProps := lane2PropsOf(t, row, "images[]")
		lane2RequireExactKeys(t, rowProps, "images[]",
			"id", "digest", "repository_id", "registry", "repository",
			"name", "tag", "media_type", "artifact_type", "config_digest",
			"size_bytes", "source_system")
		lane2RequireScalarTypes(t, rowProps, "images[]", map[string]string{
			"id": "string", "size_bytes": "integer",
		})
		lane2RequireDescribed(t, rowProps, "images[]",
			"id", "digest", "repository_id", "registry", "repository",
			"name", "tag", "media_type", "artifact_type", "config_digest",
			"size_bytes", "source_system")

		cursor := lane2Child(t, props, "next_cursor", "images")
		cursorProps := lane2PropsOf(t, cursor, "images.next_cursor")
		lane2RequireExactKeys(t, cursorProps, "images.next_cursor", "offset")
		lane2RequireScalarTypes(t, cursorProps, "images.next_cursor",
			map[string]string{"offset": "integer"})
		lane2RequireDescribed(t, cursorProps, "images.next_cursor", "offset")

		lane2RequireNoBareObjects(t, schema, "images")
	})

	t.Run("tag history", func(t *testing.T) {
		t.Parallel()
		// Owners: listTagHistory (query/tag_history.go) emits taghistory.Row
		// rows (query/taghistory/page.go); the cursor is a sealed keyset
		// token naming one row's first_observed_at and uid.
		doc := lane2Fragment(t, TagHistory, "TagHistory")
		schema := lane2SchemaAt(t, doc, "/api/v0/images/tag-history", "get")
		props := lane2PropsOf(t, schema, "tag history")
		lane2RequireExactKeys(t, props, "tag history",
			"tag_history", "count", "limit", "offset", "truncated",
			"image_ref", "repository_id", "tag", "grant_filtered",
			"next_cursor")
		lane2RequireScalarTypes(t, props, "tag history", map[string]string{
			"tag_history": "array", "count": "integer", "limit": "integer",
			"truncated": "boolean", "next_cursor": "string",
		})
		lane2RequireDescribed(t, props, "tag history",
			"tag_history", "count", "limit", "truncated",
			"image_ref", "repository_id", "tag", "grant_filtered",
			"next_cursor")

		row := lane2ItemsOf(t, lane2Child(t, props, "tag_history", "tag rows"), "tag rows[]")
		rowProps := lane2PropsOf(t, row, "tag rows[]")
		lane2RequireExactKeys(t, rowProps, "tag rows[]",
			"tag", "resolved_digest", "previous_digest", "mutated",
			"first_observed_at", "repository_id", "identity_strength")
		lane2RequireScalarTypes(t, rowProps, "tag rows[]", map[string]string{
			"mutated": "boolean",
		})
		lane2RequireDescribed(t, rowProps, "tag rows[]",
			"tag", "resolved_digest", "previous_digest", "mutated",
			"first_observed_at", "repository_id", "identity_strength")

		lane2RequireNoBareObjects(t, schema, "tag history")
	})

	t.Run("scanner contract", func(t *testing.T) {
		t.Parallel()
		// Owners: getVulnerabilityScannerReadContract
		// (scanner_contract.go) emits vulnerabilityScannerReadContract:
		// vulnerabilityScannerFilterContract rows,
		// vulnerabilityScannerRouteContract rows, and the
		// remediationPacketContract.
		doc := lane2Fragment(t, VulnerabilityScannerContract, "VulnerabilityScannerContract")
		schema := lane2SchemaAt(t, doc, "/api/v0/supply-chain/vulnerability-scanner/contract", "get")
		props := lane2PropsOf(t, schema, "scanner contract")
		lane2RequireExactKeys(t, props, "scanner contract",
			"schema_version", "filters", "response_fields", "routes",
			"remediation_packet")
		lane2RequireScalarTypes(t, props, "scanner contract", map[string]string{
			"schema_version": "string", "filters": "array",
			"response_fields": "array", "routes": "array",
			"remediation_packet": "object",
		})
		lane2RequireDescribed(t, props, "scanner contract",
			"schema_version", "filters", "response_fields", "routes",
			"remediation_packet")

		// Owner: vulnerabilityScannerFilterContract.
		filter := lane2ItemsOf(t, lane2Child(t, props, "filters", "scanner filters"), "scanner filters[]")
		filterProps := lane2PropsOf(t, filter, "scanner filters[]")
		lane2RequireExactKeys(t, filterProps, "scanner filters[]",
			"name", "support", "semantics", "parameters", "routes",
			"backing", "notes")
		lane2RequireDescribed(t, filterProps, "scanner filters[]",
			"name", "support", "semantics", "parameters", "routes",
			"backing", "notes")

		// Owner: the static response_fields table.
		rf := lane2ItemsOf(t, lane2Child(t, props, "response_fields", "scanner rf"), "scanner rf[]")
		rfProps := lane2PropsOf(t, rf, "scanner rf[]")
		lane2RequireExactKeys(t, rfProps, "scanner rf[]", "name", "notes")
		lane2RequireDescribed(t, rfProps, "scanner rf[]", "name", "notes")

		// Owner: vulnerabilityScannerRouteContract.
		route := lane2ItemsOf(t, lane2Child(t, props, "routes", "scanner routes"), "scanner routes[]")
		routeProps := lane2PropsOf(t, route, "scanner routes[]")
		lane2RequireExactKeys(t, routeProps, "scanner routes[]",
			"name", "path", "tool", "truth_capability", "limit_required",
			"timeout", "ordering", "truncated_field", "missing_evidence",
			"unsupported_query")
		lane2RequireScalarTypes(t, routeProps, "scanner routes[]", map[string]string{
			"limit_required": "boolean",
		})
		lane2RequireDescribed(t, routeProps, "scanner routes[]",
			"name", "path", "tool", "truth_capability", "limit_required",
			"timeout", "ordering", "truncated_field", "missing_evidence",
			"unsupported_query")

		// Owner: remediationPacketContract.
		packet := lane2Child(t, props, "remediation_packet", "scanner packet")
		packetProps := lane2PropsOf(t, packet, "scanner packet")
		lane2RequireExactKeys(t, packetProps, "scanner packet",
			"schema_version", "summary", "sections", "missing_states",
			"surfaces", "security_review")
		lane2RequireDescribed(t, packetProps, "scanner packet",
			"schema_version", "summary", "sections", "missing_states",
			"surfaces", "security_review")
		section := lane2ItemsOf(t, lane2Child(t, packetProps, "sections", "scanner sections"), "scanner sections[]")
		sectionProps := lane2PropsOf(t, section, "scanner sections[]")
		lane2RequireExactKeys(t, sectionProps, "scanner sections[]",
			"name", "representation", "evidence", "missing_evidence",
			"deterministic_evidence", "optional_semantic_required")
		lane2RequireScalarTypes(t, sectionProps, "scanner sections[]", map[string]string{
			"optional_semantic_required": "boolean",
		})
		lane2RequireDescribed(t, sectionProps, "scanner sections[]",
			"name", "representation", "evidence", "missing_evidence",
			"deterministic_evidence", "optional_semantic_required")
		state := lane2ItemsOf(t, lane2Child(t, packetProps, "missing_states", "scanner states"), "scanner states[]")
		stateProps := lane2PropsOf(t, state, "scanner states[]")
		lane2RequireExactKeys(t, stateProps, "scanner states[]",
			"name", "meaning", "representation")
		lane2RequireDescribed(t, stateProps, "scanner states[]",
			"name", "meaning", "representation")
		surface := lane2ItemsOf(t, lane2Child(t, packetProps, "surfaces", "scanner surfaces"), "scanner surfaces[]")
		surfaceProps := lane2PropsOf(t, surface, "scanner surfaces[]")
		lane2RequireExactKeys(t, surfaceProps, "scanner surfaces[]",
			"name", "representation", "truth_contract")
		lane2RequireDescribed(t, surfaceProps, "scanner surfaces[]",
			"name", "representation", "truth_contract")
		review := lane2Child(t, packetProps, "security_review", "scanner review")
		reviewProps := lane2PropsOf(t, review, "scanner review")
		lane2RequireExactKeys(t, reviewProps, "scanner review",
			"false_positive_controls", "leakage_controls")
		lane2RequireDescribed(t, reviewProps, "scanner review",
			"false_positive_controls", "leakage_controls")

		lane2RequireNoBareObjects(t, schema, "scanner contract")
	})

	t.Run("suppression mutation", func(t *testing.T) {
		t.Parallel()
		// Owner: createVulnerabilitySuppression (suppression_mutation.go)
		// answers 201 on creation and 200 on an idempotent repeat; both
		// carry the same three keys.
		doc := lane2Fragment(t, suppressionMutation, "suppressionMutation")
		for _, code := range []string{"200", "201"} {
			route, ok := doc["/api/v0/supply-chain/impact/suppressions"].(map[string]any)
			if !ok {
				t.Fatalf("fragment paths missing /api/v0/supply-chain/impact/suppressions")
			}
			op := lane2Child(t, route, "post", "suppressions")
			responses := lane2Child(t, op, "responses", "suppressions post")
			resp := lane2Child(t, responses, code, "suppressions post "+code)
			content := lane2Child(t, resp, "content", "suppressions "+code)
			body := lane2Child(t, content, "application/json", "suppressions "+code)
			schema := lane2Child(t, body, "schema", "suppressions "+code)
			props := lane2PropsOf(t, schema, "suppressions "+code)
			lane2RequireExactKeys(t, props, "suppressions "+code,
				"suppression_id", "generation_id", "status")
			lane2RequireDescribed(t, props, "suppressions "+code,
				"suppression_id", "generation_id", "status")
			lane2RequireNoBareObjects(t, schema, "suppressions "+code)
		}
	})

	t.Run("security alert aggregates", func(t *testing.T) {
		t.Parallel()
		// Owners: the count/inventory aggregate handlers emit
		// SecurityAlertReconciliationAggregateCount rollups and
		// SecurityAlertReconciliationInventoryRow buckets
		// (store_security_alert.go), with string-map scope echoes and
		// the shared coverage envelope.
		doc := lane2Fragment(t, SecurityAlertAggregate, "SecurityAlertAggregate")

		countSchema := lane2SchemaAt(t, doc, "/api/v0/supply-chain/security-alerts/reconciliations/count", "get")
		countProps := lane2PropsOf(t, countSchema, "alert count")
		lane2RequireExactKeys(t, countProps, "alert count",
			"total_reconciliations", "by_reconciliation_status",
			"by_provider", "by_provider_state", "by_source_freshness",
			"coverage", "collector_readiness", "scope")
		lane2RequireScalarTypes(t, countProps, "alert count", map[string]string{
			"total_reconciliations":    "integer",
			"by_reconciliation_status": "object", "by_provider": "object",
			"by_provider_state": "object", "by_source_freshness": "object",
			"coverage": "object", "scope": "object",
		})
		lane2RequireDescribed(t, countProps, "alert count",
			"total_reconciliations", "by_reconciliation_status",
			"by_provider", "by_provider_state", "by_source_freshness",
			"coverage", "scope")
		cov := lane2Child(t, countProps, "coverage", "alert count.coverage")
		covProps := lane2PropsOf(t, cov, "alert count.coverage")
		lane2RequireDescribed(t, covProps, "alert count.coverage",
			"state", "partial_rows", "rows_considered", "reasons")
		scope := lane2Child(t, countProps, "scope", "alert count.scope")
		if add, ok := scope["additionalProperties"].(map[string]any); !ok {
			t.Errorf("alert count.scope: missing additionalProperties for the string filter map")
		} else if typ, _ := add["type"].(string); typ != "string" {
			t.Errorf("alert count.scope: additionalProperties type %q, want string", typ)
		}
		if desc, _ := scope["description"].(string); desc == "" {
			t.Errorf("alert count.scope: missing filter-echo description naming key semantics")
		}

		invSchema := lane2SchemaAt(t, doc, "/api/v0/supply-chain/security-alerts/reconciliations/inventory", "get")
		invProps := lane2PropsOf(t, invSchema, "alert inventory")
		lane2RequireExactKeys(t, invProps, "alert inventory",
			"buckets", "count", "limit", "offset", "group_by", "truncated",
			"next_offset", "scope", "coverage", "collector_readiness")
		lane2RequireDescribed(t, invProps, "alert inventory",
			"buckets", "count", "limit", "offset", "group_by", "truncated",
			"next_offset", "scope", "coverage")
		bucket := lane2ItemsOf(t, lane2Child(t, invProps, "buckets", "alert buckets"), "alert buckets[]")
		bucketProps := lane2PropsOf(t, bucket, "alert buckets[]")
		lane2RequireExactKeys(t, bucketProps, "alert buckets[]",
			"dimension", "value", "count")
		lane2RequireScalarTypes(t, bucketProps, "alert buckets[]", map[string]string{
			"dimension": "string", "value": "string", "count": "integer",
		})
		lane2RequireDescribed(t, bucketProps, "alert buckets[]",
			"dimension", "value", "count")

		lane2RequireNoBareObjects(t, countSchema, "alert count")
		lane2RequireNoBareObjects(t, invSchema, "alert inventory")
	})
}
