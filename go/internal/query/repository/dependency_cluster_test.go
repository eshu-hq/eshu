// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestRepositoryDependencyClustersAssignsConnectedComponents proves that the
// union-find pass over (:Repository)-[:DEPENDS_ON]->(:Repository) edges groups
// repositories that transitively depend on each other into a single cluster
// keyed by the lexicographically-smallest repository id in the component.
//
// Edges A->B and B->C form one component {A,B,C}; the isolated repository D has
// no dependency edge and is therefore not assigned a cluster.
func TestRepositoryDependencyClustersAssignsConnectedComponents(t *testing.T) {
	t.Parallel()

	edges := []repositoryDependencyEdge{
		{Source: "repository:a", Target: "repository:b"},
		{Source: "repository:b", Target: "repository:c"},
	}
	clusters := buildRepositoryDependencyClusters(edges)

	for _, id := range []string{"repository:a", "repository:b", "repository:c"} {
		key, ok := clusters[id]
		if !ok {
			t.Fatalf("repository %q has no cluster, want clustered", id)
		}
		if key != "repository:a" {
			t.Errorf("cluster key for %q = %q, want %q (smallest id in component)", id, key, "repository:a")
		}
	}
	if _, ok := clusters["repository:d"]; ok {
		t.Errorf("isolated repository:d unexpectedly clustered: %q", clusters["repository:d"])
	}
}

// TestRepositoryDependencyClustersHandlesCyclesAndSelfLoops proves the
// connected-component grouping is robust to dependency cycles (A->B->A) and
// self-loops (E->E), neither of which should change the component membership or
// loop forever.
func TestRepositoryDependencyClustersHandlesCyclesAndSelfLoops(t *testing.T) {
	t.Parallel()

	edges := []repositoryDependencyEdge{
		{Source: "repository:a", Target: "repository:b"},
		{Source: "repository:b", Target: "repository:a"}, // cycle
		{Source: "repository:e", Target: "repository:e"}, // self-loop
	}
	clusters := buildRepositoryDependencyClusters(edges)

	if clusters["repository:a"] != "repository:a" || clusters["repository:b"] != "repository:a" {
		t.Errorf("cycle component = {a:%q, b:%q}, want both keyed by repository:a", clusters["repository:a"], clusters["repository:b"])
	}
	// A self-loop is a single-node component. Eshu treats it as a real cluster
	// because the repository participates in a DEPENDS_ON edge; the key is its
	// own id.
	if clusters["repository:e"] != "repository:e" {
		t.Errorf("self-loop cluster for repository:e = %q, want repository:e", clusters["repository:e"])
	}
}

// TestRepositoryDependencyClustersEmptyGraph proves an empty edge list yields no
// clusters, so every repository falls through to the non-cluster grouping path.
func TestRepositoryDependencyClustersEmptyGraph(t *testing.T) {
	t.Parallel()

	if clusters := buildRepositoryDependencyClusters(nil); len(clusters) != 0 {
		t.Fatalf("empty edge list produced %d clusters, want 0", len(clusters))
	}
}

// TestDecorateRepositoryDependencyClusterTakesPrecedence proves the
// dependency-cluster source is the primary grouping signal: a repository that
// participates in a DEPENDS_ON cluster is grouped by its cluster even when it
// also carries slug, remote, or dependency-flag evidence that would otherwise
// win.
func TestDecorateRepositoryDependencyClusterTakesPrecedence(t *testing.T) {
	t.Parallel()

	clusters := map[string]string{
		"repository:b": "repository:a",
	}
	repo := map[string]any{
		"id":            "repository:b",
		"repo_slug":     "preferred/leaf",
		"remote_url":    "https://github.com/other-org/leaf",
		"is_dependency": true,
	}
	decorated := decorateRepositoryGroupEvidenceWithClusters(repo, clusters)

	if got := querycontract.StringVal(decorated, "group_source"); got != repositoryGroupSourceDependencyCluster {
		t.Fatalf("group_source = %q, want %q", got, repositoryGroupSourceDependencyCluster)
	}
	if got := querycontract.StringVal(decorated, "group_kind"); got != "cluster" {
		t.Errorf("group_kind = %q, want cluster", got)
	}
	if got := querycontract.StringVal(decorated, "group_truth"); got != repositoryGroupTruthDerived {
		t.Errorf("group_truth = %q, want %q", got, repositoryGroupTruthDerived)
	}
	if got := querycontract.StringVal(decorated, "group_key"); got != "repository:a" {
		t.Errorf("group_key = %q, want repository:a (cluster id)", got)
	}
}

// TestDecorateRepositoryDependencyClusterFallsToMissingEvidence proves a
// repository with no dependency edge does NOT fall back to slug/owner/name
// heuristics for the new path: when it carries no other source-backed evidence
// it stays honest missing_evidence (issue #3504 rejects name heuristics).
func TestDecorateRepositoryDependencyClusterFallsToMissingEvidence(t *testing.T) {
	t.Parallel()

	clusters := map[string]string{"repository:b": "repository:a"}
	repo := map[string]any{
		"id":            "repository:lonely",
		"name":          "lonely",
		"is_dependency": false,
	}
	decorated := decorateRepositoryGroupEvidenceWithClusters(repo, clusters)

	if got := querycontract.StringVal(decorated, "group_source"); got != repositoryGroupSourceMissing {
		t.Fatalf("group_source = %q, want %q", got, repositoryGroupSourceMissing)
	}
	if got := querycontract.StringVal(decorated, "group_truth"); got != repositoryGroupTruthMissing {
		t.Errorf("group_truth = %q, want %q", got, repositoryGroupTruthMissing)
	}
}

// TestRepositoryDependencyTargetSet proves repositoryDependencyTargetSet
// marks exactly the repositories that appear as the TARGET of a DEPENDS_ON
// edge (i.e. the ones some other repository depends on), matching the
// is_dependency contract documented in openapi/components.go: "True when at
// least one other repository depends on this one, i.e. it is the target of
// an admitted Repository-[:DEPENDS_ON]->Repository edge." A repository that
// only appears as a source (it depends on something else) must not be
// marked.
func TestRepositoryDependencyTargetSet(t *testing.T) {
	t.Parallel()

	edges := []repositoryDependencyEdge{
		{Source: "repository:app", Target: "repository:lib"},
		{Source: "repository:app", Target: "repository:shared"},
		{Source: "repository:shared", Target: "repository:base"},
	}
	targets := repositoryDependencyTargetSet(edges)

	for _, id := range []string{"repository:lib", "repository:shared", "repository:base"} {
		if _, ok := targets[id]; !ok {
			t.Errorf("%s missing from target set, want present (it is a DEPENDS_ON target)", id)
		}
	}
	if _, ok := targets["repository:app"]; ok {
		t.Errorf("repository:app present in target set, want absent (it only appears as a source)")
	}
}

// TestRepositoryDependencyTargetSetEmptyEdges proves an empty or nil edge
// list produces an empty (non-nil) target set rather than panicking.
func TestRepositoryDependencyTargetSetEmptyEdges(t *testing.T) {
	t.Parallel()

	if targets := repositoryDependencyTargetSet(nil); len(targets) != 0 {
		t.Fatalf("nil edge list produced %d targets, want 0", len(targets))
	}
}

// TestLoadRepositoryDependencyEdgesNilGraph proves loadRepositoryDependencyEdges
// degrades to no edges, untruncated, with no error, rather than panicking
// when graph is nil.
func TestLoadRepositoryDependencyEdgesNilGraph(t *testing.T) {
	t.Parallel()

	result := loadRepositoryDependencyEdges(context.Background(), nil, querycontract.RepositoryAccessFilter{AllScopes: true})
	if len(result.Edges) != 0 {
		t.Fatalf("nil graph produced %d edges, want 0", len(result.Edges))
	}
	if result.Truncated {
		t.Error("nil graph reported Truncated = true, want false")
	}
	if result.Err != nil {
		t.Errorf("nil graph reported Err = %v, want nil", result.Err)
	}
}

// TestLoadRepositoryDependencyEdgesReportsQueryError proves a graph.Run
// failure is surfaced on Err with no edges and Truncated=false, rather than
// silently swallowed as it was before -- see
// logRepositoryDependencyEdgesDegradation, which callers use to turn this
// into a disclosed (not silent) is_dependency=false response.
func TestLoadRepositoryDependencyEdgesReportsQueryError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("graph unavailable")
	reader := querytestutil.FakeRepoGraphReader{
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return nil, wantErr
		},
	}
	result := loadRepositoryDependencyEdges(context.Background(), reader, querycontract.RepositoryAccessFilter{AllScopes: true})
	if !errors.Is(result.Err, wantErr) {
		t.Fatalf("result.Err = %v, want %v", result.Err, wantErr)
	}
	if len(result.Edges) != 0 {
		t.Errorf("result.Edges = %v, want none on error", result.Edges)
	}
	if result.Truncated {
		t.Error("result.Truncated = true, want false on error")
	}
}

// TestLoadRepositoryDependencyEdgesDetectsTruncation proves the edge
// pre-pass over-fetches one row past repositoryDependencyClusterEdgeLimit
// and, when the bound is hit, clips Edges to the bound and reports
// Truncated=true -- so a caller can disclose that some true dependency
// edges (and therefore some true is_dependency=true repos) may be missing,
// instead of presenting the clipped set as complete.
func TestLoadRepositoryDependencyEdgesDetectsTruncation(t *testing.T) {
	t.Parallel()

	rows := make([]map[string]any, 0, repositoryDependencyClusterEdgeFetchLimit)
	for i := 0; i < repositoryDependencyClusterEdgeFetchLimit; i++ {
		rows = append(rows, map[string]any{
			"source_id": fmt.Sprintf("repository:src-%06d", i),
			"target_id": fmt.Sprintf("repository:dst-%06d", i),
		})
	}
	reader := querytestutil.FakeRepoGraphReader{
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return rows, nil
		},
	}
	result := loadRepositoryDependencyEdges(context.Background(), reader, querycontract.RepositoryAccessFilter{AllScopes: true})
	if !result.Truncated {
		t.Fatal("result.Truncated = false, want true for a read at the fetch-limit-plus-one bound")
	}
	if got, want := len(result.Edges), repositoryDependencyClusterEdgeLimit; got != want {
		t.Fatalf("len(result.Edges) = %d, want %d (clipped to the bound, not the raw %d-row fetch)", got, want, repositoryDependencyClusterEdgeFetchLimit)
	}
	if result.Err != nil {
		t.Errorf("result.Err = %v, want nil (truncation is not a query error)", result.Err)
	}
}

// TestLoadRepositoryDependencyEdgesUntruncatedAtTheBound proves a read that
// returns exactly repositoryDependencyClusterEdgeLimit rows (one under the
// fetch-limit-plus-one bound) is NOT reported truncated: every edge that
// exists was returned.
func TestLoadRepositoryDependencyEdgesUntruncatedAtTheBound(t *testing.T) {
	t.Parallel()

	rows := make([]map[string]any, 0, repositoryDependencyClusterEdgeLimit)
	for i := 0; i < repositoryDependencyClusterEdgeLimit; i++ {
		rows = append(rows, map[string]any{
			"source_id": fmt.Sprintf("repository:src-%06d", i),
			"target_id": fmt.Sprintf("repository:dst-%06d", i),
		})
	}
	reader := querytestutil.FakeRepoGraphReader{
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return rows, nil
		},
	}
	result := loadRepositoryDependencyEdges(context.Background(), reader, querycontract.RepositoryAccessFilter{AllScopes: true})
	if result.Truncated {
		t.Fatal("result.Truncated = true, want false for a read exactly at the bound")
	}
	if got, want := len(result.Edges), repositoryDependencyClusterEdgeLimit; got != want {
		t.Fatalf("len(result.Edges) = %d, want %d", got, want)
	}
}

// TestLogRepositoryDependencyEdgesDegradation proves the degradation logger
// reports (and logs) degraded=true on error or truncation, and
// degraded=false with no log line when the read was clean.
func TestLogRepositoryDependencyEdgesDegradation(t *testing.T) {
	t.Parallel()

	t.Run("clean read logs nothing", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, nil))
		degraded := logRepositoryDependencyEdgesDegradation(context.Background(), logger, "repository_list", repositoryDependencyEdgeRead{})
		if degraded {
			t.Error("degraded = true for a clean read, want false")
		}
		if buf.Len() != 0 {
			t.Errorf("log output for a clean read = %q, want empty", buf.String())
		}
	})

	t.Run("error is logged and reported degraded", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, nil))
		degraded := logRepositoryDependencyEdgesDegradation(context.Background(), logger, "repository_list", repositoryDependencyEdgeRead{Err: errors.New("boom")})
		if !degraded {
			t.Fatal("degraded = false for a read error, want true")
		}
		out := buf.String()
		for _, want := range []string{"dependency-edge pre-pass degraded", "operation=repository_list", "error=true", "truncated=false"} {
			if !strings.Contains(out, want) {
				t.Errorf("log output = %q, want it to contain %q", out, want)
			}
		}
	})

	t.Run("truncation is logged and reported degraded", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, nil))
		edges := []repositoryDependencyEdge{{Source: "repository:a", Target: "repository:b"}}
		degraded := logRepositoryDependencyEdgesDegradation(context.Background(), logger, "catalog_list", repositoryDependencyEdgeRead{Edges: edges, Truncated: true})
		if !degraded {
			t.Fatal("degraded = false for a truncated read, want true")
		}
		out := buf.String()
		for _, want := range []string{"dependency-edge pre-pass degraded", "operation=catalog_list", "truncated=true", "error=false", "edge_count=1"} {
			if !strings.Contains(out, want) {
				t.Errorf("log output = %q, want it to contain %q", out, want)
			}
		}
	})

	t.Run("nil logger reports degraded without logging", func(t *testing.T) {
		degraded := logRepositoryDependencyEdgesDegradation(context.Background(), nil, "repository_list", repositoryDependencyEdgeRead{Truncated: true})
		if !degraded {
			t.Error("degraded = false for a truncated read with a nil logger, want true")
		}
	})
}

// nornicDBAndOrAfterWhitespace matches an AND/OR keyword immediately preceded
// by a newline or tab. NornicDB v1.3.3 mis-evaluates the whole WHERE clause
// when this happens (proven live, schema applied, Go driver): the character
// immediately before AND/OR must be a space, not a bare newline or tab from
// gofmt-style multi-line formatting. "\n  AND" and "\n\t AND" (a space after
// the tab) are fine; "\n\t\t\tAND" and "\nAND" are not.
var nornicDBAndOrAfterWhitespace = regexp.MustCompile(`[\n\t](AND|OR)\b`)

// TestRepositoryDependencyClusterEdgeCypherAndOrPrecededBySpace proves the
// scoped edge pre-pass query's "WHERE %s AND %s" join keeps AND on the same
// line as its left operand (immediately preceded by a space from the format
// string), never directly after the query's leading "\n\t\tWHERE" newline
// and tabs.
func TestRepositoryDependencyClusterEdgeCypherAndOrPrecededBySpace(t *testing.T) {
	t.Parallel()

	scoped := querycontract.RepositoryAccessFilter{
		AllowedRepositoryIDs: []string{"repository:a"},
		Allowed:              map[string]struct{}{"repository:a": {}},
	}
	cypher := repositoryDependencyClusterEdgeCypher(scoped)
	if loc := nornicDBAndOrAfterWhitespace.FindString(cypher); loc != "" {
		t.Fatalf("edge cypher has %q immediately after a newline/tab, which NornicDB v1.3.3 mis-evaluates:\n%s", loc, cypher)
	}
}

// TestRepositoryDependencyClusterEdgeCypherScopesBothEndpoints proves the
// bounded edge pre-pass query labels both endpoints :Repository, fixes the
// DEPENDS_ON relationship type, bounds the result with LIMIT, and — for a scoped
// caller — applies the tenant access predicate to BOTH the source and target
// repository so a scoped caller never learns cluster membership that crosses
// their grant boundary.
func TestRepositoryDependencyClusterEdgeCypherScopesBothEndpoints(t *testing.T) {
	t.Parallel()

	scoped := querycontract.RepositoryAccessFilter{
		AllowedRepositoryIDs: []string{"repository:a"},
		Allowed:              map[string]struct{}{"repository:a": {}},
	}
	cypher := repositoryDependencyClusterEdgeCypher(scoped)

	if !strings.Contains(cypher, "(s:Repository)-[:DEPENDS_ON]->(t:Repository)") {
		t.Fatalf("edge cypher does not anchor both endpoints on :Repository over DEPENDS_ON:\n%s", cypher)
	}
	if !strings.Contains(cypher, "LIMIT") {
		t.Fatalf("edge cypher is not bounded by LIMIT:\n%s", cypher)
	}
	// Both endpoints must carry the scope predicate.
	if strings.Count(cypher, "allowed_repository_ids") < 2 {
		t.Fatalf("edge cypher must scope BOTH source and target repos to the grant; got:\n%s", cypher)
	}
	if !strings.Contains(cypher, "s.id IN $allowed_repository_ids") || !strings.Contains(cypher, "t.id IN $allowed_repository_ids") {
		t.Fatalf("edge cypher must scope both s and t by $allowed_repository_ids:\n%s", cypher)
	}
}

// TestRepositoryDependencyClusterEdgeCypherUnscopedHasNoPredicate proves the
// unscoped (shared/admin/local) caller path adds no tenant predicate, so the
// whole-graph DEPENDS_ON edge set is eligible for clustering.
func TestRepositoryDependencyClusterEdgeCypherUnscopedHasNoPredicate(t *testing.T) {
	t.Parallel()

	cypher := repositoryDependencyClusterEdgeCypher(querycontract.RepositoryAccessFilter{AllScopes: true})
	if strings.Contains(cypher, "allowed_repository_ids") {
		t.Fatalf("unscoped edge cypher must not bind a tenant predicate:\n%s", cypher)
	}
	if !strings.Contains(cypher, "(s:Repository)-[:DEPENDS_ON]->(t:Repository)") {
		t.Fatalf("unscoped edge cypher must still anchor both endpoints:\n%s", cypher)
	}
}

// TestListRepositoriesGroupsByDependencyCluster proves the end-to-end handler
// path: given repos A->B, B->C and isolated D, the list response groups A/B/C
// under one dependency-cluster group_key and leaves D as missing_evidence.
func TestListRepositoriesGroupsByDependencyCluster(t *testing.T) {
	t.Parallel()

	reader := querytestutil.FakeRepoGraphReader{
		RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return map[string]any{"total": int64(4)}, nil
		},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "(s:Repository)-[:DEPENDS_ON]->(t:Repository)"):
				// dependency-cluster edge pre-pass
				return []map[string]any{
					{"source_id": "repository:a", "target_id": "repository:b"},
					{"source_id": "repository:b", "target_id": "repository:c"},
				}, nil
			case strings.Contains(cypher, "MATCH (r:Repository)"):
				// page query
				return []map[string]any{
					{"id": "repository:a", "name": "a"},
					{"id": "repository:b", "name": "b"},
					{"id": "repository:c", "name": "c"},
					{"id": "repository:d", "name": "d"},
				}, nil
			default:
				return []map[string]any{{"total": 4}}, nil
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
	data := envelope.Data.(map[string]any)
	repositories := data["repositories"].([]any)
	if len(repositories) != 4 {
		t.Fatalf("repositories len = %d, want 4", len(repositories))
	}

	groups := map[string]map[string]string{}
	for _, raw := range repositories {
		repo := raw.(map[string]any)
		groups[querycontract.StringVal(repo, "id")] = map[string]string{
			"source": querycontract.StringVal(repo, "group_source"),
			"key":    querycontract.StringVal(repo, "group_key"),
		}
	}

	for _, id := range []string{"repository:a", "repository:b", "repository:c"} {
		if groups[id]["source"] != repositoryGroupSourceDependencyCluster {
			t.Errorf("%s group_source = %q, want %q", id, groups[id]["source"], repositoryGroupSourceDependencyCluster)
		}
		if groups[id]["key"] != "repository:a" {
			t.Errorf("%s group_key = %q, want repository:a", id, groups[id]["key"])
		}
	}
	if groups["repository:d"]["source"] != repositoryGroupSourceMissing {
		t.Errorf("repository:d group_source = %q, want %q", groups["repository:d"]["source"], repositoryGroupSourceMissing)
	}
}

// TestListRepositoriesScopedDependencyClusterMembership proves a scoped caller
// only sees cluster membership computed from edges within their grant. The edge
// pre-pass query the handler issues must scope both endpoints, so a depender or
// dependency outside the grant cannot pull an in-grant repo into a cross-grant
// cluster.
func TestListRepositoriesScopedDependencyClusterMembership(t *testing.T) {
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
				return []map[string]any{}, nil
			case strings.Contains(cypher, "MATCH (r:Repository)"):
				return []map[string]any{{"id": "repository:a", "name": "a"}}, nil
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
		AllowedRepositoryIDs: []string{"repository:a"},
	}))
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	if capturedEdgeCypher == "" {
		t.Fatal("dependency-cluster edge query was not issued for the scoped caller")
	}
	if !strings.Contains(capturedEdgeCypher, "s.id IN $allowed_repository_ids") ||
		!strings.Contains(capturedEdgeCypher, "t.id IN $allowed_repository_ids") {
		t.Fatalf("scoped edge query does not scope both endpoints to the grant:\n%s", capturedEdgeCypher)
	}
}
