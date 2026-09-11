// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package language

import (
	"context"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/relationships/story"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// searchEntitiesForGrant (codequery/relationships/story/resolution.go) and
// Handler.searchLanguageEntities (metadata.go) hold the same three-branch
// dispatch on purpose: the parser-relationship kit classifies every
// go/internal/query/language*.go path as Language Query DSL source and fails
// any change to one that is not accompanied by a language-query-dsl.md
// update, so the story route cannot reduce the DSL read to a call into a
// shared helper without claiming a DSL change that never happened.
//
// Two copies of a dispatch drift. This is the guard that makes them drift
// loudly: it drives both over the same store, in the same shapes, and compares
// what comes back. Change one side alone and this test reds.

// entitySearchDispatchGrantBoundStore is a minimal grant-bound ContentStore
// double: it takes the grant into its own statement, mirroring package
// query's languageQueryGrantContentStore
// (auth_scoped_language_query_shipped_text_test.go). It shares
// querytestutil.LanguageQueryGrantEntities with that root double and with
// entitySearchDispatchPlainStore below, so the fixture data the three
// exercise cannot quietly diverge.
type entitySearchDispatchGrantBoundStore struct {
	querytestutil.FakePortContentStore
}

func (s *entitySearchDispatchGrantBoundStore) SearchEntitiesByLanguageAndTypeForAccess(
	_ context.Context,
	search querycontract.LanguageEntitySearch,
) ([]querycontract.EntityContent, error) {
	return querytestutil.LanguageQueryGrantEntities(search.RepoID, search.AllowedRepositoryIDs, search.EntityType), nil
}

// entitySearchDispatchPlainStore is a minimal per-repository-only
// ContentStore double, mirroring package query's languageQueryPlainContentStore
// (auth_scoped_language_query_grant_test.go).
type entitySearchDispatchPlainStore struct {
	querytestutil.FakePortContentStore
}

func (s *entitySearchDispatchPlainStore) SearchEntitiesByLanguageAndType(
	_ context.Context,
	repoID, _, entityType, _ string,
	_ int,
) ([]querycontract.EntityContent, error) {
	return querytestutil.LanguageQueryGrantEntities(repoID, nil, entityType), nil
}

// TestSearchEntitiesForGrantMatchesTheLanguageQueryRead covers both store
// shapes -- one that takes the grant into its own statement and one that can
// only be asked a repository at a time -- across the three inputs that select
// each branch.
func TestSearchEntitiesForGrantMatchesTheLanguageQueryRead(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		search querycontract.LanguageEntitySearch
	}{
		{
			name: "repo_id_named",
			search: querycontract.LanguageEntitySearch{
				RepoID: querytestutil.CodeGrantGrantedRepo, Language: "go", EntityType: "Variable", Limit: 10,
				AllowedRepositoryIDs: []string{querytestutil.CodeGrantGrantedRepo},
			},
		},
		{
			name: "corpus_wide_with_grant",
			search: querycontract.LanguageEntitySearch{
				Language: "go", EntityType: "Variable", Limit: 10,
				AllowedRepositoryIDs: []string{querytestutil.CodeGrantGrantedRepo},
			},
		},
		{
			name: "corpus_wide_unscoped",
			search: querycontract.LanguageEntitySearch{
				Language: "go", EntityType: "Variable", Limit: 10,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for storeName, newStore := range map[string]func() querycontract.ContentStore{
				"grant_bound_store": func() querycontract.ContentStore { return &entitySearchDispatchGrantBoundStore{} },
				"plain_store":       func() querycontract.ContentStore { return &entitySearchDispatchPlainStore{} },
			} {
				// A fresh store per call: both fakes record what they were
				// asked, and sharing one would let the second call read the
				// first call's record rather than its own.
				viaHandler, handlerErr := (&Handler{Content: newStore()}).
					searchLanguageEntities(t.Context(), tc.search)
				viaHelper, helperErr := story.SearchEntitiesForGrant(t.Context(), newStore(), tc.search)
				if (handlerErr == nil) != (helperErr == nil) {
					t.Fatalf("%s: handler err = %v, helper err = %v", storeName, handlerErr, helperErr)
				}
				if !reflect.DeepEqual(viaHandler, viaHelper) {
					t.Fatalf("%s: the two copies of the dispatch disagree:\nhandler = %#v\nhelper  = %#v",
						storeName, viaHandler, viaHelper)
				}
			}
		})
	}
}

// TestSearchEntitiesForGrantRejectsANilStore pins the one place the two
// deliberately differ. The handler method guards h and h.Content together
// because a nil handler cannot be dereferenced; the free function takes the
// store directly and guards only that.
func TestSearchEntitiesForGrantRejectsANilStore(t *testing.T) {
	t.Parallel()

	if _, err := story.SearchEntitiesForGrant(t.Context(), nil, querycontract.LanguageEntitySearch{EntityType: "Variable"}); err == nil {
		t.Fatal("a nil content store must be refused, not read")
	}
	if _, err := (&Handler{}).
		searchLanguageEntities(t.Context(), querycontract.LanguageEntitySearch{EntityType: "Variable"}); err == nil {
		t.Fatal("the handler read must refuse a nil content store too")
	}
}
