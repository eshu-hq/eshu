// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

const secretsIAMGraphEvidence = "reducer/secrets-iam-graph"

func saNodeRows() []map[string]any {
	return []map[string]any{
		{"uid": "sha256:sa1", "scope_id": "scope-1", "generation_id": "gen-1", "evidence_source": secretsIAMGraphEvidence, "confidence": "exact"},
		{"uid": "sha256:sa2", "scope_id": "scope-1", "generation_id": "gen-1", "evidence_source": secretsIAMGraphEvidence, "confidence": "exact"},
	}
}

func TestSecretsIAMGraphWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	exec := &recordingExecutor{}
	w := NewSecretsIAMGraphWriter(exec, 0)
	if err := w.WriteServiceAccountNodes(context.Background(), nil); err != nil {
		t.Fatalf("WriteServiceAccountNodes(nil) error = %v", err)
	}
	if len(exec.calls) != 0 {
		t.Fatalf("empty rows issued %d statements, want 0", len(exec.calls))
	}
}

func TestSecretsIAMGraphWriterNilExecutor(t *testing.T) {
	t.Parallel()

	w := &SecretsIAMGraphWriter{}
	if err := w.WriteServiceAccountNodes(context.Background(), saNodeRows()); err == nil {
		t.Fatal("nil executor: error = nil, want non-nil")
	}
}

func TestSecretsIAMGraphWriterNodeCypherShape(t *testing.T) {
	t.Parallel()

	exec := &recordingExecutor{}
	w := NewSecretsIAMGraphWriter(exec, 0)
	if err := w.WriteServiceAccountNodes(context.Background(), saNodeRows()); err != nil {
		t.Fatalf("WriteServiceAccountNodes error = %v", err)
	}
	if len(exec.calls) != 1 {
		t.Fatalf("statements = %d, want 1", len(exec.calls))
	}
	c := exec.calls[0].Cypher
	for _, want := range []string{
		"UNWIND $rows AS row",
		"MERGE (n:SecretsIAMServiceAccount {uid: row.uid})",
		"SET n.scope_id = row.scope_id",
	} {
		if !strings.Contains(c, want) {
			t.Fatalf("node cypher missing %q:\n%s", want, c)
		}
	}
	if exec.calls[0].Operation != OperationCanonicalUpsert {
		t.Fatalf("operation = %q, want canonical_upsert", exec.calls[0].Operation)
	}
	// MERGE identity must be uid-only — no mutable property in the MERGE clause.
	mergeLine := c[strings.Index(c, "MERGE") : strings.Index(c, "MERGE")+len("MERGE (n:SecretsIAMServiceAccount {uid: row.uid})")]
	if strings.Contains(mergeLine, "confidence") || strings.Contains(mergeLine, "scope_id") {
		t.Fatalf("MERGE identity is not uid-only: %q", mergeLine)
	}
}

func TestSecretsIAMGraphWriterEdgeCypherMatchesEndpoints(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		call func(*SecretsIAMGraphWriter) error
		want []string
	}{
		{"uses_service_account", func(w *SecretsIAMGraphWriter) error {
			return w.WriteUsesServiceAccountEdges(context.Background(), []map[string]any{{"workload_uid": "wl-1", "service_account_uid": "sha256:sa1", "scope_id": "scope-1"}})
		}, []string{"MATCH (w:KubernetesWorkload {uid: row.workload_uid})", "MATCH (s:SecretsIAMServiceAccount {uid: row.service_account_uid})", "MERGE (w)-[rel:SECRETS_IAM_USES_SERVICE_ACCOUNT]->(s)"}},
		{"grants_secret_read", func(w *SecretsIAMGraphWriter) error {
			return w.WriteGrantsSecretReadEdges(context.Background(), []map[string]any{{"vault_policy_uid": "sha256:pol", "secret_path_uid": "sha256:path", "scope_id": "scope-1", "capabilities": []string{"read"}}})
		}, []string{"MATCH (p:SecretsIAMVaultPolicy {uid: row.vault_policy_uid})", "MATCH (s:SecretsIAMSecretMetadataPath {uid: row.secret_path_uid})", "MERGE (p)-[rel:SECRETS_IAM_GRANTS_SECRET_READ]->(s)", "rel.capabilities = row.capabilities"}},
		{"assumes_iam_role", func(w *SecretsIAMGraphWriter) error {
			return w.WriteAssumesIAMRoleEdges(context.Background(), []map[string]any{{"service_account_uid": "sha256:sa1", "cloud_resource_uid": "cr-uid", "assume_mode": "web_identity", "scope_id": "scope-1"}})
		}, []string{"MATCH (s:SecretsIAMServiceAccount {uid: row.service_account_uid})", "MATCH (c:CloudResource {uid: row.cloud_resource_uid})", "MERGE (s)-[rel:SECRETS_IAM_ASSUMES_IAM_ROLE]->(c)", "rel.assume_mode = row.assume_mode"}},
		{"authenticates_vault_role", func(w *SecretsIAMGraphWriter) error {
			return w.WriteAuthenticatesVaultRoleEdges(context.Background(), []map[string]any{{"service_account_uid": "sha256:sa1", "vault_auth_role_uid": "sha256:vr", "scope_id": "scope-1"}})
		}, []string{"MATCH (s:SecretsIAMServiceAccount {uid: row.service_account_uid})", "MATCH (v:SecretsIAMVaultAuthRole {uid: row.vault_auth_role_uid})", "MERGE (s)-[rel:SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE]->(v)"}},
		{"uses_vault_policy", func(w *SecretsIAMGraphWriter) error {
			return w.WriteUsesVaultPolicyEdges(context.Background(), []map[string]any{{"vault_auth_role_uid": "sha256:vr", "vault_policy_uid": "sha256:pol", "scope_id": "scope-1"}})
		}, []string{"MATCH (v:SecretsIAMVaultAuthRole {uid: row.vault_auth_role_uid})", "MATCH (p:SecretsIAMVaultPolicy {uid: row.vault_policy_uid})", "MERGE (v)-[rel:SECRETS_IAM_USES_VAULT_POLICY]->(p)"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			exec := &recordingExecutor{}
			if err := tc.call(NewSecretsIAMGraphWriter(exec, 0)); err != nil {
				t.Fatalf("%s error = %v", tc.name, err)
			}
			if len(exec.calls) != 1 {
				t.Fatalf("%s statements = %d, want 1", tc.name, len(exec.calls))
			}
			for _, want := range tc.want {
				if !strings.Contains(exec.calls[0].Cypher, want) {
					t.Fatalf("%s cypher missing %q:\n%s", tc.name, want, exec.calls[0].Cypher)
				}
			}
		})
	}
}

func TestSecretsIAMGraphWriterRetractIsScoped(t *testing.T) {
	t.Parallel()

	exec := &recordingExecutor{}
	w := NewSecretsIAMGraphWriter(exec, 0)
	if err := w.RetractScope(context.Background(), []string{"scope-1"}, secretsIAMGraphEvidence); err != nil {
		t.Fatalf("RetractScope error = %v", err)
	}
	// One workload-edge retract + four node DETACH DELETEs.
	if len(exec.calls) != 5 {
		t.Fatalf("retract statements = %d, want 5", len(exec.calls))
	}
	var sawEdge, sawNodeDetach int
	firstEdgeRetract := -1
	lastNodeDetach := -1
	for i, stmt := range exec.calls {
		if stmt.Operation != OperationCanonicalRetract {
			t.Fatalf("retract operation = %q", stmt.Operation)
		}
		for _, want := range []string{
			"scope_id = $scope_id",
			"evidence_source = $evidence_source",
		} {
			if !strings.Contains(stmt.Cypher, want) {
				t.Fatalf("retract missing %q:\n%s", want, stmt.Cypher)
			}
		}
		if strings.Contains(stmt.Cypher, " IN $scope_ids") || strings.Contains(stmt.Cypher, "UNWIND $scope_ids") {
			t.Fatalf("retract uses list predicate or UNWIND mutation path:\n%s", stmt.Cypher)
		}
		if strings.Contains(stmt.Cypher, "DELETE rel") {
			sawEdge++
			if firstEdgeRetract == -1 {
				firstEdgeRetract = i
			}
		}
		if strings.Contains(stmt.Cypher, "DETACH DELETE n") {
			sawNodeDetach++
			lastNodeDetach = i
		}
		// Retract must never touch the retained endpoint nodes.
		if strings.Contains(stmt.Cypher, "DETACH DELETE w") || strings.Contains(stmt.Cypher, "DELETE (w:KubernetesWorkload)") || strings.Contains(stmt.Cypher, "DELETE (r:CloudResource)") {
			t.Fatalf("retract deletes a retained endpoint node:\n%s", stmt.Cypher)
		}
	}
	if sawEdge != 1 || sawNodeDetach != 4 {
		t.Fatalf("retract shape: edge=%d nodeDetach=%d, want 1 and 4", sawEdge, sawNodeDetach)
	}
	if firstEdgeRetract <= lastNodeDetach {
		t.Fatalf("workload edge cleanup must run after node DETACH DELETEs: firstEdge=%d lastNode=%d", firstEdgeRetract, lastNodeDetach)
	}
	if got, ok := exec.calls[0].Parameters["scope_id"].(string); !ok || got != "scope-1" {
		t.Fatalf("retract scope_id param = %v", exec.calls[0].Parameters["scope_id"])
	}

	// Empty scope set is a no-op.
	exec2 := &recordingExecutor{}
	if err := NewSecretsIAMGraphWriter(exec2, 0).RetractScope(context.Background(), nil, secretsIAMGraphEvidence); err != nil || len(exec2.calls) != 0 {
		t.Fatalf("empty retract: err=%v calls=%d", err, len(exec2.calls))
	}
}

func TestSecretsIAMGraphWriterRetractUsesSequentialStatements(t *testing.T) {
	t.Parallel()

	exec := &secretsIAMRecordingGroupExecutor{}
	w := NewSecretsIAMGraphWriter(exec, 0)
	if err := w.RetractScope(context.Background(), []string{"scope-1"}, secretsIAMGraphEvidence); err != nil {
		t.Fatalf("RetractScope error = %v", err)
	}
	if exec.groupCalls != 0 {
		t.Fatalf("RetractScope used ExecuteGroup %d times, want 0", exec.groupCalls)
	}
	if got, want := len(exec.calls), 5; got != want {
		t.Fatalf("RetractScope Execute calls = %d, want %d", got, want)
	}

	exec.calls = nil
	if err := w.WriteServiceAccountNodes(context.Background(), saNodeRows()); err != nil {
		t.Fatalf("WriteServiceAccountNodes error = %v", err)
	}
	if exec.groupCalls != 1 {
		t.Fatalf("WriteServiceAccountNodes ExecuteGroup calls = %d, want 1", exec.groupCalls)
	}
	if len(exec.calls) != 0 {
		t.Fatalf("WriteServiceAccountNodes Execute calls = %d, want 0", len(exec.calls))
	}
}

func TestSecretsIAMGraphWriterBatches(t *testing.T) {
	t.Parallel()

	rows := make([]map[string]any, 0, 5)
	for i := 0; i < 5; i++ {
		rows = append(rows, map[string]any{"uid": string(rune('a' + i)), "scope_id": "scope-1"})
	}
	exec := &recordingExecutor{}
	if err := NewSecretsIAMGraphWriter(exec, 2).WriteVaultPolicyNodes(context.Background(), rows); err != nil {
		t.Fatalf("WriteVaultPolicyNodes error = %v", err)
	}
	if len(exec.calls) != 3 { // 2 + 2 + 1
		t.Fatalf("batched statements = %d, want 3", len(exec.calls))
	}
}

type secretsIAMRecordingGroupExecutor struct {
	calls      []Statement
	groupCalls int
}

func (e *secretsIAMRecordingGroupExecutor) Execute(_ context.Context, stmt Statement) error {
	e.calls = append(e.calls, stmt)
	return nil
}

func (e *secretsIAMRecordingGroupExecutor) ExecuteGroup(_ context.Context, _ []Statement) error {
	e.groupCalls++
	return nil
}
