// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/entity"
	repositorycontract "github.com/eshu-hq/eshu/go/internal/query/querycontract/repository"
	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
)

// entityAdmissionCannedStore is the pin-satisfying double behind the
// wire-shape proof: it implements exactly the twenty-seven pinned
// reads over fixed canned rows and is compile-asserted against the
// pin, so the proof below runs through the admission shape, not the
// wider staying ports.
type entityAdmissionCannedStore struct {
	entity querycontract.EntityContent
	file   querycontract.FileContent
	repo   querycontract.RepositoryCatalogEntry
}

func entityAdmissionCannedEntity() querycontract.EntityContent {
	return querycontract.EntityContent{
		EntityID:     "entity-wire-1",
		RepoID:       "repo-wire",
		RelativePath: "src/wire/handler.go",
		EntityType:   "Function",
		EntityName:   "handleWire",
		StartLine:    3,
		EndLine:      9,
		Language:     "go",
		SourceCache:  "func handleWire() {}",
		Metadata:     map[string]any{"wire": "entity"},
	}
}

func entityAdmissionCannedFile() querycontract.FileContent {
	return querycontract.FileContent{
		RepoID:       "repo-wire",
		RelativePath: "src/wire/handler.go",
		Content:      "package wire",
		Language:     "go",
	}
}

func entityAdmissionCannedRepo() querycontract.RepositoryCatalogEntry {
	return querycontract.RepositoryCatalogEntry{
		ID:   "repo-wire",
		Name: "wire",
	}
}

func (s *entityAdmissionCannedStore) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return []map[string]any{{"wire": "graph-row", "n": 1}}, nil
}

func (s *entityAdmissionCannedStore) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return map[string]any{"wire": "graph-single", "n": 1}, nil
}

func (s *entityAdmissionCannedStore) GetEntityContent(context.Context, string) (*querycontract.EntityContent, error) {
	entity := s.entity
	return &entity, nil
}

func (s *entityAdmissionCannedStore) GetFileContent(context.Context, string, string) (*querycontract.FileContent, error) {
	file := s.file
	return &file, nil
}

func (s *entityAdmissionCannedStore) GetFileContents(context.Context, string, []string) ([]querycontract.FileContent, error) {
	return []querycontract.FileContent{s.file}, nil
}

func (s *entityAdmissionCannedStore) NonBlankRepoFilePaths(context.Context, string, []string) ([]string, error) {
	return []string{s.file.RelativePath}, nil
}

func (s *entityAdmissionCannedStore) entityPage() ([]querycontract.EntityContent, error) {
	return []querycontract.EntityContent{s.entity}, nil
}

func (s *entityAdmissionCannedStore) SearchEntitiesByName(context.Context, string, string, string, int) ([]querycontract.EntityContent, error) {
	return s.entityPage()
}

func (s *entityAdmissionCannedStore) SearchEntitiesByNameAnyRepo(context.Context, string, string, int) ([]querycontract.EntityContent, error) {
	return s.entityPage()
}

func (s *entityAdmissionCannedStore) SearchEntitiesReferencingComponent(context.Context, string, string, int) ([]querycontract.EntityContent, error) {
	return s.entityPage()
}

func (s *entityAdmissionCannedStore) SearchFileContentAnyRepo(context.Context, string, int) ([]querycontract.FileContent, error) {
	return []querycontract.FileContent{s.file}, nil
}

func (s *entityAdmissionCannedStore) SearchFileContentAnyRepoExactCase(context.Context, string, int) ([]querycontract.FileContent, error) {
	return []querycontract.FileContent{s.file}, nil
}

func (s *entityAdmissionCannedStore) ResolveRepository(context.Context, string) (*querycontract.RepositoryCatalogEntry, error) {
	repo := s.repo
	return &repo, nil
}

func (s *entityAdmissionCannedStore) ListRepoEntities(context.Context, string, int) ([]querycontract.EntityContent, error) {
	return s.entityPage()
}

func (s *entityAdmissionCannedStore) ListRepoEntitiesByKeys(context.Context, string, []querycontract.EntityContentKey) ([]querycontract.EntityContent, error) {
	return s.entityPage()
}

func (s *entityAdmissionCannedStore) ListRepoEntitiesByIDs(context.Context, string, []string, int) ([]querycontract.EntityContent, error) {
	return s.entityPage()
}

func (s *entityAdmissionCannedStore) ListRepoEntitiesByType(context.Context, string, string, int) ([]querycontract.EntityContent, error) {
	return s.entityPage()
}

func (s *entityAdmissionCannedStore) MatchRepositories(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error) {
	return []querycontract.RepositoryCatalogEntry{s.repo}, nil
}

func (s *entityAdmissionCannedStore) ListRepositories(context.Context) ([]querycontract.RepositoryCatalogEntry, error) {
	return []querycontract.RepositoryCatalogEntry{s.repo}, nil
}

func (s *entityAdmissionCannedStore) ListRepoFiles(context.Context, string, int) ([]querycontract.FileContent, error) {
	return []querycontract.FileContent{s.file}, nil
}

func (s *entityAdmissionCannedStore) ListFrameworkRoutes(context.Context, string) ([]querycontract.FrameworkRouteEvidence, error) {
	return []querycontract.FrameworkRouteEvidence{{
		Framework:    "wire",
		RelativePath: s.file.RelativePath,
		RoutePaths:   []string{"/wire"},
		RouteMethods: []string{"GET"},
	}}, nil
}

func (s *entityAdmissionCannedStore) SearchEntityNames(_ context.Context, search entity.EntityNameSearch) ([]querycontract.EntityContent, error) {
	if search.Name != "handleWire" {
		return nil, nil
	}
	return s.entityPage()
}

func (s *entityAdmissionCannedStore) ListCICDRunCorrelations(context.Context, querycontract.CICDRunCorrelationFilter) ([]querycontract.CICDRunCorrelationRow, error) {
	return []querycontract.CICDRunCorrelationRow{{CorrelationID: "cicd-wire-1", RepositoryID: "repo-wire"}}, nil
}

func (s *entityAdmissionCannedStore) ListContainerImageIdentities(context.Context, supplychain.ContainerImageIdentityFilter) ([]supplychain.ContainerImageIdentityRow, error) {
	return []supplychain.ContainerImageIdentityRow{{IdentityID: "image-wire-1", RepositoryID: "repo-wire"}}, nil
}

func (s *entityAdmissionCannedStore) ListSBOMAttestationAttachments(context.Context, supplychain.SBOMAttestationAttachmentFilter) (supplychain.SBOMAttestationAttachmentPage, error) {
	return supplychain.SBOMAttestationAttachmentPage{
		Attachments: []supplychain.SBOMAttestationAttachmentRow{{AttachmentID: "sbom-wire-1"}},
	}, nil
}

func (s *entityAdmissionCannedStore) ServiceStoryTargetSupportEvidence(context.Context, repositorycontract.ServiceStoryTargetSupportFilter) (repositorycontract.ServiceStoryTargetSupportReadModel, error) {
	return repositorycontract.ServiceStoryTargetSupportReadModel{
		Support: map[string]any{"wire": "support"},
	}, nil
}

func (s *entityAdmissionCannedStore) DocumentationFindings(context.Context, querycontract.DocumentationFindingFilter) (querycontract.DocumentationFindingListReadModel, error) {
	return querycontract.DocumentationFindingListReadModel{EmptyReason: ""}, nil
}

func (s *entityAdmissionCannedStore) RepositoryDeploymentEvidence(context.Context, string) (repositorycontract.RepositoryDeploymentEvidenceReadModel, error) {
	return repositorycontract.RepositoryDeploymentEvidenceReadModel{Available: true, Limit: 1}, nil
}

var _ EntityReadAdmission = (*entityAdmissionCannedStore)(nil)

// marshalEntityAdmissionWire projects one value through the same
// encoding the handlers use (no HTML escaping), so the proof compares
// wire bytes the way a satellite consumer would read them, not Go
// structs that could drift from the wire.
func marshalEntityAdmissionWire(t *testing.T, v any) string {
	t.Helper()
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatalf("marshal wire shape: %v", err)
	}
	return strings.TrimSpace(sb.String())
}

// TestEntityReadAdmissionWireShapesAreSatelliteConsumable proves a
// satellite can consume the pin using only the pin plus the
// querycontract shapes: every one of the twenty-seven reads answers
// through an EntityReadAdmission-typed value, the grant-bound name
// search carries the caller's grant alongside the filters, and the
// entity, file, catalog, framework-route, coverage, and read-model
// wire projections carry the keys a satellite record would depend on.
// A fail-closed empty-grant read (nil entity, no error) marshals to
// null, so the satellite path has no hidden non-nullable expectation.
func TestEntityReadAdmissionWireShapesAreSatelliteConsumable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var pin EntityReadAdmission = &entityAdmissionCannedStore{
		entity: entityAdmissionCannedEntity(),
		file:   entityAdmissionCannedFile(),
		repo:   entityAdmissionCannedRepo(),
	}

	rows, err := pin.Run(ctx, "MATCH (n) RETURN n LIMIT 1", map[string]any{"repo_id": "repo-wire"})
	if err != nil || len(rows) != 1 || rows[0]["wire"] != "graph-row" {
		t.Fatalf("Run() = %#v, %v, want the one canned graph row", rows, err)
	}
	single, err := pin.RunSingle(ctx, "MATCH (n) RETURN n LIMIT 1", map[string]any{"repo_id": "repo-wire"})
	if err != nil || single["wire"] != "graph-single" {
		t.Fatalf("RunSingle() = %#v, %v, want the canned graph single", single, err)
	}

	gotEntity, err := pin.GetEntityContent(ctx, "entity-wire-1")
	if err != nil || gotEntity == nil || gotEntity.EntityID != "entity-wire-1" {
		t.Fatalf("GetEntityContent() = %#v, %v, want the canned entity", gotEntity, err)
	}
	if wire := marshalEntityAdmissionWire(t, gotEntity); !strings.Contains(wire, `"entity_id":"entity-wire-1"`) || !strings.Contains(wire, `"repo_id":"repo-wire"`) || !strings.Contains(wire, `"relative_path":"src/wire/handler.go"`) {
		t.Errorf("entity wire bytes = %s, want entity_id, repo_id, and relative_path keys", wire)
	}

	file, err := pin.GetFileContent(ctx, "repo-wire", "src/wire/handler.go")
	if err != nil || file == nil || file.Content != "package wire" {
		t.Fatalf("GetFileContent() = %#v, %v, want the canned file", file, err)
	}
	if files, err := pin.GetFileContents(ctx, "repo-wire", []string{"src/wire/handler.go"}); err != nil || len(files) != 1 {
		t.Fatalf("GetFileContents() rows = %d, %v, want one", len(files), err)
	}
	if paths, err := pin.NonBlankRepoFilePaths(ctx, "repo-wire", []string{"src/wire/handler.go"}); err != nil || len(paths) != 1 {
		t.Fatalf("NonBlankRepoFilePaths() = %v, %v, want the one path", paths, err)
	}
	if wire := marshalEntityAdmissionWire(t, file); !strings.Contains(wire, `"relative_path":"src/wire/handler.go"`) {
		t.Errorf("file wire bytes = %s, want the relative_path key", wire)
	}

	if page, err := pin.SearchEntitiesByName(ctx, "repo-wire", "Function", "handleWire", 10); err != nil || len(page) != 1 {
		t.Fatalf("SearchEntitiesByName() rows = %d, %v, want one", len(page), err)
	}
	if page, err := pin.SearchEntitiesByNameAnyRepo(ctx, "Function", "handleWire", 10); err != nil || len(page) != 1 {
		t.Fatalf("SearchEntitiesByNameAnyRepo() rows = %d, %v, want one", len(page), err)
	}
	if page, err := pin.SearchEntitiesReferencingComponent(ctx, "repo-wire", "Wire", 10); err != nil || len(page) != 1 {
		t.Fatalf("SearchEntitiesReferencingComponent() rows = %d, %v, want one", len(page), err)
	}
	if page, err := pin.SearchFileContentAnyRepo(ctx, "handleWire", 10); err != nil || len(page) != 1 {
		t.Fatalf("SearchFileContentAnyRepo() rows = %d, %v, want one", len(page), err)
	}
	if page, err := pin.SearchFileContentAnyRepoExactCase(ctx, "handleWire", 10); err != nil || len(page) != 1 {
		t.Fatalf("SearchFileContentAnyRepoExactCase() rows = %d, %v, want one", len(page), err)
	}
	if page, err := pin.ListRepoEntities(ctx, "repo-wire", 10); err != nil || len(page) != 1 {
		t.Fatalf("ListRepoEntities() rows = %d, %v, want one", len(page), err)
	}
	if page, err := pin.ListRepoEntitiesByKeys(ctx, "repo-wire", nil); err != nil || len(page) != 1 {
		t.Fatalf("ListRepoEntitiesByKeys() rows = %d, %v, want one", len(page), err)
	}
	if page, err := pin.ListRepoEntitiesByIDs(ctx, "repo-wire", []string{"entity-wire-1"}, 10); err != nil || len(page) != 1 {
		t.Fatalf("ListRepoEntitiesByIDs() rows = %d, %v, want one", len(page), err)
	}
	if page, err := pin.ListRepoEntitiesByType(ctx, "repo-wire", "Function", 10); err != nil || len(page) != 1 {
		t.Fatalf("ListRepoEntitiesByType() rows = %d, %v, want one", len(page), err)
	}

	resolved, err := pin.ResolveRepository(ctx, "wire")
	if err != nil || resolved == nil || resolved.ID != "repo-wire" {
		t.Fatalf("ResolveRepository() = %#v, %v, want the canned catalog entry", resolved, err)
	}
	repos, err := pin.ListRepositories(ctx)
	if err != nil || len(repos) != 1 || repos[0].ID != "repo-wire" {
		t.Fatalf("ListRepositories() = %#v, %v, want the canned catalog entry", repos, err)
	}
	if wire := marshalEntityAdmissionWire(t, repos[0]); !strings.Contains(wire, `"ID":"repo-wire"`) {
		t.Errorf("catalog wire bytes = %s, want the ID key", wire)
	}
	matched, err := pin.MatchRepositories(ctx, "wire")
	if err != nil || len(matched) != 1 || matched[0].Name != "wire" {
		t.Fatalf("MatchRepositories() = %#v, %v, want the canned catalog entry", matched, err)
	}

	if files, err := pin.ListRepoFiles(ctx, "repo-wire", 10); err != nil || len(files) != 1 {
		t.Fatalf("ListRepoFiles() rows = %d, %v, want one", len(files), err)
	}
	frameworkRoutes, err := pin.ListFrameworkRoutes(ctx, "repo-wire")
	if err != nil || len(frameworkRoutes) != 1 || frameworkRoutes[0].Framework != "wire" {
		t.Fatalf("ListFrameworkRoutes() = %#v, %v, want the canned route evidence", frameworkRoutes, err)
	}
	if wire := marshalEntityAdmissionWire(t, frameworkRoutes[0]); !strings.Contains(wire, `"framework":"wire"`) || !strings.Contains(wire, `"route_paths":["/wire"]`) {
		t.Errorf("framework route wire bytes = %s, want framework and route_paths keys", wire)
	}

	grantSearch := entity.EntityNameSearch{
		Name:          "handleWire",
		Scope:         entity.EntityNameScopeRepositories,
		RepositoryIDs: []string{"repo-wire"},
		Limit:         10,
	}
	grantPage, err := pin.SearchEntityNames(ctx, grantSearch)
	if err != nil || len(grantPage) != 1 || grantPage[0].EntityID != "entity-wire-1" {
		t.Fatalf("SearchEntityNames() = %#v, %v, want the canned entity for the granted search", grantPage, err)
	}
	if wire := marshalEntityAdmissionWire(t, grantSearch); !strings.Contains(wire, `"RepositoryIDs":["repo-wire"]`) {
		t.Errorf("grant search wire bytes = %s, want the caller grant carried alongside the filters", wire)
	}
	emptySearch := grantSearch
	emptySearch.Name = "nothing-matches"
	if empty, err := pin.SearchEntityNames(ctx, emptySearch); err != nil || len(empty) != 0 {
		t.Fatalf("SearchEntityNames() unmatching = %#v, %v, want zero rows and no error", empty, err)
	}

	cicdRows, err := pin.ListCICDRunCorrelations(ctx, querycontract.CICDRunCorrelationFilter{RepositoryID: "repo-wire"})
	if err != nil || len(cicdRows) != 1 || cicdRows[0].CorrelationID != "cicd-wire-1" {
		t.Fatalf("ListCICDRunCorrelations() = %#v, %v, want the canned row", cicdRows, err)
	}
	imageRows, err := pin.ListContainerImageIdentities(ctx, supplychain.ContainerImageIdentityFilter{RepositoryID: "repo-wire"})
	if err != nil || len(imageRows) != 1 || imageRows[0].IdentityID != "image-wire-1" {
		t.Fatalf("ListContainerImageIdentities() = %#v, %v, want the canned row", imageRows, err)
	}
	sbomPage, err := pin.ListSBOMAttestationAttachments(ctx, supplychain.SBOMAttestationAttachmentFilter{})
	if err != nil || len(sbomPage.Attachments) != 1 || sbomPage.Attachments[0].AttachmentID != "sbom-wire-1" {
		t.Fatalf("ListSBOMAttestationAttachments() = %#v, %v, want the canned page", sbomPage, err)
	}

	support, err := pin.ServiceStoryTargetSupportEvidence(ctx, repositorycontract.ServiceStoryTargetSupportFilter{Repository: "repo-wire"})
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

	if wire := marshalEntityAdmissionWire(t, (*querycontract.EntityContent)(nil)); wire != "null" {
		t.Errorf("nil entity wire bytes = %s, want null (fail-closed emptiness has no hidden shape)", wire)
	}
	if wire := marshalEntityAdmissionWire(t, rows); !strings.Contains(wire, `"wire":"graph-row"`) {
		t.Errorf("graph wire bytes = %s, want the canned graph row", wire)
	}
}
