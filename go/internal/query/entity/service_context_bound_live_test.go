// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package entity

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// crowdedUngrantedWorkloads is more than the service-context candidate bound
// (workloadLookupCandidateBound = 50), so an unscoped candidate read overflows.
const crowdedUngrantedWorkloads = 55

// TestLiveScopedServiceContextBoundCountsGrantedRowsOnly is the #6801 review
// F-R5-1 proof. With 55 ungranted workloads and one granted workload sharing a
// name, the ungranted ones sort first by id. If the candidate bound runs before
// the grant, a granted caller gets 409 and a caller with no grant learns 51+
// same-name workloads exist. The scoped candidate read must bound granted rows
// only: the granted caller gets its workload, the no-grant caller gets 404.
func TestLiveScopedServiceContextBoundCountsGrantedRowsOnly(t *testing.T) {
	reader, baseCtx := scopedGrantLiveFixture(t)
	const serviceName = "scoped-grant-6786-crowded"
	for i := 0; i < crowdedUngrantedWorkloads; i++ {
		reader.write(baseCtx, t, fmt.Sprintf(
			`CREATE (:Workload {id: 'scoped-grant-6786:crowded-%02d', name: '%s', repo_id: 'scoped-grant-6786:repo-b'})`,
			i, serviceName))
	}
	reader.write(baseCtx, t, `CREATE (:Workload {id: 'scoped-grant-6786:crowded-zz', name: '`+serviceName+`', repo_id: 'scoped-grant-6786:repo-a'})`)
	reader.write(baseCtx, t, scopedGrantLiveEdge("Repository", "scoped-grant-6786:repo-a", "DEFINES", "Workload", "scoped-grant-6786:crowded-zz"))

	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	get := func(t *testing.T, allowed ...string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v0/services/"+serviceName+"/context", nil)
		req = req.WithContext(scopedRequestContext(baseCtx, allowed...))
		req.SetPathValue("service_name", serviceName)
		rec := httptest.NewRecorder()
		handler.GetServiceContext(rec, req)
		return rec
	}

	t.Run("granted_caller_gets_its_workload", func(t *testing.T) {
		rec := get(t, "scoped-grant-6786:repo-a")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"id":"scoped-grant-6786:crowded-zz"`) {
			t.Fatalf("body = %s, want workload crowded-zz", rec.Body.String())
		}
	})
	t.Run("no_grant_caller_gets_not_found", func(t *testing.T) {
		rec := get(t, "scoped-grant-6786:repo-z")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (no existence signal); body = %s", rec.Code, rec.Body.String())
		}
	})
}
