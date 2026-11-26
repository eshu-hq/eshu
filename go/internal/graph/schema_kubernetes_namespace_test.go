// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import "testing"

func TestSchemaStatementsContainsKubernetesNamespaceSchema(t *testing.T) {
	t.Parallel()

	stmts := SchemaStatements()

	expected := []string{
		"CREATE CONSTRAINT kubernetes_namespace_uid_unique IF NOT EXISTS FOR (n:KubernetesNamespace) REQUIRE n.uid IS UNIQUE",
		"CREATE INDEX kubernetes_namespace_cluster_id IF NOT EXISTS FOR (n:KubernetesNamespace) ON (n.cluster_id)",
		"CREATE INDEX kubernetes_namespace_namespace IF NOT EXISTS FOR (n:KubernetesNamespace) ON (n.namespace)",
	}
	for _, want := range expected {
		assertContainsStatement(t, stmts, want)
	}
}

func TestSchemaStatementsForBackendDropsKubernetesNamespaceNornicDBUIDLookupIndex(t *testing.T) {
	t.Parallel()

	stmts, err := SchemaStatementsForBackend(SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend returned error: %v", err)
	}
	assertContainsStatement(
		t,
		stmts,
		"DROP INDEX nornicdb_kubernetes_namespace_uid_lookup IF EXISTS",
	)
}
