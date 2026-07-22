// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

func benchS3InternetExposureRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"uid":              fmt.Sprintf("cloud-resource-%d", i),
			"state":            "not_exposed",
			"internet_exposed": false,
			"reason":           "public_policy_restricted_by_block_public_access",
			"source_fact_id":   fmt.Sprintf("fact-posture-%d", i),
		})
	}
	return rows
}

// BenchmarkS3InternetExposureNodeWriter measures the statement-construction and
// batching cost of the MATCH-only S3 internet-exposure node-property writer. The
// backend executor is a no-op so the benchmark isolates Eshu-owned write-path
// work from graph round trips and proves the projection has no per-row graph
// lookup or node fabrication path.
func BenchmarkS3InternetExposureNodeWriter(b *testing.B) {
	rows := benchS3InternetExposureRows(5000)
	writer := NewS3InternetExposureNodeWriter(noopGroupExecutor{}, &echoingPostureExistenceReader{}, 500)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writer.WriteS3InternetExposureNodes(ctx, rows, "scope-1", "gen-1", "reducer/s3-internet-exposure"); err != nil {
			b.Fatalf("WriteS3InternetExposureNodes: %v", err)
		}
	}
}
