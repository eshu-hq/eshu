// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	codeCallBenchmarkRows       = 5000
	codeCallBenchmarkDeltaFiles = 50
)

type codeCallBenchmarkScenario struct {
	name           string
	rows           []reducer.SharedProjectionIntentRow
	writeRows      int
	deltaFilePaths int
	retractStmts   int
}

// BenchmarkEdgeWriterCodeCallRetractAndWrite compares Eshu-owned statement
// construction for full-refresh repo-wide CALLS cleanup and delta file-scoped
// cleanup. The executor is a no-op, so results isolate row shaping, retraction
// dispatch, and write batching from backend round trips.
func BenchmarkEdgeWriterCodeCallRetractAndWrite(b *testing.B) {
	scenarios := []codeCallBenchmarkScenario{
		{
			name:         "repo_wide_full_refresh_5000_call_rows",
			rows:         benchmarkCodeCallRows(codeCallBenchmarkRows, 0, false),
			writeRows:    codeCallBenchmarkRows,
			retractStmts: 1,
		},
		{
			name:           "delta_50_files_5000_call_rows",
			rows:           benchmarkCodeCallRows(codeCallBenchmarkRows, codeCallBenchmarkDeltaFiles, false),
			writeRows:      codeCallBenchmarkRows,
			deltaFilePaths: codeCallBenchmarkDeltaFiles,
			retractStmts:   1,
		},
		{
			name:           "delta_deleted_only_50_files_0_call_rows",
			rows:           benchmarkCodeCallRows(0, codeCallBenchmarkDeltaFiles, true),
			deltaFilePaths: codeCallBenchmarkDeltaFiles,
			retractStmts:   1,
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			writer := NewEdgeWriter(noopGroupExecutor{}, 500)
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := writer.RetractEdges(ctx, reducer.DomainCodeCalls, scenario.rows, "parser/code-calls"); err != nil {
					b.Fatalf("RetractEdges: %v", err)
				}
				if err := writer.WriteEdges(ctx, reducer.DomainCodeCalls, scenario.rows, "parser/code-calls"); err != nil {
					b.Fatalf("WriteEdges: %v", err)
				}
			}
			b.ReportMetric(float64(len(scenario.rows)), "input_rows/op")
			b.ReportMetric(float64(scenario.writeRows), "write_rows/op")
			b.ReportMetric(float64(scenario.deltaFilePaths), "delta_file_paths/op")
			b.ReportMetric(float64(scenario.retractStmts), "retract_statements/op")
		})
	}
}

func benchmarkCodeCallRows(rowCount int, deltaFileCount int, deletedOnly bool) []reducer.SharedProjectionIntentRow {
	rows := make([]reducer.SharedProjectionIntentRow, 0, rowCount+1)
	if deltaFileCount > 0 {
		rows = append(rows, reducer.SharedProjectionIntentRow{
			IntentID:     "refresh-delta",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":          "repo-a",
				"delta_projection": true,
				"delta_file_paths": benchmarkCodeCallDeltaFilePaths(deltaFileCount),
				"intent_type":      "repo_refresh",
			},
		})
	} else if rowCount == 0 || deletedOnly {
		rows = append(rows, reducer.SharedProjectionIntentRow{
			IntentID:     "refresh-full",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":     "repo-a",
				"intent_type": "repo_refresh",
			},
		})
	}
	if deletedOnly {
		return rows
	}
	for i := 0; i < rowCount; i++ {
		rows = append(rows, reducer.SharedProjectionIntentRow{
			IntentID:     fmt.Sprintf("call-%d", i),
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":               "repo-a",
				"caller_entity_id":      fmt.Sprintf("content-entity:caller-%d", i),
				"callee_entity_id":      fmt.Sprintf("content-entity:callee-%d", i),
				"caller_entity_type":    "Function",
				"callee_entity_type":    "Function",
				"evidence_source":       "parser/code-calls",
				"relationship_type":     "CALLS",
				"resolution_method":     "syntactic",
				"resolution_confidence": 1.0,
			},
		})
	}
	return rows
}

func benchmarkCodeCallDeltaFilePaths(count int) []string {
	paths := make([]string, 0, count)
	for i := 0; i < count; i++ {
		paths = append(paths, fmt.Sprintf("/repo/src/service/file_%04d.go", i))
	}
	return paths
}
