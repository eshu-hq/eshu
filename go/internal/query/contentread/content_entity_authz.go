// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

type repositoryScopedEntityContentStore interface {
	GetEntityContentInRepositories(ctx context.Context, entityID string, repoIDs []string) (*querycontract.EntityContent, error)
}

// The batch helpers (getEntityContentsForRepositoryAccess and
// getEntityContentsOneAtATime) stay in root package query
// (content_entity_access_batch.go): the batch path filters through
// filterEvidenceCitationEntitiesForAccess, which belongs to the evidence
// handler family in root, so moving it here would couple this package to a
// sibling lane.

func getEntityContentForRepositoryAccess(
	ctx context.Context,
	content querycontract.ContentStore,
	entityID string,
	access querycontract.RepositoryAccessFilter,
) (*querycontract.EntityContent, error) {
	if content == nil || access.Empty() {
		return nil, nil
	}
	if !access.Scoped() {
		return content.GetEntityContent(ctx, entityID)
	}
	repoIDs := access.RepositorySearchIDs()
	if len(repoIDs) == 0 {
		return nil, nil
	}
	store, ok := content.(repositoryScopedEntityContentStore)
	if !ok {
		return nil, nil
	}
	entity, err := store.GetEntityContentInRepositories(ctx, entityID, repoIDs)
	if err != nil || entity == nil {
		return entity, err
	}
	if !access.AllowsRepositoryID(entity.RepoID) {
		return nil, nil
	}
	return entity, nil
}
