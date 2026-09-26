// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"path/filepath"
	"strings"
	"time"
)

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

// EntityContentSearchRow shapes one entity content row into the wire map the
// entity content search route returns. It carries every EntityContent JSON
// field (omitting the same optional fields when empty) plus a source_handle so
// a caller can drill into the full body through get_entity_content or
// get_file_lines when source_cache was clipped. The row is a fresh map, so the
// caller may clip it in place with ClipRowSourceCache.
func EntityContentSearchRow(entity EntityContent) map[string]any {
	row := map[string]any{
		"entity_id":     entity.EntityID,
		"repo_id":       entity.RepoID,
		"relative_path": entity.RelativePath,
		"entity_type":   entity.EntityType,
		"entity_name":   entity.EntityName,
		"start_line":    entity.StartLine,
		"end_line":      entity.EndLine,
		"source_handle": map[string]any{
			"repo_id":    entity.RepoID,
			"file_path":  entity.RelativePath,
			"start_line": entity.StartLine,
			"end_line":   entity.EndLine,
		},
	}
	if entity.RepoName != "" {
		row["repo_name"] = entity.RepoName
	}
	if entity.Language != "" {
		row["language"] = entity.Language
	}
	if entity.SourceCache != "" {
		row["source_cache"] = entity.SourceCache
	}
	if len(entity.Metadata) > 0 {
		row["metadata"] = entity.Metadata
	}
	if entity.SearchBackend != "" {
		row["search_backend"] = entity.SearchBackend
	}
	return row
}

// EntityContentSearchRows shapes a page of entity content into wire rows.
func EntityContentSearchRows(entities []EntityContent) []map[string]any {
	rows := make([]map[string]any, 0, len(entities))
	for _, entity := range entities {
		rows = append(rows, EntityContentSearchRow(entity))
	}
	return rows
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

// IsServiceEvidenceCandidate reports whether file looks like service
// evidence (an API spec, route/server/ingress definition, or deployment
// values file) for the normalized service name. It lives here (not in a
// handler family) so the repository framework-signal read and the service
// evidence stayer share one predicate without importing each other (#6060,
// lane B B3).
func IsServiceEvidenceCandidate(file FileContent, normalizedServiceName string) bool {
	path := strings.ToLower(file.RelativePath)
	if path == "" {
		return false
	}
	if normalizedServiceName != "" && strings.Contains(NormalizeEvidenceToken(path), normalizedServiceName) {
		return true
	}

	switch filepath.Ext(path) {
	case ".yaml", ".yml", ".json", ".js", ".mjs", ".cjs", ".ts", ".mts", ".cts", ".md":
	default:
		return false
	}

	for _, keyword := range []string{
		"openapi", "swagger", "spec", "docs", "route", "server", "ingress",
		"gateway", "deploy", "values", "config", "application",
	} {
		if strings.Contains(path, keyword) {
			return true
		}
	}
	return false
}

// ServiceAPIEndpointEvidence captures one API endpoint path from an API spec.
type ServiceAPIEndpointEvidence struct {
	Path         string   `json:"path"`
	Methods      []string `json:"methods,omitempty"`
	OperationIDs []string `json:"operation_ids,omitempty"`
}

// SpecFileResolver resolves a relative `$ref` path from a base spec file and
// returns the raw content of the referenced file. An empty string with a nil
// error means the reference resolved to nothing the repository holds; a read
// failure is returned as an error and never collapsed into that same empty
// string (#5720 round 10). The port lives here so both the service/evidence
// leaf (which implements the parsing) and root callers share one type
// without importing each other (#6060, lane B B3).
type SpecFileResolver func(baseRelativePath, ref string) (string, error)

// ServiceAPISpecEvidence summarizes one API spec file and its parsed routes,
// server hostnames, and operation IDs when available. The structs live here
// (not in a handler family) so the repository narrative enrichment and the
// service evidence stayer share one shape without importing each other
// (#6060, lane B B3).
type ServiceAPISpecEvidence struct {
	RelativePath     string                       `json:"relative_path"`
	Format           string                       `json:"format"`
	Parsed           bool                         `json:"parsed"`
	SpecVersion      string                       `json:"spec_version,omitempty"`
	APIVersion       string                       `json:"api_version,omitempty"`
	EndpointCount    int                          `json:"endpoint_count,omitempty"`
	MethodCount      int                          `json:"method_count,omitempty"`
	OperationIDCount int                          `json:"operation_id_count,omitempty"`
	DocsRoutes       []string                     `json:"docs_routes,omitempty"`
	Hostnames        []string                     `json:"hostnames,omitempty"`
	Endpoints        []ServiceAPIEndpointEvidence `json:"endpoints,omitempty"`
}
