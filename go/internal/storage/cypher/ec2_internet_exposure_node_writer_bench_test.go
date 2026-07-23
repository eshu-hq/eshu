// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

func benchEC2InternetExposureRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"uid":              fmt.Sprintf("cloud-resource-ec2-%d", i),
			"state":            "not_exposed",
			"internet_exposed": false,
			"reason":           "no_internet_reachable_sg",
			"source_fact_id":   fmt.Sprintf("fact-ec2-posture-%d", i),
		})
	}
	return rows
}

// BenchmarkEC2InternetExposureNodeWriter measures statement-construction and
// batching cost for the MATCH-only EC2 internet-exposure node-property writer.
// The no-op executor isolates Eshu-owned write-path work from graph round trips
// and proves this projection has no per-row node fabrication path.
func BenchmarkEC2InternetExposureNodeWriter(b *testing.B) {
	rows := benchEC2InternetExposureRows(5000)
	writer := NewEC2InternetExposureNodeWriter(noopGroupExecutor{}, &echoingPostureExistenceReader{}, 500)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writer.WriteEC2InternetExposureNodes(ctx, rows, "scope-1", "gen-1", "reducer/ec2-internet-exposure"); err != nil {
			b.Fatalf("WriteEC2InternetExposureNodes: %v", err)
		}
	}
}
