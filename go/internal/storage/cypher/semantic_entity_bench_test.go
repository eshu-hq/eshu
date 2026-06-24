// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

type semanticEntityBenchExecutor struct {
	calls atomic.Int64
}

func (e *semanticEntityBenchExecutor) Execute(context.Context, Statement) error {
	e.calls.Add(1)
	return nil
}

func BenchmarkSemanticEntityWriterNornicDBConcurrentDistinctRepos(b *testing.B) {
	writes := make([]reducer.SemanticEntityWrite, 8)
	for i := range writes {
		repoID := fmt.Sprintf("repo:bench:semantic:%02d", i)
		filePath := fmt.Sprintf("/tmp/eshu-semantic-bench/%02d/main.go", i)
		writes[i] = reducer.SemanticEntityWrite{
			RepoIDs: []string{repoID},
			Rows: []reducer.SemanticEntityRow{
				{
					RepoID:       repoID,
					EntityID:     fmt.Sprintf("function:bench:semantic:%02d", i),
					EntityType:   "Function",
					EntityName:   "handleBench",
					FilePath:     filePath,
					RelativePath: "main.go",
					Language:     "go",
					StartLine:    1,
					EndLine:      2,
					Metadata:     map[string]any{"docstring": "bench semantic function"},
				},
				{
					RepoID:       repoID,
					EntityID:     fmt.Sprintf("module:bench:semantic:%02d", i),
					EntityType:   "Module",
					EntityName:   fmt.Sprintf("bench_semantic_%02d", i),
					FilePath:     filePath,
					RelativePath: "main.go",
					Language:     "go",
					StartLine:    1,
					EndLine:      1,
					Metadata:     map[string]any{"module_kind": "package"},
				},
			},
		}
	}

	exec := &semanticEntityBenchExecutor{}
	writer := NewSemanticEntityWriterWithCanonicalNodeRows(
		ExecuteOnlyExecutor{Inner: exec},
		10,
	).WithLabelScopedRetract()

	var next atomic.Uint64
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			write := writes[int(next.Add(1))%len(writes)]
			if _, err := writer.WriteSemanticEntities(context.Background(), write); err != nil {
				b.Fatalf("WriteSemanticEntities() error = %v", err)
			}
		}
	})
	b.ReportMetric(float64(exec.calls.Load())/float64(b.N), "statements/op")
}
