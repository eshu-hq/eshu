// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

func rdsPostureNodeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"uid":                       "rds-" + string(rune('a'+i)),
			"rds_identifier":            "orders-" + string(rune('a'+i)),
			"rds_resource_type":         "aws_rds_db_instance",
			"rds_engine":                "postgres",
			"rds_publicly_accessible":   false,
			"rds_public_exposure_state": "not_public_endpoint",
			"rds_storage_encrypted":     true,
			"rds_kms_key_id":            "arn:aws:kms:us-east-1:111111111111:key/db",
			"rds_iam_database_authentication_enabled": true,
			"rds_multi_az":                            true,
			"rds_deletion_protection":                 true,
			"rds_backup_retention_period":             int64(7),
			"rds_performance_insights_enabled":        true,
			"rds_performance_insights_retention_days": int64(31),
			"rds_performance_insights_kms_key_id":     "arn:aws:kms:us-east-1:111111111111:key/pi",
			"rds_ca_certificate_identifier":           "rds-ca-rsa2048-g1",
			"rds_parameter_groups":                    []string{"orders-params"},
			"rds_option_groups":                       []string{"orders-options"},
			"rds_security_parameters":                 []string{"rds.force_ssl=1"},
			"source_fact_id":                          "fact-posture",
		})
	}
	return rows
}

func TestRDSPostureNodeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewRDSPostureNodeWriter(executor, &echoingPostureExistenceReader{}, 0)

	if err := writer.WriteRDSPostureNodes(context.Background(), nil, "scope-1", "gen-1", "reducer/rds-posture"); err != nil {
		t.Fatalf("WriteRDSPostureNodes returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestRDSPostureNodeWriterMergesConfirmedExistingCloudResourceAndSetsPosture(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewRDSPostureNodeWriter(executor, &echoingPostureExistenceReader{}, 0)

	if err := writer.WriteRDSPostureNodes(context.Background(), rdsPostureNodeRows(1), "scope-1", "gen-1", "reducer/rds-posture"); err != nil {
		t.Fatalf("WriteRDSPostureNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "UNWIND $rows AS row") {
		t.Fatalf("cypher missing UNWIND batch shape:\n%s", cypher)
	}
	// Issue #5652: a bare-MATCH-anchored UNWIND SET silently drops its write
	// on the pinned production NornicDB image, so the shipped statement
	// anchors with MERGE. Never-create is enforced in Go instead: see
	// TestRDSPostureNodeWriterNeverCreatesUnconfirmedCloudResource.
	if !strings.Contains(cypher, "MERGE (r:CloudResource {uid: row.uid})") {
		t.Fatalf("cypher must MERGE-anchor on the existing CloudResource uid:\n%s", cypher)
	}
	if strings.Contains(cypher, "CREATE (r:CloudResource") {
		t.Fatalf("RDS posture writer must not use bare CREATE:\n%s", cypher)
	}
	for _, want := range []string{
		"r.rds_publicly_accessible = row.rds_publicly_accessible",
		"r.rds_public_exposure_state = row.rds_public_exposure_state",
		"r.rds_storage_encrypted = row.rds_storage_encrypted",
		"r.rds_backup_retention_period = row.rds_backup_retention_period",
		"r.rds_posture_scope_id = row.scope_id",
		"r.rds_posture_generation_id = row.generation_id",
		"r.rds_posture_evidence_source = row.evidence_source",
		"r.rds_posture_source_fact_id = row.source_fact_id",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher missing %q:\n%s", want, cypher)
		}
	}
}

// TestRDSPostureNodeWriterNeverCreatesUnconfirmedCloudResource proves the
// never-create contract survives the MATCH->MERGE fix (issue #5652).
func TestRDSPostureNodeWriterNeverCreatesUnconfirmedCloudResource(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	reader := &echoingPostureExistenceReader{ExistingUIDs: map[string]bool{"rds-a": true}}
	writer := NewRDSPostureNodeWriter(executor, reader, 0)

	rows := rdsPostureNodeRows(1)
	rows = append(rows, map[string]any{"uid": "rds-missing", "rds_identifier": "orders-missing", "source_fact_id": "fact-2"})
	if err := writer.WriteRDSPostureNodes(context.Background(), rows, "scope-1", "gen-1", "reducer/rds-posture"); err != nil {
		t.Fatalf("WriteRDSPostureNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	writtenRows := executor.calls[0].Parameters["rows"].([]map[string]any)
	if len(writtenRows) != 1 {
		t.Fatalf("len(writtenRows) = %d, want 1 (only the confirmed-existing uid)", len(writtenRows))
	}
	if got := writtenRows[0]["uid"]; got != "rds-a" {
		t.Fatalf("writtenRows[0][uid] = %v, want rds-a", got)
	}
}

// TestRDSPostureNodeWriterRequiresReader proves the writer fails fast instead
// of silently defaulting to bare-MATCH semantics without a reader.
func TestRDSPostureNodeWriterRequiresReader(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewRDSPostureNodeWriter(executor, nil, 0)

	if err := writer.WriteRDSPostureNodes(context.Background(), rdsPostureNodeRows(1), "scope-1", "gen-1", "reducer/rds-posture"); err == nil {
		t.Fatal("WriteRDSPostureNodes() error = nil, want error for nil reader")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when reader is nil", len(executor.calls))
	}
}

func TestRDSPostureNodeWriterRetractRemovesOnlyReducerOwnedProperties(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewRDSPostureNodeWriter(executor, &echoingPostureExistenceReader{}, 0)

	if err := writer.RetractRDSPostureNodes(context.Background(), []string{"scope-1"}, "gen-1", "reducer/rds-posture"); err != nil {
		t.Fatalf("RetractRDSPostureNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "MATCH (r:CloudResource)") {
		t.Fatalf("retract must match CloudResource nodes:\n%s", cypher)
	}
	if !strings.Contains(cypher, "r.rds_posture_scope_id IN $scope_ids") {
		t.Fatalf("retract must scope by reducer posture scope id:\n%s", cypher)
	}
	if !strings.Contains(cypher, "r.rds_posture_evidence_source = $evidence_source") {
		t.Fatalf("retract must scope by reducer evidence source:\n%s", cypher)
	}
	if !strings.Contains(cypher, "REMOVE r.rds_identifier") || !strings.Contains(cypher, "r.rds_posture_source_fact_id") {
		t.Fatalf("retract must REMOVE reducer-owned posture properties:\n%s", cypher)
	}
	if strings.Contains(cypher, "DELETE") || strings.Contains(cypher, "DETACH") {
		t.Fatalf("retract must not delete CloudResource nodes:\n%s", cypher)
	}
}

func TestRDSPostureNodeWriterRetractEmptyScopesIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewRDSPostureNodeWriter(executor, &echoingPostureExistenceReader{}, 0)

	if err := writer.RetractRDSPostureNodes(context.Background(), nil, "gen-1", "reducer/rds-posture"); err != nil {
		t.Fatalf("RetractRDSPostureNodes returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty scope set", len(executor.calls))
	}
}
