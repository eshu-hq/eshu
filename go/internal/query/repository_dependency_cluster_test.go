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

	if got := StringVal(decorated, "group_source"); got != repositoryGroupSourceDependencyCluster {
		t.Fatalf("group_source = %q, want %q", got, repositoryGroupSourceDependencyCluster)
	}
	if got := StringVal(decorated, "group_kind"); got != "cluster" {
		t.Errorf("group_kind = %q, want cluster", got)
	}
	if got := StringVal(decorated, "group_truth"); got != repositoryGroupTruthDerived {
		t.Errorf("group_truth = %q, want %q", got, repositoryGroupTruthDerived)
	}
	if got := StringVal(decorated, "group_key"); got != "repository:a" {
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

	if got := StringVal(decorated, "group_source"); got != repositoryGroupSourceMissing {
		t.Fatalf("group_source = %q, want %q", got, repositoryGroupSourceMissing)
	}
	if got := StringVal(decorated, "group_truth"); got != repositoryGroupTruthMissing {
		t.Errorf("group_truth = %q, want %q", got, repositoryGroupTruthMissing)
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

	scoped := repositoryAccessFilter{
		allowedRepositoryIDs: []string{"repository:a"},
		allowed:              map[string]struct{}{"repository:a": {}},
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

	cypher := repositoryDependencyClusterEdgeCypher(repositoryAccessFilter{allScopes: true})
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

	reader := fakeRepoGraphReader{
		runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
			return map[string]any{"total": int64(4)}, nil
		},
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
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

	handler := &RepositoryHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=10", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}

	var envelope ResponseEnvelope
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
		groups[StringVal(repo, "id")] = map[string]string{
			"source": StringVal(repo, "group_source"),
			"key":    StringVal(repo, "group_key"),
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
	reader := fakeRepoGraphReader{
		runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
			return map[string]any{"total": int64(1)}, nil
		},
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
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

	handler := &RepositoryHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=10", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
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
