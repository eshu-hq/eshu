// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
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

// TestGraphReadVerdictRetryabilityIsPerVerdictNotPerStatus pins #7536: the
// retry marker belongs to the verdict, not to the 503 status code. A 503
// verdict the mapping did not mark retryable (a future permanent verdict)
// must reach the wire without Retry-After through the production seam
// (GraphReadErrorEnvelope and WriteGraphReadError), while a marked verdict
// keeps it. It swaps a package variable, so it is not parallel.
func TestGraphReadVerdictRetryabilityIsPerVerdictNotPerStatus(t *testing.T) {
	synthetic := errors.New("synthetic permanent verdict")
	original := mapGraphReadError
	t.Cleanup(func() { mapGraphReadError = original })
	mapGraphReadError = func(err error) (graphReadHTTPError, bool) {
		if errors.Is(err, synthetic) {
			return graphReadHTTPError{
				status:  http.StatusServiceUnavailable,
				code:    ErrorCodeBackendUnavailable,
				message: "synthetic permanent verdict",
			}, true
		}
		return original(err)
	}
	for name, tc := range map[string]struct {
		err        error
		retryAfter bool
	}{
		"unmarked 503 verdict": {synthetic, false},
		"graph unavailable":    {ErrGraphUnavailable, true},
		"reader stale":         {db.ErrReaderStale, true},
	} {
		status, env, ok := GraphReadErrorEnvelope(tc.err, "cap")
		if !ok || status != http.StatusServiceUnavailable {
			t.Fatalf("%s: seam ok=%v status=%d, want a 503 verdict", name, ok, status)
		}
		if env.retryable != tc.retryAfter {
			t.Fatalf("%s: envelope retryable = %v, want %v", name, env.retryable, tc.retryAfter)
		}
		for _, accept := range []string{EnvelopeMIMEType, ""} {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if accept != "" {
				req.Header.Set("Accept", accept)
			}
			if !WriteGraphReadError(rec, req, tc.err, "cap") {
				t.Fatalf("%s: WriteGraphReadError did not claim the verdict", name)
			}
			got := rec.Header().Get("Retry-After")
			if tc.retryAfter && got == "" {
				t.Fatalf("%s accept=%q: Retry-After missing on a retryable verdict", name, accept)
			}
			if !tc.retryAfter && got != "" {
				t.Fatalf("%s accept=%q: Retry-After = %q on an unmarked 503 verdict, want none", name, accept, got)
			}
		}
	}
}

// TestRetryableVerdictsAreExactlyGraphUnavailableAndReaderFence pins the set of
// mapped verdicts that carry the retry marker, and that a 504 deadline never does.
func TestRetryableVerdictsAreExactlyGraphUnavailableAndReaderFence(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		err       error
		status    int
		retryable bool
	}{
		"graph unavailable": {ErrGraphUnavailable, http.StatusServiceUnavailable, true},
		"reader stale":      {db.ErrReaderStale, http.StatusServiceUnavailable, true},
		"graph deadline":    {ErrGraphReadDeadline, http.StatusGatewayTimeout, false},
	} {
		status, env, ok := GraphReadErrorEnvelope(tc.err, "cap")
		if !ok || status != tc.status {
			t.Fatalf("%s: ok=%v status=%d, want %d", name, ok, status, tc.status)
		}
		if env.retryable != tc.retryable {
			t.Fatalf("%s: retryable = %v, want %v", name, env.retryable, tc.retryable)
		}
	}
}
