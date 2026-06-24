// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
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

func (f fakePortContentStore) CountRepositoriesByLanguage(
	_ context.Context,
	languages []string,
) (RepositoryLanguageAggregate, error) {
	if f.languageCounts == nil {
		return RepositoryLanguageAggregate{}, nil
	}
	return f.languageCounts[strings.Join(languages, ",")], nil
}

func (f fakePortContentStore) ListRepositoriesByLanguage(
	_ context.Context,
	_ []string,
	limit int,
	offset int,
) ([]RepositoryLanguageRepository, error) {
	if offset >= len(f.languageRepos) {
		return nil, nil
	}
	rows := f.languageRepos[offset:]
	if limit > 0 && limit < len(rows) {
		rows = rows[:limit]
	}
	return append([]RepositoryLanguageRepository(nil), rows...), nil
}

func (f fakePortContentStore) RepositoryLanguageInventory(
	_ context.Context,
	limit int,
	offset int,
) ([]RepositoryLanguageInventoryRow, error) {
	if offset >= len(f.languageInventory) {
		return nil, nil
	}
	rows := f.languageInventory[offset:]
	if limit > 0 && limit < len(rows) {
		rows = rows[:limit]
	}
	return append([]RepositoryLanguageInventoryRow(nil), rows...), nil
}

func (f fakePortContentStore) repositoryReadModelSummary(context.Context, string) (repositoryReadModelSummary, error) {
	return f.summary, nil
}

func (f fakePortContentStore) repositoryRelationshipReadModel(context.Context, string) (repositoryRelationshipReadModel, error) {
	return f.relationshipReadModel, nil
}

func (f fakePortContentStore) repositoryEntryPoints(context.Context, string) (repositoryEntryPointReadModel, error) {
	return f.entryPoints, nil
}

func (f fakePortContentStore) repositoryDeploymentEvidence(context.Context, string) (repositoryDeploymentEvidenceReadModel, error) {
	if f.deploymentEvidenceErr != nil {
		return repositoryDeploymentEvidenceReadModel{}, f.deploymentEvidenceErr
	}
	return f.deploymentEvidence, nil
}

func (f fakePortContentStore) relationshipEvidenceByResolvedID(context.Context, string) (relationshipEvidenceReadModel, error) {
	return f.relationshipEvidence, nil
}

func (f fakePortContentStore) documentationFindings(_ context.Context, filter documentationFindingFilter) (documentationFindingListReadModel, error) {
	if f.documentationFindingsFilter != nil {
		*f.documentationFindingsFilter = filter
	}
	if f.documentationFindingsErr != nil {
		return documentationFindingListReadModel{}, f.documentationFindingsErr
	}
	return f.documentationFindingsModel, nil
}

func (f fakePortContentStore) documentationFacts(_ context.Context, filter documentationFactFilter) (documentationFactListReadModel, error) {
	if f.documentationFactsFilter != nil {
		*f.documentationFactsFilter = filter
	}
	if f.documentationFactsErr != nil {
		return documentationFactListReadModel{}, f.documentationFactsErr
	}
	return f.documentationFactsModel, nil
}

func (f fakePortContentStore) documentationEvidencePacket(context.Context, string) (documentationEvidencePacketReadModel, error) {
	if f.documentationPacketErr != nil {
		return documentationEvidencePacketReadModel{}, f.documentationPacketErr
	}
	return f.documentationPacketModel, nil
}

func (f fakePortContentStore) documentationEvidencePacketWithFilter(
	_ context.Context,
	filter documentationEvidencePacketFilter,
) (documentationEvidencePacketReadModel, error) {
	if f.documentationPacketFilter != nil {
		*f.documentationPacketFilter = filter
	}
	if f.documentationPacketErr != nil {
		return documentationEvidencePacketReadModel{}, f.documentationPacketErr
	}
	return f.documentationPacketModel, nil
}

func (f fakePortContentStore) documentationEvidencePacketFreshness(
	context.Context,
	string,
	string,
) (documentationEvidencePacketFreshnessReadModel, error) {
	if f.documentationFreshnessErr != nil {
		return documentationEvidencePacketFreshnessReadModel{}, f.documentationFreshnessErr
	}
	return f.documentationFreshnessModel, nil
}

func (f fakePortContentStore) documentationEvidencePacketFreshnessWithFilter(
	_ context.Context,
	filter documentationEvidencePacketFreshnessFilter,
) (documentationEvidencePacketFreshnessReadModel, error) {
	if f.documentationFreshnessFilter != nil {
		*f.documentationFreshnessFilter = filter
	}
	if f.documentationFreshnessErr != nil {
		return documentationEvidencePacketFreshnessReadModel{}, f.documentationFreshnessErr
	}
	return f.documentationFreshnessModel, nil
}

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
