// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa/graphdump"
	"github.com/eshu-hq/eshu/go/internal/ifa/materializededges"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// environmentEndpointEdge builds the exact shape a live TARGETS_ENVIRONMENT
// edge carries, so both cases below drive the real endpoint rather than a
// convenient approximation, and both reach endpointID through the same
// assertMaterializedEdges path.
//
// The target property map is name-only on purpose: the reducer MERGEs the node
// as `MERGE (env:Environment {name: row.environment})`
// (canonicalKubernetesNamespaceWithEnvironmentUpsertCypher), so a live
// Environment node carries neither uid nor id. evidence_source is mandatory
// rather than decorative -- materializedEdgeEndpointsByFamily scopes this
// family by it, so an edge without it is filtered out before endpointID is
// reached and the positive case would pass for the wrong reason.
func environmentEndpointEdge(toLabels []string, toProps map[string]any) graphdump.Edge {
	return graphdump.Edge{
		Type:       "TARGETS_ENVIRONMENT",
		FromLabels: []string{"KubernetesNamespace"},
		FromProps:  map[string]any{"uid": "k8s-ns:eshu-fixture-cluster:payments-prod"},
		ToLabels:   toLabels,
		ToProps:    toProps,
		Props:      map[string]any{"evidence_source": "reducer/kubernetes-namespaces"},
	}
}

// TestNameFallbackResolvesEnvironmentEndpoint proves endpointID's fourth
// fallback: an Environment endpoint, MERGEd `{name: row.environment}` and
// carrying neither uid nor id, resolves by its name instead of reporting as an
// unmaterialized endpoint.
//
// This is not hypothetical. Driving the kubernetes_namespace_environment
// family through a live NornicDB stack materialized both TARGETS_ENVIRONMENT
// edges correctly, and `ifa assert-edges` still failed them as "an
// unmaterialized endpoint node" -- the node existed, was correct, and was
// simply keyed by name. Without this fallback the family cannot satisfy the
// live-matrix condition however right its fixture and its writer are.
func TestNameFallbackResolvesEnvironmentEndpoint(t *testing.T) {
	t.Parallel()

	types, err := materializededges.MaterializedEdgeDomainEdgeTypes("kubernetes_namespace_environment")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(kubernetes_namespace_environment): %v", err)
	}
	endpoints, ok := cypher.MaterializedEdgeEndpointLabels("kubernetes_namespace_environment")
	if !ok {
		t.Fatal("MaterializedEdgeEndpointLabels(kubernetes_namespace_environment) reported no constraints; this family is endpoint-scoped and the case would not exercise the real filter")
	}
	graph := fakeEdgeReader{edges: []graphdump.Edge{
		environmentEndpointEdge([]string{"Environment"}, map[string]any{"name": "prod"}),
	}}
	expected := []materializededges.ExpectedEdge{{
		RelationshipType: "TARGETS_ENVIRONMENT",
		SourceEntityID:   "k8s-ns:eshu-fixture-cluster:payments-prod",
		TargetEntityID:   "prod",
	}}

	if err := assertMaterializedEdges(
		context.Background(), graph, "kubernetes_namespace_environment", types, endpoints, nil, expected,
	); err != nil {
		t.Fatalf("assertMaterializedEdges(name-only Environment target) = %v, want nil; name must resolve the endpoint", err)
	}
}

// TestNameFallbackIsScopedToEnvironment proves the name fallback does NOT
// apply globally, mirroring TestRefFallbackIsScopedToCodeownerTeam.
//
// "name" is a far more common property than "ref" -- Repository, Workload and
// most content entities carry one alongside their real identity -- so an
// unscoped fallback is materially more dangerous here than it would have been
// for ref: any uid/id-keyed node that lost its real identity but kept its
// display name would read as "identified" rather than "unmaterialized", the
// exact false pass the endpoint check exists to catch. Deleting the label
// guard in endpointID turns this case green, which is what makes it a real
// regression test rather than a restatement of the positive one.
func TestNameFallbackIsScopedToEnvironment(t *testing.T) {
	t.Parallel()

	types, err := materializededges.MaterializedEdgeDomainEdgeTypes("kubernetes_namespace_environment")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(kubernetes_namespace_environment): %v", err)
	}
	graph := fakeEdgeReader{edges: []graphdump.Edge{
		// Endpoint constraints are deliberately not passed here, so the edge
		// is matched by relationship type alone and reaches endpointID with a
		// non-Environment target carrying only an incidental name.
		environmentEndpointEdge([]string{"SomeOtherLabel"}, map[string]any{"name": "incidental"}),
	}}
	expected := []materializededges.ExpectedEdge{{
		RelationshipType: "TARGETS_ENVIRONMENT",
		SourceEntityID:   "k8s-ns:eshu-fixture-cluster:payments-prod",
		TargetEntityID:   "incidental",
	}}

	err = assertMaterializedEdges(
		context.Background(), graph, "kubernetes_namespace_environment", types, nil, nil, expected,
	)
	if err == nil {
		t.Fatal("assertMaterializedEdges(non-Environment endpoint with only an incidental name) = nil, want an endpoint-defect failure")
	}
	if !strings.Contains(err.Error(), "endpoint") {
		t.Errorf("error %q does not report the endpoint defect", err)
	}
}
