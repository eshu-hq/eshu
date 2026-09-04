// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// #5167 code-family batch 2a: the shipped-text guards for
// POST /api/v0/code/language-query. The route tests in
// auth_scoped_language_query_grant_test.go drive fake stores and a statement
// interpreter, so they can prove behaviour; these prove the text the two
// builders actually emit, which is what a rewrite would silently drop. Split
// from that file only to stay under the repository's 500-line cap.

// languageQueryGrantContentStore is the production shape: a store that takes
// the grant into its own statement, so one read serves the whole granted set.
type languageQueryGrantContentStore struct {
	fakePortContentStore
	searches []languageEntitySearch
}

func (s *languageQueryGrantContentStore) SearchEntitiesByLanguageAndType(
	ctx context.Context,
	repoID, language, entityType, query string,
	limit int,
) ([]EntityContent, error) {
	return s.SearchEntitiesByLanguageAndTypeForAccess(ctx, languageEntitySearch{
		RepoID: repoID, Language: language, EntityType: entityType, Query: query, Limit: limit,
	})
}

func (s *languageQueryGrantContentStore) SearchEntitiesByLanguageAndTypeForAccess(
	_ context.Context,
	search languageEntitySearch,
) ([]EntityContent, error) {
	s.searches = append(s.searches, search)
	return languageQueryGrantEntities(search.RepoID, search.AllowedRepositoryIDs, search.EntityType), nil
}

// TestLanguageQueryGrantBoundStoreTakesOneRead pins the path production takes.
// A store that can carry the grant is asked once, with the granted ids, instead
// of once per repository -- so the statement's own LIMIT is applied to the
// granted set.
func TestLanguageQueryGrantBoundStoreTakesOneRead(t *testing.T) {
	t.Parallel()

	store := &languageQueryGrantContentStore{}
	handler := &LanguageQueryHandler{Content: store, Profile: ProfileLocalAuthoritative}
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
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
	assertBoundRepositoryGrantArray(t, args, grant)

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

// TestLanguageQueryBuildersBindTheGrantInTheShippedCypher is the same guard for
// the four Cypher builders behind buildLanguageCypherWithSemanticFilter. The
// condition has to appear in the anchoring MATCH's own WHERE, ahead of every
// WITH, ORDER BY and LIMIT.
func TestLanguageQueryBuildersBindTheGrantInTheShippedCypher(t *testing.T) {
	t.Parallel()

	scoped := repositoryAccessFilter{AllowedRepositoryIDs: []string{codeGrantGrantedRepo}}
	want := "(r.id IN $allowed_repository_ids OR r.id IN $allowed_scope_ids)"

	for _, label := range []string{"Repository", "Directory", "File", "Function"} {
		t.Run(label, func(t *testing.T) {
			t.Parallel()

			cypher, params := buildLanguageCypherWithSemanticFilter("go", label, "", "", 50, "", "", scoped)
			normalized := normalizeCypherWhitespace(cypher)
			if !strings.Contains(normalized, want) {
				t.Fatalf("%s builder missing %q:\n%s", label, want, normalized)
			}
			if !slices.Contains(repositoryGoverningPredicatesForAlias(cypher, "r"), want) {
				t.Fatalf("%s builder puts the grant outside the Repository binding's own WHERE, so it does not decide row membership:\n%s", label, normalized)
			}
			// The governing-predicates assertion above already proves the
			// grant sits in the anchoring MATCH's own WHERE, which is ahead
			// of any WITH. These pin the rest of the ordering directly. A
			// literal " WITH " scan is not usable here: the extension filter
			// spliced into the same WHERE contains `ENDS WITH`.
			for _, clause := range []string{" RETURN ", " ORDER BY ", " LIMIT "} {
				at := strings.Index(normalized, clause)
				if at >= 0 && strings.Index(normalized, want) > at {
					t.Fatalf("%s builder emits the grant after %q, so the page is taken before the grant applies:\n%s", label, strings.TrimSpace(clause), normalized)
				}
			}
			if got, ok := params["allowed_repository_ids"].([]string); !ok || !slices.Equal(got, []string{codeGrantGrantedRepo}) {
				t.Fatalf("params[allowed_repository_ids] = %#v, want the caller's granted ids; an unbound parameter fails at execution", params["allowed_repository_ids"])
			}

			unscopedCypher, unscopedParams := buildLanguageCypher("go", label, "", "", 50)
			if strings.Contains(unscopedCypher, "$allowed_repository_ids") {
				t.Fatalf("%s builder carries a grant condition for an unscoped caller:\n%s", label, normalizeCypherWhitespace(unscopedCypher))
			}
			if _, ok := unscopedParams["allowed_repository_ids"]; ok {
				t.Fatalf("%s builder bound grant params for an unscoped caller: %#v", label, unscopedParams)
			}
		})
	}
}
