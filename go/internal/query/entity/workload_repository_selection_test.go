// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil/graph"
)

func TestFetchWorkloadContextSelectsRepositoryFromActualDefinesCandidates(t *testing.T) {
	tests := []struct {
		name         string
		ctx          context.Context
		storedRepoID string
		wantRepoID   string
		wantInstance bool
	}{
		{
			name: "scoped authorized stored repository is stale",
			ctx: auth.ContextWithAuthContext(t.Context(), auth.AuthContext{
				Mode:                 auth.AuthModeScoped,
				AllowedRepositoryIDs: []string{"repo-team-a", "repo-team-b", "repo-team-stale"},
			}),
			storedRepoID: "repo-team-stale",
			wantRepoID:   "repo-team-a",
			wantInstance: false,
		},
		{
			name:         "unscoped stored repository is stale",
			ctx:          t.Context(),
			storedRepoID: "repo-team-stale",
			wantRepoID:   "repo-team-a",
			wantInstance: true,
		},
		{
			name:         "stored repository is an actual defining candidate",
			ctx:          t.Context(),
			storedRepoID: "repo-team-b",
			wantRepoID:   "repo-team-b",
			wantInstance: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidateLookupCalled := false
			reader := graph.FakeWorkloadGraphReader{
				RunSingleFn: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
					if strings.Contains(cypher, "MATCH (r:Repository)-[:DEFINES]->(w)") {
						t.Fatalf("repository candidate lookup used backend ordering instead of bounded exact-workload traversal:\n%s", cypher)
					}
					if strings.Contains(cypher, "MATCH (w:Workload)") {
						return map[string]any{
							"id": "workload:payments", "name": "payments", "kind": "service",
							"repo_id": test.storedRepoID,
						}, nil
					}
					if strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
						return map[string]any{"repo_name": "stale"}, nil
					}
					return nil, nil
				},
				RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
					switch {
					case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
						candidateLookupCalled = true
						if strings.Contains(cypher, "ORDER BY") || strings.Contains(cypher, "CASE WHEN") {
							t.Fatalf("repository candidate lookup retains backend ordering:\n%s", cypher)
						}
						if got := querycontract.StringVal(params, "workload_id"); got != "workload:payments" {
							t.Fatalf("workload_id = %q, want exact workload id", got)
						}
						if got := querycontract.IntVal(params, "repository_limit"); got != querycontract.ContextStoryItemLimit+1 {
							t.Fatalf("repository_limit = %d, want %d", got, querycontract.ContextStoryItemLimit+1)
						}
						return []map[string]any{
							{"repo_id": "repo-team-b", "repo_name": "payments-b"},
							{"repo_id": "repo-team-a", "repo_name": "payments-a"},
						}, nil
					case strings.Contains(cypher, "[instanceOf:INSTANCE_OF]"):
						repoID := querycontract.StringVal(params, "repo_id")
						if repoID != "repo-team-a" && repoID != "repo-team-b" {
							return nil, nil
						}
						return []map[string]any{{
							"repo_id": repoID, "repo_name": "payments", "workload_id": "workload:payments",
							"instance_id": "instance:payments:prod", "environment": "prod",
						}}, nil
					default:
						return nil, nil
					}
				},
			}

			got, err := (&Handler{Neo4j: reader}).FetchWorkloadContextForOperation(
				test.ctx,
				"w.id = $workload_id",
				map[string]any{"workload_id": "workload:payments"},
				"workload_context",
			)
			if err != nil {
				t.Fatalf("FetchWorkloadContextForOperation() error = %v", err)
			}
			if !candidateLookupCalled {
				t.Fatal("repository selection did not derive from DEFINES candidates")
			}
			if gotRepoID := querycontract.StringVal(got, "repo_id"); gotRepoID != test.wantRepoID {
				t.Fatalf("repo_id = %q, want actual defining repository %q", gotRepoID, test.wantRepoID)
			}
			instances := querycontract.MapSliceValue(got, "instances")
			if !test.wantInstance && len(instances) != 0 {
				t.Fatalf("instances = %#v, want scoped repository-unowned topology omitted", instances)
			}
			if test.wantInstance && (len(instances) != 1 || querycontract.StringVal(instances[0], "instance_id") != "instance:payments:prod") {
				t.Fatalf("instances = %#v, want topology for selected defining repository", instances)
			}
		})
	}
}

func TestFetchWorkloadRepositoryForAccessSelectsBoundedCandidates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		rows            []map[string]any
		preferredRepoID string
		wantRepoID      string
		wantRepoName    string
	}{
		{name: "empty candidates"},
		{
			name: "duplicate repository ids are collapsed",
			rows: []map[string]any{
				{"repo_id": "repo-b", "repo_name": "beta"},
				{"repo_id": "repo-a", "repo_name": "alpha"},
				{"repo_id": "repo-a", "repo_name": "duplicate-alpha"},
			},
			wantRepoID: "repo-a", wantRepoName: "alpha",
		},
		{
			name: "preferred defining repository wins",
			rows: []map[string]any{
				{"repo_id": "repo-a", "repo_name": "alpha"},
				{"repo_id": "repo-b", "repo_name": "beta"},
			},
			preferredRepoID: "repo-b", wantRepoID: "repo-b", wantRepoName: "beta",
		},
		{
			name: "stale preferred repository falls back deterministically",
			rows: []map[string]any{
				{"repo_id": "repo-z", "repo_name": "zulu"},
				{"repo_id": "repo-a", "repo_name": "alpha"},
			},
			preferredRepoID: "repo-stale", wantRepoID: "repo-a", wantRepoName: "alpha",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := graph.FakeWorkloadGraphReader{
				RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
					assertWorkloadRepositoryCandidateQuery(t, cypher, params, "workload:payments")
					return test.rows, nil
				},
			}
			gotID, gotName, err := (&Handler{Neo4j: reader}).FetchWorkloadRepositoryForAccess(
				t.Context(),
				"workload:payments",
				querycontract.RepositoryAccessFilter{AllScopes: true},
				test.preferredRepoID,
			)
			if err != nil {
				t.Fatalf("FetchWorkloadRepositoryForAccess() error = %v", err)
			}
			if gotID != test.wantRepoID || gotName != test.wantRepoName {
				t.Fatalf("repository = (%q, %q), want (%q, %q)", gotID, gotName, test.wantRepoID, test.wantRepoName)
			}
		})
	}
}

func TestFetchWorkloadRepositoryForAccessAppliesScopedAuthorization(t *testing.T) {
	t.Parallel()

	ctx := auth.ContextWithAuthContext(t.Context(), auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		AllowedRepositoryIDs: []string{"repo-a"},
		AllowedScopeIDs:      []string{"scope-a"},
	})
	reader := graph.FakeWorkloadGraphReader{
		RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			assertWorkloadRepositoryCandidateQuery(t, cypher, params, "workload:payments")
			if !strings.Contains(cypher, "WHERE (r.id IN $allowed_repository_ids OR r.id IN $allowed_scope_ids)") {
				t.Fatalf("candidate query missing scoped authorization:\n%s", cypher)
			}
			if got := querycontract.StringSliceVal(params, "allowed_repository_ids"); len(got) != 1 || got[0] != "repo-a" {
				t.Fatalf("allowed_repository_ids = %#v, want [repo-a]", got)
			}
			if got := querycontract.StringSliceVal(params, "allowed_scope_ids"); len(got) != 1 || got[0] != "scope-a" {
				t.Fatalf("allowed_scope_ids = %#v, want [scope-a]", got)
			}
			return []map[string]any{{"repo_id": "repo-a", "repo_name": "alpha"}}, nil
		},
	}
	gotID, _, err := (&Handler{Neo4j: reader}).FetchWorkloadRepositoryForAccess(
		ctx,
		"workload:payments",
		querycontract.RepositoryAccessFilterFromContext(ctx),
		"",
	)
	if err != nil {
		t.Fatalf("FetchWorkloadRepositoryForAccess() error = %v", err)
	}
	if gotID != "repo-a" {
		t.Fatalf("repo_id = %q, want repo-a", gotID)
	}
}

// TestFetchWorkloadRepositoryForAccessDropsUngrantedRowDespiteBackendWhere is
// the #6786 review follow-up (F1): FetchWorkloadRepositoryForAccess's
// `<-[:DEFINES]-` MATCH carries an inner WHERE grant predicate on a backward
// pattern -- the same shape class this PR already proved NornicDB v1.3.3 can
// silently fail to apply. This test simulates that failure directly: the
// fake graph reader returns a DEFINES candidate row for a repository NOT in
// the caller's grant, as if the backend's WHERE had not filtered it. Go must
// still refuse to admit it.
func TestFetchWorkloadRepositoryForAccessDropsUngrantedRowDespiteBackendWhere(t *testing.T) {
	t.Parallel()

	ctx := auth.ContextWithAuthContext(t.Context(), auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		AllowedRepositoryIDs: []string{"repo-a"},
	})
	reader := graph.FakeWorkloadGraphReader{
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			// The backend's WHERE should have excluded repo-b, but this fake
			// simulates it not doing so.
			return []map[string]any{{"repo_id": "repo-b", "repo_name": "beta"}}, nil
		},
	}
	gotID, gotName, err := (&Handler{Neo4j: reader}).FetchWorkloadRepositoryForAccess(
		ctx, "workload:payments", querycontract.RepositoryAccessFilterFromContext(ctx), "",
	)
	if err != nil {
		t.Fatalf("FetchWorkloadRepositoryForAccess() error = %v, want nil", err)
	}
	if gotID != "" || gotName != "" {
		t.Fatalf("repository = (%q, %q), want empty: an ungranted DEFINES row must never be admitted even if the backend's own WHERE failed to filter it", gotID, gotName)
	}
}

// TestGetWorkloadContextUngrantedDefinesRowReturnsNotFound is the end-to-end
// half of F1: FetchWorkloadContextForOperation must 404 a scoped caller for a
// workload whose own repo_id is ungranted and whose ONLY DEFINES candidate
// (simulated as leaking past the backend's WHERE) is also ungranted.
func TestGetWorkloadContextUngrantedDefinesRowReturnsNotFound(t *testing.T) {
	t.Parallel()

	reader := graph.FakeWorkloadGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if strings.Contains(cypher, "MATCH (w:Workload)") && strings.Contains(cypher, "w.id = $workload_id") {
				return map[string]any{
					"id":      "workload:payments",
					"name":    "payments",
					"kind":    "service",
					"repo_id": "repo-team-z",
				}, nil
			}
			return nil, nil
		},
		RunByMatch: map[string][]map[string]any{
			// Simulates the backend's WHERE failing to exclude an ungranted
			// defining repository (F1): the caller is granted only
			// repo-team-a, but this row names repo-team-z.
			"<-[:DEFINES]-(r:Repository)": {
				{"repo_id": "repo-team-z", "repo_name": "payments"},
			},
		},
	}
	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/workloads/workload:payments/context", nil)
	req.SetPathValue("workload_id", "workload:payments")
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.GetWorkloadContext(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestFetchWorkloadRepositoryForAccessFailsClosedOnOverflowAndGraphError(t *testing.T) {
	t.Parallel()

	t.Run("candidate overflow", func(t *testing.T) {
		rows := make([]map[string]any, workloadRepositoryCandidateLimit+1)
		for index := range rows {
			rows[index] = map[string]any{"repo_id": "repo"}
		}
		reader := graph.FakeWorkloadGraphReader{
			RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return rows, nil
			},
		}
		gotID, gotName, err := (&Handler{Neo4j: reader}).FetchWorkloadRepositoryForAccess(
			t.Context(), "workload:payments", querycontract.RepositoryAccessFilter{AllScopes: true}, "",
		)
		if err == nil || !strings.Contains(err.Error(), "candidates exceed bound") {
			t.Fatalf("error = %v, want bounded-candidate error", err)
		}
		if gotID != "" || gotName != "" {
			t.Fatalf("repository = (%q, %q), want empty on overflow", gotID, gotName)
		}
	})

	t.Run("graph error", func(t *testing.T) {
		wantErr := errors.New("graph unavailable")
		reader := graph.FakeWorkloadGraphReader{
			RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, wantErr
			},
		}
		_, _, err := (&Handler{Neo4j: reader}).FetchWorkloadRepositoryForAccess(
			t.Context(), "workload:payments", querycontract.RepositoryAccessFilter{AllScopes: true}, "",
		)
		if !errors.Is(err, wantErr) {
			t.Fatalf("error = %v, want %v", err, wantErr)
		}
	})
}

func TestFetchWorkloadRepositoryForAccessSkipsEmptyWorkloadID(t *testing.T) {
	t.Parallel()

	reader := graph.FakeWorkloadGraphReader{
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			t.Fatal("empty workload id queried the graph")
			return nil, nil
		},
	}
	gotID, gotName, err := (&Handler{Neo4j: reader}).FetchWorkloadRepositoryForAccess(
		t.Context(), "  ", querycontract.RepositoryAccessFilter{AllScopes: true}, "repo-a",
	)
	if err != nil || gotID != "" || gotName != "" {
		t.Fatalf("result = (%q, %q, %v), want empty nil result", gotID, gotName, err)
	}
}

func assertWorkloadRepositoryCandidateQuery(
	t *testing.T,
	cypher string,
	params map[string]any,
	wantWorkloadID string,
) {
	t.Helper()
	if !strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)") {
		t.Fatalf("candidate query is not exact connected traversal:\n%s", cypher)
	}
	if strings.Contains(cypher, "ORDER BY") || strings.Contains(cypher, "CASE WHEN") {
		t.Fatalf("candidate query retains backend ordering:\n%s", cypher)
	}
	if !strings.Contains(cypher, "RETURN DISTINCT r.id as repo_id, r.name as repo_name") {
		t.Fatalf("candidate query does not deduplicate repositories before applying the sentinel limit:\n%s", cypher)
	}
	if got := querycontract.StringVal(params, "workload_id"); got != wantWorkloadID {
		t.Fatalf("workload_id = %q, want %q", got, wantWorkloadID)
	}
	if got, want := querycontract.IntVal(params, "repository_limit"), workloadRepositoryCandidateLimit+1; got != want {
		t.Fatalf("repository_limit = %d, want %d", got, want)
	}
}
