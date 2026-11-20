// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/extraction"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// G2 Wave-1 seven-leg replication, leg 6: latency (Refs
// eshu-hq/eshu#9669, satellite eshu-hq/eshu-api#101). The pin-backed
// lane does the same single in-memory read as the static-default
// lane plus the admission indirection; these benchmarks bound that
// overhead per wave family so scripts/measure-g2-wave1-latency.sh
// can gate it in CI. Both lanes answer from memory — the numbers
// isolate handler-side cost, not backend or network time. The bound
// is the same 1.50x the satellite lanes carry: every leg does one
// bounded read, so a real pathology (a double read, a body re-read,
// an uncached load) exceeds it loudly while scheduling noise does
// not. Reference only — every line here is new core-side code.

// g2Wave1CannedRouter builds the pin-backed lane the benchmarks
// drive: canned admissions behind the wave handlers.
func g2Wave1CannedRouter() *http.ServeMux {
	mux := http.NewServeMux()
	(&APIRouter{
		ExtractionReadiness: &CollectorExtractionReadinessHandler{
			Profile: ProfileProduction,
			Reads: &cannedExtractionReadiness{rows: map[scope.CollectorKind]extraction.Readiness{
				scope.CollectorGit: {Family: scope.CollectorGit, DisplayName: "Canned Git"},
			}},
		},
		FactSchemaVersions: &FactSchemaVersionHandler{Profile: ProfileProduction, Reads: cannedFactSchema{}},
		SurfaceInventory:   &SurfaceInventoryHandler{Profile: ProfileProduction, Reads: cannedSurfaceInventory{}},
		Freshness:          &FreshnessHandler{Profile: ProfileProduction},
	}).Mount(mux)
	return mux
}

// g2Wave1StaticRouter builds the static-default lane: the same
// handlers with unset admissions serving the real
// catalog/registry/artifact.
func g2Wave1StaticRouter() *http.ServeMux {
	mux := http.NewServeMux()
	(&APIRouter{
		ExtractionReadiness: &CollectorExtractionReadinessHandler{Profile: ProfileProduction},
		FactSchemaVersions:  &FactSchemaVersionHandler{Profile: ProfileProduction},
		SurfaceInventory:    &SurfaceInventoryHandler{Profile: ProfileProduction},
		Freshness:           &FreshnessHandler{Profile: ProfileProduction},
	}).Mount(mux)
	return mux
}

// cannedSurfaceInventory is the hermetic SurfaceInventoryReadAdmission
// the pin-backed benchmark lane serves: one fixed row, no embedded
// artifact decode per iteration (the handler still caches per
// instance, so the benchmark isolates the admission call itself).
type cannedSurfaceInventory struct{}

var _ SurfaceInventoryReadAdmission = cannedSurfaceInventory{}

func (cannedSurfaceInventory) LoadSurfaceInventory() (capabilitycatalog.SurfaceInventory, error) {
	return capabilitycatalog.SurfaceInventory{Version: "bench", Surfaces: []capabilitycatalog.SurfaceRecord{}}, nil
}

// BenchmarkG2Wave1PinnedRead measures the pin-backed lane per wave
// family: one leg per family through the same admission the wave
// legs terminate through.
func BenchmarkG2Wave1PinnedRead(b *testing.B) {
	mux := g2Wave1CannedRouter()

	gets := map[string]string{
		"readiness": "/api/v0/collector-extraction-readiness",
		"fact":      "/api/v0/fact-schema-versions/canned-fact",
		"surface":   "/api/v0/surface-inventory",
	}
	for name, path := range gets {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				req := httptest.NewRequest(http.MethodGet, path, nil)
				req.Header.Set("Accept", "application/json")
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
			}
		})
	}
	b.Run("rider-judgment", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest(http.MethodPost, "/api/v0/surface-inventory", strings.NewReader(`{}`))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
		}
	})
}

// BenchmarkG2Wave1StaticRead measures the static-default lane for
// the same legs: the before legs the pin-backed lane must not
// regress. Every leg does exactly one bounded read, so the 1.50
// bound in scripts/measure-g2-wave1-latency.sh stays a real
// pathology check.
func BenchmarkG2Wave1StaticRead(b *testing.B) {
	mux := g2Wave1StaticRouter()

	gets := map[string]string{
		"readiness": "/api/v0/collector-extraction-readiness",
		"fact":      "/api/v0/fact-schema-versions",
		"surface":   "/api/v0/surface-inventory",
	}
	for name, path := range gets {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				req := httptest.NewRequest(http.MethodGet, path, nil)
				req.Header.Set("Accept", "application/json")
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
			}
		})
	}
	b.Run("rider-judgment", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest(http.MethodPost, "/api/v0/surface-inventory", strings.NewReader(`{}`))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
		}
	})
}
