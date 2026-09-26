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

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// conflictThenWorkSource returns a wrapped ErrWorkClaimConflict for the first
// conflicts Claim calls, then serves items, then reports an empty queue. It
// mirrors postgres.ProjectorQueue.Claim after its bounded 40P01/40001 retries
// run out.
type conflictThenWorkSource struct {
	mu        sync.Mutex
	conflicts int
	calls     int
	items     []projector.ScopeGenerationWork
	next      int
}

func (s *conflictThenWorkSource) Claim(context.Context) (projector.ScopeGenerationWork, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.calls <= s.conflicts {
		return projector.ScopeGenerationWork{}, false, fmt.Errorf(
			"claim projector work: %w: deadlock detected (SQLSTATE 40P01)", failure.ErrWorkClaimConflict)
	}
	if s.next >= len(s.items) {
		return projector.ScopeGenerationWork{}, false, nil
	}
	item := s.items[s.next]
	s.next++
	return item, true, nil
}

func (s *conflictThenWorkSource) claimCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func conflictTestItems(n int) []projector.ScopeGenerationWork {
	items := make([]projector.ScopeGenerationWork, n)
	for i := range items {
		items[i] = projector.ScopeGenerationWork{Scope: scope.IngestionScope{ScopeID: fmt.Sprintf("scope-%d", i)}}
	}
	return items
}

func TestDrainProjectorWorkItemSurvivesClaimConflict(t *testing.T) {
	t.Parallel()

	source := &conflictThenWorkSource{conflicts: 1, items: conflictTestItems(1)}
	sink := &concurrentWorkSink{}
	var completed atomic.Int64

	err := drainProjectorWorkItem(
		context.Background(), source, &fakeFactStore{}, &fakeProjectionRunner{}, sink,
		nil, 0, 0, &completed, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("drainProjectorWorkItem() error = %v, want nil: a claim conflict must not end the worker", err)
	}
	if got := sink.acked.Load(); got != 1 {
		t.Fatalf("acked = %d, want 1: the item claimed after the conflict must be processed", got)
	}
	if got := source.claimCalls(); got != 2 {
		t.Fatalf("Claim calls = %d, want 2 (conflict, then work)", got)
	}
}

func TestDrainProjectorSurvivesClaimConflictAcrossWorkers(t *testing.T) {
	t.Parallel()

	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprintf("workers_%d", workers), func(t *testing.T) {
			t.Parallel()
			// Every worker sees at least one conflict; siblings must keep
			// draining and no scripted item may be lost.
			source := &conflictThenWorkSource{conflicts: 2 * workers, items: conflictTestItems(6)}
			sink := &concurrentWorkSink{}

			err := drainProjector(
				context.Background(), source, &fakeFactStore{}, &fakeProjectionRunner{}, sink,
				nil, 0, workers, nil, nil, nil,
			)
			if err != nil {
				t.Fatalf("drainProjector() error = %v, want nil", err)
			}
			if got := sink.acked.Load(); got != 6 {
				t.Fatalf("acked = %d, want 6: a conflict must not cancel siblings or drop work", got)
			}
		})
	}
}

func TestDrainProjectorStillStopsOnNonConflictClaimError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("claim projector work: relation does not exist")
	for _, workers := range []int{1, 2} {
		err := drainProjector(
			context.Background(), errorClaimSource{err: wantErr}, &fakeFactStore{}, &fakeProjectionRunner{},
			&concurrentWorkSink{}, nil, 0, workers, nil, nil, nil,
		)
		if !errors.Is(err, wantErr) {
			t.Fatalf("workers=%d drainProjector() error = %v, want %v", workers, err, wantErr)
		}
	}
}

type errorClaimSource struct{ err error }

func (s errorClaimSource) Claim(context.Context) (projector.ScopeGenerationWork, bool, error) {
	return projector.ScopeGenerationWork{}, false, s.err
}

func TestDrainProjectorWorkItemClaimConflictWaitStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	source := &conflictThenWorkSource{conflicts: 1 << 30}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var completed atomic.Int64
	go func() {
		done <- drainProjectorWorkItem(
			ctx, source, &fakeFactStore{}, &fakeProjectionRunner{}, &concurrentWorkSink{},
			nil, 0, 0, &completed, nil, nil, nil,
		)
	}()
	for source.claimCalls() == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("drainProjectorWorkItem() error = %v, want context.Canceled", err)
		}
	case <-time.After(claimConflictWait / 2):
		t.Fatalf("drainProjectorWorkItem did not stop within %s of cancel; the wait must be context-aware", claimConflictWait/2)
	}
	if got := source.claimCalls(); got != 1 {
		t.Fatalf("Claim calls = %d, want 1: a canceled wait must not re-claim", got)
	}
}

func TestDrainProjectorWorkItemLogsClaimConflict(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var mu sync.Mutex
	logger := slog.New(slog.NewJSONHandler(lockedWriter{mu: &mu, w: &buf}, nil))
	source := &conflictThenWorkSource{conflicts: 1, items: conflictTestItems(1)}
	var completed atomic.Int64

	if err := drainProjectorWorkItem(
		context.Background(), source, &fakeFactStore{}, &fakeProjectionRunner{}, &concurrentWorkSink{},
		nil, 0, 3, &completed, nil, nil, logger,
	); err != nil {
		t.Fatalf("drainProjectorWorkItem() error = %v, want nil", err)
	}

	mu.Lock()
	out := buf.String()
	mu.Unlock()
	for _, want := range []string{
		`"failure_class":"projector_claim_conflict"`,
		`"worker_id":3`,
		"projector claim conflict",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("log output missing %s:\n%s", want, out)
		}
	}
}

func TestBuildBootstrapProjectorWiresQueueInstruments(t *testing.T) {
	t.Parallel()

	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider().Meter("bootstrap-claim-conflict-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	deps, err := buildBootstrapProjector(
		context.Background(), &fakeBootstrapSQLDB{}, &noopCanonicalWriter{},
		func(string) string { return "" }, nil, instruments, nil,
	)
	if err != nil {
		t.Fatalf("buildBootstrapProjector() error = %v, want nil", err)
	}
	queue, ok := deps.workSource.(postgres.ProjectorQueue)
	if !ok {
		t.Fatalf("workSource type = %T, want postgres.ProjectorQueue", deps.workSource)
	}
	if queue.Instruments != instruments {
		t.Fatal("ProjectorQueue.Instruments not wired: eshu_dp_queue_claim_conflict_retries_total would never be emitted by bootstrap-index")
	}
}
