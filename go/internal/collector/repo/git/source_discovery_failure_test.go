// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type failingSelector struct{ err error }

func (f failingSelector) SelectRepositories(context.Context) (SelectionBatch, error) {
	return SelectionBatch{}, f.err
}

// TestDiscoverRepositoriesRecordsAFailedSelection pins #8084: a failed
// discovery marks the scope.assign span as an error and writes one log line,
// so an operator can see it without a process exit. Only the success path
// logged before.
func TestDiscoverRepositoriesRecordsAFailedSelection(t *testing.T) {
	t.Parallel()

	spans := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)).Tracer("discovery-failure-test")
	var logs bytes.Buffer
	cause := errors.New("list GitHub org repositories: status 504")
	source := &GitSource{
		Selector: failingSelector{err: cause},
		Tracer:   tracer,
		Logger:   slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	if _, err := source.discoverRepositories(context.Background()); !errors.Is(err, cause) {
		t.Fatalf("discoverRepositories() error = %v, want the cause unchanged", err)
	}
	ended := spans.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	if ended[0].Status().Code != codes.Error {
		t.Fatalf("scope.assign status = %v, want Error", ended[0].Status().Code)
	}
	recorded := false
	for _, event := range ended[0].Events() {
		if event.Name == "exception" {
			recorded = true
		}
	}
	if !recorded {
		t.Fatal("scope.assign span has no recorded error event")
	}
	for _, fragment := range []string{`"msg":"collector discovery failed"`, `"collector_kind":"git"`, `"retryable":false`, "status 504"} {
		if !strings.Contains(logs.String(), fragment) {
			t.Fatalf("discovery failure log %q missing %s", logs.String(), fragment)
		}
	}
}
