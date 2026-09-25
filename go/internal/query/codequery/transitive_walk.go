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
//
// #5167 edited the pinned body on purpose, and re-derived its digest in
// the same change: each hop now binds the neighbour it reaches to the
// caller's repository grant, so the breadth-first walk can never step
// onto -- and therefore never through -- a node outside the grant.

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
	access := codeGrantAccessFilter(ctx)
	return relationships.TransitiveRows(ctx, entityID, direction, maxDepth,
		func(ctx context.Context, currentID string, direction string) ([]map[string]any, error) {
			return h.nornicDBTransitiveOneHopRows(ctx, currentID, direction, access)
		})
}

// nornicDBTransitiveOneHopRows reads one CALLS hop in one direction. For a
// scoped caller the neighbour's repo_id is bound to the grant in the same
// WHERE as the anchor, so a neighbour outside the grant (or with no
// repo_id) is never returned and never joins the next frontier.
// Pinned: see the file comment.
func (h *CodeHandler) nornicDBTransitiveOneHopRows(
	ctx context.Context,
	entityID string,
	direction string,
	access repositoryAccessFilter,
) ([]map[string]any, error) {
	params := access.GraphParams(map[string]any{"entity_id": entityID})
	if direction == "incoming" {
		return h.Neo4j.Run(ctx, `
		MATCH (source)-[:CALLS]->(target)
		WHERE `+nornicDBEntityUIDPredicate("target", "$entity_id")+access.GraphPredicateOnProperty("source", "repo_id")+`
		RETURN coalesce(source.id, source.uid) as source_id,
		       source.name as source_name,
		       coalesce(target.id, target.uid) as target_id,
		       target.name as target_name
	`, params)
	}
	return h.Neo4j.Run(ctx, `
		MATCH (source)-[:CALLS]->(target)
		WHERE `+nornicDBEntityUIDPredicate("source", "$entity_id")+access.GraphPredicateOnProperty("target", "repo_id")+`
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
