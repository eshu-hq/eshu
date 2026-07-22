// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// stubKubernetesPodTemplateListStore is a test fake implementing
// KubernetesPodTemplateStore for fetchWorkloadLiveInstanceSummary's tests: it
// records every filter ListLiveIdentityMatches was called with and returns
// canned matches keyed by TrackingID. HasLiveIdentityMatch is never called by
// the probe under test here; it panics if reached so a wiring mistake fails
// loudly instead of silently returning a wrong bool.
type stubKubernetesPodTemplateListStore struct {
	matchesByTrackingID map[string][]LiveIdentityMatch
	err                 error
	calls               []KubernetesPodTemplateFilter
}

func (s *stubKubernetesPodTemplateListStore) HasLiveIdentityMatch(
	context.Context,
	KubernetesPodTemplateFilter,
) (bool, error) {
	panic("HasLiveIdentityMatch must not be called by fetchWorkloadLiveInstanceSummary")
}

func (s *stubKubernetesPodTemplateListStore) ListLiveIdentityMatches(
	_ context.Context,
	filter KubernetesPodTemplateFilter,
) ([]LiveIdentityMatch, error) {
	s.calls = append(s.calls, filter)
	if s.err != nil {
		return nil, s.err
	}
	return s.matchesByTrackingID[filter.TrackingID], nil
}

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

// singleTrackingIDFixture builds the controllers/k8sResources pair that
// expectedArgoCDTrackingIDs resolves to exactly one tracking-id, matching the
// argoCDControllerFixture/k8sResourceFixture helpers
// (impact_trace_deployment_live_evidence_test.go).
func singleTrackingIDFixture(appName, kind, name, namespace, apiVersion string) (
	[]map[string]any, []map[string]any, string,
) {
	controllers := []map[string]any{argoCDControllerFixture(appName)}
	resources := []map[string]any{k8sResourceFixture(kind, name, namespace, apiVersion)}
	trackingIDs := expectedArgoCDTrackingIDs(controllers, resources)
	if len(trackingIDs) != 1 {
		panic(fmt.Sprintf("test fixture bug: want exactly 1 tracking id, got %d", len(trackingIDs)))
	}
	return controllers, resources, trackingIDs[0]
}

func int32Ptr(v int32) *int32 { return &v }

func TestFetchWorkloadLiveInstanceSummaryNilHandler(t *testing.T) {
	t.Parallel()

	var h *ImpactHandler
	summary, err := h.fetchWorkloadLiveInstanceSummary(t.Context(), nil, nil, nil, repositoryAccessFilter{})
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary != nil {
		t.Fatal("nil handler returned a non-nil summary, want nil")
	}
}

// TestFetchWorkloadLiveInstanceSummaryNoIdentityNeverQueriesStore proves the
// same fail-closed anchor-first discipline as
// TestFetchWorkloadLiveEvidenceNoArgoCDControllerNeverQueriesStore: no
// resolvable ArgoCD identity means the store is never queried and the count
// is absent.
func TestFetchWorkloadLiveInstanceSummaryNoIdentityNeverQueriesStore(t *testing.T) {
	t.Parallel()

	store := &stubKubernetesPodTemplateListStore{}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(),
		nil, // no controllers at all
		[]map[string]any{k8sResourceFixture("Deployment", "workload-a", "shared-ns", "apps/v1")},
		[]string{"ghcr.io/eshu-hq/supply-chain-demo@sha256:shared"},
		repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary != nil {
		t.Fatal("no ArgoCD controller returned a non-nil summary, want nil")
	}
	if got := len(store.calls); got != 0 {
		t.Fatalf("store was queried %d times, want 0 (fail-closed at the identity layer)", got)
	}
}

func TestFetchWorkloadLiveInstanceSummaryNilStore(t *testing.T) {
	t.Parallel()

	controllers, resources, _ := singleTrackingIDFixture("app-a", "Deployment", "workload-a", "ns", "apps/v1")
	h := &ImpactHandler{} // KubernetesPodTemplates is nil
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), controllers, resources, []string{"img:latest"}, repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary != nil {
		t.Fatal("nil store returned a non-nil summary, want nil")
	}
}

func TestFetchWorkloadLiveInstanceSummaryEmptyImageRefs(t *testing.T) {
	t.Parallel()

	controllers, resources, _ := singleTrackingIDFixture("app-a", "Deployment", "workload-a", "ns", "apps/v1")
	store := &stubKubernetesPodTemplateListStore{}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), controllers, resources, nil, repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary != nil {
		t.Fatal("empty image_refs returned a non-nil summary, want nil")
	}
	if got := len(store.calls); got != 0 {
		t.Fatalf("store was queried %d times, want 0", got)
	}
}

func TestFetchWorkloadLiveInstanceSummaryEmptyAccess(t *testing.T) {
	t.Parallel()

	controllers, resources, trackingID := singleTrackingIDFixture("app-a", "Deployment", "workload-a", "ns", "apps/v1")
	store := &stubKubernetesPodTemplateListStore{
		matchesByTrackingID: map[string][]LiveIdentityMatch{
			trackingID: {{ClusterID: "c1", Namespace: "ns", ReadyReplicas: int32Ptr(3)}},
		},
	}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), controllers, resources, []string{"img@sha256:a"}, repositoryAccessFilter{}, // scoped, no grants
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary != nil {
		t.Fatal("empty access filter returned a non-nil summary, want nil")
	}
	if got := len(store.calls); got != 0 {
		t.Fatalf("store was queried %d times, want 0", got)
	}
}

// TestFetchWorkloadLiveInstanceSummaryMaxNotSum is the anti-double-count RED
// test: a Deployment and its ReplicaSet share ONE tracking-id (the
// Deployment copies its annotations onto the ReplicaSet it owns) and both
// report ready_replicas=3. The correct aggregation is MAX (one running
// Deployment observed twice, from two different object kinds) yielding 3;
// summing the two matched facts would wrongly double-count to 6.
func TestFetchWorkloadLiveInstanceSummaryMaxNotSum(t *testing.T) {
	t.Parallel()

	controllers, resources, trackingID := singleTrackingIDFixture("deployable-source", "Deployment", "deployable-source", "production", "apps/v1")
	store := &stubKubernetesPodTemplateListStore{
		matchesByTrackingID: map[string][]LiveIdentityMatch{
			trackingID: {
				{ClusterID: "supply-chain-demo", Namespace: "default", ReadyReplicas: int32Ptr(3)}, // Deployment
				{ClusterID: "supply-chain-demo", Namespace: "default", ReadyReplicas: int32Ptr(3)}, // ReplicaSet, same tracking-id
			},
		},
	}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), controllers, resources,
		[]string{"ghcr.io/eshu-hq/supply-chain-demo@sha256:shared"},
		repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary == nil {
		t.Fatal("summary = nil, want non-nil (an observation was made)")
	}
	if summary.count != 3 {
		t.Fatalf("count = %d, want 3 (MAX across same-tracking-id matches, not SUM which would give 6)", summary.count)
	}
}

// TestFetchWorkloadLiveInstanceSummaryTwoTrackingIDsSum proves the second
// half of the aggregation contract: two DISTINCT tracking-ids (two different
// declared k8sResources under the traced workload) each contribute their own
// max, and the totals are summed across tracking-ids.
func TestFetchWorkloadLiveInstanceSummaryTwoTrackingIDsSum(t *testing.T) {
	t.Parallel()

	controllers := []map[string]any{argoCDControllerFixture("app-a")}
	resources := []map[string]any{
		k8sResourceFixture("Deployment", "workload-a", "ns", "apps/v1"),
		k8sResourceFixture("Deployment", "workload-b", "ns", "apps/v1"),
	}
	trackingIDs := expectedArgoCDTrackingIDs(controllers, resources)
	if len(trackingIDs) != 2 {
		t.Fatalf("test fixture bug: want 2 tracking ids, got %d", len(trackingIDs))
	}
	store := &stubKubernetesPodTemplateListStore{
		matchesByTrackingID: map[string][]LiveIdentityMatch{
			trackingIDs[0]: {{ClusterID: "c1", Namespace: "ns", ReadyReplicas: int32Ptr(2)}},
			trackingIDs[1]: {{ClusterID: "c1", Namespace: "ns", ReadyReplicas: int32Ptr(5)}},
		},
	}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), controllers, resources, []string{"img@sha256:shared"}, repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary == nil {
		t.Fatal("summary = nil, want non-nil")
	}
	if summary.count != 7 {
		t.Fatalf("count = %d, want 7 (2 + 5 summed across distinct tracking-ids)", summary.count)
	}
}

// TestFetchWorkloadLiveInstanceSummaryAllNilReadyReplicasOmitsCount proves a
// matched-but-unobserved case (every matched fact carries a nil
// ReadyReplicas, e.g. bare Pod objects) never fabricates a count: absent
// stays absent.
func TestFetchWorkloadLiveInstanceSummaryAllNilReadyReplicasOmitsCount(t *testing.T) {
	t.Parallel()

	controllers, resources, trackingID := singleTrackingIDFixture("app-a", "Deployment", "workload-a", "ns", "apps/v1")
	store := &stubKubernetesPodTemplateListStore{
		matchesByTrackingID: map[string][]LiveIdentityMatch{
			trackingID: {
				{ClusterID: "c1", Namespace: "ns", ReadyReplicas: nil},
				{ClusterID: "c1", Namespace: "ns", ReadyReplicas: nil},
			},
		},
	}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), controllers, resources, []string{"img@sha256:a"}, repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary != nil {
		t.Fatal("all-nil ready_replicas returned a non-nil summary, want nil (absent, never fabricated)")
	}
}

// TestFetchWorkloadLiveInstanceSummaryReadyZeroIsPresent proves a real
// scaled-to-zero observation (ready_replicas present and 0) is reported as a
// present 0, not treated the same as "no observation".
func TestFetchWorkloadLiveInstanceSummaryReadyZeroIsPresent(t *testing.T) {
	t.Parallel()

	controllers, resources, trackingID := singleTrackingIDFixture("app-a", "Deployment", "workload-a", "ns", "apps/v1")
	store := &stubKubernetesPodTemplateListStore{
		matchesByTrackingID: map[string][]LiveIdentityMatch{
			trackingID: {{ClusterID: "c1", Namespace: "ns", ReadyReplicas: int32Ptr(0)}},
		},
	}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), controllers, resources, []string{"img@sha256:a"}, repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary == nil {
		t.Fatal("summary = nil, want non-nil (ready_replicas=0 is a real, present observation)")
	}
	if summary.count != 0 {
		t.Fatalf("count = %d, want 0", summary.count)
	}
}

// TestFetchWorkloadLiveInstanceSummaryStoreError proves a store failure
// returns a nil summary and the error, so the call site can log-and-continue
// without setting the count/environment response fields.
func TestFetchWorkloadLiveInstanceSummaryStoreError(t *testing.T) {
	t.Parallel()

	controllers, resources, _ := singleTrackingIDFixture("app-a", "Deployment", "workload-a", "ns", "apps/v1")
	store := &stubKubernetesPodTemplateListStore{err: fmt.Errorf("postgres offline")}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), controllers, resources, []string{"img@sha256:a"}, repositoryAccessFilter{allScopes: true},
	)
	if err == nil {
		t.Fatal("store error must be surfaced, got nil")
	}
	if summary != nil {
		t.Fatal("store error returned a non-nil summary, want nil")
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
