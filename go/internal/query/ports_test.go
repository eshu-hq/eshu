// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"
	"time"
)

type fakePortGraphQuery struct{}

func (fakePortGraphQuery) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (fakePortGraphQuery) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

type fakePortContentStore struct {
	coverage                     RepositoryContentCoverage
	summary                      repositoryReadModelSummary
	relationshipReadModel        repositoryRelationshipReadModel
	entryPoints                  repositoryEntryPointReadModel
	deploymentEvidence           repositoryDeploymentEvidenceReadModel
	deploymentEvidenceErr        error
	relationshipEvidence         relationshipEvidenceReadModel
	documentationFindingsModel   documentationFindingListReadModel
	documentationFindingsErr     error
	documentationFindingsFilter  *documentationFindingFilter
	documentationFactsModel      documentationFactListReadModel
	documentationFactsErr        error
	documentationFactsFilter     *documentationFactFilter
	documentationPacketModel     documentationEvidencePacketReadModel
	documentationPacketErr       error
	documentationPacketFilter    *documentationEvidencePacketFilter
	documentationFreshnessModel  documentationEvidencePacketFreshnessReadModel
	documentationFreshnessErr    error
	documentationFreshnessFilter *documentationEvidencePacketFreshnessFilter
	targetSupportModel           serviceStoryTargetSupportReadModel
	targetSupportErr             error
	entities                     []EntityContent
	repoFiles                    []FileContent
	repositoryRefs               []RepositoryRef
	repositories                 []RepositoryCatalogEntry
	languageRepos                []RepositoryLanguageRepository
	languageCounts               map[string]RepositoryLanguageAggregate
	languageInventory            []RepositoryLanguageInventoryRow
	workloadIdentities           []CatalogWorkloadIdentityEntry
}

func (f fakePortContentStore) GetFileContent(_ context.Context, repoID, relativePath string) (*FileContent, error) {
	for i := range f.repoFiles {
		file := f.repoFiles[i]
		if file.RepoID != "" && repoID != "" && file.RepoID != repoID {
			continue
		}
		if file.RelativePath == relativePath {
			return &file, nil
		}
	}
	return nil, nil
}

func (f fakePortContentStore) GetFileLines(context.Context, string, string, int, int) (*FileContent, error) {
	return nil, nil
}

func (f fakePortContentStore) GetEntityContent(context.Context, string) (*EntityContent, error) {
	return nil, nil
}

func (f fakePortContentStore) SearchFileContent(context.Context, string, string, int) ([]FileContent, error) {
	return nil, nil
}

func (f fakePortContentStore) SearchFileContentAnyRepo(context.Context, string, int) ([]FileContent, error) {
	return nil, nil
}

func (f fakePortContentStore) SearchFileContentAnyRepoExactCase(context.Context, string, int) ([]FileContent, error) {
	return nil, nil
}

func (f fakePortContentStore) SearchEntityContent(context.Context, string, string, int) ([]EntityContent, error) {
	return nil, nil
}

func (f fakePortContentStore) SearchEntityContentAnyRepo(context.Context, string, int) ([]EntityContent, error) {
	return nil, nil
}

func (f fakePortContentStore) SearchEntitiesByName(context.Context, string, string, string, int) ([]EntityContent, error) {
	return nil, nil
}

func (f fakePortContentStore) SearchEntitiesByNameAnyRepo(context.Context, string, string, int) ([]EntityContent, error) {
	return nil, nil
}

func (f fakePortContentStore) SearchEntitiesReferencingComponent(context.Context, string, string, int) ([]EntityContent, error) {
	return nil, nil
}

func (f fakePortContentStore) ListRepoFiles(_ context.Context, repoID string, limit int) ([]FileContent, error) {
	files := make([]FileContent, 0, len(f.repoFiles))
	for _, file := range f.repoFiles {
		if file.RepoID != "" && repoID != "" && file.RepoID != repoID {
			continue
		}
		files = append(files, file)
		if limit > 0 && len(files) >= limit {
			break
		}
	}
	return files, nil
}

func (f fakePortContentStore) ListRepositoryRefs(context.Context, string) ([]RepositoryRef, error) {
	return append([]RepositoryRef(nil), f.repositoryRefs...), nil
}

func (f fakePortContentStore) ListRepoEntities(_ context.Context, _ string, limit int) ([]EntityContent, error) {
	if limit > 0 && limit < len(f.entities) {
		return append([]EntityContent(nil), f.entities[:limit]...), nil
	}
	return append([]EntityContent(nil), f.entities...), nil
}

// ListRepoEntitiesByType filters f.entities by entity_type before applying
// limit, mirroring the production ContentReader.ListRepoEntitiesByType
// predicate order (type filter first, then limit) so callers exercising the
// double still see the truncation-avoidance behavior the real query provides.
func (f fakePortContentStore) ListRepoEntitiesByType(_ context.Context, repoID, entityType string, limit int) ([]EntityContent, error) {
	filtered := make([]EntityContent, 0, len(f.entities))
	for _, entity := range f.entities {
		if repoID != "" && entity.RepoID != "" && entity.RepoID != repoID {
			continue
		}
		if entity.EntityType != entityType {
			continue
		}
		filtered = append(filtered, entity)
		if limit > 0 && len(filtered) >= limit {
			break
		}
	}
	return filtered, nil
}

// ListRepoEntitiesByTypes filters f.entities by entity_type SET membership
// before applying limit, mirroring the production
// ContentReader.ListRepoEntitiesByTypes predicate order (`entity_type =
// ANY($types)` then `LIMIT`) so callers exercising the double see the same
// type-filtered-bound behavior the real query provides (#5764 P1 review
// follow-up).
func (f fakePortContentStore) ListRepoEntitiesByTypes(_ context.Context, repoID string, entityTypes []string, limit int) ([]EntityContent, error) {
	allowed := make(map[string]struct{}, len(entityTypes))
	for _, entityType := range entityTypes {
		allowed[entityType] = struct{}{}
	}
	filtered := make([]EntityContent, 0, len(f.entities))
	for _, entity := range f.entities {
		if repoID != "" && entity.RepoID != "" && entity.RepoID != repoID {
			continue
		}
		if _, ok := allowed[entity.EntityType]; !ok {
			continue
		}
		filtered = append(filtered, entity)
		if limit > 0 && len(filtered) >= limit {
			break
		}
	}
	return filtered, nil
}

func (f fakePortContentStore) ListRepoEntitiesByPaths(
	_ context.Context,
	repoID string,
	relativePaths []string,
	limit int,
) ([]EntityContent, error) {
	pathSet := map[string]struct{}{}
	for _, path := range relativePaths {
		pathSet[path] = struct{}{}
	}
	results := make([]EntityContent, 0)
	for _, entity := range f.entities {
		if entity.RepoID != repoID {
			continue
		}
		if _, ok := pathSet[entity.RelativePath]; !ok {
			continue
		}
		results = append(results, entity)
		if limit > 0 && len(results) >= limit {
			break
		}
	}
	return results, nil
}

func (f fakePortContentStore) SearchEntitiesByLanguageAndType(context.Context, string, string, string, string, int) ([]EntityContent, error) {
	return nil, nil
}

func (f fakePortContentStore) ListFrameworkRoutes(context.Context, string) ([]FrameworkRouteEvidence, error) {
	return nil, nil
}

func (f fakePortContentStore) RepositoryCoverage(context.Context, string) (RepositoryContentCoverage, error) {
	return f.coverage, nil
}

// fakePortContentStore's language-inventory and documentation-read-model
// methods (CountRepositoriesByLanguage through
// documentationEvidencePacketFreshnessWithFilter, plus the
// fakeFilterLanguageRepos helper) live in
// ports_test_language_documentation_test.go, split out to keep this file under
// the repository's 500-line cap.

func (f fakePortContentStore) ListRepositories(context.Context) ([]RepositoryCatalogEntry, error) {
	return append([]RepositoryCatalogEntry(nil), f.repositories...), nil
}

func (f fakePortContentStore) ListWorkloadIdentities(
	context.Context,
	int,
) ([]CatalogWorkloadIdentityEntry, bool, error) {
	return append([]CatalogWorkloadIdentityEntry(nil), f.workloadIdentities...), false, nil
}

func (f fakePortContentStore) MatchRepositories(_ context.Context, selector string) ([]RepositoryCatalogEntry, error) {
	matches := make([]RepositoryCatalogEntry, 0, 1)
	for _, repo := range f.repositories {
		switch selector {
		case repo.ID, repo.Name, repo.Path, repo.LocalPath, repo.RemoteURL, repo.RepoSlug:
			matches = append(matches, repo)
		}
	}
	return matches, nil
}

func (f fakePortContentStore) ResolveRepository(context.Context, string) (*RepositoryCatalogEntry, error) {
	if len(f.repositories) == 0 {
		return nil, nil
	}
	repo := f.repositories[0]
	return &repo, nil
}

var (
	_ GraphQuery   = (*fakePortGraphQuery)(nil)
	_ ContentStore = (*fakePortContentStore)(nil)
)

func TestQueryHandlersAcceptCapabilityPorts(t *testing.T) {
	t.Parallel()

	graph := fakePortGraphQuery{}
	content := fakePortContentStore{}

	_ = &CodeHandler{Neo4j: graph, Content: content}
	_ = &EntityHandler{Neo4j: graph, Content: content}
	_ = &RepositoryHandler{Neo4j: graph, Content: content}
	_ = &ImpactHandler{Neo4j: graph, Content: content}
	_ = &IaCHandler{Content: content}
	_ = &LanguageQueryHandler{Neo4j: graph, Content: content}
	_ = &CompareHandler{Neo4j: graph, Content: content}
	_ = &ContentHandler{Content: content}
	_ = &EvidenceHandler{Content: content}
	_ = &DocumentationHandler{Content: content}
	_ = &StatusHandler{Neo4j: graph}
}

func TestQueryContentStoreCoverageUsesContentStorePort(t *testing.T) {
	t.Parallel()

	contentIndexedAt := time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC)
	entityIndexedAt := time.Date(2026, 4, 19, 10, 5, 0, 0, time.UTC)

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"count(DISTINCT e) as entity_count": {
					"file_count":   int64(12),
					"entity_count": int64(9),
				},
			},
		},
		Content: fakePortContentStore{
			coverage: RepositoryContentCoverage{
				Available:       true,
				FileCount:       10,
				EntityCount:     7,
				FileIndexedAt:   contentIndexedAt,
				EntityIndexedAt: entityIndexedAt,
				Languages: []RepositoryLanguageCount{
					{Language: "go", FileCount: 8},
					{Language: "yaml", FileCount: 2},
				},
			},
		},
	}

	got, err := handler.queryContentStoreCoverage(t.Context(), "repo-coverage")
	if err != nil {
		t.Fatalf("queryContentStoreCoverage() error = %v, want nil", err)
	}
	if got, want := got["file_count"], 10; got != want {
		t.Fatalf("file_count = %#v, want %#v", got, want)
	}
	if got, want := got["entity_count"], 7; got != want {
		t.Fatalf("entity_count = %#v, want %#v", got, want)
	}
	if got, want := got["content_last_indexed_at"], entityIndexedAt.Format(time.RFC3339Nano); got != want {
		t.Fatalf("content_last_indexed_at = %#v, want %#v", got, want)
	}
}
