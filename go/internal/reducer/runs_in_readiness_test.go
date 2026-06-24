// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

func TestRunsInReadinessGatesOnWorkloadMaterialization(t *testing.T) {
	t.Parallel()

	phase, gated := sharedProjectionReadinessPhase(DomainRunsIn)
	if !gated {
		t.Fatalf("DomainRunsIn must be readiness-gated")
	}
	if phase != GraphProjectionPhaseWorkloadMaterialization {
		t.Fatalf("readiness phase = %q, want %q", phase, GraphProjectionPhaseWorkloadMaterialization)
	}
}

func TestRunsInReadinessKeyspaceMatchesWorkloadPublication(t *testing.T) {
	t.Parallel()

	// RUNS_IN binds the handler Function to the Workload its Repository DEFINES.
	// Workload nodes commit at the workload_materialization phase, which is
	// published under the service_uid keyspace (see WorkloadMaterializationHandler),
	// so the readiness gate must look it up under the same keyspace or the edge is
	// never drained — exactly like handles_route.
	if got := sharedProjectionReadinessKeyspace(DomainRunsIn); got != GraphProjectionKeyspaceServiceUID {
		t.Fatalf("runs_in readiness keyspace = %q, want %q", got, GraphProjectionKeyspaceServiceUID)
	}
}

func TestRunsInDomainIsDrainedBySharedProjectionRunner(t *testing.T) {
	t.Parallel()

	found := false
	for _, domain := range sharedProjectionDomains {
		if domain == DomainRunsIn {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("DomainRunsIn must be enumerated in sharedProjectionDomains so it is drained")
	}
}
