// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

func TestHandlesRouteReadinessGatesOnWorkloadMaterialization(t *testing.T) {
	t.Parallel()

	phase, gated := sharedProjectionReadinessPhase(DomainHandlesRoute)
	if !gated {
		t.Fatalf("DomainHandlesRoute must be readiness-gated")
	}
	if phase != GraphProjectionPhaseWorkloadMaterialization {
		t.Fatalf("readiness phase = %q, want %q", phase, GraphProjectionPhaseWorkloadMaterialization)
	}
}

func TestHandlesRouteReadinessKeyspaceMatchesWorkloadPublication(t *testing.T) {
	t.Parallel()

	// The workload_materialization phase is published under the service_uid
	// keyspace (see WorkloadMaterializationHandler). The generic readiness gate
	// must look it up under the same keyspace or the edge is never drained.
	if got := sharedProjectionReadinessKeyspace(DomainHandlesRoute); got != GraphProjectionKeyspaceServiceUID {
		t.Fatalf("handles_route readiness keyspace = %q, want %q", got, GraphProjectionKeyspaceServiceUID)
	}
	if got := sharedProjectionReadinessKeyspace(DomainCodeCalls); got != GraphProjectionKeyspaceCodeEntitiesUID {
		t.Fatalf("code_calls readiness keyspace = %q, want %q", got, GraphProjectionKeyspaceCodeEntitiesUID)
	}
}

func TestHandlesRouteDomainIsDrainedBySharedProjectionRunner(t *testing.T) {
	t.Parallel()

	found := false
	for _, domain := range sharedProjectionDomains {
		if domain == DomainHandlesRoute {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("DomainHandlesRoute must be enumerated in sharedProjectionDomains so it is drained")
	}
}
