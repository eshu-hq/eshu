// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/openapi/schema"
)

// TestContractShapesLane6 pins the lane-6 shape-completeness fixes: the
// entity content search response, the replatforming ownership and rollup
// schemas, the shared runtime-topology limits fragment, and the shared
// ErrorResponse must name their properties with owner descriptions instead
// of bare {"type": "object"} shapes. Each entry names the Go owner that
// emits the keys the contract asserts.
func TestContractShapesLane6(t *testing.T) {
	t.Parallel()

	child := func(t *testing.T, node map[string]any, key, where string) map[string]any {
		t.Helper()
		next, ok := node[key].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing or non-object property %q", where, key)
		}
		return next
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

	schemasOf := func(t *testing.T) map[string]any {
		t.Helper()
		var spec map[string]any
		if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
			t.Fatalf("unmarshal spec: %v", err)
		}
		components, ok := spec["components"].(map[string]any)
		if !ok {
			t.Fatal("spec has no components object")
		}
		return child(t, components, "schemas", "components")
	}

	t.Run("entity content search response", func(t *testing.T) {
		t.Parallel()
		schemas := schemasOf(t)
		response := child(t, schemas, "EntityContentSearchResponse", "components.schemas")
		props := propsOf(t, response, "EntityContentSearchResponse")

		// Owner: contentread/content_handler.go contentSearchResponse plus
		// querycontract/source_cache_clip.go clip markers. The four clip
		// markers are always present (0 when nothing was clipped); partial
		// and rank_walk ride only on unscoped-budget and rank-cursor pages.
		requireExactKeys(t, props, "EntityContentSearchResponse",
			"results", "count", "limit", "offset", "truncated", "partial",
			"source_backend", "source_cache_clip_bytes", "source_cache_clipped_rows",
			"docstring_clip_bytes", "docstring_clipped_rows", "rank_walk")
		requireScalarTypes(t, props, "EntityContentSearchResponse", map[string]string{
			"results": "array", "count": "integer", "limit": "integer",
			"offset": "integer", "truncated": "boolean", "partial": "object",
			"source_backend": "string", "source_cache_clip_bytes": "integer",
			"source_cache_clipped_rows": "integer", "docstring_clip_bytes": "integer",
			"docstring_clipped_rows": "integer",
		})
		requireDescribed(t, props, "EntityContentSearchResponse",
			"results", "count", "limit", "offset", "truncated", "partial",
			"source_backend", "source_cache_clip_bytes", "source_cache_clipped_rows",
			"docstring_clip_bytes", "docstring_clipped_rows", "rank_walk")

		// Owner: querycontract/paged_content_searcher.go SearchPartial.
		partial := child(t, props, "partial", "EntityContentSearchResponse")
		partialProps := propsOf(t, partial, "EntityContentSearchResponse.partial")
		requireExactKeys(t, partialProps, "EntityContentSearchResponse.partial",
			"reason", "rows_scanned_in_order", "rows_matched", "cursor",
			"budget_ms", "elapsed_ms", "overrun_ms", "progressed", "hint")
		requireScalarTypes(t, partialProps, "EntityContentSearchResponse.partial", map[string]string{
			"reason": "string", "rows_scanned_in_order": "integer",
			"rows_matched": "integer", "cursor": "object", "budget_ms": "integer",
			"elapsed_ms": "integer", "overrun_ms": "integer",
			"progressed": "boolean", "hint": "string",
		})
		requireDescribed(t, partialProps, "EntityContentSearchResponse.partial",
			"reason", "rows_scanned_in_order", "rows_matched", "cursor",
			"budget_ms", "elapsed_ms", "overrun_ms", "progressed", "hint")

		// Owner: querycontract/paged_content_searcher.go SearchCursor.
		// start_line and entity_id carry omitempty: an entity-search
		// cursor names all four keys, a file-search cursor names two.
		cursor := child(t, partialProps, "cursor", "EntityContentSearchResponse.partial")
		cursorProps := propsOf(t, cursor, "EntityContentSearchResponse.partial.cursor")
		requireExactKeys(t, cursorProps, "EntityContentSearchResponse.partial.cursor",
			"repo_id", "relative_path", "start_line", "entity_id")
		requireDescribed(t, cursorProps, "EntityContentSearchResponse.partial.cursor",
			"repo_id", "relative_path", "start_line", "entity_id")
	})

	t.Run("replatforming rollups", func(t *testing.T) {
		t.Parallel()
		schemas := schemasOf(t)

		// Owner: iac/replatforming_rollups.go replatformingReadinessCounts.
		counts := child(t, schemas, "ReplatformingReadinessCounts", "components.schemas")
		countsProps := propsOf(t, counts, "ReplatformingReadinessCounts")
		requireExactKeys(t, countsProps, "ReplatformingReadinessCounts",
			"import_ready", "needs_review", "refused")
		requireScalarTypes(t, countsProps, "ReplatformingReadinessCounts", map[string]string{
			"import_ready": "integer", "needs_review": "integer", "refused": "integer",
		})
		requireDescribed(t, countsProps, "ReplatformingReadinessCounts",
			"import_ready", "needs_review", "refused")

		// Owner: iac/replatforming_rollups.go replatformingRollupBucket.
		bucket := child(t, schemas, "ReplatformingRollupBucket", "components.schemas")
		bucketProps := propsOf(t, bucket, "ReplatformingRollupBucket")
		requireExactKeys(t, bucketProps, "ReplatformingRollupBucket",
			"key", "total", "source_state_counts", "readiness")
		requireDescribed(t, bucketProps, "ReplatformingRollupBucket",
			"key", "total", "source_state_counts", "readiness")
	})

	t.Run("replatforming ownership packet", func(t *testing.T) {
		t.Parallel()
		schemas := schemasOf(t)

		// Owner: iac/replatforming_ownership.go
		// buildReplatformingOwnershipPacket.
		packet := child(t, schemas, "ReplatformingOwnershipPacket", "components.schemas")
		packetProps := propsOf(t, packet, "ReplatformingOwnershipPacket")
		requireExactKeys(t, packetProps, "ReplatformingOwnershipPacket",
			"item_id", "provider", "account_id", "region", "resource_type",
			"stable_id", "finding_kind", "management_status", "source_state",
			"matched_terraform_state_address", "matched_terraform_config_file",
			"matched_terraform_module_path", "owner_candidates", "safety_gate",
			"freshness", "missing_evidence", "recommended_next_calls", "limitations")
		requireDescribed(t, packetProps, "ReplatformingOwnershipPacket",
			"item_id", "provider", "account_id", "region", "resource_type",
			"stable_id", "finding_kind", "management_status", "source_state",
			"matched_terraform_state_address", "matched_terraform_config_file",
			"matched_terraform_module_path", "owner_candidates", "safety_gate",
			"freshness", "missing_evidence", "recommended_next_calls", "limitations")

		// Owner: iac/replatforming_plan_contract.go
		// ReplatformingOwnerCandidate.
		candidate := child(t, schemas, "ReplatformingOwnerCandidate", "components.schemas")
		candidateProps := propsOf(t, candidate, "ReplatformingOwnerCandidate")
		requireExactKeys(t, candidateProps, "ReplatformingOwnerCandidate",
			"kind", "value", "confidence", "ambiguity_reasons")
		requireDescribed(t, candidateProps, "ReplatformingOwnerCandidate",
			"kind", "value", "confidence", "ambiguity_reasons")

		// Owner: iac/replatforming_ownership.go
		// ownershipRecommendedNextCalls emits exactly tool, route, reason.
		calls := child(t, packetProps, "recommended_next_calls", "ReplatformingOwnershipPacket")
		callProps := propsOf(t, itemsOf(t, calls, "recommended_next_calls"), "recommended_next_calls[]")
		requireExactKeys(t, callProps, "recommended_next_calls[]",
			"tool", "route", "reason")
		requireDescribed(t, callProps, "recommended_next_calls[]",
			"tool", "route", "reason")
	})

	t.Run("runtime topology limits", func(t *testing.T) {
		t.Parallel()
		var fragment map[string]any
		if err := json.Unmarshal([]byte(schema.ImpactRuntimeTopologyLimits), &fragment); err != nil {
			t.Fatalf("unmarshal ImpactRuntimeTopologyLimits: %v", err)
		}

		// Owner: querycontract/story_collection_helpers.go
		// BoundedCollectionMetadata. The same block is spliced under
		// instances, platform_edges, and provisioned_platforms.
		topProps := propsOf(t, fragment, "ImpactRuntimeTopologyLimits")
		for _, key := range []string{"instances", "platform_edges", "provisioned_platforms"} {
			block := child(t, topProps, key, "ImpactRuntimeTopologyLimits")
			blockProps := propsOf(t, block, "ImpactRuntimeTopologyLimits."+key)
			requireExactKeys(t, blockProps, "ImpactRuntimeTopologyLimits."+key,
				"limit", "query_sentinel_limit", "returned_count",
				"observed_count", "observed_count_is_lower_bound",
				"truncated", "ordering")
			requireScalarTypes(t, blockProps, "ImpactRuntimeTopologyLimits."+key, map[string]string{
				"limit": "integer", "query_sentinel_limit": "integer",
				"returned_count": "integer", "observed_count": "integer",
				"observed_count_is_lower_bound": "boolean", "truncated": "boolean",
				"ordering": "array",
			})
			requireDescribed(t, blockProps, "ImpactRuntimeTopologyLimits."+key,
				"limit", "query_sentinel_limit", "returned_count",
				"observed_count", "observed_count_is_lower_bound",
				"truncated", "ordering")
		}
		requireDescribed(t, topProps, "ImpactRuntimeTopologyLimits",
			"instances", "platform_edges", "provisioned_platforms")
	})

	t.Run("error response", func(t *testing.T) {
		t.Parallel()
		schemas := schemasOf(t)

		// Owner: querycontract/http.go WriteError emits error and detail;
		// querycontract/truth.go ErrorEnvelope adds code, message,
		// correlation_id, and the open details map.
		response := child(t, schemas, "ErrorResponse", "components.schemas")
		props := propsOf(t, response, "ErrorResponse")
		requireKeys(t, props, "ErrorResponse",
			"error", "detail", "error_code", "message", "correlation_id", "details")
		requireDescribed(t, props, "ErrorResponse",
			"error", "detail", "error_code", "message", "correlation_id", "details")
	})
}
