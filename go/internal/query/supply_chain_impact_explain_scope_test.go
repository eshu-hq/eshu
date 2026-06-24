// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestSupplyChainExplainImpactAcceptsAdvisoryOperationalAnchors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		target     string
		wantFilter SupplyChainImpactExplanationFilter
	}{
		{
			name:   "workload",
			target: "/api/v0/supply-chain/impact/explain?advisory_id=GHSA-test&workload_id=workload:api",
			wantFilter: SupplyChainImpactExplanationFilter{
				AdvisoryID: "GHSA-test",
				WorkloadID: "workload:api",
			},
		},
		{
			name:   "service",
			target: "/api/v0/supply-chain/impact/explain?advisory_id=GHSA-test&service_id=service:payments",
			wantFilter: SupplyChainImpactExplanationFilter{
				AdvisoryID: "GHSA-test",
				ServiceID:  "service:payments",
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := &recordingSupplyChainImpactExplanationStore{
				err: ErrSupplyChainImpactExplanationNotFound,
			}
			handler := &SupplyChainHandler{ImpactExplanations: store}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			if got := store.lastFilter; !reflect.DeepEqual(got, tc.wantFilter) {
				t.Fatalf("filter = %#v, want %#v", got, tc.wantFilter)
			}
		})
	}
}
