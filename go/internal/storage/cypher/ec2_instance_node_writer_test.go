// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

func ec2InstanceNodeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"uid":                         "ec2-uid-" + string(rune('a'+i)),
			"arn":                         "arn:aws:ec2:us-east-1:111122223333:instance/i-0abc",
			"resource_id":                 "i-0abc",
			"resource_type":               "aws_ec2_instance",
			"name":                        "i-0abc",
			"state":                       "running",
			"account_id":                  "111122223333",
			"region":                      "us-east-1",
			"service_kind":                "ec2",
			"correlation_anchors":         []string{"i-0abc"},
			"imds_v2_required":            true,
			"imds_http_endpoint":          "enabled",
			"imds_http_put_hop_limit":     int32(1),
			"user_data_present":           false,
			"detailed_monitoring_enabled": false,
			"ebs_optimized":               true,
			"public_ip_associated":        true,
			"instance_profile_arn":        "arn:aws:iam::111122223333:instance-profile/app",
			"tenancy":                     "default",
			"nitro_enclave_enabled":       false,
			"source_fact_id":              "fact-1",
			"stable_fact_key":             "key-1",
			"source_system":               "aws",
			"source_record_id":            "rec-1",
			"source_confidence":           "reported",
			"collector_kind":              "awscloud",
		})
	}
	return rows
}

func TestEC2InstanceNodeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2InstanceNodeWriter(executor, 0)

	if err := writer.WriteEC2InstanceNodes(context.Background(), nil, "reducer/ec2-instances"); err != nil {
		t.Fatalf("WriteEC2InstanceNodes returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestEC2InstanceNodeWriterMergesOnUID(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2InstanceNodeWriter(executor, 0)

	if err := writer.WriteEC2InstanceNodes(context.Background(), ec2InstanceNodeRows(1), "reducer/ec2-instances"); err != nil {
		t.Fatalf("WriteEC2InstanceNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "UNWIND $rows AS row") {
		t.Fatalf("cypher missing UNWIND batch shape:\n%s", cypher)
	}
	// EC2 instances are CloudResource nodes (same label/constraint as #805), so
	// the USES_PROFILE edge (PR-B) can resolve both endpoints on cloud_resource_uid.
	if !strings.Contains(cypher, "MERGE (r:CloudResource {uid: row.uid})") {
		t.Fatalf("cypher must MERGE on the CloudResource uid identity only:\n%s", cypher)
	}
	if strings.Contains(cypher, "MERGE (r:CloudResource {uid: row.uid, instance_profile_arn") {
		t.Fatalf("cypher must not MERGE on a wide mutable map:\n%s", cypher)
	}
	// The derived posture properties must be SET, never used as identity.
	for _, prop := range []string{
		"r.instance_profile_arn = row.instance_profile_arn",
		"r.imds_v2_required = row.imds_v2_required",
		"r.user_data_present = row.user_data_present",
		"r.public_ip_associated = row.public_ip_associated",
		"r.nitro_enclave_enabled = row.nitro_enclave_enabled",
		"r.evidence_source = row.evidence_source",
	} {
		if !strings.Contains(cypher, prop) {
			t.Fatalf("cypher must SET %q:\n%s", prop, cypher)
		}
	}
	// NEVER carry user-data content or the raw public IP on the node.
	if strings.Contains(cypher, "user_data") && !strings.Contains(cypher, "user_data_present") {
		t.Fatalf("cypher must only carry user_data_present, never user-data content:\n%s", cypher)
	}
	if strings.Contains(cypher, "public_ip_address") {
		t.Fatalf("cypher must not carry the raw public IP address:\n%s", cypher)
	}
	rows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows parameter type = %T, want []map[string]any", executor.calls[0].Parameters["rows"])
	}
	if got := rows[0]["evidence_source"]; got != "reducer/ec2-instances" {
		t.Fatalf("evidence_source = %v, want reducer/ec2-instances", got)
	}
}

func TestEC2InstanceNodeWriterBatchesRows(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2InstanceNodeWriter(executor, 2)

	if err := writer.WriteEC2InstanceNodes(context.Background(), ec2InstanceNodeRows(5), "reducer/ec2-instances"); err != nil {
		t.Fatalf("WriteEC2InstanceNodes returned error: %v", err)
	}
	// 5 rows at batch size 2 -> 3 statements.
	if len(executor.calls) != 3 {
		t.Fatalf("len(calls) = %d, want 3 batched statements", len(executor.calls))
	}
}

func TestEC2InstanceNodeWriterUsesGroupExecutorAtomically(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewEC2InstanceNodeWriter(executor, 2)

	if err := writer.WriteEC2InstanceNodes(context.Background(), ec2InstanceNodeRows(5), "reducer/ec2-instances"); err != nil {
		t.Fatalf("WriteEC2InstanceNodes returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("len(groupCalls) = %d, want 1 atomic group", len(executor.groupCalls))
	}
	if len(executor.groupCalls[0]) != 3 {
		t.Fatalf("group statement count = %d, want 3", len(executor.groupCalls[0]))
	}
}

func TestEC2InstanceNodeWriterStatementMetadata(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2InstanceNodeWriter(executor, 0)

	if err := writer.WriteEC2InstanceNodes(context.Background(), ec2InstanceNodeRows(1), "reducer/ec2-instances"); err != nil {
		t.Fatalf("WriteEC2InstanceNodes returned error: %v", err)
	}
	params := executor.calls[0].Parameters
	if got := params[StatementMetadataPhaseKey]; got != canonicalPhaseEC2Instance {
		t.Fatalf("phase metadata = %v, want %q", got, canonicalPhaseEC2Instance)
	}
	if got := params[StatementMetadataEntityLabelKey]; got != "CloudResource" {
		t.Fatalf("entity label metadata = %v, want CloudResource", got)
	}
}

func TestEC2InstanceNodeWriterSatisfiesReducerInterface(t *testing.T) {
	t.Parallel()

	// Compile-time guarantee that the cypher writer satisfies the reducer-owned
	// consumer interface. This test fails to compile if the method set drifts.
	var _ interface {
		WriteEC2InstanceNodes(ctx context.Context, rows []map[string]any, evidenceSource string) error
	} = NewEC2InstanceNodeWriter(&recordingExecutor{}, 0)
}
