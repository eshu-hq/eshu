// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

func cidrBlockRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"uid":              "cidr-uid-" + string(rune('a'+i)),
			"cidr":             "10.0.0.0/8",
			"address_family":   "ipv4",
			"is_internet":      false,
			"source_fact_id":   "fact-1",
			"stable_fact_key":  "key-1",
			"source_system":    "aws",
			"source_record_id": "rec-1",
			"collector_kind":   "aws",
		})
	}
	return rows
}

func prefixListRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"uid":              "pl-uid-" + string(rune('a'+i)),
			"prefix_list_id":   "pl-123",
			"account_id":       "111122223333",
			"region":           "us-east-1",
			"source_fact_id":   "fact-1",
			"stable_fact_key":  "key-1",
			"source_system":    "aws",
			"source_record_id": "rec-1",
			"collector_kind":   "aws",
		})
	}
	return rows
}

func TestCidrBlockNodeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCidrBlockNodeWriter(executor, 0)

	if err := writer.WriteCidrBlockNodes(context.Background(), nil, "reducer/security-group-endpoints"); err != nil {
		t.Fatalf("WriteCidrBlockNodes returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestCidrBlockNodeWriterMergesOnUID(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCidrBlockNodeWriter(executor, 0)

	if err := writer.WriteCidrBlockNodes(context.Background(), cidrBlockRows(1), "reducer/security-group-endpoints"); err != nil {
		t.Fatalf("WriteCidrBlockNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "UNWIND $rows AS row") {
		t.Fatalf("cypher missing UNWIND batch shape:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (c:CidrBlock {uid: row.uid})") {
		t.Fatalf("cypher must MERGE on uid identity only:\n%s", cypher)
	}
	if strings.Contains(cypher, "MERGE (c:CidrBlock {uid: row.uid, cidr") {
		t.Fatalf("cypher must not MERGE on a wide mutable map:\n%s", cypher)
	}
}

func TestCidrBlockNodeWriterBatchesRows(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCidrBlockNodeWriter(executor, 2)

	if err := writer.WriteCidrBlockNodes(context.Background(), cidrBlockRows(5), "reducer/security-group-endpoints"); err != nil {
		t.Fatalf("WriteCidrBlockNodes returned error: %v", err)
	}
	if len(executor.calls) != 3 {
		t.Fatalf("len(calls) = %d, want 3 batched statements", len(executor.calls))
	}
}

func TestCidrBlockNodeWriterUsesGroupExecutorAtomically(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewCidrBlockNodeWriter(executor, 2)

	if err := writer.WriteCidrBlockNodes(context.Background(), cidrBlockRows(5), "reducer/security-group-endpoints"); err != nil {
		t.Fatalf("WriteCidrBlockNodes returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("len(groupCalls) = %d, want 1 atomic group", len(executor.groupCalls))
	}
	if len(executor.groupCalls[0]) != 3 {
		t.Fatalf("group statement count = %d, want 3", len(executor.groupCalls[0]))
	}
}

func TestPrefixListNodeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewPrefixListNodeWriter(executor, 0)

	if err := writer.WritePrefixListNodes(context.Background(), nil, "reducer/security-group-endpoints"); err != nil {
		t.Fatalf("WritePrefixListNodes returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestPrefixListNodeWriterMergesOnUID(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewPrefixListNodeWriter(executor, 0)

	if err := writer.WritePrefixListNodes(context.Background(), prefixListRows(1), "reducer/security-group-endpoints"); err != nil {
		t.Fatalf("WritePrefixListNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "UNWIND $rows AS row") {
		t.Fatalf("cypher missing UNWIND batch shape:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (p:PrefixList {uid: row.uid})") {
		t.Fatalf("cypher must MERGE on uid identity only:\n%s", cypher)
	}
	if strings.Contains(cypher, "MERGE (p:PrefixList {uid: row.uid, prefix_list_id") {
		t.Fatalf("cypher must not MERGE on a wide mutable map:\n%s", cypher)
	}
}

func TestPrefixListNodeWriterUsesGroupExecutorAtomically(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewPrefixListNodeWriter(executor, 2)

	if err := writer.WritePrefixListNodes(context.Background(), prefixListRows(5), "reducer/security-group-endpoints"); err != nil {
		t.Fatalf("WritePrefixListNodes returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("len(groupCalls) = %d, want 1 atomic group", len(executor.groupCalls))
	}
	if len(executor.groupCalls[0]) != 3 {
		t.Fatalf("group statement count = %d, want 3", len(executor.groupCalls[0]))
	}
}

func TestSecurityGroupEndpointWritersSatisfyReducerInterfaces(t *testing.T) {
	t.Parallel()

	// Compile-time guarantee that the cypher writers satisfy the reducer-owned
	// consumer interfaces. This fails to compile if the method set drifts.
	var _ interface {
		WriteCidrBlockNodes(ctx context.Context, rows []map[string]any, evidenceSource string) error
	} = NewCidrBlockNodeWriter(&recordingExecutor{}, 0)
	var _ interface {
		WritePrefixListNodes(ctx context.Context, rows []map[string]any, evidenceSource string) error
	} = NewPrefixListNodeWriter(&recordingExecutor{}, 0)
}
