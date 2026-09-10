// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// Two-tenant grant scaffold for the code-family auth-scoped route proofs in
// this package (auth_scoped_code_scope_grant_test.go,
// auth_scoped_code_empty_grant_shape_test.go).
//
// This mirrors auth_scoped_code_content_grant_test.go in package query, which
// keeps the canonical copy for the root-side proofs (including
// TestCodeContentFiltersBindTheGrantInTheShippedSQL, the SQL-text guard the
// fake-driven tests here cannot be). The copies cannot be shared: the store
// methods below are typed on this package's request/row structs
// (StructuralInventoryRequest, SymbolSearchRequest,
// HardcodedSecretInvestigationRequest), and a _test.go symbol is not
// importable across the package boundary (#6060). Keep the record-and-filter
// behavior identical -- the "never contains the out-of-grant repository"
// assertions on both sides depend on it.

type codeContentGrantStore interface {
	ContentStore
	boundRepositoryIDs() []string
	storeWasQueried() bool
}

// codeContentGrantRecorder is the shared record-and-filter behavior each
// route's fake store needs: it remembers the grant list the handler bound and
// applies it the way the shipped SQL does.
type codeContentGrantRecorder struct {
	bound   []string
	queried bool
}

func (r *codeContentGrantRecorder) record(repoID string, allowedRepositoryIDs []string) {
	r.queried = true
	r.bound = append([]string(nil), allowedRepositoryIDs...)
	_ = repoID
}

func (r *codeContentGrantRecorder) boundRepositoryIDs() []string { return r.bound }
func (r *codeContentGrantRecorder) storeWasQueried() bool        { return r.queried }

// codeContentGrantAdmits lives in code_grant_shared_test.go (duplicated there
// at the move with the same record-and-filter contract); the stores below
// call it.

type hardcodedSecretGrantStore struct {
	querytestutil.FakePortContentStore
	codeContentGrantRecorder
}

func (s *hardcodedSecretGrantStore) InvestigateHardcodedSecrets(
	_ context.Context,
	req HardcodedSecretInvestigationRequest,
) ([]HardcodedSecretFindingRow, error) {
	s.record(req.RepoID, req.AllowedRepositoryIDs)
	rows := make([]HardcodedSecretFindingRow, 0, 2)
	for _, repoID := range []string{codeGrantGrantedRepo, codeGrantOtherRepo} {
		if !codeContentGrantAdmits(repoID, req.RepoID, req.AllowedRepositoryIDs) {
			continue
		}
		rows = append(rows, HardcodedSecretFindingRow{
			RepoID:       repoID,
			RelativePath: "internal/config/keys.go",
			Language:     "go",
			LineNumber:   12,
			LineText:     `apiToken := "sk_live_redacted"`,
			FindingKind:  "api_token",
		})
	}
	return rows, nil
}

type symbolSearchGrantStore struct {
	querytestutil.FakePortContentStore
	codeContentGrantRecorder
}

func (s *symbolSearchGrantStore) SearchSymbols(
	_ context.Context,
	req SymbolSearchRequest,
) ([]EntityContent, error) {
	s.record(req.RepoID, req.AllowedRepositoryIDs)
	return codeContentGrantEntities(req.RepoID, req.AllowedRepositoryIDs), nil
}

type structuralInventoryGrantStore struct {
	querytestutil.FakePortContentStore
	codeContentGrantRecorder
}

func (s *structuralInventoryGrantStore) InspectStructuralInventory(
	_ context.Context,
	req StructuralInventoryRequest,
) ([]EntityContent, error) {
	s.record(req.RepoID, req.AllowedRepositoryIDs)
	return codeContentGrantEntities(req.RepoID, req.AllowedRepositoryIDs), nil
}

func (s *structuralInventoryGrantStore) CountStructuralInventoryByFile(
	_ context.Context,
	req StructuralInventoryRequest,
) ([]StructuralInventoryFileCount, error) {
	s.record(req.RepoID, req.AllowedRepositoryIDs)
	counts := make([]StructuralInventoryFileCount, 0, 2)
	for _, entity := range codeContentGrantEntities(req.RepoID, req.AllowedRepositoryIDs) {
		counts = append(counts, StructuralInventoryFileCount{
			RepoID:        entity.RepoID,
			RelativePath:  entity.RelativePath,
			Language:      entity.Language,
			FunctionCount: 3,
		})
	}
	return counts, nil
}

func codeContentGrantEntities(repoID string, allowedRepositoryIDs []string) []EntityContent {
	entities := make([]EntityContent, 0, 2)
	for _, candidate := range []string{codeGrantGrantedRepo, codeGrantOtherRepo} {
		if !codeContentGrantAdmits(candidate, repoID, allowedRepositoryIDs) {
			continue
		}
		entities = append(entities, EntityContent{
			EntityID:     candidate + "#RefreshSession",
			RepoID:       candidate,
			RelativePath: "internal/auth/session.go",
			EntityType:   "Function",
			EntityName:   "RefreshSession",
			Language:     "go",
			StartLine:    10,
			EndLine:      20,
		})
	}
	return entities
}

type codeContentGrantRoute struct {
	name     string
	path     string
	body     map[string]any
	newStore func() codeContentGrantStore
}

func codeContentGrantRoutes() []codeContentGrantRoute {
	return []codeContentGrantRoute{
		{
			name:     "investigate_hardcoded_secrets",
			path:     "/api/v0/code/security/secrets/investigate",
			body:     map[string]any{},
			newStore: func() codeContentGrantStore { return &hardcodedSecretGrantStore{} },
		},
		{
			name:     "search_symbols",
			path:     "/api/v0/code/symbols/search",
			body:     map[string]any{"symbol": "RefreshSession"},
			newStore: func() codeContentGrantStore { return &symbolSearchGrantStore{} },
		},
		{
			name:     "inspect_structural_inventory",
			path:     "/api/v0/code/structure/inventory",
			body:     map[string]any{"inventory_kind": "entity", "language": "go"},
			newStore: func() codeContentGrantStore { return &structuralInventoryGrantStore{} },
		},
		{
			name:     "inspect_structural_inventory_function_count_by_file",
			path:     "/api/v0/code/structure/inventory",
			body:     map[string]any{"inventory_kind": "function_count_by_file", "language": "go"},
			newStore: func() codeContentGrantStore { return &structuralInventoryGrantStore{} },
		},
	}
}
