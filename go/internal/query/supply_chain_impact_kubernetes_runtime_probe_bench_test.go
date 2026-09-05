// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
)

func BenchmarkApplySupplyChainKubernetesRuntimeEvidence200Digests(b *testing.B) {
	rows := make([]impact.SupplyChainImpactFindingRow, supplyChainCloudRuntimeProbeMaxDigests)
	graphRows := make([]map[string]any, supplyChainKubernetesRuntimeProbeMaxResults)
	matches := make([]KubernetesRuntimeWorkloadMatch, supplyChainKubernetesRuntimeProbeMaxResults)
	for i := range rows {
		digest := fmt.Sprintf("sha256:%064x", i+1)
		uid := fmt.Sprintf("workload-%03d", i)
		rows[i] = impact.SupplyChainImpactFindingRow{FindingID: fmt.Sprintf("finding-%03d", i), SubjectDigest: digest}
		graphRows[i] = map[string]any{
			"matched_digest": digest, "workload_uid": uid,
			"edge_scope_id": "edge-scope", "edge_generation_id": "edge-generation",
		}
		matches[i] = KubernetesRuntimeWorkloadMatch{
			Digest: digest,
			WorkloadRef: impact.KubernetesRuntimeWorkloadRef{
				UID: uid, ClusterID: "cluster-a", Namespace: "default", Name: fmt.Sprintf("workload-%03d", i),
			},
		}
	}
	handler := &SupplyChainHandler{
		Neo4j:                       &stubKubernetesRuntimeGraph{rows: graphRows},
		KubernetesWorkloadInventory: &stubKubernetesWorkloadInventory{rows: matches},
	}
	access := repositoryAccessFilter{AllScopes: true}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := handler.applySupplyChainKubernetesRuntimeEvidence(context.Background(), access, rows); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildKubernetesRuntimeWorkloadQuery200Candidates(b *testing.B) {
	candidates := make([]KubernetesRuntimeCandidate, supplyChainKubernetesRuntimeProbeMaxResults)
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
