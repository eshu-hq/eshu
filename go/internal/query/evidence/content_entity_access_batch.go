// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file holds the batch half of the entity-content repository-access
// helpers. It split from content_entity_authz.go in the #6060 lane-B1 move,
// and moved with the evidence handler family to internal/query/evidence in
// the #6642 evidence-leaf move: its sole caller is
// evidenceCitationEntityContents (citation.go), so carrying it keeps the
// batch path beside the citation synthesis that owns the access filter.

type repositoryScopedEntityBatchContentStore interface {
	GetEntityContentsInRepositories(ctx context.Context, entityIDs []string, repoIDs []string) (map[string]*querycontract.EntityContent, error)
}

type entityContentBatchStore interface {
	GetEntityContents(context.Context, []string) (map[string]*querycontract.EntityContent, error)
}

func getEntityContentsForRepositoryAccess(
	ctx context.Context,
	content querycontract.ContentStore,
	entityIDs []string,
	access querycontract.RepositoryAccessFilter,
) (map[string]*querycontract.EntityContent, error) {
	if content == nil || access.Empty() || len(entityIDs) == 0 {
		return map[string]*querycontract.EntityContent{}, nil
	}
	if !access.Scoped() {
		if store, ok := content.(entityContentBatchStore); ok {
			return store.GetEntityContents(ctx, entityIDs)
		}
		return getEntityContentsOneAtATime(ctx, content, entityIDs, access)
	}
	repoIDs := access.RepositorySearchIDs()
	if len(repoIDs) == 0 {
		return map[string]*querycontract.EntityContent{}, nil
	}
	store, ok := content.(repositoryScopedEntityBatchContentStore)
	if !ok {
		return map[string]*querycontract.EntityContent{}, nil
	}
	entities, err := store.GetEntityContentsInRepositories(ctx, entityIDs, repoIDs)
	if err != nil {
		return nil, err
	}
	return filterEvidenceCitationEntitiesForAccess(entities, access), nil
}

func getEntityContentsOneAtATime(
	ctx context.Context,
	content querycontract.ContentStore,
	entityIDs []string,
	access querycontract.RepositoryAccessFilter,
) (map[string]*querycontract.EntityContent, error) {
	results := make(map[string]*querycontract.EntityContent, len(entityIDs))
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
