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

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
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
	reader := querytestutil.FakeRepoGraphReader{
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
	reader := querytestutil.FakeRepoGraphReader{
		RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return map[string]any{"total": int64(2)}, nil
		},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "(s:Repository)-[:DEPENDS_ON]->(t:Repository)"):
				capturedEdgeCypher = cypher
				return []map[string]any{
					{"source_id": "repository:app", "target_id": "repository:lib"},
				}, nil
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
	reader := querytestutil.FakeRepoGraphReader{
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
				return []map[string]any{
					{"source_id": "repository:outside", "target_id": "repository:lib"},
				}, nil
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
	req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
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
// wrong.
func TestListRepositoriesDisclosesDegradedDependencyEvidenceOnEdgeQueryError(t *testing.T) {
	t.Parallel()

	reader := querytestutil.FakeRepoGraphReader{
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
	if got, want := querycontract.BoolVal(data, "truncated"), true; got != want {
		t.Errorf("truncated = %v, want %v (a degraded dependency read must be disclosed)", got, want)
	}
	reasons := querytestutil.RequireStringAnySlice(t, data, "partial_reasons")
	if !querytestutil.AnySliceContains(reasons, repositoryDependencyEdgesDegradedReason) {
		t.Fatalf("partial_reasons = %v, want it to contain %q", reasons, repositoryDependencyEdgesDegradedReason)
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
// still stays accurate, but the response marks itself truncated and names
// the reason so a caller does not treat an is_dependency=false repository
// outside that window as a confirmed negative.
func TestListRepositoriesDisclosesDegradedDependencyEvidenceOnTruncation(t *testing.T) {
	t.Parallel()

	truncatedEdgeRows := make([]map[string]any, 0, repositoryDependencyClusterEdgeFetchLimit)
	for i := 0; i < repositoryDependencyClusterEdgeFetchLimit; i++ {
		truncatedEdgeRows = append(truncatedEdgeRows, map[string]any{
			"source_id": "repository:app",
			"target_id": "repository:lib",
		})
	}
	reader := querytestutil.FakeRepoGraphReader{
		RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return map[string]any{"total": int64(1)}, nil
		},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "(s:Repository)-[:DEPENDS_ON]->(t:Repository)"):
				return truncatedEdgeRows, nil
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
	if got, want := querycontract.BoolVal(data, "truncated"), true; got != want {
		t.Errorf("truncated = %v, want %v (an edge-pre-pass truncation must be disclosed)", got, want)
	}
	reasons := querytestutil.RequireStringAnySlice(t, data, "partial_reasons")
	if !querytestutil.AnySliceContains(reasons, repositoryDependencyEdgesDegradedReason) {
		t.Fatalf("partial_reasons = %v, want it to contain %q", reasons, repositoryDependencyEdgesDegradedReason)
	}
	repositories := data["repositories"].([]any)
	repo := repositories[0].(map[string]any)
	if got := querycontract.BoolVal(repo, "is_dependency"); !got {
		t.Errorf("is_dependency = %v, want true -- repository:lib IS within the (truncated-but-clipped) edge set, so its positive evidence stays accurate", got)
	}
}
