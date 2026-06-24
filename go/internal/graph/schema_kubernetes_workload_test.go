// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import "testing"

func TestSchemaStatementsContainsKubernetesWorkloadSchema(t *testing.T) {
	t.Parallel()

	stmts := SchemaStatements()

	expected := []string{
		"CREATE CONSTRAINT kubernetes_workload_uid_unique IF NOT EXISTS FOR (n:KubernetesWorkload) REQUIRE n.uid IS UNIQUE",
		"CREATE INDEX kubernetes_workload_cluster_id IF NOT EXISTS FOR (w:KubernetesWorkload) ON (w.cluster_id)",
		"CREATE INDEX kubernetes_workload_namespace IF NOT EXISTS FOR (w:KubernetesWorkload) ON (w.namespace)",
	}
	for _, want := range expected {
		assertContainsStatement(t, stmts, want)
	}
}

func TestSchemaStatementsForBackendAddsKubernetesWorkloadNornicDBUIDLookup(t *testing.T) {
	t.Parallel()

	stmts, err := SchemaStatementsForBackend(SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend returned error: %v", err)
	}
	assertContainsStatement(
		t,
		stmts,
		"CREATE INDEX nornicdb_kubernetes_workload_uid_lookup IF NOT EXISTS FOR (n:KubernetesWorkload) ON (n.uid)",
	)
}
