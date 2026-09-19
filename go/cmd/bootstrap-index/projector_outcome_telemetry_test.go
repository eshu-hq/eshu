// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"errors"
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
		runner      projector.ProjectionRunner
		sink        projector.ProjectorWorkSink
		heartbeater projector.ProjectorWorkHeartbeater
		wantLogs    []string
		wantNoLogs  []string
		wantStatus  string
	}{
		{
			// Shutdown cancels the projection: no Fail runs, so no failed
			// outcome is recorded; the lease expires and the item re-projects.
			name: "shutdown during projection",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			runner:     &failingProjectionRunner{failAfter: 0, err: context.Canceled},
			sink:       &claimLostSink{},
			wantLogs:   []string{"projector work canceled during shutdown"},
			wantNoLogs: []string{"bootstrap projection failed"},
			wantStatus: "shutdown_canceled",
		},
		{
			name:       "claim lost at fail",
			runner:     &failingProjectionRunner{failAfter: 0, err: errors.New("projection failed")},
			sink:       &claimLostSink{failErr: claimLost},
			wantLogs:   []string{"projector work claim lost to another attempt"},
			wantNoLogs: []string{"bootstrap projection failed", `"status":"failed"`},
			wantStatus: "claim_lost",
		},
		{
			name:   "claim lost at heartbeat",
			runner: &blockingProjectionRunner{started: make(chan struct{})},
			sink:   &claimLostSink{},
			heartbeater: projectorHeartbeaterFunc(func(context.Context, projector.ScopeGenerationWork) error {
				return claimLost
			}),
			wantLogs:   []string{"projector work claim lost to another attempt"},
			wantNoLogs: []string{"bootstrap projector lease heartbeat failed"},
			wantStatus: "claim_lost",
		},
		{
			name:        "ack wait bound exhausted",
			sink:        &claimLostSink{ackErr: deferred},
			heartbeater: projectorHeartbeaterFunc(func(context.Context, projector.ScopeGenerationWork) error { return nil }),
			wantLogs:    []string{"projector ack abandoned after waiting for busy scope"},
			wantStatus:  "ack_wait_exhausted",
		},
		{
			name:       "claim lost at ack",
			sink:       &claimLostSink{ackErr: claimLost},
			wantLogs:   []string{"projector work claim lost to another attempt"},
			wantStatus: "claim_lost",
		},
		{
			name: "superseded while ack waits",
			sink: &claimLostSink{ackErr: deferred},
			heartbeater: projectorHeartbeaterFunc(func(context.Context, projector.ScopeGenerationWork) error {
				return projector.ErrWorkSuperseded
			}),
			wantLogs:   []string{"projector ack waiting for busy scope"},
			wantStatus: "superseded",
		},
		{
			name: "shutdown while ack waits",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			sink: &claimLostSink{ackErr: deferred},
			wantLogs: []string{
				"projector ack waiting for busy scope",
				"projector ack abandoned at shutdown while scope was busy",
			},
			wantStatus: "shutdown_canceled",
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
				&fakeFactStore{}, runnerOrDefault(tt.runner), tt.sink, tt.heartbeater,
				time.Millisecond, 0, &completed, tracer, nil, logger)
			if err != nil {
				t.Fatalf("drainProjectorWorkItem() error = %v, want nil", err)
			}
			ended := recorder.Ended()
			if got := len(ended); got != 1 {
				t.Fatalf("ended spans = %d, want 1 (projector.run must end on every drop path)", got)
			}
			var status string
			for _, attr := range ended[0].Attributes() {
				if attr.Key == "status" {
					status = attr.Value.AsString()
				}
			}
			if status != tt.wantStatus {
				t.Fatalf("span status = %q, want %q", status, tt.wantStatus)
			}
			if got := completed.Load(); got != 0 {
				t.Fatalf("completed = %d, want 0", got)
			}
			mu.Lock()
			out := logs.String()
			mu.Unlock()
			for _, want := range tt.wantLogs {
				if !strings.Contains(out, want) {
					t.Fatalf("logs missing %q:\n%s", want, out)
				}
			}
			for _, unwanted := range tt.wantNoLogs {
				if strings.Contains(out, unwanted) {
					t.Fatalf("logs contain %q for an expected claim loss:\n%s", unwanted, out)
				}
			}
		})
	}
}

func runnerOrDefault(runner projector.ProjectionRunner) projector.ProjectionRunner {
	if runner == nil {
		return &fakeProjectionRunner{}
	}
	return runner
}
