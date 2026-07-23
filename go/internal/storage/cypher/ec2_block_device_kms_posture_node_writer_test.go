// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

func ec2BlockDeviceKMSPostureRows() []map[string]any {
	return []map[string]any{{
		"uid":                      "cloud-resource-ec2-1",
		"state":                    "encrypted",
		"reason":                   "all_volumes_customer_managed_kms",
		"volume_count":             int64(1),
		"encrypted_volume_count":   int64(1),
		"unencrypted_volume_count": int64(0),
		"unresolved_volume_count":  int64(0),
		"kms_key_count":            int64(1),
		"volume_ids":               []string{"vol-0abc"},
		"kms_key_ids":              []string{"arn:aws:kms:us-east-1:111122223333:key/customer"},
		"source_fact_id":           "fact-posture-1",
	}}
}

func TestEC2BlockDeviceKMSPostureNodeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2BlockDeviceKMSPostureNodeWriter(executor, &echoingPostureExistenceReader{}, 0)

	if err := writer.WriteEC2BlockDeviceKMSPostureNodes(context.Background(), nil, "scope-1", "gen-1", "reducer/ec2-block-device-kms-posture"); err != nil {
		t.Fatalf("WriteEC2BlockDeviceKMSPostureNodes returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestEC2BlockDeviceKMSPostureNodeWriterMergesConfirmedExistingCloudResourceAndSetsProperties(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2BlockDeviceKMSPostureNodeWriter(executor, &echoingPostureExistenceReader{}, 0)

	if err := writer.WriteEC2BlockDeviceKMSPostureNodes(context.Background(), ec2BlockDeviceKMSPostureRows(), "scope-1", "gen-1", "reducer/ec2-block-device-kms-posture"); err != nil {
		t.Fatalf("WriteEC2BlockDeviceKMSPostureNodes returned error: %v", err)
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
	// TestEC2BlockDeviceKMSPostureNodeWriterNeverCreatesUnconfirmedCloudResource.
	if !strings.Contains(cypher, "MERGE (resource:CloudResource {uid: row.uid})") {
		t.Fatalf("cypher must MERGE-anchor on the CloudResource uid:\n%s", cypher)
	}
	if strings.Contains(cypher, "CREATE") {
		t.Fatalf("ec2 block-device KMS posture must never use bare CREATE:\n%s", cypher)
	}
	for _, want := range []string{
		"resource.ec2_block_device_kms_state = row.state",
		"resource.ec2_block_device_kms_reason = row.reason",
		"resource.ec2_block_device_volume_count = row.volume_count",
		"resource.ec2_block_device_encrypted_volume_count = row.encrypted_volume_count",
		"resource.ec2_block_device_unencrypted_volume_count = row.unencrypted_volume_count",
		"resource.ec2_block_device_unresolved_volume_count = row.unresolved_volume_count",
		"resource.ec2_block_device_kms_key_count = row.kms_key_count",
		"resource.ec2_block_device_volume_ids = row.volume_ids",
		"resource.ec2_block_device_kms_key_ids = row.kms_key_ids",
		"resource.ec2_block_device_kms_source_fact_id = row.source_fact_id",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher missing %q:\n%s", want, cypher)
		}
	}
	rows := executor.calls[0].Parameters["rows"].([]map[string]any)
	if got, want := rows[0]["scope_id"], "scope-1"; got != want {
		t.Fatalf("scope_id = %v, want %v", got, want)
	}
}

// TestEC2BlockDeviceKMSPostureNodeWriterNeverCreatesUnconfirmedCloudResource
// proves the never-create contract survives the MATCH->MERGE fix (#5652).
func TestEC2BlockDeviceKMSPostureNodeWriterNeverCreatesUnconfirmedCloudResource(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	reader := &echoingPostureExistenceReader{ExistingUIDs: map[string]bool{"cloud-resource-ec2-1": true}}
	writer := NewEC2BlockDeviceKMSPostureNodeWriter(executor, reader, 0)

	rows := append(ec2BlockDeviceKMSPostureRows(), map[string]any{
		"uid": "cloud-resource-ec2-missing", "state": "unknown", "reason": "unresolved",
		"volume_count": int64(0), "encrypted_volume_count": int64(0), "unencrypted_volume_count": int64(0),
		"unresolved_volume_count": int64(0), "kms_key_count": int64(0),
		"volume_ids": []string{}, "kms_key_ids": []string{}, "source_fact_id": "fact-2",
	})
	if err := writer.WriteEC2BlockDeviceKMSPostureNodes(context.Background(), rows, "scope-1", "gen-1", "reducer/ec2-block-device-kms-posture"); err != nil {
		t.Fatalf("WriteEC2BlockDeviceKMSPostureNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	writtenRows := executor.calls[0].Parameters["rows"].([]map[string]any)
	if len(writtenRows) != 1 {
		t.Fatalf("len(writtenRows) = %d, want 1 (only the confirmed-existing uid)", len(writtenRows))
	}
	if got := writtenRows[0]["uid"]; got != "cloud-resource-ec2-1" {
		t.Fatalf("writtenRows[0][uid] = %v, want cloud-resource-ec2-1", got)
	}
}

// TestEC2BlockDeviceKMSPostureNodeWriterRequiresReader proves the writer
// fails fast instead of silently defaulting to bare-MATCH semantics.
func TestEC2BlockDeviceKMSPostureNodeWriterRequiresReader(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2BlockDeviceKMSPostureNodeWriter(executor, nil, 0)

	if err := writer.WriteEC2BlockDeviceKMSPostureNodes(context.Background(), ec2BlockDeviceKMSPostureRows(), "scope-1", "gen-1", "reducer/ec2-block-device-kms-posture"); err == nil {
		t.Fatal("WriteEC2BlockDeviceKMSPostureNodes() error = nil, want error for nil reader")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when reader is nil", len(executor.calls))
	}
}

func TestEC2BlockDeviceKMSPostureNodeWriterRetractRemovesOnlyReducerOwnedProperties(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2BlockDeviceKMSPostureNodeWriter(executor, &echoingPostureExistenceReader{}, 0)

	if err := writer.RetractEC2BlockDeviceKMSPostureNodes(context.Background(), []string{"scope-1"}, "gen-1", "reducer/ec2-block-device-kms-posture"); err != nil {
		t.Fatalf("RetractEC2BlockDeviceKMSPostureNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "MATCH (resource:CloudResource)") {
		t.Fatalf("retract must match CloudResource nodes:\n%s", cypher)
	}
	if !strings.Contains(cypher, "resource.ec2_block_device_kms_scope_id IN $scope_ids") {
		t.Fatalf("retract must scope by reducer-owned scope property:\n%s", cypher)
	}
	if !strings.Contains(cypher, "resource.ec2_block_device_kms_evidence_source = $evidence_source") {
		t.Fatalf("retract must scope by evidence_source:\n%s", cypher)
	}
	if !strings.Contains(cypher, "REMOVE resource.ec2_block_device_kms_state") {
		t.Fatalf("retract must remove reducer-owned properties:\n%s", cypher)
	}
	if strings.Contains(cypher, "DELETE resource") {
		t.Fatalf("retract must never delete CloudResource nodes:\n%s", cypher)
	}
}
