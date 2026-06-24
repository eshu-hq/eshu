// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// runtimeInstanceGraphStub answers the loader's two scalar queries: the
// WorkloadInstance read (filtered by the repo_id param) and the RUNS_ON platform
// read (filtered by the instance_ids param). It records the cypher it saw so
// tests can assert the NornicDB-portable shape (no OPTIONAL MATCH, indexed
// anchors, no generation-bearing reads).
type runtimeInstanceGraphStub struct {
	instancesByRepo map[string][]map[string]any
	platformsByInst map[string][]map[string]any
	seenCypher      []string
	instanceCalls   int
	platformCalls   int
	err             error
}

func (s *runtimeInstanceGraphStub) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	s.seenCypher = append(s.seenCypher, cypher)
	if s.err != nil {
		return nil, s.err
	}
	switch {
	case strings.Contains(cypher, "RUNS_ON"):
		s.platformCalls++
		ids := map[string]bool{}
		if raw, ok := params["instance_ids"].([]string); ok {
			for _, id := range raw {
				ids[id] = true
			}
		}
		out := []map[string]any{}
		for instanceID, rows := range s.platformsByInst {
			if ids[instanceID] {
				out = append(out, rows...)
			}
		}
		return out, nil
	default:
		s.instanceCalls++
		repoID := anyToString(params["repo_id"])
		return s.instancesByRepo[repoID], nil
	}
}

func TestGraphServiceRuntimeInstanceLoaderGroupsByRepo(t *testing.T) {
	t.Parallel()

	graph := &runtimeInstanceGraphStub{
		instancesByRepo: map[string][]map[string]any{
			"repo-checkout": {
				{"repo_id": "repo-checkout", "instance_id": "workload-instance:checkout:prod", "workload_name": "checkout", "environment": "prod", "materialization_confidence": 0.9},
				{"repo_id": "repo-checkout", "instance_id": "workload-instance:checkout:staging", "workload_name": "checkout", "environment": "staging", "materialization_confidence": 0.7},
			},
			"repo-payments": {
				{"repo_id": "repo-payments", "instance_id": "workload-instance:payments:prod", "workload_name": "payments", "environment": "prod", "materialization_confidence": 0.95},
			},
		},
		platformsByInst: map[string][]map[string]any{
			"workload-instance:checkout:prod":    {{"instance_id": "workload-instance:checkout:prod", "platform_kind": "kubernetes", "platform_name": "prod-cluster"}},
			"workload-instance:checkout:staging": {{"instance_id": "workload-instance:checkout:staging", "platform_kind": "ecs", "platform_name": "staging-ecs"}},
			"workload-instance:payments:prod":    {{"instance_id": "workload-instance:payments:prod", "platform_kind": "kubernetes", "platform_name": "prod-cluster"}},
		},
	}

	lookup := GraphServiceRuntimeInstanceLoader{Graph: graph}
	result, err := lookup.GetRuntimeInstancesForRepos(context.Background(), []string{"repo-checkout", "repo-payments"})
	if err != nil {
		t.Fatalf("GetRuntimeInstancesForRepos() error = %v", err)
	}

	joined := strings.Join(graph.seenCypher, "\n")
	if !strings.Contains(joined, "WorkloadInstance") || !strings.Contains(joined, "RUNS_ON") {
		t.Fatalf("queries missing WorkloadInstance/RUNS_ON anchors: %s", joined)
	}
	// NornicDB optional-projection safety: the loader must use scalar queries,
	// never OPTIONAL MATCH or map projection (mirrors the query-layer contract).
	if strings.Contains(joined, "OPTIONAL MATCH") || strings.Contains(joined, "collect(DISTINCT {") {
		t.Fatalf("loader cypher must avoid OPTIONAL MATCH / map projection: %s", joined)
	}
	// The read must source only durable identity, never a generation id.
	for _, forbidden := range []string{"generation_id", "resolved_id", "materialization_generation"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("runtime instance query reads a generation-bearing field %q: %s", forbidden, joined)
		}
	}
	// The platform read must anchor on the indexed instance ids.
	if !strings.Contains(joined, "i.id IN $instance_ids") {
		t.Fatalf("platform read must anchor on indexed instance ids: %s", joined)
	}

	checkout := result["repo-checkout"]
	if len(checkout) != 2 {
		t.Fatalf("len(result[repo-checkout]) = %d, want 2", len(checkout))
	}
	prod := checkout[0]
	if prod.WorkloadRef != "workload-instance:checkout:prod" {
		t.Fatalf("WorkloadRef = %q, want workload-instance:checkout:prod", prod.WorkloadRef)
	}
	if prod.Environment != "prod" || prod.PlatformKind != "kubernetes" || prod.PlatformName != "prod-cluster" {
		t.Fatalf("durable identity mismatch: %#v", prod)
	}
	if prod.WorkloadName != "checkout" || prod.Confidence != 0.9 {
		t.Fatalf("observable fields mismatch: %#v", prod)
	}
	if got := len(result["repo-payments"]); got != 1 {
		t.Fatalf("len(result[repo-payments]) = %d, want 1", got)
	}
}

func TestGraphServiceRuntimeInstanceLoaderMultiPlatformAndNoPlatform(t *testing.T) {
	t.Parallel()

	graph := &runtimeInstanceGraphStub{
		instancesByRepo: map[string][]map[string]any{
			"repo-multi": {
				{"repo_id": "repo-multi", "instance_id": "workload-instance:api:prod", "workload_name": "api", "environment": "prod", "materialization_confidence": 0.8},
				// An instance with a durable environment but no inferred platform
				// must still surface so it can key on its environment identity.
				{"repo_id": "repo-multi", "instance_id": "workload-instance:api:dev", "workload_name": "api", "environment": "dev", "materialization_confidence": 0.6},
				// A row missing the durable instance id cannot be keyed.
				{"repo_id": "repo-multi", "instance_id": "", "workload_name": "api", "environment": "prod"},
			},
		},
		platformsByInst: map[string][]map[string]any{
			// One instance running on two platforms yields two distinct identities.
			"workload-instance:api:prod": {
				{"instance_id": "workload-instance:api:prod", "platform_kind": "kubernetes", "platform_name": "prod-cluster"},
				{"instance_id": "workload-instance:api:prod", "platform_kind": "ecs", "platform_name": "prod-ecs"},
			},
		},
	}

	lookup := GraphServiceRuntimeInstanceLoader{Graph: graph}
	result, err := lookup.GetRuntimeInstancesForRepos(context.Background(), []string{"repo-multi", "repo-empty"})
	if err != nil {
		t.Fatalf("GetRuntimeInstancesForRepos() error = %v", err)
	}

	multi := result["repo-multi"]
	// 2 platforms for api:prod + 1 platformless api:dev = 3 instances; the
	// instance with no id is dropped.
	if len(multi) != 3 {
		t.Fatalf("len(result[repo-multi]) = %d, want 3: %#v", len(multi), multi)
	}
	var prodPlatforms, devWithoutPlatform int
	for _, inst := range multi {
		switch inst.WorkloadRef {
		case "workload-instance:api:prod":
			prodPlatforms++
			if inst.PlatformKind == "" {
				t.Fatalf("prod instance missing platform kind: %#v", inst)
			}
		case "workload-instance:api:dev":
			devWithoutPlatform++
			if inst.PlatformKind != "" || inst.PlatformName != "" {
				t.Fatalf("dev instance must have empty platform fields: %#v", inst)
			}
			if inst.Environment != "dev" {
				t.Fatalf("dev instance environment = %q, want dev", inst.Environment)
			}
		default:
			t.Fatalf("unexpected workload ref: %#v", inst)
		}
	}
	if prodPlatforms != 2 {
		t.Fatalf("prod platform instances = %d, want 2", prodPlatforms)
	}
	if devWithoutPlatform != 1 {
		t.Fatalf("platformless dev instances = %d, want 1", devWithoutPlatform)
	}
	if got := len(result["repo-empty"]); got != 0 {
		t.Fatalf("len(result[repo-empty]) = %d, want 0 (repo with no instances emits none)", got)
	}
}

func TestGraphServiceRuntimeInstanceLoaderEmptyAndNilGuards(t *testing.T) {
	t.Parallel()

	lookup := GraphServiceRuntimeInstanceLoader{Graph: &runtimeInstanceGraphStub{}}
	result, err := lookup.GetRuntimeInstancesForRepos(context.Background(), nil)
	if err != nil {
		t.Fatalf("empty repoIDs error = %v, want nil", err)
	}
	if result != nil {
		t.Fatalf("empty repoIDs result = %#v, want nil", result)
	}

	nilGraph := GraphServiceRuntimeInstanceLoader{}
	if _, err := nilGraph.GetRuntimeInstancesForRepos(context.Background(), []string{"repo"}); err == nil {
		t.Fatal("nil graph must error rather than silently emit no instances")
	}
}

func TestGraphServiceRuntimeInstanceLoaderPropagatesGraphError(t *testing.T) {
	t.Parallel()

	graph := &runtimeInstanceGraphStub{err: errors.New("bolt timeout")}
	lookup := GraphServiceRuntimeInstanceLoader{Graph: graph}
	if _, err := lookup.GetRuntimeInstancesForRepos(context.Background(), []string{"repo"}); err == nil {
		t.Fatal("graph read failure must surface, never be swallowed into empty instances")
	}
}

// TestServiceCatalogHandlerMaterializesRuntimeFromGraphReads proves the positive
// correlation-truth case end to end through the REAL graph-backed loader (not the
// fake): a correlated service repository whose graph carries a
// WorkloadInstance/Platform runtime instance materializes a runtime-family
// snapshot row whose generation-stable key is derived from the durable graph
// identity. This ties graph truth to evidence truth: the row key is exactly
// ServiceRuntimeEvidenceKey over the platform_kind/environment/workload_ref read
// off the nodes.
func TestServiceCatalogHandlerMaterializesRuntimeFromGraphReads(t *testing.T) {
	t.Parallel()

	loader := &stubServiceCatalogCorrelationFactLoader{
		scopeFacts: []facts.Envelope{
			serviceTypedCatalogEntityFact("entity", "component:default/checkout", "Checkout"),
			serviceCatalogOwnershipFact("ownership", "component:default/checkout", "team-payments"),
			serviceCatalogRepositoryIDLinkFact("repo-link", "component:default/checkout", "repo-checkout"),
		},
		activeRepos: []facts.Envelope{
			repositoryFact("repo-checkout", "checkout", "https://github.com/acme/checkout.git", false),
		},
	}
	graph := &runtimeInstanceGraphStub{
		instancesByRepo: map[string][]map[string]any{
			"repo-checkout": {
				{"repo_id": "repo-checkout", "instance_id": "workload-instance:checkout:prod", "workload_name": "checkout", "environment": "prod", "materialization_confidence": 0.9},
			},
		},
		platformsByInst: map[string][]map[string]any{
			"workload-instance:checkout:prod": {{"instance_id": "workload-instance:checkout:prod", "platform_kind": "kubernetes", "platform_name": "prod-cluster"}},
		},
	}
	materialization := newFakeServiceMaterializationStore()
	handler := ServiceCatalogCorrelationHandler{
		FactLoader:            loader,
		Writer:                &recordingServiceCatalogCorrelationWriter{},
		MaterializationWriter: PostgresServiceMaterializationWriter{DB: materialization, Now: time.Now},
		RuntimeInstanceLoader: GraphServiceRuntimeInstanceLoader{Graph: graph},
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-service-catalog",
		ScopeID:      "service-catalog-manifest://repo-checkout/catalog-info.yaml",
		GenerationID: "generation-service-catalog",
		Domain:       DomainServiceCatalogCorrelation,
		SourceSystem: "service_catalog",
	}); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	wantInstance := ServiceRuntimeInstance{
		PlatformKind: "kubernetes",
		Environment:  "prod",
		WorkloadRef:  "workload-instance:checkout:prod",
	}
	var wantKey string
	var foundRuntime bool
	for _, rows := range materialization.snapshots {
		for _, row := range rows {
			if !strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyRuntime+":") {
				continue
			}
			foundRuntime = true
			if strings.Contains(row.evidenceKey, "generation") || strings.Contains(row.evidenceKey, "resolved") {
				t.Fatalf("runtime evidence key %q embeds a generation-bearing token", row.evidenceKey)
			}
			// The service id is the correlated service entity ref.
			wantKey = ServiceRuntimeEvidenceKey(serviceIDFromKey(row.evidenceKey), wantInstance)
			if row.evidenceKey != wantKey {
				t.Fatalf("runtime evidence key = %q, want graph-derived %q", row.evidenceKey, wantKey)
			}
		}
	}
	if !foundRuntime {
		t.Fatal("expected a runtime-family snapshot row sourced from the graph reads")
	}
}

// serviceIDFromKey extracts the <service_id> segment from a
// runtime:<service_id>:<identity> evidence key so the test can recompute the
// expected key without hardcoding the correlation's service id derivation.
func serviceIDFromKey(evidenceKey string) string {
	rest := strings.TrimPrefix(evidenceKey, ServiceEvidenceFamilyRuntime+":")
	// identity begins at platform_kind ("kubernetes:prod:workload-instance:...");
	// the service id is everything before that durable identity prefix.
	marker := ":kubernetes:"
	if idx := strings.Index(rest, marker); idx >= 0 {
		return rest[:idx]
	}
	return rest
}

// TestServiceCatalogHandlerEmitsNoRuntimeWhenGraphEmpty proves the negative case
// through the real loader: a correlated service repository whose graph carries no
// runtime instances materializes no runtime-family rows, while ownership still
// commits.
func TestServiceCatalogHandlerEmitsNoRuntimeWhenGraphEmpty(t *testing.T) {
	t.Parallel()

	loader := &stubServiceCatalogCorrelationFactLoader{
		scopeFacts: []facts.Envelope{
			serviceTypedCatalogEntityFact("entity", "component:default/checkout", "Checkout"),
			serviceCatalogOwnershipFact("ownership", "component:default/checkout", "team-payments"),
			serviceCatalogRepositoryIDLinkFact("repo-link", "component:default/checkout", "repo-checkout"),
		},
		activeRepos: []facts.Envelope{
			repositoryFact("repo-checkout", "checkout", "https://github.com/acme/checkout.git", false),
		},
	}
	// Graph has no WorkloadInstance nodes for the repo: the loader returns none.
	graph := &runtimeInstanceGraphStub{}
	materialization := newFakeServiceMaterializationStore()
	handler := ServiceCatalogCorrelationHandler{
		FactLoader:            loader,
		Writer:                &recordingServiceCatalogCorrelationWriter{},
		MaterializationWriter: PostgresServiceMaterializationWriter{DB: materialization, Now: time.Now},
		RuntimeInstanceLoader: GraphServiceRuntimeInstanceLoader{Graph: graph},
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent",
		ScopeID:      "service-catalog-manifest://repo-checkout/catalog-info.yaml",
		GenerationID: "gen",
		Domain:       DomainServiceCatalogCorrelation,
		SourceSystem: "service_catalog",
	}); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	var sawRuntime, sawOwnership bool
	for _, rows := range materialization.snapshots {
		for _, row := range rows {
			if strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyRuntime+":") {
				sawRuntime = true
			}
			if strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyOwnership+":") {
				sawOwnership = true
			}
		}
	}
	if sawRuntime {
		t.Fatal("repo with no graph runtime instances must emit no runtime-family rows")
	}
	if !sawOwnership {
		t.Fatal("ownership family must still commit when the runtime graph is empty")
	}
}
