// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/relationships"
)

// This file holds the NornicDB transitive walk of the relationships
// family. Row readers, the dialect patterns, and the row limits live in
// the relationships leaf; nornicDBTransitiveOneHopRows stays here
// because it carries a queryplan source_sha256 pin in
// grandfathered_non_hot.go -- its body is byte-identical to the
// pre-move text. The transitive walk stays because it injects that
// pinned read. Edit a pinned body only with a manifest update in the
// same change; re-freezing a digest to match an edit is not a fix.

// nornicDBTransitiveRelationshipRows walks the CALLS frontier through
// the relationships leaf, injecting the pinned one-hop read so the
// digest keeps covering the live path. The pinned transitive reader
// names this spelling.
func (h *CodeHandler) nornicDBTransitiveRelationshipRows(
	ctx context.Context,
	entityID string,
	direction string,
	maxDepth int,
) ([]map[string]any, error) {
	return relationships.TransitiveRows(ctx, entityID, direction, maxDepth,
		func(ctx context.Context, currentID string, direction string) ([]map[string]any, error) {
			return h.nornicDBTransitiveOneHopRows(ctx, currentID, direction)
		})
}

// nornicDBTransitiveOneHopRows reads one CALLS hop in one direction.
// Pinned: byte-identical body (see the file comment).
func (h *CodeHandler) nornicDBTransitiveOneHopRows(
	ctx context.Context,
	entityID string,
	direction string,
) ([]map[string]any, error) {
	params := map[string]any{"entity_id": entityID}
	if direction == "incoming" {
		return h.Neo4j.Run(ctx, `
		MATCH (source)-[:CALLS]->(target)
		WHERE `+nornicDBEntityUIDPredicate("target", "$entity_id")+`
		RETURN coalesce(source.id, source.uid) as source_id,
		       source.name as source_name,
		       coalesce(target.id, target.uid) as target_id,
		       target.name as target_name
	`, params)
	}
	return h.Neo4j.Run(ctx, `
		MATCH (source)-[:CALLS]->(target)
		WHERE `+nornicDBEntityUIDPredicate("source", "$entity_id")+`
		RETURN coalesce(source.id, source.uid) as source_id,
		       source.name as source_name,
		       coalesce(target.id, target.uid) as target_id,
		       target.name as target_name
	`, params)
}

func nornicDBEntityUIDPredicate(alias string, param string) string {
	return alias + ".uid = " + param
}

// nornicDBNodePattern renders an entity lookup anchored on the uid
// property. It forwards to the relationships leaf; the pinned call-chain
// one-hop reader in callers.go names this spelling, so the forwarder
// keeps that digest byte-identical.
func nornicDBNodePattern(alias string, label string, param string) string {
	return relationships.NornicDBNodePattern(alias, label, param)
}
