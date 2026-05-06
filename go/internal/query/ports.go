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
	SearchEntitiesByLanguageAndType(ctx context.Context, repoID, language, entityType, query string, limit int) ([]EntityContent, error)
	ListFrameworkRoutes(ctx context.Context, repoID string) ([]FrameworkRouteEvidence, error)
	RepositoryCoverage(ctx context.Context, repoID string) (RepositoryContentCoverage, error)
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
	FileIndexedAt   time.Time
	EntityIndexedAt time.Time
}

// RepositoryLanguageCount captures one language bucket in repo coverage.
type RepositoryLanguageCount struct {
	Language  string
	FileCount int
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
