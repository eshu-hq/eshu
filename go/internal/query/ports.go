// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"time"
)

// GraphQuery is the read-only graph traversal surface used by query handlers.
type GraphQuery interface {
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
	RunSingle(context.Context, string, map[string]any) (map[string]any, error)
}

// ContentStore is the relational content-query surface used by read handlers.
type ContentStore interface {
	GetFileContent(ctx context.Context, repoID, relativePath string) (*FileContent, error)
	GetFileLines(ctx context.Context, repoID, relativePath string, startLine, endLine int) (*FileContent, error)
	GetEntityContent(ctx context.Context, entityID string) (*EntityContent, error)
	SearchFileContent(ctx context.Context, repoID, pattern string, limit int) ([]FileContent, error)
	SearchFileContentAnyRepo(ctx context.Context, pattern string, limit int) ([]FileContent, error)
	SearchFileContentAnyRepoExactCase(ctx context.Context, pattern string, limit int) ([]FileContent, error)
	SearchEntityContent(ctx context.Context, repoID, pattern string, limit int) ([]EntityContent, error)
	SearchEntityContentAnyRepo(ctx context.Context, pattern string, limit int) ([]EntityContent, error)
	SearchEntitiesByName(ctx context.Context, repoID, entityType, name string, limit int) ([]EntityContent, error)
	SearchEntitiesByNameAnyRepo(ctx context.Context, entityType, name string, limit int) ([]EntityContent, error)
	SearchEntitiesReferencingComponent(ctx context.Context, repoID, componentName string, limit int) ([]EntityContent, error)
	ListRepoFiles(ctx context.Context, repoID string, limit int) ([]FileContent, error)
	ListRepoEntities(ctx context.Context, repoID string, limit int) ([]EntityContent, error)
	ListRepoEntitiesByType(ctx context.Context, repoID, entityType string, limit int) ([]EntityContent, error)
	ListRepoEntitiesByPaths(ctx context.Context, repoID string, relativePaths []string, limit int) ([]EntityContent, error)
	// ListRepoEntitiesByIDs hydrates the wide EntityContent rows for a bounded
	// entity-ID set (the impact-trace directed SELECTS scan re-fetches only the
	// Services that actually selector-match the traced Deployment; #5363).
	ListRepoEntitiesByIDs(ctx context.Context, repoID string, entityIDs []string, limit int) ([]EntityContent, error)
	// ListRepoK8sSelectCandidates returns the narrow, matcher-only projection of
	// a repository's K8sResource rows for the impact-trace directed SELECTS
	// candidate scan (#5363); it never carries the wide metadata JSONB.
	ListRepoK8sSelectCandidates(ctx context.Context, repoID string, limit int) ([]K8sSelectCandidate, error)
	SearchEntitiesByLanguageAndType(ctx context.Context, repoID, language, entityType, query string, limit int) ([]EntityContent, error)
	ListFrameworkRoutes(ctx context.Context, repoID string) ([]FrameworkRouteEvidence, error)
	RepositoryCoverage(ctx context.Context, repoID string) (RepositoryContentCoverage, error)
	// CountRepositoriesByLanguage, ListRepositoriesByLanguage, and
	// RepositoryLanguageInventory all aggregate over content_files, which is
	// keyed by repo_id but carries no scope grant of its own (#5167 Group B).
	// allScopes selects the admin/all-scopes path (no row filtering, byte-
	// identical to the pre-#5167 query). When allScopes is false, rows MUST be
	// restricted to allowedRepositoryIDs/allowedScopeIDs so a scoped caller
	// never observes another tenant's repository or language coverage; the
	// query handler (repository_language_inventory.go) short-circuits to an
	// empty page before calling these methods at all when a scoped caller
	// holds no grants, matching the #5137 LiveActivityStore precedent.
	CountRepositoriesByLanguage(
		ctx context.Context,
		languages []string,
		allScopes bool,
		allowedRepositoryIDs []string,
		allowedScopeIDs []string,
	) (RepositoryLanguageAggregate, error)
	ListRepositoriesByLanguage(
		ctx context.Context,
		languages []string,
		limit int,
		offset int,
		allScopes bool,
		allowedRepositoryIDs []string,
		allowedScopeIDs []string,
	) ([]RepositoryLanguageRepository, error)
	RepositoryLanguageInventory(
		ctx context.Context,
		limit int,
		offset int,
		allScopes bool,
		allowedRepositoryIDs []string,
		allowedScopeIDs []string,
	) ([]RepositoryLanguageInventoryRow, error)
	ListRepositories(ctx context.Context) ([]RepositoryCatalogEntry, error)
	MatchRepositories(ctx context.Context, selector string) ([]RepositoryCatalogEntry, error)
	ResolveRepository(ctx context.Context, selector string) (*RepositoryCatalogEntry, error)
}

// RepositoryContentCoverage is the content-store coverage summary for one repo.
type RepositoryContentCoverage struct {
	Available       bool
	FileCount       int
	EntityCount     int
	Languages       []RepositoryLanguageCount
	EntityTypes     []RepositoryEntityTypeCount
	FileIndexedAt   time.Time
	EntityIndexedAt time.Time
}

// RepositoryLanguageCount captures one language bucket in repo coverage.
type RepositoryLanguageCount struct {
	Language  string
	FileCount int
}

// RepositoryEntityTypeCount captures one entity-type bucket in repo coverage.
type RepositoryEntityTypeCount struct {
	EntityType string
	Count      int
}

// RepositoryLanguageAggregate captures corpus-level language coverage counts.
type RepositoryLanguageAggregate struct {
	RepositoryCount int
	FileCount       int
	LastIndexedAt   time.Time
}

// RepositoryLanguageRepository captures one repository matched by language.
type RepositoryLanguageRepository struct {
	Repository RepositoryCatalogEntry
	Languages  []RepositoryLanguageCount
	FileCount  int
	IndexedAt  time.Time
}

// RepositoryLanguageInventoryRow captures one language bucket across repositories.
type RepositoryLanguageInventoryRow struct {
	Language        string
	RepositoryCount int
	FileCount       int
	LastIndexedAt   time.Time
}

// RepositoryCatalogEntry is the relational repository catalog row used when the
// graph is unavailable in local lightweight mode.
type RepositoryCatalogEntry struct {
	ID        string
	Name      string
	Path      string
	LocalPath string
	RemoteURL string
	RepoSlug  string
	HasRemote bool
}
