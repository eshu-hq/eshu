// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestFetchWorkloadContextSelectsRepositoryFromActualDefinesCandidates(t *testing.T) {
	tests := []struct {
		name         string
		ctx          context.Context
		storedRepoID string
		wantRepoID   string
	}{
		{
			name: "scoped authorized stored repository is stale",
			ctx: ContextWithAuthContext(t.Context(), AuthContext{
				Mode:                 AuthModeScoped,
				AllowedRepositoryIDs: []string{"repo-team-a", "repo-team-b", "repo-team-stale"},
			}),
			storedRepoID: "repo-team-stale",
			wantRepoID:   "repo-team-a",
		},
		{
			name:         "unscoped stored repository is stale",
			ctx:          t.Context(),
			storedRepoID: "repo-team-stale",
			wantRepoID:   "repo-team-a",
		},
		{
			name:         "stored repository is an actual defining candidate",
			ctx:          t.Context(),
			storedRepoID: "repo-team-b",
			wantRepoID:   "repo-team-b",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidateLookupCalled := false
			reader := fakeWorkloadGraphReader{
				runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
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
				run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
					switch {
					case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
						candidateLookupCalled = true
						if strings.Contains(cypher, "ORDER BY") || strings.Contains(cypher, "CASE WHEN") {
							t.Fatalf("repository candidate lookup retains backend ordering:\n%s", cypher)
						}
						if got := StringVal(params, "workload_id"); got != "workload:payments" {
							t.Fatalf("workload_id = %q, want exact workload id", got)
						}
						if got := IntVal(params, "repository_limit"); got != contextStoryItemLimit+1 {
							t.Fatalf("repository_limit = %d, want %d", got, contextStoryItemLimit+1)
						}
						return []map[string]any{
							{"repo_id": "repo-team-b", "repo_name": "payments-b"},
							{"repo_id": "repo-team-a", "repo_name": "payments-a"},
						}, nil
					case strings.Contains(cypher, "[instanceOf:INSTANCE_OF]"):
						repoID := StringVal(params, "repo_id")
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

			got, err := (&EntityHandler{Neo4j: reader}).fetchWorkloadContextForOperation(
				test.ctx,
				"w.id = $workload_id",
				map[string]any{"workload_id": "workload:payments"},
				"workload_context",
			)
			if err != nil {
				t.Fatalf("fetchWorkloadContextForOperation() error = %v", err)
			}
			if !candidateLookupCalled {
				t.Fatal("repository selection did not derive from DEFINES candidates")
			}
			if gotRepoID := StringVal(got, "repo_id"); gotRepoID != test.wantRepoID {
				t.Fatalf("repo_id = %q, want actual defining repository %q", gotRepoID, test.wantRepoID)
			}
			instances := mapSliceValue(got, "instances")
			if len(instances) != 1 || StringVal(instances[0], "instance_id") != "instance:payments:prod" {
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
			reader := fakeWorkloadGraphReader{
				run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
					assertWorkloadRepositoryCandidateQuery(t, cypher, params, "workload:payments")
					return test.rows, nil
				},
			}
			gotID, gotName, err := (&EntityHandler{Neo4j: reader}).fetchWorkloadRepositoryForAccess(
				t.Context(),
				"workload:payments",
				repositoryAccessFilter{allScopes: true},
				test.preferredRepoID,
			)
			if err != nil {
				t.Fatalf("fetchWorkloadRepositoryForAccess() error = %v", err)
			}
			if gotID != test.wantRepoID || gotName != test.wantRepoName {
				t.Fatalf("repository = (%q, %q), want (%q, %q)", gotID, gotName, test.wantRepoID, test.wantRepoName)
			}
		})
	}
}

func TestFetchWorkloadRepositoryForAccessAppliesScopedAuthorization(t *testing.T) {
	t.Parallel()

	ctx := ContextWithAuthContext(t.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		AllowedRepositoryIDs: []string{"repo-a"},
		AllowedScopeIDs:      []string{"scope-a"},
	})
	reader := fakeWorkloadGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			assertWorkloadRepositoryCandidateQuery(t, cypher, params, "workload:payments")
			if !strings.Contains(cypher, "WHERE (r.id IN $allowed_repository_ids OR r.id IN $allowed_scope_ids)") {
				t.Fatalf("candidate query missing scoped authorization:\n%s", cypher)
			}
			if got := StringSliceVal(params, "allowed_repository_ids"); len(got) != 1 || got[0] != "repo-a" {
				t.Fatalf("allowed_repository_ids = %#v, want [repo-a]", got)
			}
			if got := StringSliceVal(params, "allowed_scope_ids"); len(got) != 1 || got[0] != "scope-a" {
				t.Fatalf("allowed_scope_ids = %#v, want [scope-a]", got)
			}
			return []map[string]any{{"repo_id": "repo-a", "repo_name": "alpha"}}, nil
		},
	}
	gotID, _, err := (&EntityHandler{Neo4j: reader}).fetchWorkloadRepositoryForAccess(
		ctx,
		"workload:payments",
		repositoryAccessFilterFromContext(ctx),
		"",
	)
	if err != nil {
		t.Fatalf("fetchWorkloadRepositoryForAccess() error = %v", err)
	}
	if gotID != "repo-a" {
		t.Fatalf("repo_id = %q, want repo-a", gotID)
	}
}

func TestFetchWorkloadRepositoryForAccessFailsClosedOnOverflowAndGraphError(t *testing.T) {
	t.Parallel()

	t.Run("candidate overflow", func(t *testing.T) {
		rows := make([]map[string]any, workloadRepositoryCandidateLimit+1)
		for index := range rows {
			rows[index] = map[string]any{"repo_id": "repo"}
		}
		reader := fakeWorkloadGraphReader{
			run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return rows, nil
			},
		}
		gotID, gotName, err := (&EntityHandler{Neo4j: reader}).fetchWorkloadRepositoryForAccess(
			t.Context(), "workload:payments", repositoryAccessFilter{allScopes: true}, "",
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
		reader := fakeWorkloadGraphReader{
			run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, wantErr
			},
		}
		_, _, err := (&EntityHandler{Neo4j: reader}).fetchWorkloadRepositoryForAccess(
			t.Context(), "workload:payments", repositoryAccessFilter{allScopes: true}, "",
		)
		if !errors.Is(err, wantErr) {
			t.Fatalf("error = %v, want %v", err, wantErr)
		}
	})
}

func TestFetchWorkloadRepositoryForAccessSkipsEmptyWorkloadID(t *testing.T) {
	t.Parallel()

	reader := fakeWorkloadGraphReader{
		run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			t.Fatal("empty workload id queried the graph")
			return nil, nil
		},
	}
	gotID, gotName, err := (&EntityHandler{Neo4j: reader}).fetchWorkloadRepositoryForAccess(
		t.Context(), "  ", repositoryAccessFilter{allScopes: true}, "repo-a",
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
	if got := StringVal(params, "workload_id"); got != wantWorkloadID {
		t.Fatalf("workload_id = %q, want %q", got, wantWorkloadID)
	}
	if got, want := IntVal(params, "repository_limit"), workloadRepositoryCandidateLimit+1; got != want {
		t.Fatalf("repository_limit = %d, want %d", got, want)
	}
}
