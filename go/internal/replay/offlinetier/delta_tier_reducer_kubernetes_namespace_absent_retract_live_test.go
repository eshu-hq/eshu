// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const nsAbsentRetractCluster = "replay-ns-absent-retract"

func TestReducerKubernetesNamespaceAbsentNodeRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 to run the KubernetesNamespace absent-node retract tier against a real graph backend", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	exec, _ := openDeltaLiveBackend(ctx, t)
	cleanupNamespaceAbsentRetractScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupNamespaceAbsentRetractScope(cleanCtx, t, exec)
	})

	writer := cypher.NewKubernetesNamespaceNodeWriter(exec, 0)
	const evidenceSource = "reducer/kubernetes-namespaces"
	legacyUID := nsAbsentRetractCluster + ":legacy-without-generation"
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher: `CREATE (n:KubernetesNamespace {
uid: $uid, cluster_id: $cluster_id, evidence_source: $evidence_source
})`,
		Parameters: map[string]any{
			"uid":             legacyUID,
			"cluster_id":      nsAbsentRetractCluster,
			"evidence_source": evidenceSource,
		},
	}); err != nil {
		t.Fatalf("seed pre-generation-stamp legacy namespace: %v", err)
	}
	row := func(uid, generationID string) map[string]any {
		result := nsEnvRetractRow(uid, "")
		result["cluster_id"] = nsAbsentRetractCluster
		result["generation_id"] = generationID
		return result
	}

	if err := writer.WriteKubernetesNamespaceNodes(ctx, []map[string]any{
		row(nsAbsentRetractCluster+":current", "generation-current"),
		row(nsAbsentRetractCluster+":stale", "generation-old"),
	}, evidenceSource); err != nil {
		t.Fatalf("seed current and stale namespaces: %v", err)
	}
	if err := writer.RetractStaleKubernetesNamespaceNodes(
		ctx, nsAbsentRetractCluster, "generation-current", evidenceSource,
	); err != nil {
		t.Fatalf("retract stale namespace: %v", err)
	}
	assertEdgeCount(ctx, t, exec,
		`MATCH (n:KubernetesNamespace {cluster_id: $cluster_id}) RETURN count(n)`,
		map[string]any{"cluster_id": nsAbsentRetractCluster}, 1,
		"complete snapshot preserves current namespace and removes stale namespace",
	)
	assertEdgeCount(ctx, t, exec,
		`MATCH (n:KubernetesNamespace {uid: $uid}) RETURN count(n)`,
		map[string]any{"uid": legacyUID}, 0,
		"first complete snapshot removes legacy namespace without generation stamp",
	)

	if err := writer.RetractStaleKubernetesNamespaceNodes(
		ctx, nsAbsentRetractCluster, "generation-empty", evidenceSource,
	); err != nil {
		t.Fatalf("retract namespace on complete empty snapshot: %v", err)
	}
	assertEdgeCount(ctx, t, exec,
		`MATCH (n:KubernetesNamespace {cluster_id: $cluster_id}) RETURN count(n)`,
		map[string]any{"cluster_id": nsAbsentRetractCluster}, 0,
		"complete empty snapshot removes last namespace",
	)
}

func cleanupNamespaceAbsentRetractScope(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     `MATCH (n:KubernetesNamespace {cluster_id: $cluster_id}) DETACH DELETE n`,
		Parameters: map[string]any{"cluster_id": nsAbsentRetractCluster},
	}); err != nil {
		t.Fatalf("cleanup kubernetes namespace absent-retract scope: %v", err)
	}
}
