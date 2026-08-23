// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "context"

// GraphQuery is the concurrent-safe, read-only graph traversal port.
type GraphQuery interface {
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
	RunSingle(context.Context, string, map[string]any) (map[string]any, error)
}

// ContentStore is the relational content-query port used by query families.
type ContentStore interface {
	GetFileContent(context.Context, string, string) (*FileContent, error)
	GetFileLines(context.Context, string, string, int, int) (*FileContent, error)
	GetEntityContent(context.Context, string) (*EntityContent, error)
	SearchFileContent(context.Context, string, string, int) ([]FileContent, error)
	SearchFileContentAnyRepo(context.Context, string, int) ([]FileContent, error)
	SearchFileContentAnyRepoExactCase(context.Context, string, int) ([]FileContent, error)
	SearchEntityContent(context.Context, string, string, int) ([]EntityContent, error)
	SearchEntityContentAnyRepo(context.Context, string, int) ([]EntityContent, error)
	SearchEntitiesByName(context.Context, string, string, string, int) ([]EntityContent, error)
	SearchEntitiesByNameAnyRepo(context.Context, string, string, int) ([]EntityContent, error)
	SearchEntitiesReferencingComponent(context.Context, string, string, int) ([]EntityContent, error)
	ListRepoFiles(context.Context, string, int) ([]FileContent, error)
	ListRepoEntities(context.Context, string, int) ([]EntityContent, error)
	ListRepoEntitiesByType(context.Context, string, string, int) ([]EntityContent, error)
	ListRepoEntitiesByTypes(context.Context, string, []string, int) ([]EntityContent, error)
	ListRepoEntitiesByPaths(context.Context, string, []string, int) ([]EntityContent, error)
	ListRepoEntitiesByIDs(context.Context, string, []string, int) ([]EntityContent, error)
	ListRepoK8sSelectCandidates(context.Context, string, int) ([]K8sSelectCandidate, error)
	SearchEntitiesByLanguageAndType(context.Context, string, string, string, string, int) ([]EntityContent, error)
	ListFrameworkRoutes(context.Context, string) ([]FrameworkRouteEvidence, error)
	RepositoryCoverage(context.Context, string) (RepositoryContentCoverage, error)
	CountRepositoriesByLanguage(context.Context, []string, bool, []string, []string) (RepositoryLanguageAggregate, error)
	ListRepositoriesByLanguage(context.Context, []string, int, int, bool, []string, []string) ([]RepositoryLanguageRepository, error)
	RepositoryLanguageInventory(context.Context, int, int, bool, []string, []string) ([]RepositoryLanguageInventoryRow, error)
	ListRepositories(context.Context) ([]RepositoryCatalogEntry, error)
	MatchRepositories(context.Context, string) ([]RepositoryCatalogEntry, error)
	ResolveRepository(context.Context, string) (*RepositoryCatalogEntry, error)
}
