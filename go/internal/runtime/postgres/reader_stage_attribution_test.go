// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

var guardedReaderStages = []Stage{StageReaderBorrow, StageReaderIdentity, StageReaderReplay, StageBusinessQuery}

// TestReaderStageSpansAreChildrenOfTheRequestSpan is the #7545 regression: the
// otel observer started every postgres.reader_access span from
// context.Background(), so each stage span was an orphan root and no request
// trace could say which stage produced a slow guarded read.
func TestReaderStageSpansAreChildrenOfTheRequestSpan(t *testing.T) {
	spans := tracetest.NewSpanRecorder()
	traces := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	observer, err := NewObserver(sdkmetric.NewMeterProvider().Meter("test"), traces.Tracer("test"))
	if err != nil {
		t.Fatal(err)
	}
	access, ctx := newStageFixtureAccess(t, observer, 0)
	ctx, request := traces.Tracer("test").Start(ctx, "request")
	runStageFixtureQuery(t, access, ctx)
	request.End()

	byStage := map[string]sdktrace.ReadOnlySpan{}
	for _, span := range spans.Ended() {
		if span.Name() != readerAccessSpanName {
			continue
		}
		for _, kv := range span.Attributes() {
			if kv.Key == attribute.Key(readerAttributeStage) {
				byStage[kv.Value.AsString()] = span
			}
		}
	}
	for _, stage := range guardedReaderStages {
		span, ok := byStage[string(stage)]
		if !ok {
			t.Fatalf("no %s span for stage %s; got %d stage spans", readerAccessSpanName, stage, len(byStage))
		}
		parent := span.Parent()
		if !parent.IsValid() || parent.SpanID() != request.SpanContext().SpanID() ||
			span.SpanContext().TraceID() != request.SpanContext().TraceID() {
			t.Errorf("stage %s span parent=%v trace=%v, want child of request span %v in trace %v (orphan root)",
				stage, parent.SpanID(), span.SpanContext().TraceID(), request.SpanContext().SpanID(), request.SpanContext().TraceID())
		}
	}
}

// legacyStageSpy implements only the original Observer contract.
type legacyStageSpy struct {
	mu     sync.Mutex
	stages []Stage
}

func (s *legacyStageSpy) Observe(_ string, stage Stage, _ Outcome, _ time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stages = append(s.stages, stage)
}

// TestLegacyObserverStillReceivesEveryStage keeps the original Observer
// contract working: an observer with no context method sees all four stages.
func TestLegacyObserverStillReceivesEveryStage(t *testing.T) {
	spy := &legacyStageSpy{}
	access, ctx := newStageFixtureAccess(t, spy, 0)
	runStageFixtureQuery(t, access, ctx)
	counts := map[Stage]int{}
	for _, stage := range spy.stages {
		counts[stage]++
	}
	for _, stage := range guardedReaderStages {
		if counts[stage] != 1 {
			t.Errorf("legacy Observe calls for %s = %d, want 1 (all=%v)", stage, counts[stage], spy.stages)
		}
	}
}

type stageCtxKey struct{}

// contextStageSpy implements the context-aware method (and, as every Observer
// must, the legacy one) so a test can tell which one the Access called.
type contextStageSpy struct {
	mu        sync.Mutex
	legacy    int
	ctxStages []Stage
	sawValue  bool
}

func (s *contextStageSpy) Observe(string, Stage, Outcome, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.legacy++
}

func (s *contextStageSpy) ObserveContext(ctx context.Context, _ string, stage Stage, _ Outcome, _ time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ctxStages = append(s.ctxStages, stage)
	s.sawValue = ctx.Value(stageCtxKey{}) == "request"
}

// TestContextObserverReceivesRequestContextInsteadOfLegacyObserve proves the
// optional interface is preferred, gets the request ctx, and does not double
// count through the legacy method.
func TestContextObserverReceivesRequestContextInsteadOfLegacyObserve(t *testing.T) {
	spy := &contextStageSpy{}
	access, ctx := newStageFixtureAccess(t, spy, 0)
	runStageFixtureQuery(t, access, context.WithValue(ctx, stageCtxKey{}, "request"))
	if spy.legacy != 0 {
		t.Errorf("legacy Observe calls = %d, want 0 when ObserveContext exists", spy.legacy)
	}
	if len(spy.ctxStages) != len(guardedReaderStages) {
		t.Errorf("ObserveContext stages = %v, want the four guarded stages", spy.ctxStages)
	}
	if !spy.sawValue {
		t.Error("ObserveContext did not receive the request context")
	}
}

// TestGuardedQueryFillsRequestStageTimings proves a query under a ctx carrying
// db.WithStageTimings accounts all four stages, for a nil observer, a legacy
// observer, and the otel observer alike.
func TestGuardedQueryFillsRequestStageTimings(t *testing.T) {
	otel, err := NewObserver(sdkmetric.NewMeterProvider().Meter("test"), sdktrace.NewTracerProvider().Tracer("test"))
	if err != nil {
		t.Fatal(err)
	}
	for name, observer := range map[string]Observer{"nil": nil, "legacy": &legacyStageSpy{}, "otel": otel} {
		t.Run(name, func(t *testing.T) {
			access, ctx := newStageFixtureAccess(t, observer, stageFixtureBusinessDelay)
			ctx, timings := db.WithStageTimings(ctx)
			runStageFixtureQuery(t, access, ctx)
			for _, stage := range []db.ReaderStage{db.ReaderStageBorrow, db.ReaderStageIdentity, db.ReaderStageReplay, db.ReaderStageBusinessQuery} {
				if got := timings.Count(stage); got < 1 {
					t.Errorf("Count(%d) = %d, want >= 1", stage, got)
				}
			}
			if got := timings.Seconds(db.ReaderStageBusinessQuery); got < stageFixtureBusinessDelay.Seconds()/2 {
				t.Errorf("business_query seconds = %v, want >= %v", got, stageFixtureBusinessDelay.Seconds()/2)
			}
			if !timings.Recorded() {
				t.Error("Recorded() = false after a guarded query")
			}
		})
	}
}

// TestRequestStageTimingsKeepEachStageInItsOwnSlot gives exactly one stage a
// delay per case and requires that stage's slot to carry it and every other
// slot to stay below it, so swapping two slots in requestReaderStage cannot
// pass. Only lower bounds and the ordering of the configured delay against
// microsecond-scale fake-driver work are used, never a tight upper bound.
func TestRequestStageTimingsKeepEachStageInItsOwnSlot(t *testing.T) {
	const delay = 25 * time.Millisecond
	stages := []struct {
		name   string
		slot   db.ReaderStage
		delays stageFixtureDelays
	}{
		{"borrow", db.ReaderStageBorrow, stageFixtureDelays{connect: delay}},
		{"identity", db.ReaderStageIdentity, stageFixtureDelays{identity: delay}},
		{"replay", db.ReaderStageReplay, stageFixtureDelays{replay: delay}},
		{"business_query", db.ReaderStageBusinessQuery, stageFixtureDelays{business: delay}},
	}
	all := []db.ReaderStage{db.ReaderStageBorrow, db.ReaderStageIdentity, db.ReaderStageReplay, db.ReaderStageBusinessQuery}
	for _, tt := range stages {
		t.Run(tt.name, func(t *testing.T) {
			access, ctx := newDelayedStageFixtureAccess(t, nil, tt.delays)
			ctx, timings := db.WithStageTimings(ctx)
			runStageFixtureQuery(t, access, ctx)
			if got := timings.Seconds(tt.slot); got < delay.Seconds() {
				t.Errorf("%s slot = %vs, want >= %vs", tt.name, got, delay.Seconds())
			}
			for _, other := range all {
				if other != tt.slot && timings.Seconds(other) >= delay.Seconds() {
					t.Errorf("slot %d = %vs while only %s was delayed %vs: stage slots are swapped",
						other, timings.Seconds(other), tt.name, delay.Seconds())
				}
			}
		})
	}
}

// TestGuardedQueryLeavesAnUnattachedAccumulatorEmpty runs a guarded query with
// no accumulator on its context and proves it neither fails nor leaks stage
// time into an accumulator that was created but not attached to that context.
func TestGuardedQueryLeavesAnUnattachedAccumulatorEmpty(t *testing.T) {
	access, ctx := newStageFixtureAccess(t, nil, 0)
	_, unattached := db.WithStageTimings(context.Background())
	runStageFixtureQuery(t, access, ctx)
	if db.StageTimingsFrom(ctx) != nil {
		t.Fatal("bare request context unexpectedly carries an accumulator")
	}
	if unattached.Recorded() {
		t.Fatal("an accumulator not attached to the request context was filled")
	}
}

// TestRequestReaderStageExcludesWriterCheckpoint pins that the writer
// checkpoint never maps to a per-request reader slot. The checkpoint is writer
// work that runs after the request's reads, so a future case that mapped it
// into a slot would put writer time into the reader stage figures an operator
// reads off the findings log line.
func TestRequestReaderStageExcludesWriterCheckpoint(t *testing.T) {
	t.Parallel()

	if stage, ok := requestReaderStage(StageWriterCheckpoint); ok {
		t.Fatalf("requestReaderStage(writer checkpoint) = (%d, true), want (_, false)", stage)
	}
}

// TestObserveKeepsWriterCheckpointOutOfRequestTimings drives Access.observe
// with the checkpoint stage against an attached accumulator, with and without
// the reader role, and proves the accumulator stays unrecorded either way.
func TestObserveKeepsWriterCheckpointOutOfRequestTimings(t *testing.T) {
	t.Parallel()

	for _, role := range []string{"writer", "reader"} {
		t.Run(role, func(t *testing.T) {
			t.Parallel()

			ctx, timings := db.WithStageTimings(context.Background())
			(&Access{}).observe(ctx, role, StageWriterCheckpoint, time.Now().Add(-time.Second), nil)
			if timings.Recorded() {
				t.Fatalf("role %q: writer checkpoint filled the request stage accumulator", role)
			}
		})
	}
}
