// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
)

func BenchmarkApplySupplyChainKubernetesRuntimeEvidence200Digests(b *testing.B) {
	rows := make([]impact.SupplyChainImpactFindingRow, SupplyChainCloudRuntimeProbeMaxDigests)
	graphRows := make([]map[string]any, SupplyChainKubernetesRuntimeProbeMaxResults)
	matches := make([]KubernetesRuntimeWorkloadMatch, SupplyChainKubernetesRuntimeProbeMaxResults)
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
	access := querycontract.RepositoryAccessFilter{AllScopes: true}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := handler.applySupplyChainKubernetesRuntimeEvidence(context.Background(), access, rows); err != nil {
			b.Fatal(err)
		}
	}
}
