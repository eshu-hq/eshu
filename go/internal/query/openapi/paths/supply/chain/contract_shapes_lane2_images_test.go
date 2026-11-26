// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"testing"
)

// TestContractShapesLane2Images pins the lane-2 shape-completeness fixes
// for the container-image identity fragments: the full reducer-owned
// identity row (ContainerImageIdentityResult), the source bridge
// (ContainerImageIdentitySourceBridge), and the aggregate envelopes.
func TestContractShapesLane2Images(t *testing.T) {
	t.Parallel()

	t.Run("container image identities", func(t *testing.T) {
		t.Parallel()
		// Owners: listContainerImageIdentities (container_images.go)
		// emits ContainerImageIdentityResult rows
		// (handler.go) plus the identity source bridge
		// (ContainerImageIdentitySourceBridge); the cursor is built
		// inline.
		doc := lane2Fragment(t, ContainerImages, "ContainerImages")
		schema := lane2SchemaAt(t, doc, "/api/v0/supply-chain/container-images/identities", "get")
		props := lane2PropsOf(t, schema, "identities")
		lane2RequireExactKeys(t, props, "identities",
			"identities", "source_bridge", "count", "limit", "truncated",
			"next_cursor", "collector_readiness")
		lane2RequireScalarTypes(t, props, "identities", map[string]string{
			"identities": "array", "source_bridge": "object", "count": "integer",
			"limit": "integer", "truncated": "boolean", "next_cursor": "object",
		})
		lane2RequireDescribed(t, props, "identities",
			"identities", "source_bridge", "count", "limit", "truncated",
			"next_cursor")

		row := lane2ItemsOf(t, lane2Child(t, props, "identities", "identities"), "identities[]")
		rowProps := lane2PropsOf(t, row, "identities[]")
		lane2RequireExactKeys(t, rowProps, "identities[]",
			"identity_id", "digest", "image_ref", "repository_id",
			"source_repository_ids", "source_revision",
			"source_revision_provenance", "workload_ids", "service_ids",
			"outcome", "reason", "identity_strength", "canonical_id",
			"canonical_writes", "source_layers", "evidence_fact_ids",
			"missing_evidence", "source_freshness", "source_confidence")
		lane2RequireScalarTypes(t, rowProps, "identities[]", map[string]string{
			"identity_id": "string", "outcome": "string",
			"canonical_writes": "integer",
		})
		lane2RequireDescribed(t, rowProps, "identities[]",
			"identity_id", "digest", "image_ref", "repository_id",
			"source_repository_ids", "source_revision",
			"source_revision_provenance", "workload_ids", "service_ids",
			"outcome", "reason", "identity_strength", "canonical_id",
			"canonical_writes", "source_layers", "evidence_fact_ids",
			"missing_evidence", "source_freshness", "source_confidence")

		// Owner: ContainerImageIdentitySourceBridge.
		bridge := lane2Child(t, props, "source_bridge", "identities.bridge")
		bridgeProps := lane2PropsOf(t, bridge, "identities.bridge")
		lane2RequireExactKeys(t, bridgeProps, "identities.bridge",
			"source_repository_id", "image_repository_ids",
			"missing_evidence", "warnings")
		lane2RequireDescribed(t, bridgeProps, "identities.bridge",
			"source_repository_id", "image_repository_ids",
			"missing_evidence", "warnings")

		// Owner: listContainerImageIdentities builds next_cursor only
		// when truncated, carrying the last identity id forward.
		cursor := lane2Child(t, props, "next_cursor", "identities")
		cursorProps := lane2PropsOf(t, cursor, "identities.next_cursor")
		lane2RequireExactKeys(t, cursorProps, "identities.next_cursor",
			"after_identity_id")
		lane2RequireScalarTypes(t, cursorProps, "identities.next_cursor",
			map[string]string{"after_identity_id": "string"})
		lane2RequireDescribed(t, cursorProps, "identities.next_cursor",
			"after_identity_id")

		lane2RequireNoBareObjects(t, schema, "identities")
	})

	t.Run("container image identity aggregates", func(t *testing.T) {
		t.Parallel()
		// Owners: the count/inventory aggregate handlers emit
		// ContainerImageIdentityAggregateCount rollups and
		// ContainerImageIdentityInventoryRow buckets
		// (store_container_image.go), with string-map scope echoes.
		doc := lane2Fragment(t, ContainerImageIdentityAggregate, "ContainerImageIdentityAggregate")

		countSchema := lane2SchemaAt(t, doc, "/api/v0/supply-chain/container-images/identities/count", "get")
		countProps := lane2PropsOf(t, countSchema, "identity count")
		lane2RequireExactKeys(t, countProps, "identity count",
			"total_identities", "by_outcome", "by_identity_strength",
			"source_bridge", "scope")
		lane2RequireScalarTypes(t, countProps, "identity count", map[string]string{
			"total_identities": "integer", "by_outcome": "object",
			"by_identity_strength": "object", "source_bridge": "object",
			"scope": "object",
		})
		lane2RequireDescribed(t, countProps, "identity count",
			"total_identities", "by_outcome", "by_identity_strength",
			"source_bridge", "scope")
		bridge := lane2Child(t, countProps, "source_bridge", "identity count.bridge")
		bridgeProps := lane2PropsOf(t, bridge, "identity count.bridge")
		lane2RequireExactKeys(t, bridgeProps, "identity count.bridge",
			"source_repository_id", "image_repository_ids",
			"missing_evidence", "warnings")
		lane2RequireDescribed(t, bridgeProps, "identity count.bridge",
			"source_repository_id", "image_repository_ids",
			"missing_evidence", "warnings")
		scope := lane2Child(t, countProps, "scope", "identity count.scope")
		if add, ok := scope["additionalProperties"].(map[string]any); !ok {
			t.Errorf("identity count.scope: missing additionalProperties for the string filter map")
		} else if typ, _ := add["type"].(string); typ != "string" {
			t.Errorf("identity count.scope: additionalProperties type %q, want string", typ)
		}
		if desc, _ := scope["description"].(string); desc == "" {
			t.Errorf("identity count.scope: missing filter-echo description naming key semantics")
		}

		invSchema := lane2SchemaAt(t, doc, "/api/v0/supply-chain/container-images/identities/inventory", "get")
		invProps := lane2PropsOf(t, invSchema, "identity inventory")
		lane2RequireExactKeys(t, invProps, "identity inventory",
			"buckets", "count", "limit", "offset", "group_by", "truncated",
			"next_offset", "scope")
		lane2RequireDescribed(t, invProps, "identity inventory",
			"buckets", "count", "limit", "offset", "group_by", "truncated",
			"next_offset", "scope")
		bucket := lane2ItemsOf(t, lane2Child(t, invProps, "buckets", "identity buckets"), "identity buckets[]")
		bucketProps := lane2PropsOf(t, bucket, "identity buckets[]")
		lane2RequireExactKeys(t, bucketProps, "identity buckets[]",
			"dimension", "value", "count")
		lane2RequireScalarTypes(t, bucketProps, "identity buckets[]", map[string]string{
			"dimension": "string", "value": "string", "count": "integer",
		})
		lane2RequireDescribed(t, bucketProps, "identity buckets[]",
			"dimension", "value", "count")

		lane2RequireNoBareObjects(t, countSchema, "identity count")
		lane2RequireNoBareObjects(t, invSchema, "identity inventory")
	})
}
