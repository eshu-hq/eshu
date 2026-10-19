// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	worker "github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
)

func TestHandlesRouteReadinessGatesOnWorkloadMaterialization(t *testing.T) {
	t.Parallel()

	phase, gated := worker.ReadinessPhase(DomainHandlesRoute)
	if !gated {
		t.Fatalf("DomainHandlesRoute must be readiness-gated")
	}
	if phase != gpphase.PhaseWorkloadMaterialization {
		t.Fatalf("readiness phase = %q, want %q", phase, gpphase.PhaseWorkloadMaterialization)
	}
}

func TestHandlesRouteReadinessKeyspaceMatchesWorkloadPublication(t *testing.T) {
	t.Parallel()

	// The workload_materialization phase is published under the service_uid
	// keyspace (see WorkloadMaterializationHandler). The generic readiness gate
	// must look it up under the same keyspace or the edge is never drained.
	if got := worker.ReadinessKeyspace(DomainHandlesRoute); got != gpphase.KeyspaceServiceUID {
		t.Fatalf("handles_route readiness keyspace = %q, want %q", got, gpphase.KeyspaceServiceUID)
	}
	if got := worker.ReadinessKeyspace(DomainCodeCalls); got != gpphase.KeyspaceCodeEntitiesUID {
		t.Fatalf("code_calls readiness keyspace = %q, want %q", got, gpphase.KeyspaceCodeEntitiesUID)
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
