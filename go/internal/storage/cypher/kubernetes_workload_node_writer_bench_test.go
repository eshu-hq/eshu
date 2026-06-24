// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

func benchKubernetesWorkloadRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"uid":                    fmt.Sprintf("object-%d", i),
			"cluster_id":             "prod-eks",
			"namespace":              "payments",
			"name":                   fmt.Sprintf("workload-%d", i),
			"workload_uid":           fmt.Sprintf("11111111-2222-3333-4444-%012d", i),
			"group_version_resource": "apps/v1/deployments",
			"service_account":        "checkout-sa",
			"image_refs":             []string{fmt.Sprintf("registry.example.com/checkout@sha256:%064d", i)},
			"selector":               []string{"app=checkout"},
			"correlation_anchors":    []string{fmt.Sprintf("object-%d", i)},
			"source_fact_id":         "fact",
			"stable_fact_key":        "key",
			"source_system":          "kubernetes_live",
			"source_record_id":       "rec",
			"source_confidence":      "reported",
			"collector_kind":         "kubernetes_live",
		})
	}
	return rows
}

// BenchmarkKubernetesWorkloadNodeWriter measures the statement-construction and
// batching cost of the KubernetesWorkload node writer for a realistic
// per-cluster-generation workload count. The backend executor is a no-op so the
// benchmark isolates Eshu-owned write-path work from graph round trips. The
// shape mirrors BenchmarkCloudResourceNodeWriter so the two canonical node
// writers stay on the same measured baseline.
func BenchmarkKubernetesWorkloadNodeWriter(b *testing.B) {
	rows := benchKubernetesWorkloadRows(5000)
	writer := NewKubernetesWorkloadNodeWriter(noopGroupExecutor{}, 500)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writer.WriteKubernetesWorkloadNodes(ctx, rows, "reducer/kubernetes-workloads"); err != nil {
			b.Fatalf("WriteKubernetesWorkloadNodes: %v", err)
		}
	}
}
