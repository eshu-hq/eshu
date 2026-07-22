// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

func kubernetesNamespaceRow(uid, environment, environmentState, evidenceClass string) map[string]any {
	return map[string]any{
		"uid":                 uid,
		"cluster_id":          "prod-eks",
		"namespace":           uid,
		"labels":              []string{"team=payments"},
		"correlation_anchors": []string{uid},
		"environment":         environment,
		"environment_state":   environmentState,
		"evidence_class":      evidenceClass,
		"source_fact_id":      "fact-1",
		"stable_fact_key":     "key-1",
		"source_system":       "kubernetes_live",
		"source_record_id":    "rec-1",
		"source_confidence":   "reported",
		"collector_kind":      "kubernetes_live",
	}
}

func TestKubernetesNamespaceNodeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewKubernetesNamespaceNodeWriter(executor, 0)

	if err := writer.WriteKubernetesNamespaceNodes(context.Background(), nil, "reducer/kubernetes-namespaces"); err != nil {
		t.Fatalf("WriteKubernetesNamespaceNodes returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

// TestKubernetesNamespaceNodeWriterUnboundRowNeverCreatesEnvironment is the
// negative-regression lock for issue #5434: an unbound namespace row (empty
// "environment") MUST route through canonicalKubernetesNamespaceUpsertCypher
// only. If the writer's environment-presence guard were removed or
// inverted -- for example if WriteKubernetesNamespaceNodes always used
// canonicalKubernetesNamespaceWithEnvironmentUpsertCypher -- this test fails
// because the recorded statement would then contain "MERGE (env:Environment".
func TestKubernetesNamespaceNodeWriterUnboundRowNeverCreatesEnvironment(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewKubernetesNamespaceNodeWriter(executor, 0)

	row := kubernetesNamespaceRow("misc", "", "environment-unbound", "")
	if err := writer.WriteKubernetesNamespaceNodes(context.Background(), []map[string]any{row}, "reducer/kubernetes-namespaces"); err != nil {
		t.Fatalf("WriteKubernetesNamespaceNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if strings.Contains(cypher, "MERGE (env:Environment") {
		t.Fatalf("unbound-row statement must never MERGE an Environment node:\n%s", cypher)
	}
	if strings.Contains(cypher, "TARGETS_ENVIRONMENT") {
		t.Fatalf("unbound-row statement must never write a TARGETS_ENVIRONMENT edge:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (n:KubernetesNamespace {uid: row.uid})") {
		t.Fatalf("cypher must MERGE on uid identity only:\n%s", cypher)
	}
	if !strings.Contains(cypher, "REMOVE n.environment, n.evidence_class") {
		t.Fatalf("unbound-row statement must REMOVE any stale environment/evidence_class from a prior bound generation:\n%s", cypher)
	}
}

// TestKubernetesNamespaceNodeWriterBoundRowCreatesEnvironment proves the
// positive counterpart: a row with a non-empty "environment" property routes
// through canonicalKubernetesNamespaceWithEnvironmentUpsertCypher and MERGEs
// an Environment node plus a TARGETS_ENVIRONMENT edge carrying the
// evidence_class the reducer classified.
func TestKubernetesNamespaceNodeWriterBoundRowCreatesEnvironment(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewKubernetesNamespaceNodeWriter(executor, 0)

	row := kubernetesNamespaceRow("payments-prod", "prod", "bound", "namespace_label")
	if err := writer.WriteKubernetesNamespaceNodes(context.Background(), []map[string]any{row}, "reducer/kubernetes-namespaces"); err != nil {
		t.Fatalf("WriteKubernetesNamespaceNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "MERGE (env:Environment {name: row.environment})") {
		t.Fatalf("bound-row statement must MERGE the Environment node:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (n)-[env_rel:TARGETS_ENVIRONMENT]->(env)") {
		t.Fatalf("bound-row statement must MERGE the TARGETS_ENVIRONMENT edge:\n%s", cypher)
	}
	if !strings.Contains(cypher, "env_rel.evidence_class = row.evidence_class") {
		t.Fatalf("bound-row statement must set the edge's evidence_class:\n%s", cypher)
	}
	rows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("rows parameter = %#v, want 1 row", executor.calls[0].Parameters["rows"])
	}
	if got := rows[0]["environment"]; got != "prod" {
		t.Fatalf("row[environment] = %v, want prod", got)
	}
	if got := rows[0]["evidence_source"]; got != "reducer/kubernetes-namespaces" {
		t.Fatalf("row[evidence_source] = %v, want reducer/kubernetes-namespaces", got)
	}
}

// TestKubernetesNamespaceNodeWriterSplitsMixedBatch proves a mixed batch (one
// bound namespace, one unbound namespace) produces two SEPARATE statements --
// the unbound row never appears in the with-environment statement's rows and
// vice versa, so one namespace's binding can never leak into another's write.
func TestKubernetesNamespaceNodeWriterSplitsMixedBatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewKubernetesNamespaceNodeWriter(executor, 0)

	bound := kubernetesNamespaceRow("payments-prod", "prod", "bound", "namespace_label")
	unbound := kubernetesNamespaceRow("misc", "", "environment-unbound", "")
	if err := writer.WriteKubernetesNamespaceNodes(context.Background(), []map[string]any{bound, unbound}, "reducer/kubernetes-namespaces"); err != nil {
		t.Fatalf("WriteKubernetesNamespaceNodes returned error: %v", err)
	}
	if len(executor.calls) != 2 {
		t.Fatalf("len(calls) = %d, want 2 (one per variant)", len(executor.calls))
	}
	var sawBoundStatement, sawUnboundStatement bool
	for _, call := range executor.calls {
		rows, ok := call.Parameters["rows"].([]map[string]any)
		if !ok || len(rows) != 1 {
			t.Fatalf("rows parameter = %#v, want exactly 1 row per statement", call.Parameters["rows"])
		}
		containsEnvMerge := strings.Contains(call.Cypher, "MERGE (env:Environment")
		switch rows[0]["uid"] {
		case "payments-prod":
			sawBoundStatement = true
			if !containsEnvMerge {
				t.Fatalf("bound row's statement must MERGE Environment:\n%s", call.Cypher)
			}
		case "misc":
			sawUnboundStatement = true
			if containsEnvMerge {
				t.Fatalf("unbound row's statement must NOT MERGE Environment:\n%s", call.Cypher)
			}
		default:
			t.Fatalf("unexpected row uid %v", rows[0]["uid"])
		}
	}
	if !sawBoundStatement || !sawUnboundStatement {
		t.Fatalf("expected one statement per variant: sawBound=%v sawUnbound=%v", sawBoundStatement, sawUnboundStatement)
	}
}

func TestKubernetesNamespaceNodeWriterUsesGroupExecutorAtomically(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewKubernetesNamespaceNodeWriter(executor, 0)

	bound := kubernetesNamespaceRow("payments-prod", "prod", "bound", "namespace_label")
	unbound := kubernetesNamespaceRow("misc", "", "environment-unbound", "")
	if err := writer.WriteKubernetesNamespaceNodes(context.Background(), []map[string]any{bound, unbound}, "reducer/kubernetes-namespaces"); err != nil {
		t.Fatalf("WriteKubernetesNamespaceNodes returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("len(groupCalls) = %d, want 1 atomic group", len(executor.groupCalls))
	}
	if len(executor.groupCalls[0]) != 2 {
		t.Fatalf("group statement count = %d, want 2 (bound + unbound variants)", len(executor.groupCalls[0]))
	}
}

func TestKubernetesNamespaceNodeWriterSatisfiesReducerInterface(t *testing.T) {
	t.Parallel()

	// Compile-time guarantee that the cypher writer satisfies the reducer-owned
	// consumer interface. This test fails to compile if the method set drifts.
	var _ interface {
		WriteKubernetesNamespaceNodes(ctx context.Context, rows []map[string]any, evidenceSource string) error
	} = NewKubernetesNamespaceNodeWriter(&recordingExecutor{}, 0)
}
