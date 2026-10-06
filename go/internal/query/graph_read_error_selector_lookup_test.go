// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// selectorLookupSelector is a non-canonical repository_id, so resolution
// reaches the catalog and graph reads instead of short-circuiting on the
// canonical-id shape.
const selectorLookupSelector = "payments-svc"

// selectorLookupContent fails the selector's catalog read with matchErr, or
// answers from the embedded fixture store when matchErr is nil.
type selectorLookupContent struct {
	fakePortContentStore
	matchErr error
}

func (s selectorLookupContent) MatchRepositories(ctx context.Context, selector string) ([]RepositoryCatalogEntry, error) {
	if s.matchErr != nil {
		return nil, s.matchErr
	}
	return s.fakePortContentStore.MatchRepositories(ctx, selector)
}

// selectorLookupRoute is one handler family behind
// selector.ResolveForRequestWithAccess. graphBacked reports whether the route
// passes a graph reader to the selector; the CI/CD and service-catalog read
// models pass nil, so only their catalog read can fail.
type selectorLookupRoute struct {
	name        string
	target      string
	graphBacked bool
	mount       func(mux *http.ServeMux, graph GraphQuery, content ContentStore)
}

func selectorLookupRoutes() []selectorLookupRoute {
	return []selectorLookupRoute{
		{
			name:        "package registry dependency chains",
			target:      "/api/v0/package-registry/dependency-chains?limit=10&repository_id=" + selectorLookupSelector,
			graphBacked: true,
			mount: func(mux *http.ServeMux, graph GraphQuery, content ContentStore) {
				(&PackageRegistryHandler{Neo4j: graph, Content: content}).Mount(mux)
			},
		},
		{
			name:        "supply chain advisory evidence",
			target:      "/api/v0/supply-chain/advisories/evidence?limit=10&repository_id=" + selectorLookupSelector,
			graphBacked: true,
			mount: func(mux *http.ServeMux, graph GraphQuery, content ContentStore) {
				(&SupplyChainHandler{Neo4j: graph, Content: content}).Mount(mux)
			},
		},
		{
			name:   "service catalog correlations",
			target: "/api/v0/service-catalog/correlations?limit=10&repository_id=" + selectorLookupSelector,
			mount: func(mux *http.ServeMux, _ GraphQuery, content ContentStore) {
				(&ServiceCatalogHandler{Content: content, Correlations: &testutil.RecordingServiceCatalogCorrelationStore{}}).Mount(mux)
			},
		},
		{
			name:   "ci/cd run correlations",
			target: "/api/v0/ci-cd/run-correlations?limit=10&repository_id=" + selectorLookupSelector,
			mount: func(mux *http.ServeMux, _ GraphQuery, content ContentStore) {
				(&CICDHandler{Content: content, Correlations: &recordingCICDRunCorrelationStore{}}).Mount(mux)
			},
		},
	}
}

// TestSelectorLookupFailureAnswers500 is the #7626 route-level regression. A
// catalog or graph failure during repository-selector resolution that is not a
// reader fence or graph-availability verdict is a server fault: it must answer
// 500, not the 400 that blamed the client. Fence verdicts keep 503/504, an
// unmatched selector keeps 404, and an ambiguous one keeps 400. One handler per
// family behind selector.ResolveForRequestWithAccess is driven through its own
// mux.
func TestSelectorLookupFailureAnswers500(t *testing.T) {
	t.Parallel()

	failingGraph := func(err error) GraphQuery {
		return fakeGraphReader{
			run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, err
			},
			runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return nil, err
			},
		}
	}
	ambiguous := fakePortContentStore{repositories: []RepositoryCatalogEntry{
		{ID: "repository:r_a", Name: selectorLookupSelector},
		{ID: "repository:r_b", Name: selectorLookupSelector},
	}}
	cases := []struct {
		name       string
		graphOnly  bool
		graph      GraphQuery
		content    ContentStore
		wantStatus int
	}{
		{name: "store bare reader unavailable", content: selectorLookupContent{matchErr: fmt.Errorf("private read store: %w", db.ErrReaderUnavailable)}, wantStatus: http.StatusInternalServerError},
		{name: "store plain sql error", content: selectorLookupContent{matchErr: errors.New("private pq: connection refused")}, wantStatus: http.StatusInternalServerError},
		{name: "graph plain driver error", graphOnly: true, graph: failingGraph(errors.New("private neo4j: connection reset")), wantStatus: http.StatusInternalServerError},
		{name: "store reader stale", content: selectorLookupContent{matchErr: fmt.Errorf("private read store: %w", db.ErrReaderStale)}, wantStatus: http.StatusServiceUnavailable},
		{name: "graph unavailable", graphOnly: true, graph: failingGraph(fmt.Errorf("private: %w", ErrGraphUnavailable)), wantStatus: http.StatusServiceUnavailable},
		{name: "graph deadline", graphOnly: true, graph: failingGraph(fmt.Errorf("private: %w", ErrGraphReadDeadline)), wantStatus: http.StatusGatewayTimeout},
		{name: "not found", content: selectorLookupContent{}, wantStatus: http.StatusNotFound},
		{name: "ambiguous", content: ambiguous, wantStatus: http.StatusBadRequest},
	}
	for _, route := range selectorLookupRoutes() {
		for _, tc := range cases {
			if tc.graphOnly && !route.graphBacked {
				continue
			}
			t.Run(route.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				mux := http.NewServeMux()
				route.mount(mux, tc.graph, tc.content)
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, route.target, nil))

				if rec.Code != tc.wantStatus {
					t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantStatus, rec.Body.String())
				}
				body := rec.Body.String()
				if strings.Contains(body, "private") {
					t.Fatalf("body leaked the backend error text: %s", body)
				}
				if tc.wantStatus == http.StatusInternalServerError && strings.Contains(body, selectorLookupSelector) {
					t.Fatalf("500 body carries the raw selector: %s", body)
				}
			})
		}
	}
}
