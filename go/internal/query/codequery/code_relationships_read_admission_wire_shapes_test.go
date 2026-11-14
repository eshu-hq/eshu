// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/taxonomy"
)

// codeRelationshipAdmissionCannedStore is the pin-satisfying double
// behind the wire-shape proof: it implements exactly the seventeen
// pinned reads over fixed canned rows and is compile-asserted against
// the pin, so the proof below runs through the admission shape, not
// the wider staying ports.
type codeRelationshipAdmissionCannedStore struct {
	entity querycontract.EntityContent
	file   querycontract.FileContent
	repo   querycontract.RepositoryCatalogEntry
}

func codeRelationshipAdmissionCannedEntity() querycontract.EntityContent {
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

func codeRelationshipAdmissionCannedFile() querycontract.FileContent {
	return querycontract.FileContent{
		RepoID:       "repo-wire",
		RelativePath: "src/wire/handler.go",
		Content:      "package wire",
		Language:     "go",
	}
}

func codeRelationshipAdmissionCannedRepo() querycontract.RepositoryCatalogEntry {
	return querycontract.RepositoryCatalogEntry{
		ID:   "repo-wire",
		Name: "wire",
	}
}

func (s *codeRelationshipAdmissionCannedStore) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return []map[string]any{{"wire": "graph-row", "n": 1}}, nil
}

func (s *codeRelationshipAdmissionCannedStore) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return map[string]any{"wire": "graph-single", "n": 1}, nil
}

func (s *codeRelationshipAdmissionCannedStore) GetEntityContent(context.Context, string) (*querycontract.EntityContent, error) {
	entity := s.entity
	return &entity, nil
}

func (s *codeRelationshipAdmissionCannedStore) entityPage() ([]querycontract.EntityContent, error) {
	return []querycontract.EntityContent{s.entity}, nil
}

func (s *codeRelationshipAdmissionCannedStore) SearchEntitiesByName(context.Context, string, string, string, int) ([]querycontract.EntityContent, error) {
	return s.entityPage()
}

func (s *codeRelationshipAdmissionCannedStore) SearchEntitiesByNameAnyRepo(context.Context, string, string, int) ([]querycontract.EntityContent, error) {
	return s.entityPage()
}

func (s *codeRelationshipAdmissionCannedStore) SearchEntitiesByExactName(context.Context, string, string, string, int) ([]querycontract.EntityContent, error) {
	return s.entityPage()
}

func (s *codeRelationshipAdmissionCannedStore) SearchEntitiesByExactNameAnyRepo(context.Context, string, string, int) ([]querycontract.EntityContent, error) {
	return s.entityPage()
}

func (s *codeRelationshipAdmissionCannedStore) SearchEntitiesByLanguageAndType(context.Context, string, string, string, string, int) ([]querycontract.EntityContent, error) {
	return s.entityPage()
}

func (s *codeRelationshipAdmissionCannedStore) SearchEntitiesReferencingComponent(context.Context, string, string, int) ([]querycontract.EntityContent, error) {
	return s.entityPage()
}

func (s *codeRelationshipAdmissionCannedStore) ListRepoFiles(context.Context, string, int) ([]querycontract.FileContent, error) {
	return []querycontract.FileContent{s.file}, nil
}

func (s *codeRelationshipAdmissionCannedStore) ListRepoEntitiesByType(context.Context, string, string, int) ([]querycontract.EntityContent, error) {
	return s.entityPage()
}

func (s *codeRelationshipAdmissionCannedStore) ListRepoEntitiesByIDs(context.Context, string, []string, int) ([]querycontract.EntityContent, error) {
	return s.entityPage()
}

func (s *codeRelationshipAdmissionCannedStore) ListRepositories(context.Context) ([]querycontract.RepositoryCatalogEntry, error) {
	return []querycontract.RepositoryCatalogEntry{s.repo}, nil
}

func (s *codeRelationshipAdmissionCannedStore) MatchRepositories(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error) {
	return []querycontract.RepositoryCatalogEntry{s.repo}, nil
}

func (s *codeRelationshipAdmissionCannedStore) GetEntityContentInRepositories(context.Context, string, []string) (*querycontract.EntityContent, error) {
	entity := s.entity
	return &entity, nil
}

func (s *codeRelationshipAdmissionCannedStore) SearchEntitiesByNameInRepositories(context.Context, []string, string, string, int) ([]querycontract.EntityContent, error) {
	return s.entityPage()
}

func (s *codeRelationshipAdmissionCannedStore) SearchEntitiesByLanguageAndTypeForAccess(_ context.Context, search taxonomy.LanguageEntitySearch) ([]querycontract.EntityContent, error) {
	if search.Query != "handleWire" {
		return nil, nil
	}
	return s.entityPage()
}

var _ CodeRelationshipReadAdmission = (*codeRelationshipAdmissionCannedStore)(nil)

// marshalCodeRelationshipAdmissionWire projects one value through the
// same encoding the handlers use (no HTML escaping), so the proof
// compares wire bytes the way a satellite consumer would read them,
// not Go structs that could drift from the wire.
func marshalCodeRelationshipAdmissionWire(t *testing.T, v any) string {
	t.Helper()
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatalf("marshal wire shape: %v", err)
	}
	return strings.TrimSpace(sb.String())
}

// TestCodeRelationshipReadAdmissionWireShapesAreSatelliteConsumable
// proves a satellite can consume the pin using only the pin plus the
// querycontract and taxonomy shapes: every one of the seventeen reads
// answers through a CodeRelationshipReadAdmission-typed value, the
// grant-bound language search carries taxonomy.LanguageEntitySearch
// (with the caller's grant alongside the filters) end to end, and the
// entity, file, catalog, and graph wire projections carry the keys a
// satellite record would depend on. A fail-closed empty grant read
// (nil entity, no error) marshals to null, so the satellite path has
// no hidden non-nullable expectation.
func TestCodeRelationshipReadAdmissionWireShapesAreSatelliteConsumable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var pin CodeRelationshipReadAdmission = &codeRelationshipAdmissionCannedStore{
		entity: codeRelationshipAdmissionCannedEntity(),
		file:   codeRelationshipAdmissionCannedFile(),
		repo:   codeRelationshipAdmissionCannedRepo(),
	}

	rows, err := pin.Run(ctx, "MATCH (n) RETURN n LIMIT 1", map[string]any{"repo_id": "repo-wire"})
	if err != nil || len(rows) != 1 || rows[0]["wire"] != "graph-row" {
		t.Fatalf("Run() = %#v, %v, want the one canned graph row", rows, err)
	}
	single, err := pin.RunSingle(ctx, "MATCH (n) RETURN n LIMIT 1", map[string]any{"repo_id": "repo-wire"})
	if err != nil || single["wire"] != "graph-single" {
		t.Fatalf("RunSingle() = %#v, %v, want the canned graph single", single, err)
	}

	entity, err := pin.GetEntityContent(ctx, "entity-wire-1")
	if err != nil || entity == nil || entity.EntityID != "entity-wire-1" {
		t.Fatalf("GetEntityContent() = %#v, %v, want the canned entity", entity, err)
	}
	if wire := marshalCodeRelationshipAdmissionWire(t, entity); !strings.Contains(wire, `"entity_id":"entity-wire-1"`) || !strings.Contains(wire, `"repo_id":"repo-wire"`) || !strings.Contains(wire, `"relative_path":"src/wire/handler.go"`) {
		t.Errorf("entity wire bytes = %s, want entity_id, repo_id, and relative_path keys", wire)
	}

	namePage, err := pin.SearchEntitiesByName(ctx, "repo-wire", "Function", "handleWire", 10)
	if err != nil || len(namePage) != 1 || namePage[0].EntityName != "handleWire" {
		t.Fatalf("SearchEntitiesByName() = %#v, %v, want the canned entity page", namePage, err)
	}
	if page, err := pin.SearchEntitiesByNameAnyRepo(ctx, "Function", "handleWire", 10); err != nil || len(page) != 1 {
		t.Fatalf("SearchEntitiesByNameAnyRepo() rows = %d, %v, want one", len(page), err)
	}
	if page, err := pin.SearchEntitiesByExactName(ctx, "repo-wire", "Function", "handleWire", 10); err != nil || len(page) != 1 {
		t.Fatalf("SearchEntitiesByExactName() rows = %d, %v, want one", len(page), err)
	}
	if page, err := pin.SearchEntitiesByExactNameAnyRepo(ctx, "Function", "handleWire", 10); err != nil || len(page) != 1 {
		t.Fatalf("SearchEntitiesByExactNameAnyRepo() rows = %d, %v, want one", len(page), err)
	}
	if page, err := pin.SearchEntitiesByLanguageAndType(ctx, "repo-wire", "go", "Function", "handleWire", 10); err != nil || len(page) != 1 {
		t.Fatalf("SearchEntitiesByLanguageAndType() rows = %d, %v, want one", len(page), err)
	}
	if page, err := pin.SearchEntitiesReferencingComponent(ctx, "repo-wire", "Wire", 10); err != nil || len(page) != 1 {
		t.Fatalf("SearchEntitiesReferencingComponent() rows = %d, %v, want one", len(page), err)
	}
	if files, err := pin.ListRepoFiles(ctx, "repo-wire", 10); err != nil || len(files) != 1 {
		t.Fatalf("ListRepoFiles() rows = %d, %v, want one", len(files), err)
	} else if wire := marshalCodeRelationshipAdmissionWire(t, files[0]); !strings.Contains(wire, `"relative_path":"src/wire/handler.go"`) {
		t.Errorf("file wire bytes = %s, want the relative_path key", wire)
	}
	if page, err := pin.ListRepoEntitiesByType(ctx, "repo-wire", "Function", 10); err != nil || len(page) != 1 {
		t.Fatalf("ListRepoEntitiesByType() rows = %d, %v, want one", len(page), err)
	}
	if page, err := pin.ListRepoEntitiesByIDs(ctx, "repo-wire", []string{"entity-wire-1"}, 10); err != nil || len(page) != 1 {
		t.Fatalf("ListRepoEntitiesByIDs() rows = %d, %v, want one", len(page), err)
	}

	repos, err := pin.ListRepositories(ctx)
	if err != nil || len(repos) != 1 || repos[0].ID != "repo-wire" {
		t.Fatalf("ListRepositories() = %#v, %v, want the canned catalog entry", repos, err)
	}
	if wire := marshalCodeRelationshipAdmissionWire(t, repos[0]); !strings.Contains(wire, `"ID":"repo-wire"`) {
		t.Errorf("catalog wire bytes = %s, want the ID key", wire)
	}
	matched, err := pin.MatchRepositories(ctx, "wire")
	if err != nil || len(matched) != 1 || matched[0].Name != "wire" {
		t.Fatalf("MatchRepositories() = %#v, %v, want the canned catalog entry", matched, err)
	}

	scoped, err := pin.GetEntityContentInRepositories(ctx, "entity-wire-1", []string{"repo-wire"})
	if err != nil || scoped == nil || scoped.RepoID != "repo-wire" {
		t.Fatalf("GetEntityContentInRepositories() = %#v, %v, want the canned entity", scoped, err)
	}
	scopedPage, err := pin.SearchEntitiesByNameInRepositories(ctx, []string{"repo-wire"}, "Function", "handleWire", 10)
	if err != nil || len(scopedPage) != 1 {
		t.Fatalf("SearchEntitiesByNameInRepositories() rows = %d, %v, want one", len(scopedPage), err)
	}

	grantSearch := taxonomy.LanguageEntitySearch{
		Language:             "go",
		EntityType:           "Function",
		Query:                "handleWire",
		Limit:                10,
		AllowedRepositoryIDs: []string{"repo-wire"},
	}
	grantPage, err := pin.SearchEntitiesByLanguageAndTypeForAccess(ctx, grantSearch)
	if err != nil || len(grantPage) != 1 || grantPage[0].EntityID != "entity-wire-1" {
		t.Fatalf("SearchEntitiesByLanguageAndTypeForAccess() = %#v, %v, want the canned entity for the granted search", grantPage, err)
	}
	if wire := marshalCodeRelationshipAdmissionWire(t, grantSearch); !strings.Contains(wire, `"AllowedRepositoryIDs":["repo-wire"]`) {
		t.Errorf("grant search wire bytes = %s, want the caller grant carried alongside the filters", wire)
	}

	emptySearch := grantSearch
	emptySearch.Query = "nothing-matches"
	empty, err := pin.SearchEntitiesByLanguageAndTypeForAccess(ctx, emptySearch)
	if err != nil || len(empty) != 0 {
		t.Fatalf("SearchEntitiesByLanguageAndTypeForAccess() unmatching = %#v, %v, want zero rows and no error", empty, err)
	}
	if wire := marshalCodeRelationshipAdmissionWire(t, (*querycontract.EntityContent)(nil)); wire != "null" {
		t.Errorf("nil entity wire bytes = %s, want null (fail-closed emptiness has no hidden shape)", wire)
	}
	if wire := marshalCodeRelationshipAdmissionWire(t, rows); !strings.Contains(wire, `"wire":"graph-row"`) {
		t.Errorf("graph wire bytes = %s, want the canned graph row", wire)
	}
}
