// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil/graph"
)

// TestListRepositoriesPageCypherHasNoDependencyMarkerExpression proves the
// repository list page query no longer renders a per-row
// EXISTS-as-RETURN-expression for is_dependency (issue #6786 defect 1): on
// NornicDB v1.3.3 an EXISTS used as a RETURN expression is always false, and
// the scoped caller's grant predicate used to be spliced directly after the
// MATCH pattern with no WHERE keyword, which Neo4j rejects as a SyntaxError.
// is_dependency is now derived in Go from the separate, already-scoped
// dependency-edge pre-pass (loadRepositoryDependencyEdges), so the page
// query's RETURN carries no EXISTS, DEPENDS_ON, or is_dependency text at
// all.
func TestListRepositoriesPageCypherHasNoDependencyMarkerExpression(t *testing.T) {
	t.Parallel()

	var capturedPageCypher string
	reader := graph.FakeRepoGraphReader{
		RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return map[string]any{"total": int64(1)}, nil
		},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "(s:Repository)-[:DEPENDS_ON]->(t:Repository)"):
				return []map[string]any{}, nil
			case strings.Contains(cypher, "MATCH (r:Repository)"):
				capturedPageCypher = cypher
				return []map[string]any{{"id": "repository:lib", "name": "lib"}}, nil
			default:
				return []map[string]any{{"total": 1}}, nil
			}
		},
	}

	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=10", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	if capturedPageCypher == "" {
		t.Fatal("page cypher was not captured; check that the fake matched the MATCH (r:Repository) query")
	}
	for _, forbidden := range []string{"EXISTS", "DEPENDS_ON", "is_dependency"} {
		if strings.Contains(capturedPageCypher, forbidden) {
			t.Errorf("page cypher still contains %q, want it removed in favor of the Go-derived marker:\n%s", forbidden, capturedPageCypher)
		}
	}
}

// TestListRepositoriesMarksDependencyFromInboundEdge proves the repositories
// list response derives is_dependency from the bounded dependency-edge
// pre-pass rather than a per-row graph expression: a repository that is the
// target of an admitted inbound DEPENDS_ON edge is marked true, one that
// only has an outbound edge (or none) is marked false.
func TestListRepositoriesMarksDependencyFromInboundEdge(t *testing.T) {
	t.Parallel()

	var capturedEdgeCypher string
	reader := graph.FakeRepoGraphReader{
		RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return map[string]any{"total": int64(2)}, nil
		},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "(s:Repository)-[:DEPENDS_ON]->(t:Repository)"):
				capturedEdgeCypher = cypher
				return dependencyEdgeRowsForRead(cypher, []map[string]any{
					{"source_id": "repository:app", "target_id": "repository:lib"},
				}), nil
			case strings.Contains(cypher, "MATCH (r:Repository)"):
				return []map[string]any{
					{"id": "repository:lib", "name": "lib"},
					{"id": "repository:app", "name": "app"},
				}, nil
			default:
				return nil, nil
			}
		},
	}

	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=10", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	if capturedEdgeCypher == "" {
		t.Fatal("dependency-edge pre-pass cypher was not issued")
	}

	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map", envelope.Data)
	}
	repositories, ok := data["repositories"].([]any)
	if !ok || len(repositories) != 2 {
		t.Fatalf("repositories = %#v, want two rows", data["repositories"])
	}

	markers := map[string]bool{}
	for _, raw := range repositories {
		repo, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("repository row type = %T, want map", raw)
		}
		markers[querycontract.StringVal(repo, "id")] = querycontract.BoolVal(repo, "is_dependency")
	}
	if !markers["repository:lib"] {
		t.Fatalf("repository:lib is_dependency = false, want true (inbound DEPENDS_ON target)")
	}
	if markers["repository:app"] {
		t.Fatalf("repository:app is_dependency = true, want false (outbound edge only, not a target)")
	}
}

// TestListRepositoriesScopedDependencyMarkerUsesScopedEdgePrePass proves
// that a scoped caller's is_dependency marker comes from the same
// dependency-edge pre-pass query dependency-cluster grouping already scopes
// to both endpoints ($allowed_repository_ids / $allowed_scope_ids on both s
// and t), so a scoped caller cannot learn dependency-marker truth from an
// edge whose depending repository is outside their grant.
func TestListRepositoriesScopedDependencyMarkerUsesScopedEdgePrePass(t *testing.T) {
	t.Parallel()

	var capturedEdgeCypher string
	reader := graph.FakeRepoGraphReader{
		RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return map[string]any{"total": int64(1)}, nil
		},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "(s:Repository)-[:DEPENDS_ON]->(t:Repository)"):
				capturedEdgeCypher = cypher
				// The fake graph honors no predicate itself; it returns the
				// edge unconditionally so the assertion below is purely
				// about the rendered Cypher text, matching the existing
				// dependency_cluster_test.go pattern for the same query.
				return dependencyEdgeRowsForRead(cypher, []map[string]any{
					{"source_id": "repository:outside", "target_id": "repository:lib"},
				}), nil
			case strings.Contains(cypher, "MATCH (r:Repository)"):
				return []map[string]any{{"id": "repository:lib", "name": "lib"}}, nil
			default:
				return []map[string]any{{"total": 1}}, nil
			}
		},
	}

	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=10", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		SubjectClass:         "team",
		SubjectIDHash:        "sha256:team-a",
		PolicyRevisionHash:   "sha256:policy",
		AllowedRepositoryIDs: []string{"repository:lib"},
	}))
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	if capturedEdgeCypher == "" {
		t.Fatal("dependency-edge pre-pass cypher was not issued for the scoped caller")
	}
	if !strings.Contains(capturedEdgeCypher, "s.id IN $allowed_repository_ids") ||
		!strings.Contains(capturedEdgeCypher, "t.id IN $allowed_repository_ids") {
		t.Fatalf("scoped edge query does not scope both endpoints to the grant:\n%s", capturedEdgeCypher)
	}
}

// TestListRepositoriesDisclosesDegradedDependencyEvidenceOnEdgeQueryError
// proves the response still succeeds (the primary repository page is
// healthy) but discloses that is_dependency may be incomplete when the
// dependency-edge pre-pass errors, rather than silently reporting every
// repository as is_dependency=false with no indication anything went
// wrong. Disclosure MUST be via partial_reasons only: truncated (and the
// result_limits.truncated / repository_inventory_truncated fields it
// drives) means "more repositories exist beyond this returned page" per
// the OpenAPI/HTTP-API-reference contract, an unrelated claim the
// dependency-edge pre-pass has no bearing on -- a complete 1-of-1 page
// must still report truncated=false even when this auxiliary read is
// degraded (#6786 review F1).
func TestListRepositoriesDisclosesDegradedDependencyEvidenceOnEdgeQueryError(t *testing.T) {
	t.Parallel()

	reader := graph.FakeRepoGraphReader{
		RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return map[string]any{"total": int64(1)}, nil
		},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "(s:Repository)-[:DEPENDS_ON]->(t:Repository)"):
				return nil, errors.New("graph unavailable")
			case strings.Contains(cypher, "MATCH (r:Repository)"):
				return []map[string]any{{"id": "repository:lib", "name": "lib"}}, nil
			default:
				return []map[string]any{{"total": 1}}, nil
			}
		},
	}

	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=10", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s (the primary page read is healthy; an auxiliary read error must not fail the whole request)", got, want, rec.Body.String())
	}

	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map", envelope.Data)
	}
	if got, want := querycontract.BoolVal(data, "truncated"), false; got != want {
		t.Errorf("truncated = %v, want %v -- a degraded dependency pre-pass must NOT claim more pages exist (this is a complete 1-of-1 page)", got, want)
	}
	reasons := querytestutil.RequireStringAnySlice(t, data, "partial_reasons")
	if querytestutil.AnySliceContains(reasons, "repository_inventory_truncated") {
		t.Errorf("partial_reasons = %v, want it NOT to contain repository_inventory_truncated (no page truncation occurred)", reasons)
	}
	if !querytestutil.AnySliceContains(reasons, repositoryDependencyEdgesDegradedReason) {
		t.Fatalf("partial_reasons = %v, want it to contain %q", reasons, repositoryDependencyEdgesDegradedReason)
	}
	resultLimits, ok := data["result_limits"].(map[string]any)
	if !ok {
		t.Fatalf("result_limits type = %T, want map", data["result_limits"])
	}
	if got, want := querycontract.BoolVal(resultLimits, "truncated"), false; got != want {
		t.Errorf("result_limits.truncated = %v, want %v", got, want)
	}
	repositories := data["repositories"].([]any)
	repo := repositories[0].(map[string]any)
	if got := querycontract.BoolVal(repo, "is_dependency"); got {
		t.Errorf("is_dependency = %v, want false (no edges could be read) -- disclosure is via partial_reasons, not by changing this field's type/shape", got)
	}
}

// TestListRepositoriesDisclosesDegradedDependencyEvidenceOnTruncation proves
// the same disclosure fires when the edge pre-pass hits its bound instead of
// erroring: is_dependency for repositories within the returned edge set
// still stays accurate, and the response names the dependency-evidence
// reason in partial_reasons, but truncated stays false -- the PAGE (one
// repository, requested and returned) is complete; only the auxiliary
// dependency-edge read was clipped (#6786 review F1).
func TestListRepositoriesDisclosesDegradedDependencyEvidenceOnTruncation(t *testing.T) {
	t.Parallel()

	truncatedEdgeRows := make([]map[string]any, 0, repositoryDependencyClusterEdgeFetchLimit)
	for i := 0; i < repositoryDependencyClusterEdgeFetchLimit; i++ {
		truncatedEdgeRows = append(truncatedEdgeRows, map[string]any{
			"source_id": "repository:app",
			"target_id": "repository:lib",
		})
	}
	reader := graph.FakeRepoGraphReader{
		RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return map[string]any{"total": int64(1)}, nil
		},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "(s:Repository)-[:DEPENDS_ON]->(t:Repository)"):
				return dependencyEdgeRowsForRead(cypher, truncatedEdgeRows), nil
			case strings.Contains(cypher, "MATCH (r:Repository)"):
				return []map[string]any{{"id": "repository:lib", "name": "lib"}}, nil
			default:
				return []map[string]any{{"total": 1}}, nil
			}
		},
	}

	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=10", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}

	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map", envelope.Data)
	}
	if got, want := querycontract.BoolVal(data, "truncated"), false; got != want {
		t.Errorf("truncated = %v, want %v -- an edge-pre-pass truncation must NOT claim more pages exist (this is a complete 1-of-1 page)", got, want)
	}
	reasons := querytestutil.RequireStringAnySlice(t, data, "partial_reasons")
	if querytestutil.AnySliceContains(reasons, "repository_inventory_truncated") {
		t.Errorf("partial_reasons = %v, want it NOT to contain repository_inventory_truncated (no page truncation occurred)", reasons)
	}
	if !querytestutil.AnySliceContains(reasons, repositoryDependencyEdgesDegradedReason) {
		t.Fatalf("partial_reasons = %v, want it to contain %q", reasons, repositoryDependencyEdgesDegradedReason)
	}
	resultLimits, ok := data["result_limits"].(map[string]any)
	if !ok {
		t.Fatalf("result_limits type = %T, want map", data["result_limits"])
	}
	if got, want := querycontract.BoolVal(resultLimits, "truncated"), false; got != want {
		t.Errorf("result_limits.truncated = %v, want %v", got, want)
	}
	repositories := data["repositories"].([]any)
	repo := repositories[0].(map[string]any)
	if got := querycontract.BoolVal(repo, "is_dependency"); !got {
		t.Errorf("is_dependency = %v, want true -- repository:lib IS within the (truncated-but-clipped) edge set, so its positive evidence stays accurate", got)
	}
}
