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
	coverage                    RepositoryContentCoverage
	summary                     repositoryReadModelSummary
	relationshipReadModel       repositoryRelationshipReadModel
	entryPoints                 repositoryEntryPointReadModel
	deploymentEvidence          repositoryDeploymentEvidenceReadModel
	deploymentEvidenceErr       error
	relationshipEvidence        relationshipEvidenceReadModel
	documentationFindingsModel  documentationFindingListReadModel
	documentationFindingsErr    error
	documentationPacketModel    documentationEvidencePacketReadModel
	documentationPacketErr      error
	documentationFreshnessModel documentationEvidencePacketFreshnessReadModel
	documentationFreshnessErr   error
	entities                    []EntityContent
	repositories                []RepositoryCatalogEntry
}

func (f fakePortContentStore) GetFileContent(context.Context, string, string) (*FileContent, error) {
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

func (f fakePortContentStore) ListRepoFiles(context.Context, string, int) ([]FileContent, error) {
	return nil, nil
}

func (f fakePortContentStore) ListRepoEntities(context.Context, string, int) ([]EntityContent, error) {
	return append([]EntityContent(nil), f.entities...), nil
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

func (f fakePortContentStore) documentationFindings(context.Context, documentationFindingFilter) (documentationFindingListReadModel, error) {
	if f.documentationFindingsErr != nil {
		return documentationFindingListReadModel{}, f.documentationFindingsErr
	}
	return f.documentationFindingsModel, nil
}

func (f fakePortContentStore) documentationEvidencePacket(context.Context, string) (documentationEvidencePacketReadModel, error) {
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

func (f fakePortContentStore) ListRepositories(context.Context) ([]RepositoryCatalogEntry, error) {
	return append([]RepositoryCatalogEntry(nil), f.repositories...), nil
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

var _ GraphQuery = (*fakePortGraphQuery)(nil)
var _ ContentStore = (*fakePortContentStore)(nil)

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
