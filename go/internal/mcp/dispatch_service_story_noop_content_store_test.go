// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// mcpNoopContentStore is a no-op query.ContentStore double used by the
// dispatch_service_story_test.go handler tests. Split out of that file to
// keep it under the repository's 500-line cap (#5764 round-6 review
// follow-up added ListRepoEntitiesByTypes, which pushed the combined file
// over the limit).
type mcpNoopContentStore struct{}

func (mcpNoopContentStore) GetFileContent(context.Context, string, string) (*query.FileContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) GetFileLines(context.Context, string, string, int, int) (*query.FileContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) GetEntityContent(context.Context, string) (*query.EntityContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) SearchFileContent(context.Context, string, string, int) ([]query.FileContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) SearchFileContentAnyRepo(context.Context, string, int) ([]query.FileContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) SearchFileContentAnyRepoExactCase(context.Context, string, int) ([]query.FileContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) SearchEntityContent(context.Context, string, string, int) ([]query.EntityContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) SearchEntityContentAnyRepo(context.Context, string, int) ([]query.EntityContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) SearchEntitiesByName(context.Context, string, string, string, int) ([]query.EntityContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) SearchEntitiesByNameAnyRepo(context.Context, string, string, int) ([]query.EntityContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) SearchEntitiesReferencingComponent(context.Context, string, string, int) ([]query.EntityContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) ListRepoFiles(context.Context, string, int) ([]query.FileContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) ListRepoEntities(context.Context, string, int) ([]query.EntityContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) ListRepoEntitiesByType(context.Context, string, string, int) ([]query.EntityContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) ListRepoEntitiesByTypes(context.Context, string, []string, int) ([]query.EntityContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) ListRepoEntitiesByPaths(context.Context, string, []string, int) ([]query.EntityContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) SearchEntitiesByLanguageAndType(
	context.Context,
	string,
	string,
	string,
	string,
	int,
) ([]query.EntityContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) ListFrameworkRoutes(context.Context, string) ([]query.FrameworkRouteEvidence, error) {
	return nil, nil
}

func (mcpNoopContentStore) RepositoryCoverage(context.Context, string) (query.RepositoryContentCoverage, error) {
	return query.RepositoryContentCoverage{}, nil
}

func (mcpNoopContentStore) CountRepositoriesByLanguage(
	context.Context,
	[]string,
	bool,
	[]string,
	[]string,
) (query.RepositoryLanguageAggregate, error) {
	return query.RepositoryLanguageAggregate{}, nil
}

func (mcpNoopContentStore) ListRepositoriesByLanguage(
	context.Context,
	[]string,
	int,
	int,
	bool,
	[]string,
	[]string,
) ([]query.RepositoryLanguageRepository, error) {
	return nil, nil
}

func (mcpNoopContentStore) RepositoryLanguageInventory(
	context.Context,
	int,
	int,
	bool,
	[]string,
	[]string,
) ([]query.RepositoryLanguageInventoryRow, error) {
	return nil, nil
}

func (mcpNoopContentStore) ListRepositories(context.Context) ([]query.RepositoryCatalogEntry, error) {
	return nil, nil
}

func (mcpNoopContentStore) MatchRepositories(context.Context, string) ([]query.RepositoryCatalogEntry, error) {
	return nil, nil
}

func (mcpNoopContentStore) ResolveRepository(context.Context, string) (*query.RepositoryCatalogEntry, error) {
	return nil, nil
}

var _ query.ContentStore = mcpNoopContentStore{}
