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

// TestInfraResourceScopePredicateAdmitsDeploymentTopology pins the scope
// predicate shape: it must authorize a Repository neighbor by its own id and a
// WorkloadInstance anchored to a granted repo via DEFINES/INSTANCE_OF, in
// addition to the pre-existing repo_id and USES-path admissions. Without these
// the deployment-source topology is dropped under scope (#3519).
func TestInfraResourceScopePredicateAdmitsDeploymentTopology(t *testing.T) {
	t.Parallel()

	pred := infraResourceScopePredicate("target")

	// Pre-existing admissions stay intact.
	if !strings.Contains(pred, "target.repo_id IN $allowed_repository_ids") {
		t.Fatalf("predicate lost the repo_id admission:\n%s", pred)
	}
	if !strings.Contains(pred, "-[:USES]->(target)") {
		t.Fatalf("predicate lost the USES-path admission:\n%s", pred)
	}
	// New: Repository neighbor authorized by its own id.
	if !strings.Contains(pred, "target.id IN $allowed_repository_ids") ||
		!strings.Contains(pred, "target.id IN $allowed_scope_ids") {
		t.Fatalf("predicate missing Repository-by-id admission:\n%s", pred)
	}
	// New: WorkloadInstance anchored to a granted repo via DEFINES/INSTANCE_OF
	// (no USES hop), so a deployment-source seed/neighbor instance is in scope.
	if !strings.Contains(pred, "-[:DEFINES]->(:Workload)<-[:INSTANCE_OF]-(target)") {
		t.Fatalf("predicate missing WorkloadInstance DEFINES/INSTANCE_OF admission:\n%s", pred)
	}
}
