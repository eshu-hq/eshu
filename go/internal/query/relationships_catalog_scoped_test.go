// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

// This file holds the #5167 F-6 W6 access-scoping tests for
// POST /api/v0/relationships/edges (relationships_catalog.go /
// relationships_catalog_cypher.go), split out of relationships_catalog_test.go
// to keep that file under the repository's 500-line file cap.

// scopedRelationshipEdgesGraphReader simulates a real endpoint-anchored graph
// backend for the #5167 scoped tests below: for a query whose Cypher carries
// a WHERE clause, it returns edges only when the bound
// allowed_repository_ids/allowed_scope_ids params include "repo-tenant-a"
// (mirroring what a real WHERE-filtered graph read would return for an
// unrelated or missing grant); for an unscoped query (no WHERE) it always
// returns the fixture edges. It records every dispatched cypher/params pair.
type scopedRelationshipEdgesGraphReader struct {
	edges []map[string]any
	calls []struct {
		cypher string
		params map[string]any
	}
}

func (f *scopedRelationshipEdgesGraphReader) RunSingle(
	context.Context, string, map[string]any,
) (map[string]any, error) {
	return nil, nil
}

func (f *scopedRelationshipEdgesGraphReader) Run(
	_ context.Context, cypher string, params map[string]any,
) ([]map[string]any, error) {
	f.calls = append(f.calls, struct {
		cypher string
		params map[string]any
	}{cypher, params})

	if !strings.Contains(cypher, "WHERE") {
		return f.edges, nil
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
		return nil, nil
	}
	return f.edges, nil
}

func relationshipEdgesTenantAFixture() []map[string]any {
	return []map[string]any{
		{"source_id": "fn-tenant-a-1", "source_name": "tenantAHandler", "target_id": "fn-tenant-a-2", "target_name": "tenantACallee", "evidence": "call site"},
	}
}

// TestGetRelationshipEdgesScopedEmptyGrantReturnsEmptyWithoutGraphRead is the
// #5167 counterpart to the other Group B empty-grant precedents: this is a
// whole-graph edge scan, so a scoped caller with no granted repository or
// ingestion scope must see an empty edge list without a graph read.
func TestGetRelationshipEdgesScopedEmptyGrantReturnsEmptyWithoutGraphRead(t *testing.T) {
	t.Parallel()

	reader := &scopedRelationshipEdgesGraphReader{edges: relationshipEdgesTenantAFixture()}
	handler := &InfraHandler{Neo4j: reader, Profile: ProfileProduction}

	body, _ := json.Marshal(map[string]any{"verb": "calls"})
	req := httptest.NewRequest(http.MethodPost, "/api/v0/relationships/edges", bytes.NewReader(body))
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{Mode: AuthModeScoped, TenantID: "tenant-a"}))
	rec := httptest.NewRecorder()
	handler.getRelationshipEdges(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if len(reader.calls) != 0 {
		t.Fatalf("graph received %d calls, want 0 for an empty-grant scoped caller", len(reader.calls))
	}
	var env ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := env.Data.(map[string]any)
	if got, want := len(data["edges"].([]any)), 0; got != want {
		t.Fatalf("edges = %d, want %d", got, want)
	}
}

// TestGetRelationshipEdgesScopedGrantBindsBothEndpointsAndReturnsRealRowData
// covers CALLS (targetAttributable == true): a scoped caller with a matching
// grant gets a WHERE clause binding BOTH s and t to the grant, and the real
// fixture edge data flows through -- not just a 200 shape. Deleting the
// target-endpoint binding for this verb (or the whole scope check) would let
// a request with a non-matching grant slip through; the paired
// out-of-grant/unattributable tests below catch that.
func TestGetRelationshipEdgesScopedGrantBindsBothEndpointsAndReturnsRealRowData(t *testing.T) {
	t.Parallel()

	reader := &scopedRelationshipEdgesGraphReader{edges: relationshipEdgesTenantAFixture()}
	handler := &InfraHandler{Neo4j: reader, Profile: ProfileProduction}

	body, _ := json.Marshal(map[string]any{"verb": "calls"})
	req := httptest.NewRequest(http.MethodPost, "/api/v0/relationships/edges", bytes.NewReader(body))
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		AllowedRepositoryIDs: []string{"repo-tenant-a"},
	}))
	rec := httptest.NewRecorder()
	handler.getRelationshipEdges(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got, want := len(reader.calls), 1; got != want {
		t.Fatalf("graph received %d calls, want %d", got, want)
	}
	cypher := reader.calls[0].cypher
	if !strings.Contains(cypher, "WHERE") {
		t.Fatalf("scoped CALLS query missing WHERE clause: %s", cypher)
	}
	if !strings.Contains(cypher, "s.repo_id IN $allowed_repository_ids") {
		t.Fatalf("scoped CALLS query must bind source endpoint s: %s", cypher)
	}
	if !strings.Contains(cypher, "t.repo_id IN $allowed_repository_ids") {
		t.Fatalf("scoped CALLS query must bind target endpoint t (CALLS.targetAttributable == true): %s", cypher)
	}
	var env ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := env.Data.(map[string]any)
	edges := data["edges"].([]any)
	if len(edges) != 1 {
		t.Fatalf("edges = %d, want 1; body = %s", len(edges), rec.Body.String())
	}
	first := edges[0].(map[string]any)
	if got, want := first["source_name"], "tenantAHandler"; got != want {
		t.Fatalf("source_name = %#v, want %#v (real row data)", got, want)
	}
}

// TestGetRelationshipEdgesScopedGrantUnattributableTargetBindsSourceOnly
// covers RUNS_ON (targetAttributable == false, Platform has no tenant
// attribution): the WHERE clause must bind s but must not attempt to bind t,
// so a scoped caller with a matching grant on the source still sees the edge
// to the shared/global target rather than an empty page.
func TestGetRelationshipEdgesScopedGrantUnattributableTargetBindsSourceOnly(t *testing.T) {
	t.Parallel()

	reader := &scopedRelationshipEdgesGraphReader{edges: []map[string]any{
		{"source_id": "instance-tenant-a-1", "source_name": "tenant-a-instance", "target_id": "platform-shared", "target_name": "prod-cluster", "evidence": ""},
	}}
	handler := &InfraHandler{Neo4j: reader, Profile: ProfileProduction}

	body, _ := json.Marshal(map[string]any{"verb": "runs_on"})
	req := httptest.NewRequest(http.MethodPost, "/api/v0/relationships/edges", bytes.NewReader(body))
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		AllowedRepositoryIDs: []string{"repo-tenant-a"},
	}))
	rec := httptest.NewRecorder()
	handler.getRelationshipEdges(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	cypher := reader.calls[0].cypher
	if !strings.Contains(cypher, "WHERE") {
		t.Fatalf("scoped RUNS_ON query missing WHERE clause: %s", cypher)
	}
	if !strings.Contains(cypher, "s.repo_id IN $allowed_repository_ids") {
		t.Fatalf("scoped RUNS_ON query must bind source endpoint s: %s", cypher)
	}
	if strings.Contains(cypher, "t.repo_id IN $allowed_repository_ids") {
		t.Fatalf("RUNS_ON target (Platform) is unattributable; query must not bind t: %s", cypher)
	}
	var env ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := env.Data.(map[string]any)
	edges := data["edges"].([]any)
	if len(edges) != 1 {
		t.Fatalf("edges = %d, want 1 (source-granted edge to a shared/global target must still be visible); body = %s", len(edges), rec.Body.String())
	}
	first := edges[0].(map[string]any)
	if got, want := first["target_name"], "prod-cluster"; got != want {
		t.Fatalf("target_name = %#v, want %#v (real row data)", got, want)
	}
}

// TestGetRelationshipEdgesUnscopedQueryStaysUnfiltered is the no-regression
// counterpart: a shared/admin caller (no AuthContext) must still issue the
// byte-identical unscoped query with no WHERE clause.
func TestGetRelationshipEdgesUnscopedQueryStaysUnfiltered(t *testing.T) {
	t.Parallel()

	reader := &scopedRelationshipEdgesGraphReader{edges: relationshipEdgesTenantAFixture()}
	handler := &InfraHandler{Neo4j: reader, Profile: ProfileProduction}

	body, _ := json.Marshal(map[string]any{"verb": "calls"})
	req := httptest.NewRequest(http.MethodPost, "/api/v0/relationships/edges", bytes.NewReader(body))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.getRelationshipEdges(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(reader.calls[0].cypher, "WHERE") {
		t.Fatalf("unscoped/admin query must stay unfiltered, got:\n%s", reader.calls[0].cypher)
	}
}

// collidedWorkloadDependsOnGraphReader simulates the #5167 F-6 W6 review P1
// ("do not authorize relationship endpoints through shared workloads")
// fixture: a Workload W materializes repo_id "repo-tenant-b" (tenant-b's
// ingestion won the name-collision merge), but tenant-a's repository ALSO
// DEFINES a same-named Workload -- the collapsed graph identity. W has one
// DEPENDS_ON edge to X that is tenant-b's own private dependency fact.
//
// A tenant-a-scoped caller must never see this edge: W's own durable repo_id
// is tenant-b's, not tenant-a's, and tenant-a has no USES/DEPLOYMENT_SOURCE
// path to it either. The pre-fix predicate admitted W as a DEPENDS_ON source
// via its DEFINES disjunct (tenant-a DEFINES the shared W too), leaking
// tenant-b's edge; the fix drops that disjunct for relationship endpoints.
type collidedWorkloadDependsOnGraphReader struct {
	calls []struct {
		cypher string
		params map[string]any
	}
}

func (f *collidedWorkloadDependsOnGraphReader) RunSingle(
	context.Context, string, map[string]any,
) (map[string]any, error) {
	return nil, nil
}

func (f *collidedWorkloadDependsOnGraphReader) Run(
	_ context.Context, cypher string, params map[string]any,
) ([]map[string]any, error) {
	f.calls = append(f.calls, struct {
		cypher string
		params map[string]any
	}{cypher, params})

	if !strings.Contains(cypher, "WHERE") {
		return nil, nil
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
		return nil, nil
	}
	// Fixed predicate: no DEFINES disjunct, so the shared-identity W (durable
	// repo_id "repo-tenant-b") is correctly excluded from a tenant-a grant.
	if !strings.Contains(cypher, "DEFINES") {
		return nil, nil
	}
	// Vulnerable predicate: DEFINES admits W because tenant-a also defines the
	// collided name, leaking tenant-b's private DEPENDS_ON edge.
	return []map[string]any{
		{"source_id": "workload-collided", "source_name": "shared-workload", "target_id": "x-tenant-b-secret", "target_name": "tenantBPrivateDependency", "evidence": ""},
	}, nil
}

// TestGetRelationshipEdgesScopedGrantExcludesSharedWorkloadCollisionLeak pins
// the #5167 F-6 W6 review P1 fix: relationshipEndpointScopePredicate must not
// admit a bare Workload endpoint via DEFINES reachability, so a tenant-a grant
// never returns a DEPENDS_ON edge whose source Workload durably belongs to
// tenant-b just because the two tenants' workload names collided. Before the
// fix this test returns tenant-b's edge (leak); after the fix it returns zero
// edges (fail-closed, no durable provenance for tenant-a).
func TestGetRelationshipEdgesScopedGrantExcludesSharedWorkloadCollisionLeak(t *testing.T) {
	t.Parallel()

	reader := &collidedWorkloadDependsOnGraphReader{}
	handler := &InfraHandler{Neo4j: reader, Profile: ProfileProduction}

	body, _ := json.Marshal(map[string]any{"verb": "depends_on"})
	req := httptest.NewRequest(http.MethodPost, "/api/v0/relationships/edges", bytes.NewReader(body))
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		AllowedRepositoryIDs: []string{"repo-tenant-a"},
	}))
	rec := httptest.NewRecorder()
	handler.getRelationshipEdges(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if len(reader.calls) != 1 {
		t.Fatalf("graph received %d calls, want 1", len(reader.calls))
	}
	if strings.Contains(reader.calls[0].cypher, "DEFINES") {
		t.Fatalf("DEPENDS_ON scope predicate must not admit a bare Workload endpoint via DEFINES reachability, got:\n%s", reader.calls[0].cypher)
	}
	var env ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := env.Data.(map[string]any)
	if got, want := len(data["edges"].([]any)), 0; got != want {
		t.Fatalf("edges = %d, want %d (tenant-b's collided-workload edge must not leak to tenant-a)", got, want)
	}
}
