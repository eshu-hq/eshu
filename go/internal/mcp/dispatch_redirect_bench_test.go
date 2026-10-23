// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
)

// benchDispatch measures one direct dispatchTool call through a real ServeMux
// that answers the two routes the benchmarks use with a small JSON body. It
// uses only symbols that exist on origin/main, so the same file runs on the
// base commit for the #8772 before/after comparison.
func benchDispatch(b *testing.B, tool string, args map[string]any) {
	b.Helper()
	body := `{"ok":true}`
	mux := http.NewServeMux()
	for _, pattern := range []string{"GET /api/v0/workloads/{id}/story", "GET /api/v0/repositories"} {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, body)
		})
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = dispatchTool(ctx, mux, tool, args, "", logger)
	}
}

// BenchmarkDispatchPathIDSupplied is a successful call whose route carries a
// path id.
func BenchmarkDispatchPathIDSupplied(b *testing.B) {
	benchDispatch(b, "get_workload_story", map[string]any{"workload_id": "w1"})
}

// BenchmarkDispatchNoPathID is a successful call whose route has no path id.
func BenchmarkDispatchNoPathID(b *testing.B) {
	benchDispatch(b, "list_indexed_repositories", map[string]any{})
}

// BenchmarkDispatchEmptyPathID is the #8772 error path: the id is empty. On
// origin/main it ends in a mux 307 reported as success; at this head
// resolveRoute refuses it before any request is built.
func BenchmarkDispatchEmptyPathID(b *testing.B) {
	benchDispatch(b, "get_workload_story", map[string]any{})
}
