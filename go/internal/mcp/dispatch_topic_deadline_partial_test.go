// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

// #7008: when the dispatch deadline fires after the handler already wrote a
// bounded deadline-partial envelope, dispatch must surface that partial
// instead of the data-nil mcp_dispatch_timeout error.

// deadlinePartialEnvelopeHandler waits for the dispatch deadline, then writes
// the canonical envelope shape a deadline-truncated handler produces.
func deadlinePartialEnvelopeHandler(markDeadline bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		data := `{"deadline_truncated":false,"count":0}`
		if markDeadline {
			data = `{"deadline_truncated":true,"count":2,"coverage":{"deadline_truncated":true}}`
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":` + data + `,"truth":{"capability":"code_search.topic_investigation"},"error":null}`))
	})
}

func TestDispatchToolWithOptionsSurfacesDeadlinePartialEnvelope(t *testing.T) {
	t.Parallel()

	start := time.Now()
	result, err := dispatchToolWithOptions(
		context.Background(),
		deadlinePartialEnvelopeHandler(true),
		"investigate_code_topic",
		map[string]any{"topic": "auth"},
		"",
		discardDispatchLogger(),
		dispatchOptions{timeout: 20 * time.Millisecond},
	)
	if err != nil {
		t.Fatalf("dispatchToolWithOptions() error = %v, want the bounded partial surfaced", err)
	}
	if result == nil {
		t.Fatal("dispatchToolWithOptions() result = nil, want the bounded partial envelope")
	}
	if result.IsError {
		t.Fatal("dispatchToolWithOptions() IsError = true, want the partial as a success")
	}
	if result.Envelope == nil {
		t.Fatal("dispatchToolWithOptions() Envelope = nil, want the canonical envelope")
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map", result.Envelope.Data)
	}
	if got, want := data["deadline_truncated"], true; got != want {
		t.Fatalf("data.deadline_truncated = %#v, want %#v", got, want)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("dispatchToolWithOptions() elapsed = %s, want deadline-bounded return", elapsed)
	}
}

func TestDispatchToolWithOptionsKeepsTimeoutErrorWithoutPartialMarker(t *testing.T) {
	t.Parallel()

	result, err := dispatchToolWithOptions(
		context.Background(),
		deadlinePartialEnvelopeHandler(false),
		"investigate_code_topic",
		map[string]any{"topic": "auth"},
		"",
		discardDispatchLogger(),
		dispatchOptions{timeout: 20 * time.Millisecond},
	)
	if result != nil {
		t.Fatalf("dispatchToolWithOptions() result = %#v, want nil without a partial marker", result)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("dispatchToolWithOptions() error = %v, want context deadline exceeded", err)
	}
}
