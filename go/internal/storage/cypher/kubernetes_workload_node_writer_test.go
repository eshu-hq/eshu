// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

func kubernetesWorkloadRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"uid":                    "object-" + string(rune('a'+i)),
			"cluster_id":             "prod-eks",
			"namespace":              "payments",
			"name":                   "checkout",
			"workload_uid":           "11111111-2222-3333-4444-555555555555",
			"group_version_resource": "apps/v1/deployments",
			"service_account":        "checkout-sa",
			"image_refs":             []string{"registry.example.com/checkout@sha256:abc"},
			"selector":               []string{"app=checkout"},
			"correlation_anchors":    []string{"object-a", "registry.example.com/checkout@sha256:abc"},
			"source_fact_id":         "fact-1",
			"stable_fact_key":        "key-1",
			"source_system":          "kubernetes_live",
			"source_record_id":       "rec-1",
			"source_confidence":      "reported",
			"collector_kind":         "kubernetes_live",
		})
	}
	return rows
}

func TestKubernetesWorkloadNodeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewKubernetesWorkloadNodeWriter(executor, 0)

	if err := writer.WriteKubernetesWorkloadNodes(context.Background(), nil, "reducer/kubernetes-workloads"); err != nil {
		t.Fatalf("WriteKubernetesWorkloadNodes returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestKubernetesWorkloadNodeWriterMergesOnUID(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewKubernetesWorkloadNodeWriter(executor, 0)

	if err := writer.WriteKubernetesWorkloadNodes(context.Background(), kubernetesWorkloadRows(1), "reducer/kubernetes-workloads"); err != nil {
		t.Fatalf("WriteKubernetesWorkloadNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "UNWIND $rows AS row") {
		t.Fatalf("cypher missing UNWIND batch shape:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (w:KubernetesWorkload {uid: row.uid})") {
		t.Fatalf("cypher must MERGE on uid identity only:\n%s", cypher)
	}
	if strings.Contains(cypher, "MERGE (w:KubernetesWorkload {uid: row.uid, name") {
		t.Fatalf("cypher must not MERGE on a wide mutable map:\n%s", cypher)
	}
	if !strings.Contains(cypher, "w.evidence_source = row.evidence_source") {
		t.Fatalf("cypher must set the reducer evidence_source:\n%s", cypher)
	}
	rows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows parameter type = %T, want []map[string]any", executor.calls[0].Parameters["rows"])
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got := rows[0]["evidence_source"]; got != "reducer/kubernetes-workloads" {
		t.Fatalf("evidence_source = %v, want reducer/kubernetes-workloads", got)
	}
}

func TestKubernetesWorkloadNodeWriterBatchesRows(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewKubernetesWorkloadNodeWriter(executor, 2)

	if err := writer.WriteKubernetesWorkloadNodes(context.Background(), kubernetesWorkloadRows(5), "reducer/kubernetes-workloads"); err != nil {
		t.Fatalf("WriteKubernetesWorkloadNodes returned error: %v", err)
	}
	// 5 rows at batch size 2 -> 3 statements.
	if len(executor.calls) != 3 {
		t.Fatalf("len(calls) = %d, want 3 batched statements", len(executor.calls))
	}
}

func TestKubernetesWorkloadNodeWriterUsesGroupExecutorAtomically(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewKubernetesWorkloadNodeWriter(executor, 2)

	if err := writer.WriteKubernetesWorkloadNodes(context.Background(), kubernetesWorkloadRows(5), "reducer/kubernetes-workloads"); err != nil {
		t.Fatalf("WriteKubernetesWorkloadNodes returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("len(groupCalls) = %d, want 1 atomic group", len(executor.groupCalls))
	}
	if len(executor.groupCalls[0]) != 3 {
		t.Fatalf("group statement count = %d, want 3", len(executor.groupCalls[0]))
	}
}

func TestKubernetesWorkloadNodeWriterSatisfiesReducerInterface(t *testing.T) {
	t.Parallel()

	// Compile-time guarantee that the cypher writer satisfies the reducer-owned
	// consumer interface. This test fails to compile if the method set drifts.
	var _ interface {
		WriteKubernetesWorkloadNodes(ctx context.Context, rows []map[string]any, evidenceSource string) error
	} = NewKubernetesWorkloadNodeWriter(&recordingExecutor{}, 0)
}
