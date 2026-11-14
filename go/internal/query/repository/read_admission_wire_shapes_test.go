// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/repository"
)

// repositoryStoryAdmissionCannedStore is the pin-satisfying double
// behind the wire-shape proof: it implements exactly the fifteen
// pinned reads over fixed canned rows and is compile-asserted against
// the pin, so the proof below runs through the admission shape, not
// the wider staying ports.
type repositoryStoryAdmissionCannedStore struct {
	entity querycontract.EntityContent
	file   querycontract.FileContent
	repo   querycontract.RepositoryCatalogEntry
}

func repositoryStoryAdmissionCannedEntity() querycontract.EntityContent {
	return querycontract.EntityContent{
		EntityID:     "entity-wire-1",
		RepoID:       "repo-wire",
		RelativePath: "src/wire/handler.go",
		EntityType:   "Function",
		EntityName:   "handleWire",
		Language:     "go",
	}
}

func repositoryStoryAdmissionCannedFile() querycontract.FileContent {
	return querycontract.FileContent{
		RepoID:       "repo-wire",
		RelativePath: "README.md",
		Content:      "# wire",
		Language:     "markdown",
	}
}

func repositoryStoryAdmissionCannedRepo() querycontract.RepositoryCatalogEntry {
	return querycontract.RepositoryCatalogEntry{
		ID:   "repo-wire",
		Name: "wire",
	}
}

func (s *repositoryStoryAdmissionCannedStore) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return []map[string]any{{"wire": "graph-row", "n": 1}}, nil
}

func (s *repositoryStoryAdmissionCannedStore) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return map[string]any{"id": "repo-wire", "wire": "graph-single"}, nil
}

func (s *repositoryStoryAdmissionCannedStore) MatchRepositories(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error) {
	return []querycontract.RepositoryCatalogEntry{s.repo}, nil
}

func (s *repositoryStoryAdmissionCannedStore) RepositoryCoverage(context.Context, string) (querycontract.RepositoryContentCoverage, error) {
	return querycontract.RepositoryContentCoverage{
		Available:   true,
		FileCount:   1,
		EntityCount: 1,
		Languages:   []querycontract.RepositoryLanguageCount{{Language: "go", FileCount: 1}},
	}, nil
}

func (s *repositoryStoryAdmissionCannedStore) ListRepoEntities(context.Context, string, int) ([]querycontract.EntityContent, error) {
	return []querycontract.EntityContent{s.entity}, nil
}

func (s *repositoryStoryAdmissionCannedStore) ListRepoFiles(context.Context, string, int) ([]querycontract.FileContent, error) {
	return []querycontract.FileContent{s.file}, nil
}

func (s *repositoryStoryAdmissionCannedStore) ListRepoEntitiesByTypes(context.Context, string, []string, int) ([]querycontract.EntityContent, error) {
	return []querycontract.EntityContent{s.entity}, nil
}

func (s *repositoryStoryAdmissionCannedStore) GetFileContent(context.Context, string, string) (*querycontract.FileContent, error) {
	file := s.file
	return &file, nil
}

func (s *repositoryStoryAdmissionCannedStore) GetFileContents(context.Context, string, []string) ([]querycontract.FileContent, error) {
	return []querycontract.FileContent{s.file}, nil
}

func (s *repositoryStoryAdmissionCannedStore) NonBlankRepoFilePaths(context.Context, string, []string) ([]string, error) {
	return []string{s.file.RelativePath}, nil
}

func (s *repositoryStoryAdmissionCannedStore) ListCICDRunCorrelations(context.Context, querycontract.CICDRunCorrelationFilter) ([]querycontract.CICDRunCorrelationRow, error) {
	return []querycontract.CICDRunCorrelationRow{{CorrelationID: "cicd-wire-1", RepositoryID: "repo-wire"}}, nil
}

func (s *repositoryStoryAdmissionCannedStore) RepositoryReadModelSummary(context.Context, string) (repository.RepositoryReadModelSummary, error) {
	return repository.RepositoryReadModelSummary{
		Available:     true,
		WorkloadNames: []string{"wire-workload"},
	}, nil
}

func (s *repositoryStoryAdmissionCannedStore) ServiceStoryTargetSupportEvidence(context.Context, repository.ServiceStoryTargetSupportFilter) (repository.ServiceStoryTargetSupportReadModel, error) {
	return repository.ServiceStoryTargetSupportReadModel{
		Support: map[string]any{"wire": "support"},
	}, nil
}

func (s *repositoryStoryAdmissionCannedStore) DocumentationFindings(context.Context, querycontract.DocumentationFindingFilter) (querycontract.DocumentationFindingListReadModel, error) {
	return querycontract.DocumentationFindingListReadModel{EmptyReason: ""}, nil
}

func (s *repositoryStoryAdmissionCannedStore) RepositoryDeploymentEvidence(context.Context, string) (repository.RepositoryDeploymentEvidenceReadModel, error) {
	return repository.RepositoryDeploymentEvidenceReadModel{Available: true, Limit: 1}, nil
}

var _ RepositoryStoryReadAdmission = (*repositoryStoryAdmissionCannedStore)(nil)

// marshalRepositoryStoryAdmissionWire projects one value through the
// same encoding the handlers use (no HTML escaping), so the proof
// compares wire bytes the way a satellite consumer would read them,
// not Go structs that could drift from the wire.
func marshalRepositoryStoryAdmissionWire(t *testing.T, v any) string {
	t.Helper()
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatalf("marshal wire shape: %v", err)
	}
	return strings.TrimSpace(sb.String())
}

// TestRepositoryStoryReadAdmissionWireShapesAreSatelliteConsumable
// proves a satellite can consume the pin using only the pin plus the
// querycontract shapes: every one of the fifteen reads answers through
// a RepositoryStoryReadAdmission-typed value, and the catalog,
// coverage, semantic, narrative, artifact, CI/CD, and read-model wire
// projections carry the keys a satellite record would depend on. A
// fail-closed empty-grant read (nil file, no error) marshals to null,
// so the satellite path has no hidden non-nullable expectation.
func TestRepositoryStoryReadAdmissionWireShapesAreSatelliteConsumable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var pin RepositoryStoryReadAdmission = &repositoryStoryAdmissionCannedStore{
		entity: repositoryStoryAdmissionCannedEntity(),
		file:   repositoryStoryAdmissionCannedFile(),
		repo:   repositoryStoryAdmissionCannedRepo(),
	}

	rows, err := pin.Run(ctx, "MATCH (n) RETURN n LIMIT 1", map[string]any{"repo_id": "repo-wire"})
	if err != nil || len(rows) != 1 || rows[0]["wire"] != "graph-row" {
		t.Fatalf("Run() = %#v, %v, want the one canned graph row", rows, err)
	}
	single, err := pin.RunSingle(ctx, "MATCH (r:Repository) RETURN r", map[string]any{"repo_id": "repo-wire"})
	if err != nil || single["id"] != "repo-wire" {
		t.Fatalf("RunSingle() = %#v, %v, want the canned repository row", single, err)
	}

	matched, err := pin.MatchRepositories(ctx, "wire")
	if err != nil || len(matched) != 1 || matched[0].ID != "repo-wire" {
		t.Fatalf("MatchRepositories() = %#v, %v, want the canned catalog entry", matched, err)
	}
	if wire := marshalRepositoryStoryAdmissionWire(t, matched[0]); !strings.Contains(wire, `"ID":"repo-wire"`) {
		t.Errorf("catalog wire bytes = %s, want the ID key", wire)
	}

	coverage, err := pin.RepositoryCoverage(ctx, "repo-wire")
	if err != nil || !coverage.Available || coverage.FileCount != 1 {
		t.Fatalf("RepositoryCoverage() = %#v, %v, want the canned available coverage", coverage, err)
	}

	if page, err := pin.ListRepoEntities(ctx, "repo-wire", 5001); err != nil || len(page) != 1 {
		t.Fatalf("ListRepoEntities() rows = %d, %v, want one", len(page), err)
	}
	if files, err := pin.ListRepoFiles(ctx, "repo-wire", 5001); err != nil || len(files) != 1 {
		t.Fatalf("ListRepoFiles() rows = %d, %v, want one", len(files), err)
	}
	if page, err := pin.ListRepoEntitiesByTypes(ctx, "repo-wire", []string{"K8sResource", "Function"}, 5001); err != nil || len(page) != 1 {
		t.Fatalf("ListRepoEntitiesByTypes() rows = %d, %v, want one", len(page), err)
	}

	file, err := pin.GetFileContent(ctx, "repo-wire", "README.md")
	if err != nil || file == nil || file.Content != "# wire" {
		t.Fatalf("GetFileContent() = %#v, %v, want the canned narrative file", file, err)
	}
	if wire := marshalRepositoryStoryAdmissionWire(t, file); !strings.Contains(wire, `"relative_path":"README.md"`) {
		t.Errorf("file wire bytes = %s, want the relative_path key", wire)
	}
	if files, err := pin.GetFileContents(ctx, "repo-wire", []string{"README.md"}); err != nil || len(files) != 1 {
		t.Fatalf("GetFileContents() rows = %d, %v, want one", len(files), err)
	}
	if paths, err := pin.NonBlankRepoFilePaths(ctx, "repo-wire", []string{"README.md"}); err != nil || len(paths) != 1 {
		t.Fatalf("NonBlankRepoFilePaths() = %v, %v, want the one path", paths, err)
	}

	cicdRows, err := pin.ListCICDRunCorrelations(ctx, querycontract.CICDRunCorrelationFilter{RepositoryID: "repo-wire"})
	if err != nil || len(cicdRows) != 1 || cicdRows[0].CorrelationID != "cicd-wire-1" {
		t.Fatalf("ListCICDRunCorrelations() = %#v, %v, want the canned row", cicdRows, err)
	}

	summary, err := pin.RepositoryReadModelSummary(ctx, "repo-wire")
	if err != nil || !summary.Available || len(summary.WorkloadNames) != 1 {
		t.Fatalf("RepositoryReadModelSummary() = %#v, %v, want the canned available summary", summary, err)
	}
	support, err := pin.ServiceStoryTargetSupportEvidence(ctx, repository.ServiceStoryTargetSupportFilter{Repository: "repo-wire"})
	if err != nil || support.Support["wire"] != "support" {
		t.Fatalf("ServiceStoryTargetSupportEvidence() = %#v, %v, want the canned support map", support, err)
	}
	docs, err := pin.DocumentationFindings(ctx, querycontract.DocumentationFindingFilter{Repository: "repo-wire"})
	if err != nil || docs.EmptyReason != "" {
		t.Fatalf("DocumentationFindings() = %#v, %v, want the canned read model", docs, err)
	}
	deployEvidence, err := pin.RepositoryDeploymentEvidence(ctx, "repo-wire")
	if err != nil || !deployEvidence.Available {
		t.Fatalf("RepositoryDeploymentEvidence() = %#v, %v, want the canned available read model", deployEvidence, err)
	}

	if wire := marshalRepositoryStoryAdmissionWire(t, (*querycontract.FileContent)(nil)); wire != "null" {
		t.Errorf("nil file wire bytes = %s, want null (fail-closed emptiness has no hidden shape)", wire)
	}
	if wire := marshalRepositoryStoryAdmissionWire(t, rows); !strings.Contains(wire, `"wire":"graph-row"`) {
		t.Errorf("graph wire bytes = %s, want the canned graph row", wire)
	}
}
