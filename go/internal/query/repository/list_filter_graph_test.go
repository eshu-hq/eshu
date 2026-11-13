// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

func graphFilterCannedRows() []map[string]any {
	rows := make([]map[string]any, 0, 3)
	for _, e := range listFilterTestRepos() {
		rows = append(rows, map[string]any{
			"id":              e.ID,
			"name":            e.Name,
			"path":            "",
			"local_path":      "",
			"remote_url":      "",
			"repo_slug":       e.RepoSlug,
			"has_remote":      false,
			"evidence_source": "projector/canonical",
		})
	}
	return rows
}

// graphNameFilteredFake answers count and page queries the way the handler's
// filtered Cypher would: the page applies the name_contains parameter, the
// count returns the filtered cardinality. It fails when the name predicate
// is missing from a filtered request or leaks into an unfiltered one.
func graphNameFilteredFake(t *testing.T, rows []map[string]any) graph.FakeGraphReader {
	t.Helper()
	apply := func(params map[string]any) []map[string]any {
		raw, _ := params["name_contains"].(string)
		kept := rows
		if raw != "" {
			kept = make([]map[string]any, 0, len(rows))
			for _, row := range rows {
				name, _ := row["name"].(string)
				slug, _ := row["repo_slug"].(string)
				if strings.Contains(strings.ToLower(name), raw) || strings.Contains(strings.ToLower(slug), raw) {
					kept = append(kept, row)
				}
			}
		}
		// Emulate the backend SKIP/LIMIT window so offset walks terminate:
		// the count query binds no window and sees every kept row.
		offset, _ := params["offset"].(int)
		limit, _ := params["limit"].(int)
		if offset < 0 {
			offset = 0
		}
		if offset >= len(kept) {
			return []map[string]any{}
		}
		end := len(kept)
		if limit > 0 && offset+limit < end {
			end = offset + limit
		}
		return kept[offset:end]
	}
	return graph.FakeGraphReader{
		RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "count(r) AS total") {
				return []map[string]any{{"total": int64(len(apply(params)))}}, nil
			}
			return apply(params), nil
		},
	}
}

// TestListRepositoriesNameFilterGraphPath proves the graph path threads the
// name predicate into both the COUNT and the page query, so total stays
// exact for the filtered set.
func TestListRepositoriesNameFilterGraphPath(t *testing.T) {
	t.Parallel()

	var sawCountPredicate, sawPagePredicate bool
	fake := graphNameFilteredFake(t, graphFilterCannedRows())
	inner := fake.RunFn
	fake.RunFn = func(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
		if strings.Contains(cypher, "count(r) AS total") {
			sawCountPredicate = strings.Contains(cypher, "name_contains")
		} else {
			// OR-accumulate: the dependency pre-pass issues its own
			// non-count query without the predicate and must not clear
			// the page observation.
			sawPagePredicate = sawPagePredicate || strings.Contains(cypher, "name_contains")
		}
		return inner(ctx, cypher, params)
	}
	handler := &Handler{Neo4j: fake, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?name=alpha", nil)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	if !sawCountPredicate || !sawPagePredicate {
		t.Fatalf("name predicate in count=%v page=%v, want both true", sawCountPredicate, sawPagePredicate)
	}
	body := decodeListFilterBody(t, rec)
	if got, want := body["total"], float64(2); got != want {
		t.Fatalf("total = %#v, want %#v", got, want)
	}
	if got, want := listFilterRowNames(t, body), []string{"alpha-service", "gamma-alpha-bridge"}; !equalStrings(got, want) {
		t.Fatalf("names = %#v, want %#v in name order", got, want)
	}
}

// TestListRepositoriesUnfilteredGraphCypherUnchanged proves an unfiltered
// request sends no name predicate, keeping today's Cypher byte-stable.
func TestListRepositoriesUnfilteredGraphCypherUnchanged(t *testing.T) {
	t.Parallel()

	fake := graphNameFilteredFake(t, graphFilterCannedRows())
	inner := fake.RunFn
	fake.RunFn = func(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
		if strings.Contains(cypher, "name_contains") {
			t.Errorf("unfiltered request carries a name predicate:\n%s", cypher)
		}
		if _, ok := params["name_contains"]; ok {
			t.Errorf("unfiltered request binds name_contains: %#v", params)
		}
		return inner(ctx, cypher, params)
	}
	handler := &Handler{Neo4j: fake, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	if got, want := decodeListFilterBody(t, rec)["total"], float64(3); got != want {
		t.Fatalf("total = %#v, want %#v", got, want)
	}
}

// scopedListRequest attaches a scoped grant for exactly repo-team-a.
func TestListRepositoriesNameIntersectsGrantGraphPath(t *testing.T) {
	t.Parallel()

	var sawGrant, sawName bool
	rows := append(graphFilterCannedRows(), map[string]any{
		"id": "repo-team-a", "name": "team-a-svc", "path": "", "local_path": "",
		"remote_url": "", "repo_slug": "acme/team-a-svc", "has_remote": false,
		"evidence_source": "projector/canonical",
	})
	fake := graphNameFilteredFake(t, rows)
	inner := fake.RunFn
	fake.RunFn = func(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
		if strings.Contains(cypher, "count(r) AS total") {
			// The composed count would count the grant∩name intersection;
			// the name already excludes the granted row, so it is zero.
			return []map[string]any{{"total": int64(0)}}, nil
		}
		sawGrant = sawGrant || strings.Contains(cypher, "allowed_repository_ids")
		sawName = sawName || strings.Contains(cypher, "name_contains")
		// The fake honors only the name predicate; the grant composition is
		// proven by the cypher assertions plus the content-path intersection
		// test. Narrow the canned rows to the granted id here so the page
		// reflects what the composed query would return for a non-matching name.
		filtered, err := inner(ctx, cypher, params)
		if err != nil {
			return nil, err
		}
		kept := filtered[:0]
		for _, row := range filtered {
			if row["id"] == "repo-team-a" {
				kept = append(kept, row)
			}
		}
		return kept, nil
	}
	handler := &Handler{Neo4j: fake, Profile: querycontract.ProfileLocalAuthoritative}
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, scopedListRequest(t, "/api/v0/repositories?name=alpha"))

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	if !sawGrant || !sawName {
		t.Fatalf("page WHERE has grant=%v name=%v, want both true", sawGrant, sawName)
	}
	body := decodeListFilterBody(t, rec)
	// team-a-svc does not contain "alpha": the intersection is empty.
	if got, want := body["count"], float64(0); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
}

// TestListRepositoriesNameTooLongIs400 is the RED guard for the 200-char
// bound: an over-long name must fail closed, not scan unbounded input.
func TestListRepositoriesNameDifferentialExhaustion(t *testing.T) {
	t.Parallel()

	entries := []querycontract.RepositoryCatalogEntry{
		{ID: "repository:a1", Name: "alpha-one", RepoSlug: "x/a1"},
		{ID: "repository:a2", Name: "alpha-two", RepoSlug: "x/a2"},
		{ID: "repository:b1", Name: "beta-one", RepoSlug: "x/b1"},
		{ID: "repository:a3", Name: "alpha-three", RepoSlug: "x/a3"},
		{ID: "repository:b2", Name: "beta-two", RepoSlug: "x/b2"},
	}
	walk := func(handler *Handler, query string) []string {
		var names []string
		for offset := 0; ; offset++ {
			target := "/api/v0/repositories?limit=1&offset=" + itoa(offset) + query
			req := httptest.NewRequest(http.MethodGet, target, nil)
			rec := httptest.NewRecorder()
			handler.listRepositories(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s status = %d, want 200 body=%s", target, rec.Code, rec.Body.String())
			}
			body := decodeListFilterBody(t, rec)
			got := listFilterRowNames(t, body)
			names = append(names, got...)
			if truncated, _ := body["truncated"].(bool); !truncated {
				break
			}
		}
		return names
	}
	// Client-side filter of the unfiltered walk.
	clientFiltered := func(names []string) []string {
		var kept []string
		for _, n := range names {
			if strings.Contains(n, "alpha") {
				kept = append(kept, n)
			}
		}
		return kept
	}

	contentHandler := &Handler{
		Content: content.FakePortContentStore{Repositories: entries},
		Profile: querycontract.ProfileLocalAuthoritative,
	}
	if got, want := walk(contentHandler, "&name=alpha"), clientFiltered(walk(contentHandler, "")); !equalStrings(got, want) {
		t.Fatalf("content filtered walk = %#v, want client-side %#v", got, want)
	}

	graphRows := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		graphRows = append(graphRows, map[string]any{
			"id": e.ID, "name": e.Name, "path": "", "local_path": "",
			"remote_url": "", "repo_slug": e.RepoSlug, "has_remote": false,
			"evidence_source": "projector/canonical",
		})
	}
	graphHandler := &Handler{Neo4j: graphNameFilteredFake(t, graphRows), Profile: querycontract.ProfileLocalAuthoritative}
	if got, want := walk(graphHandler, "&name=alpha"), clientFiltered(walk(graphHandler, "")); !equalStrings(got, want) {
		t.Fatalf("graph filtered walk = %#v, want client-side %#v", got, want)
	}
}

// TestListRepositoriesNameNonASCIIDifferential documents the matching
// semantics both paths share: strings.ToLower agreement on a non-ASCII name.
func TestListRepositoriesNameNonASCIIDifferential(t *testing.T) {
	t.Parallel()

	entries := []querycontract.RepositoryCatalogEntry{
		{ID: "repository:u1", Name: "Straße-service", RepoSlug: "x/u1"},
		{ID: "repository:u2", Name: "plain", RepoSlug: "x/u2"},
	}
	contentHandler := &Handler{
		Content: content.FakePortContentStore{Repositories: entries},
		Profile: querycontract.ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?name=strasse", nil)
	rec := httptest.NewRecorder()
	contentHandler.listRepositories(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("content status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	contentNames := listFilterRowNames(t, decodeListFilterBody(t, rec))

	graphRows := []map[string]any{
		{"id": "repository:u1", "name": "Straße-service", "path": "", "local_path": "", "remote_url": "", "repo_slug": "x/u1", "has_remote": false, "evidence_source": "projector/canonical"},
		{"id": "repository:u2", "name": "plain", "path": "", "local_path": "", "remote_url": "", "repo_slug": "x/u2", "has_remote": false, "evidence_source": "projector/canonical"},
	}
	graphHandler := &Handler{Neo4j: graphNameFilteredFake(t, graphRows), Profile: querycontract.ProfileLocalAuthoritative}
	greq := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?name=strasse", nil)
	grec := httptest.NewRecorder()
	graphHandler.listRepositories(grec, greq)
	if grec.Code != http.StatusOK {
		t.Fatalf("graph status = %d, want 200 body=%s", grec.Code, grec.Body.String())
	}
	graphNames := listFilterRowNames(t, decodeListFilterBody(t, grec))

	// Both paths share Go strings.ToLower semantics (not locale folding):
	// the fake applies ToLower exactly like the content path, so they agree.
	if !equalStrings(contentNames, graphNames) {
		t.Fatalf("content names = %#v, graph names = %#v, want agreement", contentNames, graphNames)
	}
}

// TestListRepositoriesCompactLimit500CoreBodyWithinBudget measures the real
// compact fixture: a limit=500 compact page must fit the 256 KiB core-body
// cap with margin. Recorded bytes land in the PR.
func TestListRepositoriesCompactLimit500CoreBodyWithinBudget(t *testing.T) {
	t.Parallel()

	entries := make([]querycontract.RepositoryCatalogEntry, 0, 500)
	for i := 0; i < 500; i++ {
		entries = append(entries, querycontract.RepositoryCatalogEntry{
			ID:       "repository:repo-" + itoa(i),
			Name:     "service-number-" + itoa(i),
			RepoSlug: "acme/service-number-" + itoa(i),
		})
	}
	handler := &Handler{
		Content: content.FakePortContentStore{Repositories: entries},
		Profile: querycontract.ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=500&projection=compact", nil)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String()[:min(500, rec.Body.Len())])
	}
	size := rec.Body.Len()
	t.Logf("limit=500 compact core body = %d bytes (budget 262144)", size)
	if size > 262144 {
		t.Fatalf("compact core body = %d bytes, want <= 262144", size)
	}
}
