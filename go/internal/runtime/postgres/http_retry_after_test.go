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
// middleware's 503 is the same retryable condition as a fence failure later in
// the handler, so it carries the same Retry-After hint.
func TestCheckpointHandlerFailureCarriesRetryAfter(t *testing.T) {
	for name, source := range map[string]CheckpointSource{
		"checkpoint failed": &checkpointStub{err: errors.New("private DSN secret")},
		"missing source":    nil,
	} {
		handler := WithCheckpoint(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Error("business handler dispatched after a failed checkpoint")
		}), source, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/business", nil))
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: status = %d, want 503", name, response.Code)
		}
		if got, want := response.Header().Get("Retry-After"), strconv.Itoa(db.ReaderRetryAfterSeconds); got != want {
			t.Fatalf("%s: Retry-After = %q, want %q", name, got, want)
		}
	}
}
