// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kubernetes

import (
	"fmt"
	"testing"

	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
)

// BenchmarkBuildKubernetesRuntimeWorkloadQuery200Candidates pins the
// allocation cost of the workload store's candidate SQL build. It lives
// with the store implementation it exercises (runtime_workload_store.go); the
// probe-side benchmark moved with the hub to internal/query/supply/chain
// (#6060 lane A).
func BenchmarkBuildKubernetesRuntimeWorkloadQuery200Candidates(b *testing.B) {
	candidates := make([]supplychain.KubernetesRuntimeCandidate, supplychain.KubernetesRuntimeProbeMaxResults)
	for i := range candidates {
		candidates[i] = supplychain.KubernetesRuntimeCandidate{
			WorkloadUID: fmt.Sprintf("workload-%03d", i), Digest: fmt.Sprintf("sha256:%064x", i+1),
			EdgeScopeID: "edge-scope", EdgeGenerationID: "edge-generation",
		}
	}
	b.ReportAllocs()
	for range b.N {
		query, args := BuildRuntimeWorkloadQuery(candidates, false, nil, []string{"edge-scope"})
		if len(query) == 0 || len(args) != 7 {
			b.Fatalf("query length=%d args=%d", len(query), len(args))
		}
	}
}
