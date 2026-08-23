// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "time"

// FileContent is one file from the content store.
type FileContent struct {
	RepoID       string `json:"repo_id"`
	RelativePath string `json:"relative_path"`
	CommitSHA    string `json:"commit_sha,omitempty"`
	Content      string `json:"content"`
	ContentHash  string `json:"content_hash"`
	LineCount    int    `json:"line_count"`
	Language     string `json:"language,omitempty"`
	ArtifactType string `json:"artifact_type,omitempty"`
	// SearchBackend is set to "hybrid" only on rows reordered by the bounded
	// in-request BM25+vector re-rank; it is empty (and omitted on the wire) when
	// the lexical content-index order was served, so the lexical truth basis
	// stays authoritative.
	SearchBackend string `json:"search_backend,omitempty"`
}

// EntityContent is one parsed entity from the content store.
type EntityContent struct {
	EntityID     string         `json:"entity_id"`
	RepoID       string         `json:"repo_id"`
	RepoName     string         `json:"repo_name,omitempty"`
	RelativePath string         `json:"relative_path"`
	EntityType   string         `json:"entity_type"`
	EntityName   string         `json:"entity_name"`
	StartLine    int            `json:"start_line"`
	EndLine      int            `json:"end_line"`
	Language     string         `json:"language,omitempty"`
	SourceCache  string         `json:"source_cache,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	// SearchBackend is set to "hybrid" only on rows reordered by the bounded
	// in-request BM25+vector re-rank; it is empty (and omitted on the wire) when
	// the lexical content-index order was served, so the lexical truth basis
	// stays authoritative.
	SearchBackend string `json:"search_backend,omitempty"`
}

// K8sSelectCandidate is the narrow content projection used for SELECTS matching.
type K8sSelectCandidate struct {
	EntityID                 string
	EntityName               string
	Kind                     string
	Namespace                string
	Selector                 string
	SelectorPresent          bool
	PodTemplateLabels        string
	PodTemplateLabelsPresent bool
}

// FrameworkRouteEvidence captures parser-observed framework routes.
type FrameworkRouteEvidence struct {
	Framework    string                        `json:"framework"`
	RelativePath string                        `json:"relative_path"`
	RoutePaths   []string                      `json:"route_paths"`
	RouteMethods []string                      `json:"route_methods"`
	RouteEntries []FrameworkRouteEntryEvidence `json:"route_entries,omitempty"`
}

// FrameworkRouteEntryEvidence captures one parser-observed route declaration.
type FrameworkRouteEntryEvidence struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Handler string `json:"handler,omitempty"`
}

// RepositoryContentCoverage summarizes indexed content for one repository.
type RepositoryContentCoverage struct {
	Available       bool
	FileCount       int
	EntityCount     int
	Languages       []RepositoryLanguageCount
	EntityTypes     []RepositoryEntityTypeCount
	FileIndexedAt   time.Time
	EntityIndexedAt time.Time
}

// RepositoryLanguageCount captures one language bucket in repository coverage.
type RepositoryLanguageCount struct {
	Language  string
	FileCount int
}

// RepositoryEntityTypeCount captures one entity-type bucket in repository coverage.
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

// RepositoryLanguageInventoryRow captures one language across repositories.
type RepositoryLanguageInventoryRow struct {
	Language        string
	RepositoryCount int
	FileCount       int
	LastIndexedAt   time.Time
}

// RepositoryCatalogEntry is one relational repository catalog row.
type RepositoryCatalogEntry struct {
	ID        string
	Name      string
	Path      string
	LocalPath string
	RemoteURL string
	RepoSlug  string
	HasRemote bool
}
