// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

func benchS3ExternalPrincipalGrantRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"source_uid":           fmt.Sprintf("bucket-%d", i),
			"principal_uid":        fmt.Sprintf("principal-%d", i),
			"principal_kind":       "aws_account",
			"principal_value":      fmt.Sprintf("%012d", i),
			"principal_account_id": fmt.Sprintf("%012d", i),
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

// BenchmarkS3ExternalPrincipalGrantWriter measures statement construction and
// batching for the metadata-only ExternalPrincipal node plus GRANTS_ACCESS_TO
// edge writer. The no-op executor isolates Eshu-owned row shaping from graph
// round trips and proves the write path has no N+1 behavior.
func BenchmarkS3ExternalPrincipalGrantWriter(b *testing.B) {
	rows := benchS3ExternalPrincipalGrantRows(5000)
	writer := NewS3ExternalPrincipalGrantWriter(noopGroupExecutor{}, 500)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writer.WriteS3ExternalPrincipalGrants(ctx, rows, "scope-1", "gen-1", "reducer/s3-external-principal-grant"); err != nil {
			b.Fatalf("WriteS3ExternalPrincipalGrants: %v", err)
		}
	}
}
