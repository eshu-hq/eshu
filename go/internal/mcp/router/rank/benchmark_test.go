// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

// BenchmarkRank163Tools ranks one query over a 163-tool registry with a
// prebuilt index. The target is under 1 ms per op on the CI runner. Run with
// -benchmem.
func BenchmarkRank163Tools(b *testing.B) {
	tools, entries := synthetic(163)
	r := New(tools, entries)
	q := router.Query{
		Intent:  "show the deployment story for a service and its dependency drift",
		Limit:   router.DefaultLimit,
		Anchors: router.Anchors{RepoID: "repo-1"},
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Rank(ctx, q, tools, entries, allowAll); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkNew163Tools measures the one-time index build.
func BenchmarkNew163Tools(b *testing.B) {
	tools, entries := synthetic(163)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = New(tools, entries)
	}
}
