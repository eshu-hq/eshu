// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGetEcosystemOverviewCountsEachLabelIndependently pins the regression where
// the overview used a single chained-aggregation statement:
//
//	MATCH (r:Repository) WITH count(r) ...
//	MATCH (w:Workload)   WITH ...          // empty label collapses the whole row
//
// On the NornicDB backend that chained form does not work: an empty intermediate
// label collapsed the result and the handler reported repo_count: 0, hiding real
// repositories, and the chained form otherwise returned all-null rows. Each
// label must be counted with its own single-label count query so repo_count
// survives regardless of whether workloads/platforms are materialized yet.
func TestGetEcosystemOverviewCountsEachLabelIndependently(t *testing.T) {
	t.Parallel()

	var captured []string
	handler := &InfraHandler{
		Profile: ProfileProduction,
		Neo4j: fakeRepoGraphReader{
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				captured = append(captured, cypher)
				switch {
				case strings.Contains(cypher, "(r:Repository)"):
					return map[string]any{"c": int64(33)}, nil
				case strings.Contains(cypher, "(w:Workload)"):
					return map[string]any{"c": int64(21)}, nil
				case strings.Contains(cypher, "(p:Platform)"):
					return map[string]any{"c": int64(7)}, nil
				case strings.Contains(cypher, "WorkloadInstance"):
					return map[string]any{"c": int64(0)}, nil
				}
				return nil, nil
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/ecosystem/overview", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	// No single statement may chain two label matches together; that is the
	// pattern that collapses repo_count on NornicDB.
	for _, cypher := range captured {
		if strings.Contains(cypher, "(r:Repository)") && strings.Contains(cypher, "(w:Workload)") {
			t.Fatalf("ecosystem overview chained repository+workload in one statement; counts must be independent:\n%s", cypher)
		}
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	for field, want := range map[string]float64{
		"repo_count":     33,
		"workload_count": 21,
		"platform_count": 7,
		"instance_count": 0,
	} {
		if got := resp[field]; got != want {
			t.Fatalf("%s = %#v, want %#v (each label counted independently)", field, got, want)
		}
	}
}

// ecosystemOverviewScopedGraphReader simulates a real repo-anchored graph
// backend for the #5167 scoped tests below: it returns tenant-a's counts only
// when the dispatched Cypher carries a WHERE clause and the bound
// allowed_repository_ids/allowed_scope_ids params include "repo-tenant-a", and
// zero otherwise (mirroring what a real WHERE-filtered graph read would
// return for an unrelated or missing grant). It also records every dispatched
// cypher/params pair so tests can assert on the query shape directly.
type ecosystemOverviewScopedGraphReader struct {
	calls []struct {
		cypher string
		params map[string]any
	}
}

func (f *ecosystemOverviewScopedGraphReader) Run(
	context.Context, string, map[string]any,
) ([]map[string]any, error) {
	return nil, nil
}

func (f *ecosystemOverviewScopedGraphReader) RunSingle(
	_ context.Context, cypher string, params map[string]any,
) (map[string]any, error) {
	f.calls = append(f.calls, struct {
		cypher string
		params map[string]any
	}{cypher, params})

	grantedTenantA := false
	if params != nil {
		for _, key := range []string{"allowed_repository_ids", "allowed_scope_ids"} {
			ids, _ := params[key].([]string)
			for _, id := range ids {
				if id == "repo-tenant-a" {
					grantedTenantA = true
				}
			}
		}
	}
	if !strings.Contains(cypher, "WHERE") {
		// Unscoped/admin path: pretend the whole corpus has these counts.
		switch {
		case strings.Contains(cypher, "(r:Repository)"):
			return map[string]any{"c": int64(50)}, nil
		case strings.Contains(cypher, "(w:Workload)"):
			return map[string]any{"c": int64(40)}, nil
		}
		return map[string]any{"c": int64(0)}, nil
	}
	if !grantedTenantA {
		return map[string]any{"c": int64(0)}, nil
	}
	// Scoped + granted: pretend tenant-a owns exactly one repo/workload.
	switch {
	case strings.Contains(cypher, "(r:Repository)"):
		return map[string]any{"c": int64(1)}, nil
	case strings.Contains(cypher, "(w:Workload)"):
		return map[string]any{"c": int64(1)}, nil
	}
	return map[string]any{"c": int64(0)}, nil
}

// TestGetEcosystemOverviewScopedEmptyGrantReturnsAllZeroCountsWithoutGraphRead
// is the #5167 counterpart to the other Group B empty-grant precedents: a
// scoped caller with no granted repository or ingestion scope must see
// all-zero counts without a graph read.
func TestGetEcosystemOverviewScopedEmptyGrantReturnsAllZeroCountsWithoutGraphRead(t *testing.T) {
	t.Parallel()

	reader := &ecosystemOverviewScopedGraphReader{}
	handler := &InfraHandler{Profile: ProfileProduction, Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/ecosystem/overview", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{Mode: AuthModeScoped, TenantID: "tenant-a"}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if len(reader.calls) != 0 {
		t.Fatalf("graph received %d calls, want 0 for an empty-grant scoped caller", len(reader.calls))
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	for _, field := range []string{"repo_count", "workload_count", "platform_count", "instance_count"} {
		if got, want := resp[field], float64(0); got != want {
			t.Fatalf("%s = %#v, want %#v", field, got, want)
		}
	}
}

// TestGetEcosystemOverviewScopedGrantBindsWhereClauseAndReturnsRealRowData
// proves the #5167 fix drives the real repo-anchored WHERE clause with the
// caller's granted ids bound as params, and that the response reflects the
// simulated real-backend counts (1, not the unscoped 50/40), not just a 200
// shape. Deleting runEcosystemOverviewCounts's access.scoped() branch would
// make this test's counts regress to 50/40/0/0 and fail.
func TestGetEcosystemOverviewScopedGrantBindsWhereClauseAndReturnsRealRowData(t *testing.T) {
	t.Parallel()

	reader := &ecosystemOverviewScopedGraphReader{}
	handler := &InfraHandler{Profile: ProfileProduction, Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/ecosystem/overview", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		AllowedRepositoryIDs: []string{"repo-tenant-a"},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := len(reader.calls), len(ecosystemOverviewCounts); got != want {
		t.Fatalf("graph received %d calls, want %d (one per label)", got, want)
	}
	for _, call := range reader.calls {
		if !strings.Contains(call.cypher, "WHERE") {
			t.Fatalf("scoped call missing WHERE clause: %s", call.cypher)
		}
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp["repo_count"], float64(1); got != want {
		t.Fatalf("repo_count = %#v, want %#v (real grant-bound row data, not the unscoped 50)", got, want)
	}
	if got, want := resp["workload_count"], float64(1); got != want {
		t.Fatalf("workload_count = %#v, want %#v (real grant-bound row data, not the unscoped 40)", got, want)
	}
}

// TestGetEcosystemOverviewUnscopedQueryStaysUnfiltered is the no-regression
// counterpart: a shared/admin caller (no AuthContext) must still issue the
// byte-identical unscoped queries with no WHERE clause.
func TestGetEcosystemOverviewUnscopedQueryStaysUnfiltered(t *testing.T) {
	t.Parallel()

	reader := &ecosystemOverviewScopedGraphReader{}
	handler := &InfraHandler{Profile: ProfileProduction, Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/ecosystem/overview", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	for _, call := range reader.calls {
		if strings.Contains(call.cypher, "WHERE") {
			t.Fatalf("unscoped/admin call must stay unfiltered, got: %s", call.cypher)
		}
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp["repo_count"], float64(50); got != want {
		t.Fatalf("repo_count = %#v, want %#v", got, want)
	}
}

// collisionWorkloadGraphReader simulates a two-tenant name-collision fixture
// for the #5167 F-6 W6 review P1 ("keep colliding workload instances
// tenant-scoped"): a single Workload node W is DEFINES-reachable from BOTH
// repo-tenant-a and repo-tenant-b (the name-collision materializes one shared
// node), tenant-a's WorkloadInstance i-a{repo_id:"repo-tenant-a"} and
// tenant-b's WorkloadInstance i-b{repo_id:"repo-tenant-b"} both realize W via
// INSTANCE_OF, and each instance RUNS_ON its own Platform (p-a, p-b).
//
// It answers instance_count/platform_count queries against two possible
// Cypher shapes:
//   - The vulnerable pre-fix shape reaches instances/platforms purely through
//     repo->DEFINES->Workload<-INSTANCE_OF reachability, with no binding on the
//     instance's own repo_id -- against the fixture above that shape cannot
//     distinguish i-a from i-b once ANY grant defines W, so it returns BOTH
//     (count 2), reproducing the leak.
//   - The fixed shape binds directly on the WorkloadInstance alias's own
//     repo_id (infraResourceScopeCoreDisjuncts/infraResourceScopePredicate on
//     alias "i"), which correctly excludes the other tenant's instance
//     (count 1).
type collisionWorkloadGraphReader struct {
	calls []struct {
		cypher string
		params map[string]any
	}
}

func (f *collisionWorkloadGraphReader) Run(
	context.Context, string, map[string]any,
) ([]map[string]any, error) {
	return nil, nil
}

func (f *collisionWorkloadGraphReader) RunSingle(
	_ context.Context, cypher string, params map[string]any,
) (map[string]any, error) {
	f.calls = append(f.calls, struct {
		cypher string
		params map[string]any
	}{cypher, params})

	if !strings.Contains(cypher, "WorkloadInstance") {
		return map[string]any{"c": int64(0)}, nil
	}
	grantedTenantA := false
	if params != nil {
		for _, key := range []string{"allowed_repository_ids", "allowed_scope_ids"} {
			ids, _ := params[key].([]string)
			for _, id := range ids {
				if id == "repo-tenant-a" {
					grantedTenantA = true
				}
			}
		}
	}
	if !grantedTenantA {
		return map[string]any{"c": int64(0)}, nil
	}
	// Durable per-instance binding: only i-a (repo_id == repo-tenant-a)
	// qualifies for a tenant-a grant, regardless of the shared Workload W.
	if strings.Contains(cypher, "i.repo_id IN $allowed_repository_ids") {
		return map[string]any{"c": int64(1)}, nil
	}
	// Vulnerable reachability-only shape: DEFINES->Workload<-INSTANCE_OF
	// admits W (and everything realizing it) once ANY grant defines W, so
	// both i-a and i-b (and their platforms) leak through.
	if strings.Contains(cypher, "DEFINES]->(:Workload)<-[:INSTANCE_OF]-") {
		return map[string]any{"c": int64(2)}, nil
	}
	return map[string]any{"c": int64(0)}, nil
}

// TestGetEcosystemOverviewScopedGrantExcludesCollidedWorkloadInstancesAndPlatforms
// pins the #5167 F-6 W6 review P1 fix: instance_count and platform_count must
// bind the grant check directly on the WorkloadInstance's own durable
// repo_id, not on reachability through a (possibly name-collision) Workload,
// so a tenant-a grant never counts tenant-b's instances/platforms just
// because their Workload names collided. Before the fix this test fails with
// instance_count/platform_count == 2 (both tenants' rows); after the fix it
// must be 1 (tenant-a's own instance/platform only).
func TestGetEcosystemOverviewScopedGrantExcludesCollidedWorkloadInstancesAndPlatforms(t *testing.T) {
	t.Parallel()

	reader := &collisionWorkloadGraphReader{}
	handler := &InfraHandler{Profile: ProfileProduction, Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/ecosystem/overview", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		AllowedRepositoryIDs: []string{"repo-tenant-a"},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	for _, call := range reader.calls {
		if strings.Contains(call.cypher, "WorkloadInstance") &&
			strings.Contains(call.cypher, "DEFINES]->(:Workload)<-[:INSTANCE_OF]-") {
			t.Fatalf("instance/platform query must not admit through Workload DEFINES reachability, got: %s", call.cypher)
		}
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp["instance_count"], float64(1); got != want {
		t.Fatalf("instance_count = %#v, want %#v (tenant-a's own instance only, not tenant-b's collided instance)", got, want)
	}
	if got, want := resp["platform_count"], float64(1); got != want {
		t.Fatalf("platform_count = %#v, want %#v (tenant-a's own platform only, not tenant-b's collided platform)", got, want)
	}
}
