// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"
)

// TestMaterializationConfidenceConstantsAreParameterized guards against a
// regression to bare confidence literals: every materialization edge-confidence
// Cypher statement must inject the weight through the $edge_confidence parameter
// rather than embedding a magic number in the query text.
func TestMaterializationConfidenceConstantsAreParameterized(t *testing.T) {
	t.Parallel()

	param := "$" + MaterializationConfidenceParam
	statements := map[string]string{
		"batchInfraPlatformUpsertCypher":      batchInfraPlatformUpsertCypher,
		"batchRepoDependencyUpsertCypher":     batchRepoDependencyUpsertCypher,
		"batchWorkloadDependencyUpsertCypher": batchWorkloadDependencyUpsertCypher,
	}
	for name, cypher := range statements {
		if !strings.Contains(cypher, "SET rel.confidence = "+param) {
			t.Errorf("%s: confidence is not parameterized via %s", name, param)
		}
		for _, lit := range []string{"= 0.9", "= 0.98"} {
			if strings.Contains(cypher, "rel.confidence "+lit) {
				t.Errorf("%s: still embeds bare confidence literal %q", name, lit)
			}
		}
	}
}

// TestMaterializeDependenciesStampRegistryConfidence proves the repo and
// workload DEPENDS_ON edges carry the documented runtime-service-dependency
// weight as a statement-scoped parameter.
func TestMaterializeDependenciesStampRegistryConfidence(t *testing.T) {
	t.Parallel()

	executor := &fakeNeo4jExecutor{}
	m := NewWorkloadMaterializer(executor)

	repoDeps := []RepoDependencyRow{{DependencyName: "dep", RepoID: "repo-1", TargetRepoID: "repo-2"}}
	workloadDeps := []WorkloadDependencyRow{{
		DependencyName:   "dep",
		RepoID:           "repo-1",
		TargetRepoID:     "repo-2",
		WorkloadID:       "workload:a",
		TargetWorkloadID: "workload:b",
	}}

	if _, err := m.MaterializeDependencies(context.Background(), repoDeps, workloadDeps); err != nil {
		t.Fatalf("MaterializeDependencies() error = %v", err)
	}
	if len(executor.calls) != 2 {
		t.Fatalf("executor calls = %d, want 2", len(executor.calls))
	}
	for _, call := range executor.calls {
		got, ok := call.Parameters[MaterializationConfidenceParam]
		if !ok {
			t.Fatalf("call missing %q parameter: %s", MaterializationConfidenceParam, call.Cypher)
		}
		if got != RuntimeServiceDependencyEdgeConfidence {
			t.Errorf("confidence param = %v, want %v", got, RuntimeServiceDependencyEdgeConfidence)
		}
	}
}

// TestInfrastructurePlatformEdgeStampsRegistryConfidence proves the
// PROVISIONS_PLATFORM edge carries the documented provisioning weight as a
// statement-scoped parameter.
func TestInfrastructurePlatformEdgeStampsRegistryConfidence(t *testing.T) {
	t.Parallel()

	executor := &fakeNeo4jExecutor{}
	m := NewInfrastructurePlatformMaterializer(executor)

	rows := []InfrastructurePlatformRow{{
		RepoID:       "repo-1",
		PlatformID:   "platform:kubernetes:none:prod:prod:none",
		PlatformKind: "kubernetes",
		PlatformName: "prod",
	}}

	if _, err := m.Materialize(context.Background(), rows); err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("executor calls = %d, want 1", len(executor.calls))
	}
	got, ok := executor.calls[0].Parameters[MaterializationConfidenceParam]
	if !ok {
		t.Fatalf("call missing %q parameter", MaterializationConfidenceParam)
	}
	if got != ProvisionsPlatformEdgeConfidence {
		t.Errorf("confidence param = %v, want %v", got, ProvisionsPlatformEdgeConfidence)
	}
}

// TestRuntimePlatformConfidenceFallbackUsesConstant proves the RUNS_ON fallback
// confidence is sourced from the documented constant and a positive projected
// confidence is preserved unchanged.
func TestRuntimePlatformConfidenceFallbackUsesConstant(t *testing.T) {
	t.Parallel()

	if got := runtimePlatformConfidence(0); got != DefaultRuntimePlatformEdgeConfidence {
		t.Errorf("runtimePlatformConfidence(0) = %v, want %v", got, DefaultRuntimePlatformEdgeConfidence)
	}
	if got := runtimePlatformConfidence(-1); got != DefaultRuntimePlatformEdgeConfidence {
		t.Errorf("runtimePlatformConfidence(-1) = %v, want %v", got, DefaultRuntimePlatformEdgeConfidence)
	}
	if got := runtimePlatformConfidence(0.42); got != 0.42 {
		t.Errorf("runtimePlatformConfidence(0.42) = %v, want 0.42", got)
	}
}
