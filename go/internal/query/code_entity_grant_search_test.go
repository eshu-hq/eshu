// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"testing"
)

// searchEntitiesForGrant (code_relationship_story_resolution.go) and
// LanguageQueryHandler.searchLanguageEntities (language_query_metadata.go) hold
// the same three-branch dispatch on purpose: the parser-relationship kit
// classifies every go/internal/query/language*.go path as Language Query DSL
// source and fails any change to one that is not accompanied by a
// language-query-dsl.md update, so the story route cannot reduce the DSL read
// to a call into a shared helper without claiming a DSL change that never
// happened.
//
// Two copies of a dispatch drift. This is the guard that makes them drift
// loudly: it drives both over the same store, in the same shapes, and compares
// what comes back. Change one side alone and this test reds.

// TestSearchEntitiesForGrantMatchesTheLanguageQueryRead covers both store
// shapes -- one that takes the grant into its own statement and one that can
// only be asked a repository at a time -- across the three inputs that select
// each branch.
func TestSearchEntitiesForGrantMatchesTheLanguageQueryRead(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		search languageEntitySearch
	}{
		{
			name: "repo_id_named",
			search: languageEntitySearch{
				RepoID: codeGrantGrantedRepo, Language: "go", EntityType: "Variable", Limit: 10,
				AllowedRepositoryIDs: []string{codeGrantGrantedRepo},
			},
		},
		{
			name: "corpus_wide_with_grant",
			search: languageEntitySearch{
				Language: "go", EntityType: "Variable", Limit: 10,
				AllowedRepositoryIDs: []string{codeGrantGrantedRepo},
			},
		},
		{
			name: "corpus_wide_unscoped",
			search: languageEntitySearch{
				Language: "go", EntityType: "Variable", Limit: 10,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for storeName, newStore := range map[string]func() ContentStore{
				"grant_bound_store": func() ContentStore { return &languageQueryGrantContentStore{} },
				"plain_store":       func() ContentStore { return &languageQueryPlainContentStore{} },
			} {
				// A fresh store per call: both fakes record what they were
				// asked, and sharing one would let the second call read the
				// first call's record rather than its own.
				viaHandler, handlerErr := (&LanguageQueryHandler{Content: newStore()}).
					searchLanguageEntities(t.Context(), tc.search)
				viaHelper, helperErr := searchEntitiesForGrant(t.Context(), newStore(), tc.search)
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

	if _, err := searchEntitiesForGrant(t.Context(), nil, languageEntitySearch{EntityType: "Variable"}); err == nil {
		t.Fatal("a nil content store must be refused, not read")
	}
	if _, err := (&LanguageQueryHandler{}).
		searchLanguageEntities(t.Context(), languageEntitySearch{EntityType: "Variable"}); err == nil {
		t.Fatal("the handler read must refuse a nil content store too")
	}
}
