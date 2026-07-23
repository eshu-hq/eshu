// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

func cloudResourceRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"uid":                 "uid-" + string(rune('a'+i)),
			"arn":                 "arn:aws:ec2:us-east-1:sample-account:vpc/vpc-1",
			"resource_id":         "vpc-1",
			"resource_type":       "aws_ec2_vpc",
			"name":                "main",
			"state":               "available",
			"account_id":          "sample-account",
			"region":              "us-east-1",
			"service_kind":        "vpc",
			"correlation_anchors": []string{"vpc-1"},
			"source_fact_id":      "fact-1",
			"stable_fact_key":     "key-1",
			"source_system":       "aws",
			"source_record_id":    "rec-1",
			"source_confidence":   "reported",
			"collector_kind":      "aws",
		})
	}
	return rows
}

func TestCloudResourceNodeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceNodeWriter(executor, 0)

	if err := writer.WriteCloudResourceNodes(context.Background(), nil, "reducer/aws-resources"); err != nil {
		t.Fatalf("WriteCloudResourceNodes returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestCloudResourceNodeWriterMergesOnUID(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceNodeWriter(executor, 0)

	if err := writer.WriteCloudResourceNodes(context.Background(), cloudResourceRows(1), "reducer/aws-resources"); err != nil {
		t.Fatalf("WriteCloudResourceNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "UNWIND $rows AS row") {
		t.Fatalf("cypher missing UNWIND batch shape:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (r:CloudResource {uid: row.uid})") {
		t.Fatalf("cypher must MERGE on uid identity only:\n%s", cypher)
	}
	if strings.Contains(cypher, "MERGE (r:CloudResource {uid: row.uid, name") {
		t.Fatalf("cypher must not MERGE on a wide mutable map:\n%s", cypher)
	}
	rows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows parameter type = %T, want []map[string]any", executor.calls[0].Parameters["rows"])
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
}

func TestCloudResourceNodeWriterPersistsServiceAnchorFields(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceNodeWriter(executor, 0)
	rows := cloudResourceRows(1)
	rows[0]["service_anchor_status"] = "strong"
	rows[0]["service_anchor_source"] = "attributes.service_name"
	rows[0]["service_anchor_reason"] = "explicit_service_anchor"
	rows[0]["service_anchor_names"] = []string{"orders-api"}
	rows[0]["service_anchor_name_tokens"] = "orders-api"
	rows[0]["service_name"] = "orders-api"

	if err := writer.WriteCloudResourceNodes(context.Background(), rows, "reducer/aws-resources"); err != nil {
		t.Fatalf("WriteCloudResourceNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"r.service_anchor_status = row.service_anchor_status",
		"r.service_anchor_source = row.service_anchor_source",
		"r.service_anchor_reason = row.service_anchor_reason",
		"r.service_anchor_names = row.service_anchor_names",
		"r.service_anchor_name_tokens = row.service_anchor_name_tokens",
		"r.service_name = row.service_name",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher missing %q:\n%s", want, cypher)
		}
	}
}

// TestCloudResourceNodeWriterPersistsRunningImageFields proves the
// running_image_ref/running_image_digest CloudResource node props (issue
// #5450) reach the actual SET clause the writer executes — a regression the
// in-memory row-map extraction tests (aws_resource_running_image_test.go)
// cannot catch, since ExtractCloudResourceNodeRows building the right map key
// is necessary but not sufficient: Cypher only persists a property named in
// the SET clause, so a row key with no matching `r.<key> = row.<key>` SET
// fragment is silently dropped by the backend, never reaching the graph. This
// mirrors TestCloudResourceNodeWriterPersistsServiceAnchorFields for the
// service-anchor fields.
func TestCloudResourceNodeWriterPersistsRunningImageFields(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceNodeWriter(executor, 0)
	rows := cloudResourceRows(1)
	rows[0]["running_image_ref"] = "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest"
	rows[0]["running_image_digest"] = "sha256:cc"

	if err := writer.WriteCloudResourceNodes(context.Background(), rows, "reducer/aws-resources"); err != nil {
		t.Fatalf("WriteCloudResourceNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"r.running_image_ref = row.running_image_ref",
		"r.running_image_digest = row.running_image_digest",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher missing %q:\n%s", want, cypher)
		}
	}
	// Assert the actual dispatched params carry the values (not just that the
	// SET fragment exists in the template) — a fake executor stand-in for the
	// real graph write, as far as this package can prove without a live
	// backend (the golden-corpus live gate proves the rest).
	dispatchedRows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok || len(dispatchedRows) != 1 {
		t.Fatalf("dispatched rows = %#v, want 1 row", executor.calls[0].Parameters["rows"])
	}
	if got := dispatchedRows[0]["running_image_ref"]; got != "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest" {
		t.Fatalf("dispatched running_image_ref = %#v", got)
	}
	if got := dispatchedRows[0]["running_image_digest"]; got != "sha256:cc" {
		t.Fatalf("dispatched running_image_digest = %#v", got)
	}
}

func TestCloudResourceNodeWriterBatchesRows(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceNodeWriter(executor, 2)

	if err := writer.WriteCloudResourceNodes(context.Background(), cloudResourceRows(5), "reducer/aws-resources"); err != nil {
		t.Fatalf("WriteCloudResourceNodes returned error: %v", err)
	}
	// 5 rows at batch size 2 -> 3 statements.
	if len(executor.calls) != 3 {
		t.Fatalf("len(calls) = %d, want 3 batched statements", len(executor.calls))
	}
}

func TestCloudResourceNodeWriterUsesGroupExecutorAtomically(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewCloudResourceNodeWriter(executor, 2)

	if err := writer.WriteCloudResourceNodes(context.Background(), cloudResourceRows(5), "reducer/aws-resources"); err != nil {
		t.Fatalf("WriteCloudResourceNodes returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("len(groupCalls) = %d, want 1 atomic group", len(executor.groupCalls))
	}
	if len(executor.groupCalls[0]) != 3 {
		t.Fatalf("group statement count = %d, want 3", len(executor.groupCalls[0]))
	}
}

func TestCloudResourceNodeWriterSatisfiesReducerInterface(t *testing.T) {
	t.Parallel()

	// Compile-time guarantee that the cypher writer satisfies the reducer-owned
	// consumer interface. Verified via the reducer wiring assignment below; this
	// test fails to compile if the method set drifts.
	var _ interface {
		WriteCloudResourceNodes(ctx context.Context, rows []map[string]any, evidenceSource string) error
	} = NewCloudResourceNodeWriter(&recordingExecutor{}, 0)
}
