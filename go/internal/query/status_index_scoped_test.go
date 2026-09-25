// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// indexStatusScopedPaths are both routes that share getIndexStatus.
var indexStatusScopedPaths = []string{"/api/v0/index-status", "/api/v0/status/index"}

// indexStatusScopedWithheld is the exact section list a scoped caller is told
// was withheld: every key of the unscoped payload except the two a scoped
// caller receives.
var indexStatusScopedWithheld = []string{
	"status", "reasons", "queue", "queue_blockages", "coordinator",
	"scope_activity", "aws_materialization", "semantic_extraction", "terraform_state",
}

type indexStatusGraphCall struct {
	cypher string
	params map[string]any
}

// indexStatusRepoGraph is a graph double that holds the deployment's
// repositories and answers the count statement by evaluating the grant params
// it was actually sent, so a count of 1 proves the grant reached the query and
// was not applied after the fact.
type indexStatusRepoGraph struct {
	mu    sync.Mutex
	repos []string
	calls []indexStatusGraphCall
}

func (g *indexStatusRepoGraph) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (g *indexStatusRepoGraph) RunSingle(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, indexStatusGraphCall{cypher: cypher, params: params})
	if !strings.Contains(cypher, "IN $allowed_repository_ids") {
		return map[string]any{"count": int64(len(g.repos))}, nil
	}
	allowedRepos, _ := params["allowed_repository_ids"].([]string)
	allowedScopes, _ := params["allowed_scope_ids"].([]string)
	var n int64
	for _, repo := range g.repos {
		if slices.Contains(allowedRepos, repo) || slices.Contains(allowedScopes, repo) {
			n++
		}
	}
	return map[string]any{"count": n}, nil
}

func (g *indexStatusRepoGraph) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.calls)
}

const (
	indexStatusRepoA      = "repo-a"
	indexStatusRepoB      = "repo-b"
	indexStatusScopeB     = "scope-repo-b-raw"
	indexStatusRepoBScope = "repository:r_repob0001"
)

func indexStatusOtherTenantSnapshot(asOf time.Time) statuspkg.RawSnapshot {
	return statuspkg.RawSnapshot{
		AsOf:  asOf,
		Queue: statuspkg.QueueSnapshot{Total: 4242, Outstanding: 3131},
		QueueBlockages: []statuspkg.QueueBlockage{{
			Stage:       "reducer",
			Domain:      "repository_projection",
			ConflictKey: indexStatusScopeB,
			Blocked:     7,
		}},
		Coordinator: &statuspkg.CoordinatorSnapshot{
			CollectorInstances: []statuspkg.CollectorInstanceSummary{{
				InstanceID:     "collector-" + indexStatusRepoB,
				CollectorKind:  "git",
				DisplayName:    indexStatusRepoBScope,
				Enabled:        true,
				LastObservedAt: asOf,
				UpdatedAt:      asOf,
			}},
		},
	}
}

func indexStatusScopedMux(t *testing.T, graph *indexStatusRepoGraph, authCtx AuthContext) http.Handler {
	t.Helper()
	handler := &StatusHandler{
		StatusReader: fakeStatusReader{snapshot: indexStatusOtherTenantSnapshot(time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC))},
		Neo4j:        graph,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	return AuthMiddlewareWithScopedTokens("", &fakeScopedTokenResolver{context: authCtx, ok: true}, mux)
}

func getIndexStatusBody(t *testing.T, h http.Handler, path string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer tenant-a-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

// TestGetIndexStatusScopedCallerCountsOnlyGrantedRepositoriesInCypher is N1:
// the grant is bound in the count statement, not applied to an unfiltered
// count afterwards.
func TestGetIndexStatusScopedCallerCountsOnlyGrantedRepositoriesInCypher(t *testing.T) {
	t.Parallel()

	for _, path := range indexStatusScopedPaths {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			graph := &indexStatusRepoGraph{repos: []string{indexStatusRepoA, indexStatusRepoB}}
			h := indexStatusScopedMux(t, graph, AuthContext{
				Mode: AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a",
				AllowedRepositoryIDs: []string{indexStatusRepoA},
			})
			code, body := getIndexStatusBody(t, h, path)
			if code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %s", code, body)
			}
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("json.Unmarshal() error = %v; body = %s", err, body)
			}
			if got, want := payload["repository_count"], float64(1); got != want {
				t.Fatalf("repository_count = %#v, want %#v (the deployment holds 2 repositories)", got, want)
			}
			if graph.callCount() != 1 {
				t.Fatalf("graph calls = %d, want exactly 1", graph.callCount())
			}
			call := graph.calls[0]
			if !strings.Contains(call.cypher, "IN $allowed_repository_ids") || !strings.Contains(call.cypher, "IN $allowed_scope_ids") {
				t.Fatalf("count statement does not bind the grant: %q", call.cypher)
			}
			if got, want := call.params["allowed_repository_ids"], []string{indexStatusRepoA}; !reflect.DeepEqual(got, want) {
				t.Fatalf("allowed_repository_ids = %#v, want %#v", got, want)
			}
			if got, ok := call.params["allowed_scope_ids"].([]string); !ok || len(got) != 0 {
				t.Fatalf("allowed_scope_ids = %#v, want an empty []string", call.params["allowed_scope_ids"])
			}
		})
	}
}

// TestGetIndexStatusScopedEmptyGrantSkipsGraph is N2.
func TestGetIndexStatusScopedEmptyGrantSkipsGraph(t *testing.T) {
	t.Parallel()

	for _, path := range indexStatusScopedPaths {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			graph := &indexStatusRepoGraph{repos: []string{indexStatusRepoA, indexStatusRepoB}}
			h := indexStatusScopedMux(t, graph, AuthContext{
				Mode: AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a",
			})
			code, body := getIndexStatusBody(t, h, path)
			if code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %s", code, body)
			}
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if got, want := payload["repository_count"], float64(0); got != want {
				t.Fatalf("repository_count = %#v, want %#v", got, want)
			}
			if n := graph.callCount(); n != 0 {
				t.Fatalf("graph calls = %d, want 0 for an empty grant", n)
			}
		})
	}
}

// TestGetIndexStatusScopedBodyWithholdsDeploymentWideSections is N3: the body
// is exactly the settled scoped shape, withheld_sections is exactly the keys
// the unscoped payload carries beyond it, and nothing of another tenant
// appears anywhere in the serialized body -- including a queue blockage whose
// conflict_key is that tenant's raw scope id.
func TestGetIndexStatusScopedBodyWithholdsDeploymentWideSections(t *testing.T) {
	t.Parallel()

	graph := &indexStatusRepoGraph{repos: []string{indexStatusRepoA, indexStatusRepoB}}
	handler := &StatusHandler{
		StatusReader: fakeStatusReader{snapshot: indexStatusOtherTenantSnapshot(time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC))},
		Neo4j:        graph,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	// The unscoped payload is the source of truth for what "withheld" means.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v0/index-status", nil))
	var unscoped map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &unscoped); err != nil {
		t.Fatalf("json.Unmarshal(unscoped) error = %v", err)
	}
	if !strings.Contains(rec.Body.String(), indexStatusScopeB) {
		t.Fatalf("fixture is inert: the unscoped body does not carry the raw scope id %q: %s", indexStatusScopeB, rec.Body.String())
	}
	wantWithheld := make([]string, 0, len(unscoped))
	for key := range unscoped {
		if key != "version" && key != "repository_count" {
			wantWithheld = append(wantWithheld, key)
		}
	}
	sort.Strings(wantWithheld)

	authed := AuthMiddlewareWithScopedTokens("", &fakeScopedTokenResolver{
		context: AuthContext{
			Mode: AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a",
			AllowedRepositoryIDs: []string{indexStatusRepoA},
		},
		ok: true,
	}, mux)
	for _, path := range indexStatusScopedPaths {
		code, body := getIndexStatusBody(t, authed, path)
		if code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200; body = %s", path, code, body)
		}
		var scoped map[string]any
		if err := json.Unmarshal(body, &scoped); err != nil {
			t.Fatalf("json.Unmarshal(scoped) error = %v", err)
		}
		gotKeys := make([]string, 0, len(scoped))
		for key := range scoped {
			gotKeys = append(gotKeys, key)
		}
		sort.Strings(gotKeys)
		wantKeys := []string{"completeness_state", "repository_count", "scoped", "version", "withheld_sections"}
		if !reflect.DeepEqual(gotKeys, wantKeys) {
			t.Errorf("%s scoped keys = %v, want exactly %v", path, gotKeys, wantKeys)
		}
		if scoped["scoped"] != true {
			t.Errorf("%s scoped = %#v, want true", path, scoped["scoped"])
		}
		if got, want := scoped["completeness_state"], "scoped_repository_count_only"; got != want {
			t.Errorf("%s completeness_state = %#v, want %#v", path, got, want)
		}
		withheld, _ := scoped["withheld_sections"].([]any)
		gotWithheld := make([]string, 0, len(withheld))
		for _, v := range withheld {
			gotWithheld = append(gotWithheld, v.(string))
		}
		if !reflect.DeepEqual(gotWithheld, indexStatusScopedWithheld) {
			t.Errorf("%s withheld_sections = %v, want %v", path, gotWithheld, indexStatusScopedWithheld)
		}
		sortedWithheld := slices.Clone(gotWithheld)
		sort.Strings(sortedWithheld)
		if !reflect.DeepEqual(sortedWithheld, wantWithheld) {
			t.Errorf("%s withheld_sections %v drifted from the unscoped payload keys %v", path, sortedWithheld, wantWithheld)
		}
		for _, leaked := range []string{indexStatusRepoB, indexStatusScopeB, indexStatusRepoBScope, "collector-"} {
			if strings.Contains(string(body), leaked) {
				t.Errorf("%s scoped body leaked %q: %s", path, leaked, body)
			}
		}
	}
}

// TestGetIndexStatusScopedDoesNotReadStatusSnapshot proves the process-global
// snapshot is never loaded for a scoped caller: nothing it holds is disclosed,
// so reading it would only cost a status query for a caller who cannot see it.
func TestGetIndexStatusScopedDoesNotReadStatusSnapshot(t *testing.T) {
	t.Parallel()

	reader := &selectionRecordingReader{snapshot: indexStatusOtherTenantSnapshot(time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC))}
	graph := &indexStatusRepoGraph{repos: []string{indexStatusRepoA, indexStatusRepoB}}
	handler := &StatusHandler{StatusReader: reader, Neo4j: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)
	h := AuthMiddlewareWithScopedTokens("", &fakeScopedTokenResolver{
		context: AuthContext{Mode: AuthModeScoped, TenantID: "t", WorkspaceID: "w", AllowedRepositoryIDs: []string{indexStatusRepoA}},
		ok:      true,
	}, mux)
	if code, body := getIndexStatusBody(t, h, "/api/v0/index-status"); code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", code, body)
	}
	if reader.filteredCallCount != 0 {
		t.Fatalf("status snapshot reads = %d, want 0 for a scoped caller", reader.filteredCallCount)
	}
}

// TestGetIndexStatusScopedGraphFailureIsNotZero: a failed count must surface,
// because zero is a valid, materially different answer for a scoped caller.
func TestGetIndexStatusScopedGraphFailureIsNotZero(t *testing.T) {
	t.Parallel()

	handler := &StatusHandler{
		StatusReader: fakeStatusReader{},
		Neo4j: fakeGraphReader{runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
			return nil, context.DeadlineExceeded
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	h := AuthMiddlewareWithScopedTokens("", &fakeScopedTokenResolver{
		context: AuthContext{Mode: AuthModeScoped, TenantID: "t", WorkspaceID: "w", AllowedRepositoryIDs: []string{indexStatusRepoA}},
		ok:      true,
	}, mux)
	code, body := getIndexStatusBody(t, h, "/api/v0/index-status")
	if code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body = %s", code, body)
	}
}

// TestGetIndexStatusSharedCallerPayloadUnchanged is N4's handler half: a
// shared-key caller keeps the full unfiltered report and the unfiltered count.
func TestGetIndexStatusSharedCallerPayloadUnchanged(t *testing.T) {
	t.Parallel()

	graph := &indexStatusRepoGraph{repos: []string{indexStatusRepoA, indexStatusRepoB}}
	handler := &StatusHandler{
		StatusReader: fakeStatusReader{snapshot: indexStatusOtherTenantSnapshot(time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC))},
		Neo4j:        graph,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v0/index-status", nil))
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got, want := payload["repository_count"], float64(2); got != want {
		t.Fatalf("repository_count = %#v, want %#v", got, want)
	}
	for _, key := range indexStatusScopedWithheld {
		if _, ok := payload[key]; !ok {
			t.Errorf("unscoped payload lost %q", key)
		}
	}
	for _, key := range []string{"scoped", "completeness_state", "withheld_sections"} {
		if _, ok := payload[key]; ok {
			t.Errorf("unscoped payload gained scoped-only key %q", key)
		}
	}
	if len(graph.calls) != 1 || strings.Contains(graph.calls[0].cypher, "$allowed_") {
		t.Fatalf("unscoped count statement = %#v, want the ungranted count", graph.calls)
	}
}
