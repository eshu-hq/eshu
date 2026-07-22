// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// stubLiveInstanceGraphQuery is the unit-test GraphQuery substitute for
// fetchLiveInstanceEnvironments: it records the Cypher/params it received and
// returns canned rows, mirroring the stubGraphQuery pattern used elsewhere in
// this package (package_registry_aggregates_store_test.go) under a distinct
// name to avoid a package-level type collision.
type stubLiveInstanceGraphQuery struct {
	rows []map[string]any
	err  error
	// cypher and params record the last call; fetchLiveInstanceEnvironments
	// issues one Run call per distinct namespace pair with scalar params
	// (cluster_id/namespace), never an UNWIND over map rows -- NornicDB does not
	// project pair.<field> after unwinding maps.
	cypher string
	params map[string]any
	calls  int
}

func (s *stubLiveInstanceGraphQuery) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	s.calls++
	s.cypher = cypher
	s.params = params
	if s.err != nil {
		return nil, s.err
	}
	return s.rows, nil
}

func (s *stubLiveInstanceGraphQuery) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, fmt.Errorf("RunSingle not used by live-instance environment lookup")
}

// TestFetchWorkloadLiveInstanceSummaryTwoDistinctNamespacePairs proves the
// per-pair environment loop over N>1 distinct (cluster_id, namespace) pairs:
// two tracking-ids matched in two different namespaces produce two environment
// entries, each carrying its OWN pair's cluster_id/namespace (attached in Go,
// never from Cypher), and issue exactly one graph query per distinct pair.
func TestFetchWorkloadLiveInstanceSummaryTwoDistinctNamespacePairs(t *testing.T) {
	t.Parallel()

	controllers := []map[string]any{argoCDControllerFixture("app-a")}
	resources := []map[string]any{
		k8sResourceFixture("Deployment", "workload-a", "ns-a", "apps/v1"),
		k8sResourceFixture("Deployment", "workload-b", "ns-b", "apps/v1"),
	}
	trackingIDs := expectedArgoCDTrackingIDs(controllers, resources)
	if len(trackingIDs) != 2 {
		t.Fatalf("test fixture bug: want 2 tracking ids, got %d", len(trackingIDs))
	}
	store := &stubKubernetesPodTemplateListStore{
		matchesByTrackingID: map[string][]LiveIdentityMatch{
			trackingIDs[0]: {{ClusterID: "c1", Namespace: "ns-a", ReadyReplicas: int32Ptr(3)}},
			trackingIDs[1]: {{ClusterID: "c1", Namespace: "ns-b", ReadyReplicas: int32Ptr(3)}},
		},
	}
	// No KubernetesNamespace node for either pair -> zero rows each -> both
	// default to environment-unbound; the point is the loop runs once per pair.
	graph := &stubLiveInstanceGraphQuery{rows: []map[string]any{}}
	h := &ImpactHandler{KubernetesPodTemplates: store, Neo4j: graph}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), controllers, resources, []string{"img@sha256:shared"}, repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary == nil || summary.count != 6 {
		t.Fatalf("summary = %v, want count 6 (3 + 3)", summary)
	}
	if graph.calls != 2 {
		t.Fatalf("graph queried %d times, want exactly 2 (one per distinct namespace pair)", graph.calls)
	}
	if len(summary.environments) != 2 {
		t.Fatalf("environments = %v, want exactly 2 entries", summary.environments)
	}
	gotNamespaces := map[string]bool{}
	for _, env := range summary.environments {
		if env["cluster_id"] != "c1" {
			t.Fatalf("cluster_id = %v, want c1", env["cluster_id"])
		}
		if env["state"] != "environment-unbound" {
			t.Fatalf("state = %v, want environment-unbound", env["state"])
		}
		gotNamespaces[fmt.Sprint(env["namespace"])] = true
	}
	if !gotNamespaces["ns-a"] || !gotNamespaces["ns-b"] {
		t.Fatalf("namespaces = %v, want both ns-a and ns-b", gotNamespaces)
	}
}

// TestFetchWorkloadLiveInstanceSummaryEnvironmentBound proves the full path
// through fetchLiveInstanceEnvironments: a matched fact's (cluster_id,
// namespace) resolves to a bound KubernetesNamespace/Environment pair.
func TestFetchWorkloadLiveInstanceSummaryEnvironmentBound(t *testing.T) {
	t.Parallel()

	controllers, resources, trackingID := singleTrackingIDFixture("app-a", "Deployment", "workload-a", "ns", "apps/v1")
	store := &stubKubernetesPodTemplateListStore{
		matchesByTrackingID: map[string][]LiveIdentityMatch{
			trackingID: {{ClusterID: "supply-chain-demo", Namespace: "default", ReadyReplicas: int32Ptr(3)}},
		},
	}
	// The real query returns ONLY environment_state/environment_name; cluster_id
	// and namespace are attached in Go from the pair (NornicDB will not project
	// pair.<field> after an UNWIND of maps). The stub mirrors that shape so the
	// test would have caught the projection gap the golden gate found.
	graph := &stubLiveInstanceGraphQuery{rows: []map[string]any{
		{"environment_state": "bound", "environment_name": "prod"},
	}}
	h := &ImpactHandler{KubernetesPodTemplates: store, Neo4j: graph}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), controllers, resources, []string{"img@sha256:a"}, repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary == nil {
		t.Fatal("summary = nil, want non-nil")
	}
	if len(summary.environments) != 1 {
		t.Fatalf("environments = %v, want exactly 1 entry", summary.environments)
	}
	env := summary.environments[0]
	if env["state"] != "bound" {
		t.Fatalf("state = %v, want bound", env["state"])
	}
	if env["environment"] != "prod" {
		t.Fatalf("environment = %v, want prod", env["environment"])
	}
	if env["cluster_id"] != "supply-chain-demo" || env["namespace"] != "default" {
		t.Fatalf("cluster_id/namespace = %v/%v, want supply-chain-demo/default", env["cluster_id"], env["namespace"])
	}
	if graph.calls != 1 {
		t.Fatalf("graph queried %d times, want exactly 1", graph.calls)
	}
	for _, forbidden := range []string{"MERGE", "CREATE"} {
		if containsCypherKeyword(graph.cypher, forbidden) {
			t.Fatalf("environment lookup Cypher contains %q, must be MATCH-only:\n%s", forbidden, graph.cypher)
		}
	}
}

// TestLiveInstanceNamespaceEnvironmentQueryIsMatchOnly is a static assertion
// on liveInstanceNamespaceEnvironmentQuery itself, independent of any test
// double: the committed query text must never contain MERGE or CREATE. This
// guards the query constant directly, not just what a particular stub
// happened to be called with.
func TestLiveInstanceNamespaceEnvironmentQueryIsMatchOnly(t *testing.T) {
	t.Parallel()

	for _, forbidden := range []string{"MERGE", "CREATE"} {
		if containsCypherKeyword(liveInstanceNamespaceEnvironmentQuery, forbidden) {
			t.Fatalf("liveInstanceNamespaceEnvironmentQuery contains %q, must be MATCH-only:\n%s", forbidden, liveInstanceNamespaceEnvironmentQuery)
		}
	}
	for _, want := range []string{"MATCH (n:KubernetesNamespace {cluster_id: $cluster_id, namespace: $namespace})", "OPTIONAL MATCH (n)-[:TARGETS_ENVIRONMENT]->(env:Environment)"} {
		if !strings.Contains(liveInstanceNamespaceEnvironmentQuery, want) {
			t.Fatalf("liveInstanceNamespaceEnvironmentQuery missing %q:\n%s", want, liveInstanceNamespaceEnvironmentQuery)
		}
	}
}

// TestFetchWorkloadLiveInstanceSummaryEnvironmentUnboundExistingNode proves
// the existing-but-unbound case: a KubernetesNamespace node exists (its
// environment_state property is the literal "environment-unbound" string
// environment.StateEnvironmentUnbound) and carries no environment.
func TestFetchWorkloadLiveInstanceSummaryEnvironmentUnboundExistingNode(t *testing.T) {
	t.Parallel()

	controllers, resources, trackingID := singleTrackingIDFixture("app-a", "Deployment", "workload-a", "ns", "apps/v1")
	store := &stubKubernetesPodTemplateListStore{
		matchesByTrackingID: map[string][]LiveIdentityMatch{
			trackingID: {{ClusterID: "supply-chain-demo", Namespace: "default", ReadyReplicas: int32Ptr(3)}},
		},
	}
	graph := &stubLiveInstanceGraphQuery{rows: []map[string]any{
		{"environment_state": "environment-unbound", "environment_name": nil},
	}}
	h := &ImpactHandler{KubernetesPodTemplates: store, Neo4j: graph}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), controllers, resources, []string{"img@sha256:a"}, repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	env := summary.environments[0]
	if env["state"] != "environment-unbound" {
		t.Fatalf("state = %v, want environment-unbound", env["state"])
	}
	if _, ok := env["environment"]; ok {
		t.Fatalf("environment key must be absent for an unbound namespace, got %v", env["environment"])
	}
}

// TestFetchWorkloadLiveInstanceSummaryEnvironmentNoNodeDefaultsUnbound proves
// the no-node-at-all default: when no KubernetesNamespace node exists for a
// pair, the driving MATCH returns ZERO rows and the Go side defaults to
// unbound, never invents a bound environment.
func TestFetchWorkloadLiveInstanceSummaryEnvironmentNoNodeDefaultsUnbound(t *testing.T) {
	t.Parallel()

	controllers, resources, trackingID := singleTrackingIDFixture("app-a", "Deployment", "workload-a", "ns", "apps/v1")
	store := &stubKubernetesPodTemplateListStore{
		matchesByTrackingID: map[string][]LiveIdentityMatch{
			trackingID: {{ClusterID: "supply-chain-demo", Namespace: "production", ReadyReplicas: int32Ptr(3)}},
		},
	}
	// No KubernetesNamespace node -> the driving MATCH returns zero rows.
	graph := &stubLiveInstanceGraphQuery{rows: []map[string]any{}}
	h := &ImpactHandler{KubernetesPodTemplates: store, Neo4j: graph}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), controllers, resources, []string{"img@sha256:a"}, repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	env := summary.environments[0]
	if env["state"] != "environment-unbound" {
		t.Fatalf("state = %v, want environment-unbound", env["state"])
	}
	if _, ok := env["environment"]; ok {
		t.Fatal("environment key must be absent when no namespace node exists")
	}
}
