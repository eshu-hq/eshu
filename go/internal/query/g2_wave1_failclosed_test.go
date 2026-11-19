// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/extraction"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// G2 Wave-1 seven-leg replication, leg 3: fail-closed (Refs
// eshu-hq/eshu#9669, satellite eshu-hq/eshu-api#101). Core-side
// reading of the W8a fail-closed legs: a failing admission surfaces
// the fixed 500 with no Retry-After (never a silent empty), unknown
// families/kinds stay 404, method judgment holds before any
// admission is touched, and an unset admission keeps serving the
// real catalog (never fail-open-to-empty).
//
// No 503 leg exists for these 9 rows: none reads a live backend —
// the permanent-503 shape stays with the backend-backed legs owned
// by later waves. Reference only — every line here is new core-side
// code.

// TestG2Wave1FailingInventory500 pins that a failing
// surface-inventory admission surfaces the fixed 500 with no
// Retry-After in both negotiated envelopes, never a silent empty
// inventory.
func TestG2Wave1FailingInventory500(t *testing.T) {
	t.Parallel()

	for _, accept := range []string{EnvelopeMIMEType, "application/json"} {
		t.Run("accept="+accept, func(t *testing.T) {
			t.Parallel()

			mux := http.NewServeMux()
			(&SurfaceInventoryHandler{
				Profile: ProfileProduction,
				Reads:   errSurfaceInventory{err: errors.New("surface inventory probe: store unavailable")},
			}).Mount(mux)

			req := httptest.NewRequest(http.MethodGet, "/api/v0/surface-inventory", nil)
			req.Header.Set("Accept", accept)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("failing-inventory status = %d, want 500; body=%s", rec.Code, rec.Body.String())
			}
			if rec.Header().Get("Retry-After") != "" {
				t.Errorf("failing-inventory Retry-After = %q, want empty (fixed, never retry)", rec.Header().Get("Retry-After"))
			}
			if !strings.Contains(rec.Body.String(), surfaceInventoryUnavailableMessage) {
				t.Errorf("failing-inventory body = %q, want the fixed message %q", rec.Body.String(), surfaceInventoryUnavailableMessage)
			}
		})
	}
}

// TestG2Wave1RidersJudgeBeforeAdmission pins that the POST rider
// judgment holds even when the backing admission fails or is
// unset: 405 with zero admission contact.
func TestG2Wave1RidersJudgeBeforeAdmission(t *testing.T) {
	t.Parallel()

	surface := &countingSurfaceInventory{inner: errSurfaceInventory{err: errors.New("surface inventory probe: store unavailable")}}
	mux := http.NewServeMux()
	(&APIRouter{
		ExtractionReadiness: &CollectorExtractionReadinessHandler{Profile: ProfileProduction, Reads: &cannedExtractionReadiness{rows: map[scope.CollectorKind]extraction.Readiness{
			scope.CollectorGit: {Family: scope.CollectorGit, DisplayName: "Canned Git"},
		}}},
		FactSchemaVersions: &FactSchemaVersionHandler{Profile: ProfileProduction, Reads: cannedFactSchema{}},
		SurfaceInventory:   &SurfaceInventoryHandler{Profile: ProfileProduction, Reads: surface},
		Freshness:          &FreshnessHandler{Profile: ProfileProduction},
	}).Mount(mux)

	for _, path := range []string{
		"/api/v0/freshness/changed-since",
		"/api/v0/freshness/generations",
		"/api/v0/freshness/services/changed-since",
		"/api/v0/surface-inventory",
	} {
		t.Run("POST "+path, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`)))
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("POST %s with failing admission: status = %d, want 405 (judged before admission)", path, rec.Code)
			}
		})
	}
	if surface.loadCalls != 0 {
		t.Errorf("rider judgments loaded the inventory %d times, want zero contact", surface.loadCalls)
	}
}

// TestG2Wave1UnknownFamilyKind404 pins the closed shape for
// untracked static rows: unknown collector families and fact kinds
// stay 404 in both envelopes.
func TestG2Wave1UnknownFamilyKind404(t *testing.T) {
	t.Parallel()

	for _, accept := range []string{EnvelopeMIMEType, "application/json"} {
		for _, path := range []string{
			"/api/v0/collector-extraction-readiness/not-a-family",
			"/api/v0/fact-schema-versions/unknown-kind",
		} {
			t.Run(accept+" "+path, func(t *testing.T) {
				t.Parallel()

				rec := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodGet, path, nil)
				req.Header.Set("Accept", accept)
				g2Wave1CannedMux().ServeHTTP(rec, req)
				if rec.Code != http.StatusNotFound {
					t.Errorf("GET %s: status = %d, want 404", path, rec.Code)
				}
			})
		}
	}
}

// TestG2Wave1UnsetAdmissionKeepsServing pins the empty-state leg:
// a handler built without an admission keeps serving the real
// catalog/registry/artifact (never fail-open-to-empty).
func TestG2Wave1UnsetAdmissionKeepsServing(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	(&APIRouter{
		ExtractionReadiness: &CollectorExtractionReadinessHandler{Profile: ProfileProduction},
		FactSchemaVersions:  &FactSchemaVersionHandler{Profile: ProfileProduction},
		SurfaceInventory:    &SurfaceInventoryHandler{Profile: ProfileProduction},
		Freshness:           &FreshnessHandler{Profile: ProfileProduction},
	}).Mount(mux)

	for _, path := range []string{
		"/api/v0/collector-extraction-readiness",
		"/api/v0/fact-schema-versions",
		"/api/v0/surface-inventory",
	} {
		t.Run("GET "+path, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s with unset admission: status = %d, want 200 (static default serves)", path, rec.Code)
			}
			if rec.Body.Len() == 0 {
				t.Errorf("GET %s with unset admission: empty body, want the real catalog (never fail-open-to-empty)", path)
			}
		})
	}
}
