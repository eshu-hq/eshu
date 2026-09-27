// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/trace"
)

// noopSecretLines is a secret-lines lifecycle that does nothing, for tests of
// unrelated bootstrap behavior.
func noopSecretLines() secretLinesLifecycle {
	return secretLinesLifecycle{
		lock: func(context.Context, bootstrapDB, *slog.Logger, time.Duration) (func(context.Context) error, error) {
			return func(context.Context) error { return nil }, nil
		},
		begin: func(context.Context, bootstrapDB) (int64, error) { return 1, nil },
		finalize: func(context.Context, bootstrapDB, int64, *slog.Logger, *telemetry.Instruments) error {
			return nil
		},
	}
}

// TestRunBeginsSecretLinesDeferralBeforeWritesAndFinalizesAfterThePipeline
// proves the #7125 bulk-load bracket: readiness is taken away after the schema
// applies and before the graph, collector, or projector run (so no deferred
// content write precedes it), and the finalizer runs only after the collector
// pipeline finished, with the epoch begin returned.
func TestRunBeginsSecretLinesDeferralBeforeWritesAndFinalizesAfterThePipeline(t *testing.T) {
	t.Parallel()

	var (
		mu        sync.Mutex
		events    []string
		committer = &fakeCommitter{}
	)
	record := func(event string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	}
	secretLines := secretLinesLifecycle{
		lock: func(context.Context, bootstrapDB, *slog.Logger, time.Duration) (func(context.Context) error, error) {
			record("secret_lines_lock")
			return func(context.Context) error { return nil }, nil
		},
		begin: func(context.Context, bootstrapDB) (int64, error) {
			record("secret_lines_begin")
			return 41, nil
		},
		finalize: func(_ context.Context, _ bootstrapDB, epoch int64, _ *slog.Logger, _ *telemetry.Instruments) error {
			if got := committer.snapshotCalls(); len(got) == 0 || got[len(got)-1] != "enqueue_drift" {
				t.Errorf("secret lines finalizer ran before the bootstrap pipeline completed: calls=%v", got)
			}
			if epoch != 41 {
				t.Errorf("finalize epoch = %d, want the epoch begin returned (41)", epoch)
			}
			record("secret_lines_finalize")
			return nil
		},
	}

	err := run(
		context.Background(),
		func(string) string { return "" },
		func(context.Context, func(string) string) (bootstrapDB, error) { return &fakeBootstrapDB{}, nil },
		func(context.Context, bootstrapDB, *slog.Logger) error {
			record("schema")
			return nil
		},
		func(context.Context, bootstrapDB) error {
			record("content_indexes_finalize")
			return nil
		},
		secretLines,
		func(context.Context, bootstrapDB, func(string) string, *slog.Logger) error {
			record("graph_schema")
			return nil
		},
		func(context.Context, bootstrapDB, func(string) string, trace.Tracer, *telemetry.Instruments) (graphDeps, error) {
			record("graph_open")
			return graphDeps{writer: &noopCanonicalWriter{}, close: func() error { return nil }}, nil
		},
		func(context.Context, bootstrapDB, func(string) string, trace.Tracer, *telemetry.Instruments, *slog.Logger) (collectorDeps, error) {
			record("collector_build")
			return collectorDeps{
				source: &fakeSource{generations: []collector.CollectedGeneration{
					{Scope: scope.IngestionScope{ScopeID: "s1"}},
				}},
				committer: committer,
			}, nil
		},
		func(context.Context, bootstrapDB, runtime.CanonicalWriter, func(string) string, trace.Tracer, *telemetry.Instruments, *slog.Logger) (projectorDeps, error) {
			return projectorDeps{
				workSource: &fakeWorkSource{items: []projector.ScopeGenerationWork{{Scope: scope.IngestionScope{ScopeID: "s1"}}}},
				factStore:  &fakeFactStore{},
				runner:     &fakeProjectionRunner{},
				workSink:   &fakeWorkSink{},
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	mu.Lock()
	defer mu.Unlock()
	indexOf := func(event string) int {
		for i, got := range events {
			if got == event {
				return i
			}
		}
		t.Fatalf("event %q never ran; events = %v", event, events)
		return -1
	}
	begin := indexOf("secret_lines_begin")
	if indexOf("schema") >= begin || begin >= indexOf("graph_schema") || begin >= indexOf("collector_build") {
		t.Fatalf("secret lines deferral must begin after the schema and before any content write path; events = %v", events)
	}
	if indexOf("secret_lines_finalize") < indexOf("collector_build") {
		t.Fatalf("secret lines finalizer ran before the collector; events = %v", events)
	}
	indexOf("content_indexes_finalize")
}

// TestRunReportsSecretLinesFinalizationFailure proves a finalizer failure is not
// swallowed: the run fails, and the other finalizer still ran to completion.
func TestRunReportsSecretLinesFinalizationFailure(t *testing.T) {
	t.Parallel()

	boom := errors.New("secret lines backfill failed")
	indexesFinalized := false
	secretLines := noopSecretLines()
	secretLines.finalize = func(context.Context, bootstrapDB, int64, *slog.Logger, *telemetry.Instruments) error {
		return boom
	}
	err := finalizeBootstrapContent(
		context.Background(), &fakeBootstrapDB{}, slog.New(slog.DiscardHandler), nil,
		func(context.Context, bootstrapDB) error { indexesFinalized = true; return nil },
		secretLines, 1,
	)
	if !errors.Is(err, boom) {
		t.Fatalf("finalizeBootstrapContent error = %v, want the secret lines failure", err)
	}
	if !indexesFinalized {
		t.Fatal("content index finalization did not run to completion beside a failing secret lines finalizer")
	}
}
