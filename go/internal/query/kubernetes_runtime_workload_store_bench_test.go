// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"testing"
)

// BenchmarkBuildKubernetesRuntimeWorkloadQuery200Candidates pins the
// allocation cost of the staying workload store's candidate SQL build. It
// lives in root with the store implementation it exercises
// (kubernetes_runtime_workload_store.go); the probe-side benchmark moved
// with the hub to internal/query/supplychain (#6060 lane A).
func BenchmarkBuildKubernetesRuntimeWorkloadQuery200Candidates(b *testing.B) {
	candidates := make([]KubernetesRuntimeCandidate, SupplyChainKubernetesRuntimeProbeMaxResults)
	for i := range candidates {
		candidates[i] = KubernetesRuntimeCandidate{
			WorkloadUID: fmt.Sprintf("workload-%03d", i), Digest: fmt.Sprintf("sha256:%064x", i+1),
			EdgeScopeID: "edge-scope", EdgeGenerationID: "edge-generation",
		}
	}
	b.ReportAllocs()
	for range b.N {
		query, args := buildKubernetesRuntimeWorkloadQuery(candidates, false, nil, []string{"edge-scope"})
		if len(query) == 0 || len(args) != 7 {
			b.Fatalf("query length=%d args=%d", len(query), len(args))
		}
	}
}
