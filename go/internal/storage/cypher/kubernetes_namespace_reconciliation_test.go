// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

func TestKubernetesNamespaceNodeWriterRetractsOnlyAbsentOwnedClusterNodes(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewKubernetesNamespaceNodeWriter(executor, 0)
	if err := writer.RetractStaleKubernetesNamespaceNodes(
		context.Background(),
		"prod-eks",
		"generation-current",
		"reducer/kubernetes-namespaces",
	); err != nil {
		t.Fatalf("RetractStaleKubernetesNamespaceNodes() error = %v, want nil", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("Execute calls = %d, want %d", got, want)
	}
	stmt := executor.calls[0]
	if got, want := stmt.Operation, OperationCanonicalRetract; got != want {
		t.Fatalf("Operation = %q, want %q", got, want)
	}
	for _, required := range []string{
		"MATCH (n:KubernetesNamespace {cluster_id: $cluster_id})",
		"n.evidence_source = $evidence_source",
		"coalesce(n.generation_id, \"\") <> $generation_id",
		"DETACH DELETE n",
	} {
		if !strings.Contains(stmt.Cypher, required) {
			t.Fatalf("retract Cypher missing %q:\n%s", required, stmt.Cypher)
		}
	}
	if got, want := stmt.Parameters["cluster_id"], "prod-eks"; got != want {
		t.Fatalf("cluster_id = %#v, want %#v", got, want)
	}
	if got, want := stmt.Parameters["generation_id"], "generation-current"; got != want {
		t.Fatalf("generation_id = %#v, want %#v", got, want)
	}
	if got, want := stmt.Parameters["evidence_source"], "reducer/kubernetes-namespaces"; got != want {
		t.Fatalf("evidence_source = %#v, want %#v", got, want)
	}
	if !stmt.Drain || stmt.DrainVar != "n" {
		t.Fatalf("Drain = %t, DrainVar = %q, want true and n", stmt.Drain, stmt.DrainVar)
	}
}

func TestKubernetesNamespaceNodeWriterRetractRequiresScopeIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		clusterID      string
		generationID   string
		evidenceSource string
		wantError      string
	}{
		{name: "cluster", generationID: "gen-1", evidenceSource: "reducer/kubernetes-namespaces", wantError: "cluster_id"},
		{name: "generation", clusterID: "prod-eks", evidenceSource: "reducer/kubernetes-namespaces", wantError: "generation_id"},
		{name: "evidence", clusterID: "prod-eks", generationID: "gen-1", wantError: "evidence_source"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			writer := NewKubernetesNamespaceNodeWriter(&recordingExecutor{}, 0)
			err := writer.RetractStaleKubernetesNamespaceNodes(
				context.Background(), tt.clusterID, tt.generationID, tt.evidenceSource,
			)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want text %q", err, tt.wantError)
			}
		})
	}
}
