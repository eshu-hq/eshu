// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

func s3ExternalPrincipalGrantRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"source_uid":           "bucket-" + string(rune('a'+i)),
			"principal_uid":        "principal-" + string(rune('a'+i)),
			"principal_kind":       "aws_account",
			"principal_value":      "99998888777" + string(rune('0'+i)),
			"principal_account_id": "99998888777" + string(rune('0'+i)),
			"principal_partition":  "aws",
			"principal_service":    "",
			"relationship_type":    "GRANTS_ACCESS_TO",
			"grant_outcome":        "cross_account",
			"is_public":            false,
			"is_cross_account":     true,
			"is_service_principal": false,
			"resolution_mode":      "bucket_name",
		})
	}
	return rows
}

func TestS3ExternalPrincipalGrantWriterUsesStaticNodeAndEdgeMerge(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewS3ExternalPrincipalGrantWriter(executor, 0)

	if err := writer.WriteS3ExternalPrincipalGrants(context.Background(), s3ExternalPrincipalGrantRows(1), "scope-1", "gen-1", "reducer/s3-external-principal-grant"); err != nil {
		t.Fatalf("WriteS3ExternalPrincipalGrants returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "MATCH (source:CloudResource {uid: row.source_uid})") {
		t.Fatalf("cypher must MATCH the source S3 bucket CloudResource by uid:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (principal:ExternalPrincipal {uid: row.principal_uid})") {
		t.Fatalf("cypher must MERGE ExternalPrincipal by uid:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (source)-[rel:GRANTS_ACCESS_TO]->(principal)") {
		t.Fatalf("edge MERGE must use static GRANTS_ACCESS_TO relationship type:\n%s", cypher)
	}
	if strings.Contains(cypher, "policy_document") || strings.Contains(cypher, "condition") || strings.Contains(cypher, "acl_grants") || strings.Contains(cypher, "object_keys") {
		t.Fatalf("cypher must not write raw policy/ACL/object fields:\n%s", cypher)
	}
}

func TestS3ExternalPrincipalGrantWriterRejectsForeignRelationshipType(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewS3ExternalPrincipalGrantWriter(executor, 0)

	rows := s3ExternalPrincipalGrantRows(1)
	rows[0]["relationship_type"] = "OWNS_THE_INTERNET"
	if err := writer.WriteS3ExternalPrincipalGrants(context.Background(), rows, "scope-1", "gen-1", "reducer/s3-external-principal-grant"); err == nil {
		t.Fatal("WriteS3ExternalPrincipalGrants accepted an out-of-vocabulary relationship_type")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when validation rejects the row", len(executor.calls))
	}
}

func TestS3ExternalPrincipalGrantWriterPreservesOptionalMetadataWhenRowIsEmpty(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewS3ExternalPrincipalGrantWriter(executor, 0)

	rows := s3ExternalPrincipalGrantRows(1)
	rows[0]["principal_account_id"] = ""
	rows[0]["principal_partition"] = ""
	rows[0]["principal_service"] = ""
	if err := writer.WriteS3ExternalPrincipalGrants(context.Background(), rows, "scope-1", "gen-1", "reducer/s3-external-principal-grant"); err != nil {
		t.Fatalf("WriteS3ExternalPrincipalGrants returned error: %v", err)
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"principal.principal_account_id = CASE WHEN row.principal_account_id <> '' THEN row.principal_account_id ELSE principal.principal_account_id END",
		"principal.principal_partition = CASE WHEN row.principal_partition <> '' THEN row.principal_partition ELSE principal.principal_partition END",
		"principal.principal_service = CASE WHEN row.principal_service <> '' THEN row.principal_service ELSE principal.principal_service END",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher must preserve prior optional principal metadata when the row is empty; missing %q in:\n%s", want, cypher)
		}
	}
}

func TestS3ExternalPrincipalGrantWriterRetractScopesToEvidenceSource(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewS3ExternalPrincipalGrantWriter(executor, 0)

	if err := writer.RetractS3ExternalPrincipalGrants(context.Background(), []string{"scope-1"}, "gen-1", "reducer/s3-external-principal-grant"); err != nil {
		t.Fatalf("RetractS3ExternalPrincipalGrants returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "MATCH (:CloudResource)-[rel:GRANTS_ACCESS_TO]->(:ExternalPrincipal)") {
		t.Fatalf("retract must target reducer-owned grant edges only:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.scope_id IN $scope_ids") {
		t.Fatalf("retract must scope by edge scope_id:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.evidence_source = $evidence_source") {
		t.Fatalf("retract must scope by evidence_source:\n%s", cypher)
	}
}
