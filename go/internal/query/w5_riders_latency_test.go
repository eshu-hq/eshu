// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
)

// Wave-5 rider seven-leg replication, leg 6: latency (Refs
// eshu-hq/eshu#9669, satellite eshu-hq/eshu-api#101). Core-side
// reading of the W8a latency legs: the rider-judgment path (mux match
// plus 404/405 page, zero admission contact) must not regress past
// 1.50x of the static-default lane. Hermetic by design: both legs run
// against the same in-process muxes, so the numbers isolate
// judgment-path cost, not backend or network time. The bound in
// scripts/measure-w5-riders-latency.sh stays a real pathology check:
// every leg is one in-memoryMux judgment, so a double read, a body
// re-read, or an uncached load exceeds it loudly while scheduling
// noise does not.

// w5RidersCannedRouter builds the pin-backed benchmark lane: canned
// admissions behind the rider handlers.
func w5RidersCannedRouter() *http.ServeMux {
	mux, _, _ := w5RidersCannedMux()
	return mux
}

// w5RidersStaticRouter builds the static-default lane: the same
// handlers with unset admissions. Judgments are identical — the leg
// isolates pin-indirection cost, which for riders is zero by
// construction (the mux answers before any admission runs).
func w5RidersStaticRouter() *http.ServeMux {
	mux := http.NewServeMux()
	(&APIRouter{
		Code:              &codequery.CodeHandler{},
		SignInPolicyReads: &SignInPolicyReadHandler{},
		AuthProviders:     &AuthProviderListHandler{},
	}).Mount(mux)
	return mux
}

// BenchmarkW5RidersJudgment measures the pin-backed rider-judgment
// path per wave spelling.
func BenchmarkW5RidersJudgment(b *testing.B) {
	mux := w5RidersCannedRouter()

	judgments := map[string][2]string{
		"r1-patch-bundles":   {http.MethodPatch, "/api/v0/code/bundles"},
		"r2-slash-policy":    {http.MethodGet, "/api/v0/auth/sign-in-policy/"},
		"r3-slash-providers": {http.MethodGet, "/api/v0/auth/providers/"},
	}
	for name, spell := range judgments {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var req *http.Request
				if spell[0] == http.MethodPatch {
					req = httptest.NewRequest(http.MethodPatch, spell[1], strings.NewReader(`{}`))
				} else {
					req = httptest.NewRequest(http.MethodGet, spell[1], nil)
				}
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
			}
		})
	}
}

// BenchmarkW5RidersStaticJudgment measures the static-default lane
// for the same spellings: the before legs the pin-backed lane must
// not regress.
func BenchmarkW5RidersStaticJudgment(b *testing.B) {
	mux := w5RidersStaticRouter()

	judgments := map[string][2]string{
		"r1-patch-bundles":   {http.MethodPatch, "/api/v0/code/bundles"},
		"r2-slash-policy":    {http.MethodGet, "/api/v0/auth/sign-in-policy/"},
		"r3-slash-providers": {http.MethodGet, "/api/v0/auth/providers/"},
	}
	for name, spell := range judgments {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var req *http.Request
				if spell[0] == http.MethodPatch {
					req = httptest.NewRequest(http.MethodPatch, spell[1], strings.NewReader(`{}`))
				} else {
					req = httptest.NewRequest(http.MethodGet, spell[1], nil)
				}
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
			}
		})
	}
}
