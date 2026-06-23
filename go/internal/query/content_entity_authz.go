package query

import "context"

type repositoryScopedEntityContentStore interface {
	GetEntityContentInRepositories(ctx context.Context, entityID string, repoIDs []string) (*EntityContent, error)
}

type repositoryScopedEntityBatchContentStore interface {
	GetEntityContentsInRepositories(ctx context.Context, entityIDs []string, repoIDs []string) (map[string]*EntityContent, error)
}

type entityContentBatchStore interface {
	GetEntityContents(context.Context, []string) (map[string]*EntityContent, error)
}

func getEntityContentForRepositoryAccess(
	ctx context.Context,
	content ContentStore,
	entityID string,
	access repositoryAccessFilter,
) (*EntityContent, error) {
	if content == nil || access.empty() {
		return nil, nil
	}
	if !access.scoped() {
		return content.GetEntityContent(ctx, entityID)
	}
	repoIDs := access.repositorySearchIDs()
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
	if !access.allowsRepositoryID(entity.RepoID) {
		return nil, nil
	}
	return entity, nil
}

func getEntityContentsForRepositoryAccess(
	ctx context.Context,
	content ContentStore,
	entityIDs []string,
	access repositoryAccessFilter,
) (map[string]*EntityContent, error) {
	if content == nil || access.empty() || len(entityIDs) == 0 {
		return map[string]*EntityContent{}, nil
	}
	if !access.scoped() {
		if store, ok := content.(entityContentBatchStore); ok {
			return store.GetEntityContents(ctx, entityIDs)
		}
		return getEntityContentsOneAtATime(ctx, content, entityIDs, access)
	}
	repoIDs := access.repositorySearchIDs()
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
		if entity != nil && access.allowsRepositoryID(entity.RepoID) {
			results[entityID] = entity
		}
	}
	return results, nil
}
