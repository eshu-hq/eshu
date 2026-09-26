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

// grantScopedEntityContentStore is the optional content port that reads one
// entity only when its repository is in an authorized set
// (ContentReader.GetEntityContentInRepositories, `entity_id = $1 AND
// repo_id = ANY(...)`).
type grantScopedEntityContentStore interface {
	GetEntityContentInRepositories(ctx context.Context, entityID string, repoIDs []string) (*EntityContent, error)
}

// grantScopedEntityNameSearcher is the optional content port that runs the
// corpus-wide substring name search bound to an authorized repository set in
// one statement (ContentReader.SearchEntitiesByNameInRepositories).
type grantScopedEntityNameSearcher interface {
	SearchEntitiesByNameInRepositories(ctx context.Context, repoIDs []string, entityType, name string, limit int) ([]EntityContent, error)
}

// relationshipsGrantBlocked reports whether the caller's grant admits nothing,
// so the route answers its unknown-entity 404 without reading either backend.
// Not an empty 200 and not a 403: the same answer an entity that does not exist
// produces, so a grantless caller cannot probe which symbols the index holds.
func relationshipsGrantBlocked(ctx context.Context, repoID string) bool {
	_, blocked := codeContentGrantScope(ctx, repoID)
	return blocked
}

// relationshipEntityContentForAccess reads one entity by id for the caller's
// grant. An unscoped caller reads it directly. A scoped caller reads it through
// GetEntityContentInRepositories, so an entity outside the grant is never
// fetched at all -- it answers nil, exactly like an id that does not exist,
// ahead of the fallback's builder check, so neither the body nor a 503-vs-404
// difference can confirm that an ungranted id exists. A store without the
// grant-scoped port fails closed. The shape mirrors contentread's
// getEntityContentForRepositoryAccess, which that package does not export.
func relationshipEntityContentForAccess(ctx context.Context, content ContentStore, entityID string) (*EntityContent, error) {
	access := codeGrantAccessFilter(ctx)
	if content == nil || access.Empty() {
		return nil, nil
	}
	if !access.Scoped() {
		return content.GetEntityContent(ctx, entityID)
	}
	store, ok := content.(grantScopedEntityContentStore)
	if !ok {
		return nil, nil
	}
	entity, err := store.GetEntityContentInRepositories(ctx, entityID, access.RepositorySearchIDs())
	if err != nil || entity == nil {
		return entity, err
	}
	if !access.AllowsRepositoryID(entity.RepoID) {
		return nil, nil
	}
	return entity, nil
}

// relationshipNameMatchesInGrant is the scoped caller's replacement for the
// corpus-wide SearchEntitiesByNameAnyRepo lookup of the content fallback: one
// statement bound to the whole grant, with the same LIMIT, so the fallback's
// "exactly one match" rule applies across the grant just as it applied across
// the corpus. A name held by two granted repositories stays ambiguous; a name
// held once in the grant and once outside it resolves to the granted copy.
// A store without the grant-scoped port fails closed.
func relationshipNameMatchesInGrant(
	ctx context.Context,
	content ContentStore,
	name string,
	allowed []string,
	limit int,
) ([]EntityContent, error) {
	searcher, ok := content.(grantScopedEntityNameSearcher)
	if !ok {
		return nil, nil
	}
	return searcher.SearchEntitiesByNameInRepositories(ctx, allowed, "", name, limit)
}
