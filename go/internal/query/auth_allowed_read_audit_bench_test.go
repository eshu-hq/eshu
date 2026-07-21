// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/governanceauditasync"
)

// discardResponseWriter is a minimal-allocation http.ResponseWriter so this
// benchmark measures authMiddleware's overhead, not httptest.ResponseRecorder's
// growing response-body buffer.
type discardResponseWriter struct {
	header http.Header
}

func (w *discardResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *discardResponseWriter) Write(b []byte) (int, error) { return len(b), nil }

func (w *discardResponseWriter) WriteHeader(int) {}

// benchAllowedReadNullSink is a no-op Appender: Bench A measures
// AsyncAppender's enqueue-side cost only (the design addendum's "AuditOnAsync
// (null/in-memory sink)"), never a real Postgres round trip — that
// synchronous cost is Bench B, in
// go/internal/storage/postgres/governance_audit_append_bench_test.go.
type benchAllowedReadNullSink struct{}

func (benchAllowedReadNullSink) Append(context.Context, []governanceaudit.Event) error {
	return nil
}

// benchScopedResolverAlwaysOK returns a resolver whose ResolveScopedToken
// always resolves the SAME AuthContext shape recordScopedReadAuthorized
// builds an event from, so both sub-benchmarks below do identical resolver
// and event-construction work and differ only in the allowedAudit call.
func benchScopedResolverAlwaysOK() *fakeScopedTokenResolver {
	return &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:               AuthModeScoped,
			TenantID:           "tenant_bench",
			WorkspaceID:        "workspace_bench",
			SubjectIDHash:      "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd",
			PolicyRevisionHash: "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba98765432",
			AllScopes:          true,
		},
		ok: true,
	}
}

// BenchmarkAuthMiddlewareScopedAllowed is the F-9 (#5170) design addendum's
// Bench A gate: middleware ON (async allowed-read audit over a null sink) vs
// OFF (nil allowedAudit), otherwise byte-identical composition and request.
// Run:
//
//	cd go && go test ./internal/query -run '^$' \
//	  -bench 'AuthMiddlewareScopedAllowed' -benchmem -count=10 | tee bench.txt
//	benchstat (compare the AuditOff and AuditOnAsync columns)
//
// Budget (addendum §4): the ON-minus-OFF delta must stay under 2µs/op and at
// most 4 allocs/op; a delta at or above 10µs/op disqualifies the async
// design and the change must stop and be redesigned rather than ship.
func BenchmarkAuthMiddlewareScopedAllowed(b *testing.B) {
	b.Run("AuditOff", func(b *testing.B) {
		resolver := benchScopedResolverAlwaysOK()
		handler := AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementOAuthChallengeAndAllowedReadAudit(
			"", resolver, mockHandler(), nil, false, nil, nil,
		)
		runAuthMiddlewareScopedAllowedBenchmark(b, handler)
	})
	b.Run("AuditOnAsync", func(b *testing.B) {
		resolver := benchScopedResolverAlwaysOK()
		appender := governanceauditasync.NewAsyncAppender(benchAllowedReadNullSink{}, governanceauditasync.Metrics{})
		b.Cleanup(func() { _ = appender.Close() })
		handler := AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementOAuthChallengeAndAllowedReadAudit(
			"", resolver, mockHandler(), nil, false, nil, appender,
		)
		runAuthMiddlewareScopedAllowedBenchmark(b, handler)
	})
}

func runAuthMiddlewareScopedAllowedBenchmark(b *testing.B, handler http.Handler) {
	b.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	// A real MCP client presents a correlation ID; set one so this benchmark
	// measures the emission logic's added cost, not documentationCorrelationID's
	// unrelated crypto/rand fallback (shared, pre-existing, identical cost for
	// the denial paths) that only triggers when no header is present.
	req.Header.Set("X-Correlation-ID", "corr-bench-request")
	var w discardResponseWriter

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handler.ServeHTTP(&w, req)
	}
}
