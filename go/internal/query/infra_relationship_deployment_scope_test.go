// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// deploymentScopeGraph is a Cypher-interpreting fake graph reader for the
// scoped what_deploys regression. It models one
// WorkloadInstance-[:DEPLOYMENT_SOURCE]->Repository edge whose anchoring repo is
// either in-grant or out-of-grant, and admits the neighbor only when the
// generated neighbor predicate would authorize that repository. It inspects the
// real handler Cypher rather than returning canned rows, so the test proves the
// scope predicate admits the deployment-source topology end to end.
type deploymentScopeGraph struct {
	// neighborRepoID is the DEPLOYMENT_SOURCE target Repository id.
	neighborRepoID string
	// grantInScope reports whether neighborRepoID is in the token grant.
	grantInScope bool
	lastCypher   string
}

func (g *deploymentScopeGraph) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (g *deploymentScopeGraph) RunSingle(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
	g.lastCypher = cypher

	// The seed WorkloadInstance is in-grant (anchor passes). Model the
	// DEPLOYMENT_SOURCE -> Repository neighbor: it binds only when the neighbor
	// predicate admits an in-grant Repository by its own id. Before the fix the
	// predicate checked only target.repo_id (null on a Repository node) and the
	// USES path (a Repository is not a USES target), so the neighbor never bound.
	neighborAdmitted := g.grantInScope &&
		strings.Contains(cypher, "target.id IN $allowed_repository_ids")

	outgoing := []any{}
	if neighborAdmitted {
		outgoing = []any{map[string]any{
			"direction":     "outgoing",
			"type":          "DEPLOYMENT_SOURCE",
			"target_name":   "deploy-repo",
			"target_id":     g.neighborRepoID,
			"target_labels": []any{"Repository"},
		}}
	}
	return map[string]any{
		"id":       "instance:eshu",
		"name":     "eshu",
		"labels":   []any{"WorkloadInstance"},
		"outgoing": outgoing,
		"incoming": []any{},
	}, nil
}

func scopedDeploymentRequest(entityID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/relationships", strings.NewReader(`{"entity_id":"`+entityID+`","relationship_type":"what_deploys"}`))
	return req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
}

// TestScopedWhatDeploysReturnsInGrantDeploymentSource is the #3519 follow-up
// regression guard: a scoped what_deploys call on an in-grant WorkloadInstance
// must return its WorkloadInstance-[:DEPLOYMENT_SOURCE]->Repository edge when the
// target repository is in grant. Before the scope-predicate fix the neighbor
// predicate dropped the Repository neighbor (it carries id, not repo_id, and is
// not a USES target), so the edge was silently excluded under scope.
func TestScopedWhatDeploysReturnsInGrantDeploymentSource(t *testing.T) {
	t.Parallel()

	graph := &deploymentScopeGraph{neighborRepoID: "repo-team-a", grantInScope: true}
	handler := &InfraHandler{Neo4j: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, scopedDeploymentRequest("instance:eshu"))

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	payload := decodeInfraData(t, rec.Body.Bytes())
	outgoing, ok := payload["outgoing"].([]any)
	if !ok || len(outgoing) != 1 {
		t.Fatalf("scoped what_deploys dropped the in-grant DEPLOYMENT_SOURCE edge: outgoing = %#v", payload["outgoing"])
	}
	edge, _ := outgoing[0].(map[string]any)
	if got := StringVal(edge, "type"); got != "DEPLOYMENT_SOURCE" {
		t.Fatalf("outgoing[0].type = %q, want DEPLOYMENT_SOURCE", got)
	}
}

// TestScopedWhatDeploysExcludesOutOfGrantDeploymentSource proves the fix does
// not over-authorize: a DEPLOYMENT_SOURCE edge whose target repository is NOT in
// grant is still excluded under scope.
func TestScopedWhatDeploysExcludesOutOfGrantDeploymentSource(t *testing.T) {
	t.Parallel()

	graph := &deploymentScopeGraph{neighborRepoID: "repo-team-b", grantInScope: false}
	handler := &InfraHandler{Neo4j: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, scopedDeploymentRequest("instance:eshu"))

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	payload := decodeInfraData(t, rec.Body.Bytes())
	outgoing, ok := payload["outgoing"].([]any)
	if !ok || len(outgoing) != 0 {
		t.Fatalf("scoped what_deploys leaked an out-of-grant DEPLOYMENT_SOURCE edge: outgoing = %#v", payload["outgoing"])
	}
}

// TestInfraResourceScopePredicateAdmitsDeploymentTopology pins the SHAPE-A
// scope predicate (#5384): a Repository neighbor is authorized by its own id, a
// CloudResource by its using WorkloadInstance's repo_id (inline-map), a
// WorkloadInstance by its own repo_id or a forward DEPLOYMENT_SOURCE to a
// granted repo, and a collision-defined Workload by a granted DEFINES-ing
// repository (inline-map). The dead n-last bridge and the always-true
// backward-EXISTS-WHERE shapes must be absent (both mis-evaluate on the pinned
// NornicDB build).
func TestInfraResourceScopePredicateAdmitsDeploymentTopology(t *testing.T) {
	t.Parallel()

	scalars := []string{"repo-team-a"}
	pred := infraResourceScopePredicate("target", scalars)

	for _, want := range []string{
		// Direct ownership (flat).
		"target.repo_id IN $allowed_repository_ids",
		"target.id IN $allowed_repository_ids",
		"target.id IN $allowed_scope_ids",
		// CloudResource via its using instance's repo_id (inline-map).
		"(target)<-[:USES]-(:WorkloadInstance {repo_id:$scope_grant_0})",
		// WorkloadInstance via forward DEPLOYMENT_SOURCE to a granted repo.
		"EXISTS { MATCH (target)-[:DEPLOYMENT_SOURCE]->(scopeDeployRepo:Repository)",
		// Collision-defined Workload via a granted DEFINES-ing repository.
		"(target)<-[:DEFINES]-(:Repository {id:$scope_grant_0})",
	} {
		if !strings.Contains(pred, want) {
			t.Fatalf("predicate missing SHAPE-A admission %q:\n%s", want, pred)
		}
	}
	for _, forbidden := range []string{
		"-[:DEFINES]->(:Workload)<-[:INSTANCE_OF]",
		"(target)<-[:USES]-(scopeInstance",
		"scopeRepo.id IN",
	} {
		if strings.Contains(pred, forbidden) {
			t.Fatalf("predicate must not contain forbidden shape %q:\n%s", forbidden, pred)
		}
	}
}
