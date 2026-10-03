// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestCheckpointHandlerFailureCarriesRetryAfter pins #7523: the checkpoint
// middleware's 503 for a failed checkpoint step is the same retryable condition
// as a fence failure later in the handler, so it carries the same Retry-After.
func TestCheckpointHandlerFailureCarriesRetryAfter(t *testing.T) {
	handler := WithCheckpoint(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("business handler dispatched after a failed checkpoint")
	}), &checkpointStub{err: errors.New("private DSN secret")}, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/business", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	if got, want := response.Header().Get("Retry-After"), strconv.Itoa(db.ReaderRetryAfterSeconds); got != want {
		t.Fatalf("Retry-After = %q, want %q", got, want)
	}
}

// TestCheckpointHandlerNilSourceCarriesNoRetryAfter pins #7536: a nil
// checkpoint source is a permanent wiring state, not a transient replay or
// capture failure, so the 503 keeps its code and body but carries no retry hint.
func TestCheckpointHandlerNilSourceCarriesNoRetryAfter(t *testing.T) {
	handler := WithCheckpoint(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("business handler dispatched with no checkpoint source")
	}), nil, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/business", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	if got := response.Header().Get("Retry-After"); got != "" {
		t.Fatalf("Retry-After = %q on a nil checkpoint source, want none", got)
	}
	if got, want := response.Body.String(), "database read unavailable\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}
