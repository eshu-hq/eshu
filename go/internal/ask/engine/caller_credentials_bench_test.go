// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package engine

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// BenchmarkMCPRunnerCredentialBinding compares the existing header-only runner
// path with a captured request using the same in-process response and token.
func BenchmarkMCPRunnerCredentialBinding(b *testing.B) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":null,"truth":null,"error":null}`)
	})
	runner := NewMCPRunner(handler, "Bearer caller", nil)
	args := map[string]any{"query": "x", "repo_id": "r"}
	outer := httptest.NewRequest(http.MethodPost, "/api/v0/ask", nil)
	outer.Header.Set("Authorization", "Bearer caller")
	captured := ContextWithCallerRequestCredentials(context.Background(), outer)
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{"existing_header", context.Background()},
		{"captured_request", captured},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if _, err := runner.Run(tc.ctx, "find_code", args); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
