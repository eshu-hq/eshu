// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestDrainProjectorWorkItemEndsSpanAndLogsDroppedWork proves every bootstrap
// path that drops a work item without Ack or Fail still ends its projector.run
// span and leaves an operator log line, so the trace is exported and the drop
// is visible at 3 AM.
func TestDrainProjectorWorkItemEndsSpanAndLogsDroppedWork(t *testing.T) {
	t.Parallel()

	claimLost := fmt.Errorf("stale attempt: %w", projector.ErrWorkClaimLost)
	deferred := fmt.Errorf("lock timeout: %w", projector.ErrWorkAckDeferred)
	tests := []struct {
		name        string
		ctx         func() context.Context
		sink        projector.ProjectorWorkSink
		heartbeater projector.ProjectorWorkHeartbeater
		wantLog     string
	}{
		{
			name:    "claim lost at ack",
			sink:    &claimLostSink{ackErr: claimLost},
			wantLog: "projector work claim lost to another attempt",
		},
		{
			name: "superseded while ack waits",
			sink: &claimLostSink{ackErr: deferred},
			heartbeater: projectorHeartbeaterFunc(func(context.Context, projector.ScopeGenerationWork) error {
				return projector.ErrWorkSuperseded
			}),
			wantLog: "projector ack waiting for busy scope",
		},
		{
			name: "shutdown while ack waits",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			sink:    &claimLostSink{ackErr: deferred},
			wantLog: "projector ack waiting for busy scope",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			recorder := tracetest.NewSpanRecorder()
			tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test")
			var mu sync.Mutex
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(lockedWriter{mu: &mu, w: &logs}, nil))
			ctx := context.Background()
			if tt.ctx != nil {
				ctx = tt.ctx()
			}
			work := projector.ScopeGenerationWork{
				Scope:        scope.IngestionScope{ScopeID: "scope-drop", SourceSystem: "git"},
				Generation:   scope.ScopeGeneration{GenerationID: "generation-1"},
				AttemptCount: 1,
			}
			var completed atomic.Int64
			err := drainProjectorWorkItem(ctx,
				&fakeWorkSource{items: []projector.ScopeGenerationWork{work}},
				&fakeFactStore{}, &fakeProjectionRunner{}, tt.sink, tt.heartbeater,
				time.Hour, 0, &completed, tracer, nil, logger)
			if err != nil {
				t.Fatalf("drainProjectorWorkItem() error = %v, want nil", err)
			}
			if got := len(recorder.Ended()); got != 1 {
				t.Fatalf("ended spans = %d, want 1 (projector.run must end on every drop path)", got)
			}
			mu.Lock()
			out := logs.String()
			mu.Unlock()
			if !strings.Contains(out, tt.wantLog) {
				t.Fatalf("logs missing %q:\n%s", tt.wantLog, out)
			}
		})
	}
}
