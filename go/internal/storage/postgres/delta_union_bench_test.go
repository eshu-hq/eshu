// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"testing"
)

// deltaUnionBenchTargetRow is the union target read's row for the benchmark:
// a cumulative delta (#8780) with a fence commit and a union-base commit.
func deltaUnionBenchTargetRow() []any { return []any{true, "commit-active", "commit-base"} }

// BenchmarkFactStoreLoadProjectionFactsUnion measures the Go side of one
// delta union load (#8780 no-regression): the delta's 200 rows, the target
// read, the base lookup, and a 2,000-file base, scanned, overlaid, and
// recorded. The fake queryer has no network or planner cost, so the number
// isolates scan, overlay, and decision recording.
func BenchmarkFactStoreLoadProjectionFactsUnion(b *testing.B) {
	deltaRows := [][]any{deltaUnionFactRow("delta-repo", "gen-delta-1", "repository", "repository:repo-1",
		`{"repo_id":"repo-1","local_path":"/repos/alpha","delta_generation":true,"delta_relative_paths":["f0000.go"],"delta_deleted_relative_paths":[]}`)}
	for i := range 200 {
		rel := fmt.Sprintf("f%04d.go", i)
		deltaRows = append(deltaRows, deltaUnionFactRow("delta-"+rel, "gen-delta-1", "file", "file:repo-1:"+rel,
			`{"repo_id":"repo-1","relative_path":"`+rel+`"}`))
	}
	baseRows := [][]any{deltaUnionFactRow("base-repo", "gen-base-1", "repository", "repository:repo-1",
		`{"repo_id":"repo-1","local_path":"/repos/alpha"}`)}
	for i := range 2000 {
		rel := fmt.Sprintf("f%04d.go", i)
		baseRows = append(baseRows, deltaUnionFactRow("base-"+rel, "gen-base-1", "file", "file:repo-1:"+rel,
			`{"repo_id":"repo-1","relative_path":"`+rel+`"}`))
	}
	work := deltaUnionLoadWork("gen-delta-1")
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		db := &fakeExecQueryer{queryResponses: []queueFakeRows{
			{rows: deltaRows},
			{rows: [][]any{deltaUnionBenchTargetRow()}},
			{rows: [][]any{{"gen-base-1", "active"}}},
			{rows: baseRows},
		}}
		if _, err := NewFactStore(db).LoadProjectionFacts(context.Background(), work); err != nil {
			b.Fatalf("LoadProjectionFacts() = %v", err)
		}
	}
}
