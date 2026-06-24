// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

// benchSecretsIAMNodeRows builds n uid-keyed node rows shaped like the extractor
// output: a stable uid plus scope/generation/evidence/confidence metadata. The
// uid space is wide (one per row) so the writer's uid-only MERGE batching is
// exercised without artificial dedupe collapsing the row count.
func benchSecretsIAMNodeRows(n int, uidPrefix string) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"uid":             fmt.Sprintf("sha256:%s-%d", uidPrefix, i),
			"scope_id":        "scope-1",
			"generation_id":   "gen-1",
			"evidence_source": "reducer/secrets-iam-graph",
			"confidence":      "exact",
		})
	}
	return rows
}

// benchSecretsIAMVaultAuthRoleNodeRows adds the vault_mount_join_key property the
// VaultAuthRole and SecretMetadataPath node templates SET.
func benchSecretsIAMVaultAuthRoleNodeRows(n int) []map[string]any {
	rows := benchSecretsIAMNodeRows(n, "vrole")
	for i := range rows {
		rows[i]["vault_mount_join_key"] = fmt.Sprintf("sha256:mount-%d", i%64)
	}
	return rows
}

// benchSecretsIAMSecretPathNodeRows adds the two join properties the
// SecretMetadataPath node template SETs.
func benchSecretsIAMSecretPathNodeRows(n int) []map[string]any {
	rows := benchSecretsIAMNodeRows(n, "path")
	for i := range rows {
		rows[i]["vault_mount_join_key"] = fmt.Sprintf("sha256:mount-%d", i%64)
		rows[i]["kv_path_fingerprint"] = fmt.Sprintf("sha256:kv-%d", i)
	}
	return rows
}

// benchSecretsIAMUsesServiceAccountEdgeRows shapes KubernetesWorkload->ServiceAccount
// edge rows. Both endpoint uids are present so the writer builds the full
// MATCH/MATCH/MERGE batch (a missing endpoint would still build a row; the no-op
// is a backend concern the no-op executor does not model).
func benchSecretsIAMUsesServiceAccountEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"workload_uid":        fmt.Sprintf("k8s://prod/apps/v1/deployments/ns/w-%d", i),
			"service_account_uid": fmt.Sprintf("sha256:sa-%d", i),
			"scope_id":            "scope-1",
			"generation_id":       "gen-1",
			"evidence_source":     "reducer/secrets-iam-graph",
			"confidence":          "exact",
			"evidence_fact_ids":   []string{fmt.Sprintf("f-%d", i)},
		})
	}
	return rows
}

// benchSecretsIAMAssumesIAMRoleEdgeRows shapes ServiceAccount->CloudResource
// edges, including the bounded assume_mode property the template SETs.
func benchSecretsIAMAssumesIAMRoleEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"service_account_uid": fmt.Sprintf("sha256:sa-%d", i),
			"cloud_resource_uid":  fmt.Sprintf("cloud:iam-role-%d", i),
			"assume_mode":         "web_identity",
			"scope_id":            "scope-1",
			"generation_id":       "gen-1",
			"evidence_source":     "reducer/secrets-iam-graph",
			"confidence":          "exact",
			"evidence_fact_ids":   []string{fmt.Sprintf("f-%d", i)},
		})
	}
	return rows
}

// benchSecretsIAMAuthVaultRoleEdgeRows shapes ServiceAccount->VaultAuthRole edges.
func benchSecretsIAMAuthVaultRoleEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"service_account_uid": fmt.Sprintf("sha256:sa-%d", i),
			"vault_auth_role_uid": fmt.Sprintf("sha256:vrole-%d", i),
			"scope_id":            "scope-1",
			"generation_id":       "gen-1",
			"evidence_source":     "reducer/secrets-iam-graph",
			"confidence":          "exact",
			"evidence_fact_ids":   []string{fmt.Sprintf("f-%d", i)},
		})
	}
	return rows
}

// benchSecretsIAMUsesVaultPolicyEdgeRows shapes VaultAuthRole->VaultPolicy edges.
func benchSecretsIAMUsesVaultPolicyEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"vault_auth_role_uid": fmt.Sprintf("sha256:vrole-%d", i),
			"vault_policy_uid":    fmt.Sprintf("sha256:pol-%d", i),
			"scope_id":            "scope-1",
			"generation_id":       "gen-1",
			"evidence_source":     "reducer/secrets-iam-graph",
			"confidence":          "exact",
			"evidence_fact_ids":   []string{fmt.Sprintf("f-%d", i)},
		})
	}
	return rows
}

// benchSecretsIAMGrantsSecretReadEdgeRows shapes VaultPolicy->SecretMetadataPath
// edges, including the capabilities slice property the template SETs.
func benchSecretsIAMGrantsSecretReadEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"vault_policy_uid":  fmt.Sprintf("sha256:pol-%d", i),
			"secret_path_uid":   fmt.Sprintf("sha256:path-%d", i),
			"capabilities":      []string{"read", "list"},
			"scope_id":          "scope-1",
			"generation_id":     "gen-1",
			"evidence_source":   "reducer/secrets-iam-graph",
			"confidence":        "exact",
			"evidence_fact_ids": []string{fmt.Sprintf("f-%d", i)},
		})
	}
	return rows
}

// BenchmarkSecretsIAMGraphWriter measures the statement-construction and batching
// cost of the secrets/IAM graph writer (ADR #1314 §12 proof) across all four
// SecretsIAM* node families and all five resolvable SECRETS_IAM_* edge families
// for a realistic per-scope-generation count. The backend executor is a no-op so
// the benchmark isolates Eshu-owned write-path work (uid-only MERGE node batches
// and MATCH/MATCH/MERGE edge batches) from graph round trips, proving the write
// side has no N+1 and stays in the same shape class as the proven CloudResource
// node writer and the RUNS_IMAGE / COVERS edge writers. It mirrors
// BenchmarkSecurityGroupReachabilityWriter and BenchmarkKubernetesCorrelationEdgeWriter
// so the no-regression comparison is against an established same-shape baseline.
func BenchmarkSecretsIAMGraphWriter(b *testing.B) {
	const n = 5000
	saNodes := benchSecretsIAMNodeRows(n, "sa")
	vaultRoleNodes := benchSecretsIAMVaultAuthRoleNodeRows(n)
	vaultPolicyNodes := benchSecretsIAMNodeRows(n, "pol")
	secretPathNodes := benchSecretsIAMSecretPathNodeRows(n)
	usesSAEdges := benchSecretsIAMUsesServiceAccountEdgeRows(n)
	assumesIAMRoleEdges := benchSecretsIAMAssumesIAMRoleEdgeRows(n)
	authRoleEdges := benchSecretsIAMAuthVaultRoleEdgeRows(n)
	usesPolicyEdges := benchSecretsIAMUsesVaultPolicyEdgeRows(n)
	grantsEdges := benchSecretsIAMGrantsSecretReadEdgeRows(n)

	writer := NewSecretsIAMGraphWriter(noopGroupExecutor{}, 500)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writer.WriteServiceAccountNodes(ctx, saNodes); err != nil {
			b.Fatalf("WriteServiceAccountNodes: %v", err)
		}
		if err := writer.WriteVaultAuthRoleNodes(ctx, vaultRoleNodes); err != nil {
			b.Fatalf("WriteVaultAuthRoleNodes: %v", err)
		}
		if err := writer.WriteVaultPolicyNodes(ctx, vaultPolicyNodes); err != nil {
			b.Fatalf("WriteVaultPolicyNodes: %v", err)
		}
		if err := writer.WriteSecretMetadataPathNodes(ctx, secretPathNodes); err != nil {
			b.Fatalf("WriteSecretMetadataPathNodes: %v", err)
		}
		if err := writer.WriteUsesServiceAccountEdges(ctx, usesSAEdges); err != nil {
			b.Fatalf("WriteUsesServiceAccountEdges: %v", err)
		}
		if err := writer.WriteAssumesIAMRoleEdges(ctx, assumesIAMRoleEdges); err != nil {
			b.Fatalf("WriteAssumesIAMRoleEdges: %v", err)
		}
		if err := writer.WriteAuthenticatesVaultRoleEdges(ctx, authRoleEdges); err != nil {
			b.Fatalf("WriteAuthenticatesVaultRoleEdges: %v", err)
		}
		if err := writer.WriteUsesVaultPolicyEdges(ctx, usesPolicyEdges); err != nil {
			b.Fatalf("WriteUsesVaultPolicyEdges: %v", err)
		}
		if err := writer.WriteGrantsSecretReadEdges(ctx, grantsEdges); err != nil {
			b.Fatalf("WriteGrantsSecretReadEdges: %v", err)
		}
	}
}
