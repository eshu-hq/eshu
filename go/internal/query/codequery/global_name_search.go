// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/search"
)

// searchGlobalEntityNames runs the global entity-name lookup through
// the search leaf. The handler names this spelling.
func (h *CodeHandler) searchGlobalEntityNames(ctx context.Context, name, language string, limit int, exact bool) ([]map[string]any, error) {
	return search.SearchGlobalEntityNames(ctx, contentStoreOf(h), name, language, limit, exact)
}

// This file is the narrow surviving remnant of the #6060 lane-A P0 shim
// (formerly family_code_shim_story.go in root package query). Every other
// entry in that shim was deleted at the move and its call sites repointed
// to name codemodel/codeshaping/codeowners/querycontract directly (see the
// #6060 move commit). The entries below stay because their callers,
// relationshipsGraphRow and transitiveRelationshipsGraphRow, carry a typed
// non_hot pin in
// internal/queryplan/testdata/query-source-coverage.yaml whose source_sha256
// covers the function text from the `func` keyword through the closing brace.
// Resolving these names locally keeps those bodies free of package
// qualifiers, so a pure rename or move does not re-derive the digest. A
// change that alters what a pinned reader queries re-derives its digest on
// purpose, as #7057 did for relationshipsGraphRow and #5167 did for both
// readers when it bound the caller's repository grant into them. Delete an entry only when
// no pinned reader calls it.

// relationshipsRequest aliases the leaf-owned lookup request so
// transitiveRelationshipsGraphRow's signature stays unchanged.
type relationshipsRequest = codemodel.RelationshipsRequest

// relationshipGraphRowCypher forwards to the leaf-owned row fragment so
// relationshipsGraphRow's call site stays unchanged.
func relationshipGraphRowCypher(predicate string, access repositoryAccessFilter) string {
	return codemodel.RelationshipGraphRowCypher(predicate, access)
}

// relationshipGraphRowCypherFromAnchor forwards to the leaf-owned row
// fragment that takes a whole entity-binding clause, which the Neo4j
// entity-id branch of relationshipsGraphRow uses (issue #7057).
func relationshipGraphRowCypherFromAnchor(anchorClause string, access repositoryAccessFilter) string {
	return codemodel.RelationshipGraphRowCypherFromAnchor(anchorClause, access)
}

// neo4jEntityIDAnchor forwards to the leaf-owned indexed Neo4j entity-id
// anchor (issue #7057).
func neo4jEntityIDAnchor(alias string, param string) string {
	return codemodel.Neo4jEntityIDAnchor(alias, param)
}

// relationshipGraphRowCypherAnchored forwards to the leaf-owned anchored row
// fragment so relationshipsGraphRow's repo-anchored call site stays
// unchanged (issue #6786 defect 2).
func relationshipGraphRowCypherAnchored(matchClause, predicate string, access repositoryAccessFilter) string {
	return codemodel.RelationshipGraphRowCypherAnchored(matchClause, predicate, access)
}

// buildTransitiveRelationshipRowsCypher forwards to the leaf-owned
// traversal builder so transitiveRelationshipsGraphRow's call site stays
// unchanged.
func buildTransitiveRelationshipRowsCypher(
	entityID string,
	direction string,
	maxDepth int,
	backend GraphBackend,
	access repositoryAccessFilter,
) (string, map[string]any) {
	return codemodel.BuildTransitiveRelationshipRowsCypher(entityID, direction, maxDepth, backend, access)
}

// buildTransitiveRelationshipGraphResponse forwards to the leaf-owned
// traversal shaper so transitiveRelationshipsGraphRow's call site stays
// unchanged.
func buildTransitiveRelationshipGraphResponse(
	metadataRow map[string]any,
	rows []map[string]any,
	direction string,
) map[string]any {
	return codemodel.BuildTransitiveRelationshipGraphResponse(metadataRow, rows, direction)
}
