package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveRepositoryCatalogMatchesMatchesNameSlugAndPath(t *testing.T) {
	t.Parallel()

	entries := []RepositoryCatalogEntry{
		{
			ID:        "repository:r_payments",
			Name:      "payments",
			Path:      "/src/payments",
			LocalPath: "/src/payments",
			RepoSlug:  "acme/payments",
		},
	}

	for _, selector := range []string{"repository:r_payments", "payments", "/src/payments", "acme/payments"} {
		matches := resolveRepositoryCatalogMatches(entries, selector)
		if got, want := matches, []string{"repository:r_payments"}; len(got) != len(want) || got[0] != want[0] {
			t.Fatalf("resolveRepositoryCatalogMatches(%q) = %#v, want %#v", selector, got, want)
		}
	}
}

func TestResolveRepositorySelectorRejectsAmbiguousMatches(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{
				{ID: "repository:r_one", Name: "payments"},
				{ID: "repository:r_two", Name: "payments"},
			},
		},
	}

	_, err := handler.resolveRepositorySelector(context.Background(), "payments")
	if err == nil {
		t.Fatal("resolveRepositorySelector() error = nil, want non-nil")
	}
	if got, want := err.Error(), `repository selector "payments" matched multiple repositories: repository:r_one, repository:r_two`; got != want {
		t.Fatalf("resolveRepositorySelector() error = %q, want %q", got, want)
	}
}

func TestHandleDeadCodeResolvesRepositorySelectorAlias(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["repo_id"], "repository:r_payments"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				return nil, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			fakePortContentStore: fakePortContentStore{
				repositories: []RepositoryCatalogEntry{
					{ID: "repository:r_payments", Name: "payments", RepoSlug: "acme/payments", LocalPath: "/src/payments"},
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"payments"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["repo_id"], "repository:r_payments"; got != want {
		t.Fatalf("resp[repo_id] = %#v, want %#v", got, want)
	}
}

func TestHandleCallChainResolvesRepositorySelectorAlias(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "start.repo_id = $repo_id") || !strings.Contains(cypher, "end.repo_id = $repo_id") {
					t.Fatalf("cypher = %q, want repo scoping predicates", cypher)
				}
				if got, want := params["repo_id"], "repository:r_payments"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				return []map[string]any{{"chain": []any{}, "depth": int64(0)}}, nil
			},
		},
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{
				{ID: "repository:r_payments", Name: "payments", RepoSlug: "acme/payments", LocalPath: "/src/payments"},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/call-chain",
		bytes.NewBufferString(`{"start":"wrapper","end":"helper","repo_id":"payments","max_depth":4}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}
