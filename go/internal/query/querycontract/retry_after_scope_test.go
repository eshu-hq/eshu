// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestWriteErrorEnvelopeBackendUnavailableCarriesNoRetryAfter pins the #7523
// review fix: the generic envelope writer must not claim every 503
// backend_unavailable is retryable. A configuration or profile state is
// permanent; only the shared graph-read verdicts carry the hint.
func TestWriteErrorEnvelopeBackendUnavailableCarriesNoRetryAfter(t *testing.T) {
	t.Parallel()
	for _, accept := range []string{EnvelopeMIMEType, ""} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		WriteErrorEnvelope(rec, req, http.StatusServiceUnavailable, &ErrorEnvelope{
			Code:    ErrorCodeBackendUnavailable,
			Message: "requires a configured graph backend",
		})
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("accept=%q status = %d", accept, rec.Code)
		}
		if got := rec.Header().Get("Retry-After"); got != "" {
			t.Fatalf("accept=%q Retry-After = %q on a generic backend_unavailable 503, want none", accept, got)
		}
	}
}

// TestGraphUnavailableVerdictKeepsRetryAfterThroughWriteGraphReadError keeps
// the pre-existing transient graph-unavailable 503 retryable.
func TestGraphUnavailableVerdictKeepsRetryAfterThroughWriteGraphReadError(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	if !WriteGraphReadError(rec, req, ErrGraphUnavailable, "cap") {
		t.Fatal("ErrGraphUnavailable not claimed")
	}
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("status=%d Retry-After=%q, want 503 with a retry hint", rec.Code, rec.Header().Get("Retry-After"))
	}
}

// TestGraphReadDeadlineStaysWithoutRetryAfter: the 504 budget verdict is not a
// 503 and never carried the hint.
func TestGraphReadDeadlineStaysWithoutRetryAfter(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if !WriteGraphReadError(rec, req, ErrGraphReadDeadline, "cap") {
		t.Fatal("deadline not claimed")
	}
	if rec.Code != http.StatusGatewayTimeout || rec.Header().Get("Retry-After") != "" {
		t.Fatalf("status=%d Retry-After=%q", rec.Code, rec.Header().Get("Retry-After"))
	}
}
