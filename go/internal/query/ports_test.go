// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

type fakePortGraphQuery struct{}

func (fakePortGraphQuery) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (fakePortGraphQuery) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// fakePortContentStore is this package's adapter onto
// querytestutil.FakePortContentStore. The behavior lives there so handler families
// moving out of package query (#6060, epic #6053) can reach it -- a _test.go
// symbol is not importable across a package boundary. The field names stay
// lowercase and unchanged so the ~126 test files that build this double with
// keyed literals did not have to be rewritten.
//
// Every method below forwards. None of them reimplement anything: a second
// copy of the filter-then-limit logic would be free to drift from the one the
// promoted double shares with the handler families, and both copies would keep
// passing while they diverged.
type fakePortContentStore struct {
	coverage                     RepositoryContentCoverage
	summary                      RepositoryReadModelSummary
	relationshipReadModel        RepositoryRelationshipReadModel
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

// promoted is the single place the adapter's fixtures cross into the shared
// double. Every method goes through it, so a field added here reaches all of
// them at once. The types line up because each read model in this package is
// an alias onto the querycontract declaration the promoted double names.
func (f fakePortContentStore) promoted() querytestutil.FakePortContentStore {
	return querytestutil.FakePortContentStore{
		Coverage:                     f.coverage,
		Summary:                      f.summary,
		RelationshipReadModel:        f.relationshipReadModel,
		EntryPoints:                  f.entryPoints,
		DeploymentEvidence:           f.deploymentEvidence,
		DeploymentEvidenceErr:        f.deploymentEvidenceErr,
		RelationshipEvidence:         f.relationshipEvidence,
		DocumentationFindingsModel:   f.documentationFindingsModel,
		DocumentationFindingsErr:     f.documentationFindingsErr,
		DocumentationFindingsFilter:  f.documentationFindingsFilter,
		DocumentationFactsModel:      f.documentationFactsModel,
		DocumentationFactsErr:        f.documentationFactsErr,
		DocumentationFactsFilter:     f.documentationFactsFilter,
		DocumentationPacketModel:     f.documentationPacketModel,
		DocumentationPacketErr:       f.documentationPacketErr,
		DocumentationPacketFilter:    f.documentationPacketFilter,
		DocumentationFreshnessModel:  f.documentationFreshnessModel,
		DocumentationFreshnessErr:    f.documentationFreshnessErr,
		DocumentationFreshnessFilter: f.documentationFreshnessFilter,
		TargetSupportModel:           f.targetSupportModel,
		TargetSupportErr:             f.targetSupportErr,
		Entities:                     f.entities,
		RepoFiles:                    f.repoFiles,
		RepositoryRefs:               f.repositoryRefs,
		Repositories:                 f.repositories,
		LanguageRepos:                f.languageRepos,
		LanguageCounts:               f.languageCounts,
		LanguageInventory:            f.languageInventory,
		WorkloadIdentities:           f.workloadIdentities,
	}
}

func (f fakePortContentStore) GetFileContent(ctx context.Context, repoID, relativePath string) (*FileContent, error) {
	return f.promoted().GetFileContent(ctx, repoID, relativePath)
}

func (f fakePortContentStore) GetFileLines(
	ctx context.Context,
	repoID, relativePath string,
	startLine, endLine int,
) (*FileContent, error) {
	return f.promoted().GetFileLines(ctx, repoID, relativePath, startLine, endLine)
}

func (f fakePortContentStore) GetEntityContent(ctx context.Context, entityID string) (*EntityContent, error) {
	return f.promoted().GetEntityContent(ctx, entityID)
}

func (f fakePortContentStore) SearchFileContent(
	ctx context.Context,
	repoID, pattern string,
	limit int,
) ([]FileContent, error) {
	return f.promoted().SearchFileContent(ctx, repoID, pattern, limit)
}

func (f fakePortContentStore) SearchFileContentAnyRepo(
	ctx context.Context,
	pattern string,
	limit int,
) ([]FileContent, error) {
	return f.promoted().SearchFileContentAnyRepo(ctx, pattern, limit)
}

func (f fakePortContentStore) SearchFileContentAnyRepoExactCase(
	ctx context.Context,
	pattern string,
	limit int,
) ([]FileContent, error) {
	return f.promoted().SearchFileContentAnyRepoExactCase(ctx, pattern, limit)
}

func (f fakePortContentStore) SearchEntityContent(
	ctx context.Context,
	repoID, pattern string,
	limit int,
) ([]EntityContent, error) {
	return f.promoted().SearchEntityContent(ctx, repoID, pattern, limit)
}

func (f fakePortContentStore) SearchEntityContentAnyRepo(
	ctx context.Context,
	pattern string,
	limit int,
) ([]EntityContent, error) {
	return f.promoted().SearchEntityContentAnyRepo(ctx, pattern, limit)
}

func (f fakePortContentStore) SearchEntitiesByName(
	ctx context.Context,
	repoID, entityType, name string,
	limit int,
) ([]EntityContent, error) {
	return f.promoted().SearchEntitiesByName(ctx, repoID, entityType, name, limit)
}

func (f fakePortContentStore) SearchEntitiesByNameAnyRepo(
	ctx context.Context,
	entityType, name string,
	limit int,
) ([]EntityContent, error) {
	return f.promoted().SearchEntitiesByNameAnyRepo(ctx, entityType, name, limit)
}

func (f fakePortContentStore) SearchEntitiesReferencingComponent(
	ctx context.Context,
	repoID, componentName string,
	limit int,
) ([]EntityContent, error) {
	return f.promoted().SearchEntitiesReferencingComponent(ctx, repoID, componentName, limit)
}

func (f fakePortContentStore) ListRepoFiles(ctx context.Context, repoID string, limit int) ([]FileContent, error) {
	return f.promoted().ListRepoFiles(ctx, repoID, limit)
}

func (f fakePortContentStore) ListRepositoryRefs(ctx context.Context, repoID string) ([]RepositoryRef, error) {
	return f.promoted().ListRepositoryRefs(ctx, repoID)
}

func (f fakePortContentStore) ListRepoEntities(
	ctx context.Context,
	repoID string,
	limit int,
) ([]EntityContent, error) {
	return f.promoted().ListRepoEntities(ctx, repoID, limit)
}

func (f fakePortContentStore) ListRepoEntitiesByType(
	ctx context.Context,
	repoID, entityType string,
	limit int,
) ([]EntityContent, error) {
	return f.promoted().ListRepoEntitiesByType(ctx, repoID, entityType, limit)
}

func (f fakePortContentStore) ListRepoEntitiesByTypes(
	ctx context.Context,
	repoID string,
	entityTypes []string,
	limit int,
) ([]EntityContent, error) {
	return f.promoted().ListRepoEntitiesByTypes(ctx, repoID, entityTypes, limit)
}

func (f fakePortContentStore) ListRepoEntitiesByPaths(
	ctx context.Context,
	repoID string,
	relativePaths []string,
	limit int,
) ([]EntityContent, error) {
	return f.promoted().ListRepoEntitiesByPaths(ctx, repoID, relativePaths, limit)
}

func (f fakePortContentStore) SearchEntitiesByLanguageAndType(
	ctx context.Context,
	repoID, language, entityType, query string,
	limit int,
) ([]EntityContent, error) {
	return f.promoted().SearchEntitiesByLanguageAndType(ctx, repoID, language, entityType, query, limit)
}

func (f fakePortContentStore) ListFrameworkRoutes(
	ctx context.Context,
	repoID string,
) ([]FrameworkRouteEvidence, error) {
	return f.promoted().ListFrameworkRoutes(ctx, repoID)
}

func (f fakePortContentStore) RepositoryCoverage(
	ctx context.Context,
	repoID string,
) (RepositoryContentCoverage, error) {
	return f.promoted().RepositoryCoverage(ctx, repoID)
}

// fakePortContentStore's language-inventory, catalog, and documentation
// read-model forwarders live in ports_test_language_documentation_test.go, and
// its #5363 entity-by-ID and K8s-candidate forwarders in
// ports_k8s_select_candidates_test.go, split out to keep this file under the
// repository's 500-line cap.

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
