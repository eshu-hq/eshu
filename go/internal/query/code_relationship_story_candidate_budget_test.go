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

	// A second GRANTED repository. codeGrantOtherRepo is the ungranted tenant
	// in the sibling suites, and reusing it here would read as a leak assertion.
	codeGrantSecondRepo = "repo://tenant-a/second-service"
)

type storyBudgetContentStore struct {
	fakePortContentStore
	rows      []EntityContent
	askedRepo []string
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

func (s *storyBudgetContentStore) SearchEntitiesByNameAnyRepo(
	ctx context.Context,
	entityType, name string,
	limit int,
) ([]EntityContent, error) {
	return s.SearchEntitiesByName(ctx, "", entityType, name, limit)
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
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo, codeGrantSecondRepo})
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
			store.askedRepo, body)
	}
	if !strings.Contains(body, storyBudgetExactID) {
		t.Fatalf("the resolved target is not the exact match: asked=%v body=%s", store.askedRepo, body)
	}
	if !slices.Contains(store.askedRepo, codeGrantSecondRepo) {
		t.Fatalf("the second granted repository was never queried: asked=%v", store.askedRepo)
	}
}
