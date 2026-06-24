// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

func TestDispatchToolWithOptionsCancelsHandlerAtDeadline(t *testing.T) {
	t.Parallel()

	handlerDone := make(chan error, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, ok := r.Context().Deadline()
		if !ok {
			handlerDone <- errors.New("request context has no deadline")
			return
		}
		if remaining := time.Until(deadline); remaining <= 0 || remaining > 500*time.Millisecond {
			handlerDone <- fmt.Errorf("request deadline remaining = %s, want bounded short deadline", remaining)
			return
		}

		<-r.Context().Done()
		handlerDone <- r.Context().Err()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ignored":true}`))
	})

	start := time.Now()
	result, err := dispatchToolWithOptions(
		context.Background(),
		handler,
		"list_indexed_repositories",
		map[string]any{},
		"",
		discardDispatchLogger(),
		dispatchOptions{timeout: 20 * time.Millisecond},
	)
	if result != nil {
		t.Fatalf("dispatchToolWithOptions() result = %#v, want nil on timeout", result)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("dispatchToolWithOptions() error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("dispatchToolWithOptions() elapsed = %s, want deadline-bounded return", elapsed)
	}
	select {
	case handlerErr := <-handlerDone:
		if !errors.Is(handlerErr, context.DeadlineExceeded) {
			t.Fatalf("handler context error = %v, want context deadline exceeded", handlerErr)
		}
	case <-time.After(time.Second):
		t.Fatal("handler did not observe request context cancellation")
	}
}

func TestDispatchToolWithOptionsKeepsParentDeadline(t *testing.T) {
	t.Parallel()

	parentCtx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	parentDeadline, ok := parentCtx.Deadline()
	if !ok {
		t.Fatal("parent context has no deadline")
	}

	observedDeadline := make(chan time.Time, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, ok := r.Context().Deadline()
		if !ok {
			t.Error("request context has no deadline")
			return
		}
		observedDeadline <- deadline
		<-r.Context().Done()
		w.WriteHeader(http.StatusOK)
	})

	result, err := dispatchToolWithOptions(
		parentCtx,
		handler,
		"list_indexed_repositories",
		map[string]any{},
		"",
		discardDispatchLogger(),
		dispatchOptions{timeout: time.Second},
	)
	if result != nil {
		t.Fatalf("dispatchToolWithOptions() result = %#v, want nil on parent timeout", result)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("dispatchToolWithOptions() error = %v, want context deadline exceeded", err)
	}

	select {
	case gotDeadline := <-observedDeadline:
		if gotDeadline.Sub(parentDeadline) > 5*time.Millisecond || parentDeadline.Sub(gotDeadline) > 5*time.Millisecond {
			t.Fatalf("request deadline = %s, want parent deadline %s", gotDeadline, parentDeadline)
		}
	case <-time.After(time.Second):
		t.Fatal("handler did not observe a request deadline")
	}
}

func TestMCPToolErrorResultIncludesStructuredDispatchTimeout(t *testing.T) {
	t.Parallel()

	err := dispatchContextError(
		"list_indexed_repositories",
		20*time.Millisecond,
		context.DeadlineExceeded,
		discardDispatchLogger(),
	)

	result := mcpToolErrorResult(err)
	if !result.IsError {
		t.Fatal("mcpToolErrorResult() IsError = false, want true")
	}
	if len(result.Content) != 2 {
		t.Fatalf("mcpToolErrorResult() content length = %d, want text plus resource", len(result.Content))
	}
	if result.Content[1].Resource == nil {
		t.Fatal("mcpToolErrorResult() missing structured dispatch error resource")
	}
	if got, want := result.Content[1].Resource.URI, "eshu://tool-error/dispatch"; got != want {
		t.Fatalf("resource URI = %q, want %q", got, want)
	}

	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content type = %T, want map[string]any", result.StructuredContent)
	}
	if _, ok := structured["truth"]; !ok {
		t.Fatal("structured content missing truth slot")
	}
	errorContent, ok := structured["error"].(map[string]any)
	if !ok {
		t.Fatalf("structured error type = %T, want map[string]any", structured["error"])
	}
	if got, want := errorContent["code"], "mcp_dispatch_timeout"; got != want {
		t.Fatalf("structured error code = %#v, want %#v", got, want)
	}
	if got, want := errorContent["capability"], "mcp.dispatch"; got != want {
		t.Fatalf("structured error capability = %#v, want %#v", got, want)
	}
	details, ok := errorContent["details"].(map[string]any)
	if !ok {
		t.Fatalf("structured error details type = %T, want map[string]any", errorContent["details"])
	}
	if got, want := details["tool"], "list_indexed_repositories"; got != want {
		t.Fatalf("structured error details tool = %#v, want %#v", got, want)
	}
	if got, want := details["configured_timeout"], "20ms"; got != want {
		t.Fatalf("structured error configured timeout = %#v, want %#v", got, want)
	}
}

func discardDispatchLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
