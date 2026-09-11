// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// ScopedTestAuthContext builds the scoped auth context scope-enforcement tests
// exercise: tenant identity with an explicit repository allow-list.
//
// It lives here rather than in a package query test file because of the Go
// rule documented on FakeScopedTokenResolver (scopedtoken.go): the moved
// impact/ investigation tests build scoped contexts from outside package
// query, so the constructor must be importable. query.AuthContext is an alias
// of queryauth.AuthContext, so this builds exactly the value the root helper
// built.
func ScopedTestAuthContext(tenant string, allowedRepositoryIDs []string) queryauth.AuthContext {
	return queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
		TenantID:             tenant,
		WorkspaceID:          tenant,
		SubjectClass:         "team",
		SubjectIDHash:        "sha256:" + tenant,
		PolicyRevisionHash:   "sha256:policy",
		AllowedRepositoryIDs: allowedRepositoryIDs,
	}
}

// CodeGrantScopedAuthContext builds the scoped-token AuthContext the #5167
// code-family batch-1 proof set shares: tenant-a, granted exactly the
// repository ids passed in (nil for the empty-grant fail-closed case).
//
// It lives here rather than in a package query test file for the same reason
// documented on ScopedTestAuthContext above: as internal/query splits into
// handler-family subpackages (#6060), each family's tests need the same
// helpers, and they cannot reach root's _test.go declarations at all.
// query.AuthContext is an alias of queryauth.AuthContext, so this builds
// exactly the value the root helper built.
func CodeGrantScopedAuthContext(allowedRepositoryIDs []string) queryauth.AuthContext {
	return queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: allowedRepositoryIDs,
	}
}

// AssertBoundRepositoryGrantArray proves a grant predicate's placeholder is
// actually bound to the caller's id list, not left dangling: a predicate whose
// parameter never arrives fails at execution, and a predicate bound to the
// wrong list silently widens the scan. Shipped builders bind the list with
// pgarray.Array, so the assertion scans args for the *pgarray.StringArray
// carrying want rather than demanding an exact string element.
func AssertBoundRepositoryGrantArray(t *testing.T, args []any, want []string) {
	t.Helper()
	for _, arg := range args {
		bound, ok := arg.(*pgarray.StringArray)
		if !ok {
			continue
		}
		if got := []string(*bound); slices.Equal(got, want) {
			return
		}
		t.Fatalf("bound grant array = %#v, want %#v", []string(*bound), want)
	}
	t.Fatalf("args = %#v, want one bound *pgarray.StringArray carrying %#v", args, want)
}

// BoundCanonicalLanguage returns the first entry of a Cypher builder's bound
// $languages list, which graphLanguageSpellings (package language's
// registry.go) documents as the canonical name. Shared by package query's
// typescript_function_graph_first_test.go and typescript_graph_metadata_test.go
// and by package language's own Cypher-builder tests, so a _test.go copy in
// either package cannot drift from the other.
func BoundCanonicalLanguage(t *testing.T, params map[string]any) string {
	t.Helper()
	bound, ok := params["languages"].([]string)
	if !ok || len(bound) == 0 {
		t.Fatalf("params[languages] = %#v, want a non-empty []string", params["languages"])
	}
	return bound[0]
}

// CodeGrantGrantedRepo and CodeGrantOtherRepo are canonical repository ids
// (the repo:// form queryselector.LooksCanonicalRepositoryID recognises) the
// #5167/#6642 code and language grant tests use to prove a scoped read
// admits the granted repository and excludes the other one. They live here,
// rather than only in package query's code_grant_test_fixtures_test.go,
// because package language's own grant tests (#6642) need the identical
// values and a _test.go symbol is not importable across a package boundary.
// Package query keeps its own codeGrantGrantedRepo/codeGrantOtherRepo consts
// defined as this package's spelling, so its 15+ existing callers compile
// unchanged.
const (
	CodeGrantGrantedRepo = "repo://tenant-a/granted-service"
	CodeGrantOtherRepo   = "repo://tenant-b/other-service"
)

// SearchString reports whether sub occurs within s. It is the shared
// substring-match primitive several handler-family Cypher/SQL-text tests use
// to assert a builder's shipped text without pulling in a regexp or
// strings.Contains substitute that could hide a spacing difference the
// original loop would have caught. package query's neo4j_test.go keeps a
// forwarding searchString so its own callers compile unchanged.
func SearchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// LanguageMetadataSharedPath, LanguageMetadataSharedName, and
// LanguageMetadataSharedStart are the file path, entity name, and start line
// two repositories share in the #6642 language-query metadata-merge-key
// tests (both package query's language_query_metadata_repository_key_test.go
// and package language's repository_match_key_test.go), so the two repeated
// keys collide on every field except the repository id -- otherwise the
// tests would exercise a different, more forgiving key collision than the
// one languageResultRepositoryMatchKey exists to prevent.
const (
	LanguageMetadataSharedPath  = "internal/auth/session.go"
	LanguageMetadataSharedName  = "SharedKeyProbe"
	LanguageMetadataSharedStart = 10
)

// LanguageGrantGrantedEntity and LanguageGrantUngrantedEntity name the
// entities LanguageQueryGrantEntities attaches to CodeGrantGrantedRepo and
// CodeGrantOtherRepo respectively, so a grant test can assert a response
// carries the first and never leaks the second.
const (
	LanguageGrantGrantedEntity   = "GrantedLanguageProbe"
	LanguageGrantUngrantedEntity = "UngrantedLanguageProbe"
)

// languageQueryGrantAdmits mirrors the shipped grant predicate
// buildLanguageTypeEntityFilters (package query's
// content_reader_entity_search.go) applies: an explicit repoID anchors the
// scan to that repository alone; otherwise an empty allowedRepositoryIDs
// list is unscoped (admits every row), and a non-empty one restricts the
// scan to membership. Keeping this fake faithful to the shipped predicate is
// what makes a grant-leak regression in the real builder fail the fixture
// too, rather than only a copy of the predicate that could drift from it.
func languageQueryGrantAdmits(rowRepoID, repoID string, allowedRepositoryIDs []string) bool {
	if anchor := strings.TrimSpace(repoID); anchor != "" && rowRepoID != anchor {
		return false
	}
	if len(allowedRepositoryIDs) == 0 {
		return true
	}
	return slices.Contains(allowedRepositoryIDs, rowRepoID)
}

// LanguageQueryGrantEntities returns the fixture rows a language-query grant
// test's content-store doubles hand back: one row for CodeGrantGrantedRepo
// and one for CodeGrantOtherRepo, each admitted or dropped by
// languageQueryGrantAdmits against repoID and allowedRepositoryIDs. It is
// shared by package query's languageQueryGrantContentStore and
// languageQueryPlainContentStore (both #5167/#6642 grant-test doubles) and by
// package language's entity-search-dispatch doubles, so all three test the
// same fixture data instead of three copies that could quietly diverge.
func LanguageQueryGrantEntities(repoID string, allowedRepositoryIDs []string, entityType string) []querycontract.EntityContent {
	if entityType == "" {
		entityType = "Variable"
	}
	entities := make([]querycontract.EntityContent, 0, 2)
	for _, candidate := range []string{CodeGrantGrantedRepo, CodeGrantOtherRepo} {
		if !languageQueryGrantAdmits(candidate, repoID, allowedRepositoryIDs) {
			continue
		}
		name := LanguageGrantGrantedEntity
		if candidate == CodeGrantOtherRepo {
			name = LanguageGrantUngrantedEntity
		}
		entities = append(entities, querycontract.EntityContent{
			EntityID:     candidate + "#" + name,
			RepoID:       candidate,
			RelativePath: "internal/auth/session.go",
			EntityType:   entityType,
			EntityName:   name,
			Language:     "go",
			StartLine:    10,
			EndLine:      20,
		})
	}
	return entities
}

// MockLanguageQueryGraphReader is a minimal querycontract.GraphQuery double
// that answers every Run with Rows and every RunSingle with Rows' first
// element. package query's language_query_metadata_test.go keeps a
// forwarding mockLanguageQueryGraphReader so its ~17 existing callers compile
// unchanged; package language's own tests construct this type directly.
type MockLanguageQueryGraphReader struct {
	Rows []map[string]any
}

// Run answers every call with Rows, ignoring the Cypher text and params.
func (m *MockLanguageQueryGraphReader) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return m.Rows, nil
}

// RunSingle answers with Rows' first element, or nil if Rows is empty.
func (m *MockLanguageQueryGraphReader) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	if len(m.Rows) == 0 {
		return nil, nil
	}
	return m.Rows[0], nil
}
