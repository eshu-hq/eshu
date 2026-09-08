// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// The story route resolves a target name by reading candidates a granted
// repository at a time, then keeping only the EXACT name matches
// (exactEntityNameMatches in entity_resolution.go). The content read behind it
// is a substring search -- entity_name ILIKE '%name%' in
// ContentReader.SearchEntitiesByName -- so a repository full of near-misses can
// fill the caller's budget with rows that are all discarded a moment later.
//
// storyBudgetContentStore mirrors that substring contract rather than the exact
// one the other story fakes use, because the exact-match fake cannot express
// this defect at all: every row it returns survives the filter.

const (
	storyBudgetTarget   = "PaymentGateway"
	storyBudgetOtherOne = "PaymentGatewayFactory"
	storyBudgetOtherTwo = "PaymentGatewayBuilder"
	storyBudgetOtherTre = "PaymentGatewayAdapter"
	storyBudgetExactID  = "entity:payment-gateway-exact"

	// The single-repository case (#6555). The exact symbol shares a repository
	// with the near-misses instead of living behind them in a later one, so no
	// amount of per-repository budgeting reaches it: the substring page is
	// full before the row that matches exactly is read.
	storyBudgetSingleExactID = "entity:payment-gateway-single-repo-exact"

	// A second GRANTED repository. codeGrantOtherRepo is the ungranted tenant
	// in the sibling suites, and reusing it here would read as a leak assertion.
	codeGrantSecondRepo = "repo://tenant-a/second-service"
)

type storyBudgetContentStore struct {
	fakePortContentStore
	rows           []EntityContent
	askedRepo      []string
	askedExactRepo []string
}

// SearchEntitiesByName mirrors the shipped SQL: repo_id = $1 AND entity_name
// ILIKE '%' || $2 || '%', ordered and capped by LIMIT.
func (s *storyBudgetContentStore) SearchEntitiesByName(
	_ context.Context,
	repoID, _, name string,
	limit int,
) ([]EntityContent, error) {
	s.askedRepo = append(s.askedRepo, repoID)
	out := make([]EntityContent, 0, limit)
	for _, row := range s.rows {
		if len(out) >= limit {
			break
		}
		if repoID != "" && row.RepoID != repoID {
			continue
		}
		if name != "" && !strings.Contains(row.EntityName, name) {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

// SearchEntitiesByExactName mirrors the exact-name SQL: repo_id = $1 AND
// entity_name = $3, ordered and capped by LIMIT. It is the read the story
// route's target resolution actually wants, and the one whose absence this
// suite's near-miss rows expose.
func (s *storyBudgetContentStore) SearchEntitiesByExactName(
	_ context.Context,
	repoID, _, name string,
	limit int,
) ([]EntityContent, error) {
	s.askedExactRepo = append(s.askedExactRepo, repoID)
	out := make([]EntityContent, 0, limit)
	for _, row := range s.rows {
		if len(out) >= limit {
			break
		}
		if repoID != "" && row.RepoID != repoID {
			continue
		}
		if name != "" && row.EntityName != name {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func (s *storyBudgetContentStore) SearchEntitiesByNameAnyRepo(
	ctx context.Context,
	entityType, name string,
	limit int,
) ([]EntityContent, error) {
	return s.SearchEntitiesByName(ctx, "", entityType, name, limit)
}

// askedRepositories is every repository the route asked about, through either
// read. Tests assert against this rather than one field: which of the two reads
// the route issues is the thing #6555 changed, and a walk-reached-repository-N
// assertion is about the walk, not about the read it is made of.
func (s *storyBudgetContentStore) askedRepositories() []string {
	asked := make([]string, 0, len(s.askedRepo)+len(s.askedExactRepo))
	asked = append(asked, s.askedRepo...)
	return append(asked, s.askedExactRepo...)
}

func storyBudgetStore() *storyBudgetContentStore {
	return &storyBudgetContentStore{rows: []EntityContent{
		// The granted repository listed first is all near-misses. Enough of
		// them to fill a limit of 3 on their own.
		{EntityID: "entity:pg-factory", EntityName: storyBudgetOtherOne, EntityType: "Class", RepoID: codeGrantGrantedRepo, RelativePath: "a/one.go", Language: "go"},
		{EntityID: "entity:pg-builder", EntityName: storyBudgetOtherTwo, EntityType: "Class", RepoID: codeGrantGrantedRepo, RelativePath: "a/two.go", Language: "go"},
		{EntityID: "entity:pg-adapter", EntityName: storyBudgetOtherTre, EntityType: "Class", RepoID: codeGrantGrantedRepo, RelativePath: "a/three.go", Language: "go"},
		// The exact symbol lives in the second granted repository.
		{EntityID: storyBudgetExactID, EntityName: storyBudgetTarget, EntityType: "Class", RepoID: codeGrantSecondRepo, RelativePath: "b/gateway.go", Language: "go"},
	}}
}

// TestRelationshipStoryFindsAnExactMatchBehindAFullRepositoryPage is the
// regression. Two granted repositories, the first holding `limit` substring
// near-misses and the second holding the exact symbol: the route must resolve
// to the exact one rather than answering not_found because the first
// repository consumed the shared budget.
func TestRelationshipStoryFindsAnExactMatchBehindAFullRepositoryPage(t *testing.T) {
	t.Parallel()

	store := storyBudgetStore()
	auth := querytestutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo, codeGrantSecondRepo})
	rec := runStoryRequest(t, storyGrantHandler(GraphBackendNornicDB, &storyClauseGraph{}, store), map[string]any{
		"target":            storyBudgetTarget,
		"relationship_type": "CALLS",
		"limit":             2,
	}, &auth)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, `"status":"not_found"`) {
		t.Fatalf("the exact match in the second granted repository was never read; asked=%v body=%s",
			store.askedRepositories(), body)
	}
	if !strings.Contains(body, storyBudgetExactID) {
		t.Fatalf("the resolved target is not the exact match: asked=%v body=%s",
			store.askedRepositories(), body)
	}
	if !slices.Contains(store.askedRepositories(), codeGrantSecondRepo) {
		t.Fatalf("the second granted repository was never queried: substring=%v exact=%v",
			store.askedRepo, store.askedExactRepo)
	}
}

// storyBudgetSingleRepoStore puts every row in ONE granted repository, with the
// near-misses sorted ahead of the exact symbol. `ORDER BY relative_path,
// start_line` is what decides the page, so a/one.go, a/two.go and a/three.go
// fill a limit of 3 before z/gateway.go is ever read.
func storyBudgetSingleRepoStore() *storyBudgetContentStore {
	return &storyBudgetContentStore{rows: []EntityContent{
		{EntityID: "entity:pg-factory-single", EntityName: storyBudgetOtherOne, EntityType: "Class", RepoID: codeGrantGrantedRepo, RelativePath: "a/one.go", Language: "go"},
		{EntityID: "entity:pg-builder-single", EntityName: storyBudgetOtherTwo, EntityType: "Class", RepoID: codeGrantGrantedRepo, RelativePath: "a/two.go", Language: "go"},
		{EntityID: "entity:pg-adapter-single", EntityName: storyBudgetOtherTre, EntityType: "Class", RepoID: codeGrantGrantedRepo, RelativePath: "a/three.go", Language: "go"},
		{EntityID: storyBudgetSingleExactID, EntityName: storyBudgetTarget, EntityType: "Class", RepoID: codeGrantGrantedRepo, RelativePath: "z/gateway.go", Language: "go"},
	}}
}

// TestRelationshipStoryFindsAnExactMatchBehindNearMissesInOneRepository is the
// #6555 regression, and the case #6553's per-repository budget cannot reach.
//
// One granted repository holds three substring near-misses and the exact
// symbol. The read the route issues is `entity_name ILIKE '%PaymentGateway%'`
// bounded by LIMIT, so the page comes back as the three near-misses;
// exactEntityNameMatches then discards all three and the caller is told a
// symbol that exists in its own granted repository is not_found.
//
// Reading one repository at a time does not help here -- there is only one --
// and neither does a larger budget, which just moves the row count at which the
// same thing happens. The fix is to ask for the name the caller named.
func TestRelationshipStoryFindsAnExactMatchBehindNearMissesInOneRepository(t *testing.T) {
	t.Parallel()

	store := storyBudgetSingleRepoStore()
	auth := querytestutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	rec := runStoryRequest(t, storyGrantHandler(GraphBackendNornicDB, &storyClauseGraph{}, store), map[string]any{
		"target":            storyBudgetTarget,
		"relationship_type": "CALLS",
		"limit":             2,
	}, &auth)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, `"status":"not_found"`) {
		t.Fatalf("an exact symbol in the caller's own granted repository answered not_found;"+
			" substring=%v exact=%v body=%s", store.askedRepo, store.askedExactRepo, body)
	}
	if !strings.Contains(body, storyBudgetSingleExactID) {
		t.Fatalf("the resolved target is not the exact match: substring=%v exact=%v body=%s",
			store.askedRepo, store.askedExactRepo, body)
	}
	// The near-misses must not be offered as candidates either: they are not
	// what the caller named, and the pre-#6555 read is the only thing that
	// could have put them in the response.
	for _, nearMiss := range []string{"entity:pg-factory-single", "entity:pg-builder-single", "entity:pg-adapter-single"} {
		if strings.Contains(body, nearMiss) {
			t.Fatalf("a substring near-miss %q reached the response: %s", nearMiss, body)
		}
	}
	if !slices.Contains(store.askedExactRepo, codeGrantGrantedRepo) {
		t.Fatalf("the granted repository was never asked for an exact name: substring=%v exact=%v",
			store.askedRepo, store.askedExactRepo)
	}
	// The inverse of the pre-fix evidence, which read
	// `substring=[repo://tenant-a/granted-service] exact=[]`. Resolving the
	// right entity is not the whole fix: the route also has to stop asking the
	// substring question, because that is what filled the page.
	if len(store.askedRepo) != 0 {
		t.Fatalf("the route still issued the substring read: substring=%v exact=%v",
			store.askedRepo, store.askedExactRepo)
	}
}
