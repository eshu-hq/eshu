// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "context"

// This file holds the batch half of the entity-content repository-access
// helpers. It split from content_entity_authz.go in the #6060 lane-B1 move:
// the single-entity helper moved with the ContentHandler family to
// internal/query/contentread, while this batch path stays in root because it
// filters through filterEvidenceCitationEntitiesForAccess, which belongs to
// the evidence handler family (evidence_citation.go) -- moving it would
// couple contentread to a sibling lane.

type repositoryScopedEntityBatchContentStore interface {
	GetEntityContentsInRepositories(ctx context.Context, entityIDs []string, repoIDs []string) (map[string]*EntityContent, error)
}

type entityContentBatchStore interface {
	GetEntityContents(context.Context, []string) (map[string]*EntityContent, error)
}

func getEntityContentsForRepositoryAccess(
	ctx context.Context,
	content ContentStore,
	entityIDs []string,
	access repositoryAccessFilter,
) (map[string]*EntityContent, error) {
	if content == nil || access.Empty() || len(entityIDs) == 0 {
		return map[string]*EntityContent{}, nil
	}
	if !access.Scoped() {
		if store, ok := content.(entityContentBatchStore); ok {
			return store.GetEntityContents(ctx, entityIDs)
		}
		return getEntityContentsOneAtATime(ctx, content, entityIDs, access)
	}
	repoIDs := access.RepositorySearchIDs()
	if len(repoIDs) == 0 {
		return map[string]*EntityContent{}, nil
	}
	store, ok := content.(repositoryScopedEntityBatchContentStore)
	if !ok {
		return map[string]*EntityContent{}, nil
	}
	entities, err := store.GetEntityContentsInRepositories(ctx, entityIDs, repoIDs)
	if err != nil {
		return nil, err
	}
	return filterEvidenceCitationEntitiesForAccess(entities, access), nil
}

func getEntityContentsOneAtATime(
	ctx context.Context,
	content ContentStore,
	entityIDs []string,
	access repositoryAccessFilter,
) (map[string]*EntityContent, error) {
	results := make(map[string]*EntityContent, len(entityIDs))
	for _, entityID := range entityIDs {
		entity, err := content.GetEntityContent(ctx, entityID)
		if err != nil {
			return nil, err
		}
		if entity != nil && access.AllowsRepositoryID(entity.RepoID) {
			results[entityID] = entity
		}
	}
	return results, nil
}
