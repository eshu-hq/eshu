// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// #5167 code-family batch 2a: the shipped-text guards for
// POST /api/v0/code/language-query. The route tests in
// auth_scoped_language_query_grant_test.go drive fake stores and a statement
// interpreter, so they can prove behaviour; these prove the text the builders
// actually emit -- that the grant reaches it, and that an unscoped caller's
// statement has not otherwise moved -- which is what a rewrite would silently
// drop. Split from that file only to stay under the repository's 500-line cap.

// languageQueryGrantContentStore is the production shape: a store that takes
// the grant into its own statement, so one read serves the whole granted set.
type languageQueryGrantContentStore struct {
	fakePortContentStore
	searches []querycontract.LanguageEntitySearch
}

func (s *languageQueryGrantContentStore) SearchEntitiesByLanguageAndType(
	ctx context.Context,
	repoID, language, entityType, query string,
	limit int,
) ([]EntityContent, error) {
	return s.SearchEntitiesByLanguageAndTypeForAccess(ctx, querycontract.LanguageEntitySearch{
		RepoID: repoID, Language: language, EntityType: entityType, Query: query, Limit: limit,
	})
}

func (s *languageQueryGrantContentStore) SearchEntitiesByLanguageAndTypeForAccess(
	_ context.Context,
	search querycontract.LanguageEntitySearch,
) ([]EntityContent, error) {
	s.searches = append(s.searches, search)
	return querytestutil.LanguageQueryGrantEntities(search.RepoID, search.AllowedRepositoryIDs, search.EntityType), nil
}

// TestLanguageQueryGrantBoundStoreTakesOneRead pins the path production takes.
// A store that can carry the grant is asked once, with the granted ids, instead
// of once per repository -- so the statement's own LIMIT is applied to the
// granted set.
func TestLanguageQueryGrantBoundStoreTakesOneRead(t *testing.T) {
	t.Parallel()

	store := &languageQueryGrantContentStore{}
	handler := &LanguageQueryHandler{Content: store, Profile: ProfileLocalAuthoritative}
	auth := querytestutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	rec := runLanguageQueryGrantRequest(t, handler, languageQueryGrantBody("variable"), &auth)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if len(store.searches) != 1 {
		t.Fatalf("content searches = %d, want 1; a grant-bound store must not be asked once per repository", len(store.searches))
	}
	if got := store.searches[0].AllowedRepositoryIDs; !slices.Equal(got, []string{codeGrantGrantedRepo}) {
		t.Fatalf("AllowedRepositoryIDs = %#v, want [%q] bound into the content query", got, codeGrantGrantedRepo)
	}
	if strings.Contains(rec.Body.String(), languageGrantUngrantedEntity) {
		t.Fatalf("grant-bound content read leaked %q: %s", languageGrantUngrantedEntity, rec.Body.String())
	}
}

// TestLanguageTypeEntityFiltersBindTheGrantInTheShippedSQL is the guard the
// handler tests above cannot be: they drive fake stores, so no SQL text is ever
// built. Delete the builder's grant branch and every handler test still passes.
func TestLanguageTypeEntityFiltersBindTheGrantInTheShippedSQL(t *testing.T) {
	t.Parallel()

	grant := []string{codeGrantGrantedRepo}
	filters, args, _ := buildLanguageTypeEntityFilters("", grant, []string{"go"}, "Function", "")
	if !slices.Contains(filters, "repo_id = ANY($2)") {
		t.Fatalf("buildLanguageTypeEntityFilters() = %#v, want a repo_id = ANY($2) grant predicate; without it a scoped caller's grant is resolved but never applied", filters)
	}
	querytestutil.AssertBoundRepositoryGrantArray(t, args, grant)

	// A caller who named one repository keeps the single-repo equality
	// predicate and must not gain a second, wider ANY() scan.
	anchored, _, _ := buildLanguageTypeEntityFilters(codeGrantGrantedRepo, grant, []string{"go"}, "Function", "")
	for _, filter := range anchored {
		if strings.Contains(filter, "repo_id = ANY(") {
			t.Fatalf("buildLanguageTypeEntityFilters() emitted %q for a repo-anchored request; the grant list must not widen an anchored scan", filter)
		}
	}

	// An unscoped caller's statement stays exactly what it was.
	unscoped, _, _ := buildLanguageTypeEntityFilters("", nil, []string{"go"}, "Function", "")
	for _, filter := range unscoped {
		if strings.Contains(filter, "repo_id") {
			t.Fatalf("buildLanguageTypeEntityFilters() = %#v, want no repository predicate for an unscoped corpus-wide caller", unscoped)
		}
	}
}

// TestLanguageQueryBuildersBindTheGrantInTheShippedCypher and
// TestLanguageQueryUnscopedCypherTextIsFrozen moved to
// language_query_cypher_shipped_text_test.go (#6642): both call
// language.BuildCypherWithSemanticFilter, buildLanguageCypher, and
// graphSemanticMetadataProjection directly to pin the raw Cypher text the
// builders emit -- text no HTTP response body ever carries -- so they belong
// in a family-owned white-box test file rather than here alongside the
// route-level and root-owned-SQL-builder tests above.
