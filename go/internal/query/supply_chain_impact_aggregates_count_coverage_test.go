// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCountImpactFindingsMapsGraphReadAvailabilityErrors proves that a
// bounded graph-read failure (unavailable or deadline) surfaced while
// resolving a non-canonical repository_id selector on countImpactFindings
// maps to the stable 503/504 envelope instead of a bare 500. The selector
// "my-repo-name" is intentionally not repo://-/repo--/repository:-prefixed
// (see looksCanonicalRepositoryID in repository_selector.go) so resolution
// falls through past the catalog (Content is nil) into the live graph read.
func TestCountImpactFindingsMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()

	for _, test := range graphReadSweepCases() {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			graph := fakeGraphReader{
				run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
					return nil, test.err
				},
				runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
					return nil, test.err
				},
			}
			handler := &SupplyChainHandler{
				Neo4j:            graph,
				Content:          nil,
				ImpactAggregates: &stubSupplyChainImpactAggregateStore{},
			}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(
				http.MethodGet,
				"/api/v0/supply-chain/impact/findings/count?repository_id=my-repo-name",
				nil,
			)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}
