// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

func benchCidrBlockRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"uid":              fmt.Sprintf("cidr-uid-%d", i),
			"cidr":             fmt.Sprintf("10.%d.0.0/16", i%256),
			"address_family":   "ipv4",
			"is_internet":      false,
			"source_fact_id":   "fact",
			"stable_fact_key":  "key",
			"source_system":    "aws",
			"source_record_id": "rec",
			"collector_kind":   "aws",
		})
	}
	return rows
}

func benchPrefixListRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"uid":              fmt.Sprintf("pl-uid-%d", i),
			"prefix_list_id":   fmt.Sprintf("pl-%d", i),
			"account_id":       "111122223333",
			"region":           "us-east-1",
			"source_fact_id":   "fact",
			"stable_fact_key":  "key",
			"source_system":    "aws",
			"source_record_id": "rec",
			"collector_kind":   "aws",
		})
	}
	return rows
}

// BenchmarkCidrBlockNodeWriter measures the statement-construction and batching
// cost of the CidrBlock node writer for a realistic per-scope-generation endpoint
// count. The backend executor is a no-op so the benchmark isolates Eshu-owned
// write-path work from graph round trips, matching the CloudResource node writer
// baseline so the two canonical writers can be compared on the same input shape.
func BenchmarkCidrBlockNodeWriter(b *testing.B) {
	rows := benchCidrBlockRows(5000)
	writer := NewCidrBlockNodeWriter(noopGroupExecutor{}, 500)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writer.WriteCidrBlockNodes(ctx, rows, "reducer/security-group-endpoints"); err != nil {
			b.Fatalf("WriteCidrBlockNodes: %v", err)
		}
	}
}

// BenchmarkPrefixListNodeWriter measures the same write-path cost for the
// PrefixList node writer.
func BenchmarkPrefixListNodeWriter(b *testing.B) {
	rows := benchPrefixListRows(5000)
	writer := NewPrefixListNodeWriter(noopGroupExecutor{}, 500)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writer.WritePrefixListNodes(ctx, rows, "reducer/security-group-endpoints"); err != nil {
			b.Fatalf("WritePrefixListNodes: %v", err)
		}
	}
}
