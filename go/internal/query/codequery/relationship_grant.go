// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
)

// This file holds the #5167 grant helpers for POST /api/v0/code/relationships
// that sit outside its pinned graph readers. The graph reads bind the grant in
// their own statements (relationships.OneHopRelationshipsCypher,
// relationships.MetadataPredicate, nornicDBTransitiveOneHopRows,
// codemodel.RelationshipGraphRowCypherFromAnchor); what is here keeps the
// content-store fallback and the empty-grant case from answering around them.

// relationshipsGrantBlocked reports whether the caller's grant admits nothing,
// so the route answers its unknown-entity 404 without reading either backend.
// Not an empty 200 and not a 403: the same answer an entity that does not exist
// produces, so a grantless caller cannot probe which symbols the index holds.
func relationshipsGrantBlocked(ctx context.Context, repoID string) bool {
	_, blocked := codeContentGrantScope(ctx, repoID)
	return blocked
}

// relationshipNameMatchesInGrant is the scoped caller's replacement for the
// corpus-wide SearchEntitiesByNameAnyRepo name lookup of the content fallback.
// It asks each granted repository in turn and stops as soon as limit matches
// are in hand, the same per-repository shape
// story.ExactCandidatesPerRepository uses. The worst case reads limit rows per
// granted repository; the fallback asks for limit 2 because it resolves only a
// unique match.
//
// Reading the whole corpus and filtering afterward would be wrong twice: a
// page of the other tenant's rows could fill the limit and hide the granted
// match, and a name that exists once in the grant and once outside it would
// count as ambiguous and resolve to nothing.
func relationshipNameMatchesInGrant(
	ctx context.Context,
	content ContentStore,
	name string,
	allowed []string,
	limit int,
) ([]EntityContent, error) {
	matches := make([]EntityContent, 0, limit)
	for _, repoID := range allowed {
		if len(matches) >= limit {
			break
		}
		rows, err := content.SearchEntitiesByName(ctx, repoID, "", name, limit-len(matches))
		if err != nil {
			return nil, err
		}
		matches = append(matches, rows...)
	}
	return matches, nil
}
