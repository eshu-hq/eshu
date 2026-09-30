// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// passBootstrapBaselineFence passes every generation as a full one.
type passBootstrapBaselineFence struct{}

func (passBootstrapBaselineFence) ReadDeltaBaseline(context.Context, projector.ScopeGenerationWork) (projector.DeltaBaselineState, error) {
	return projector.DeltaBaselineState{TargetFound: true, TargetStatus: scope.GenerationStatusPending}, nil
}

func (passBootstrapBaselineFence) RefuseDeltaBaseline(context.Context, projector.ScopeGenerationWork, projector.DeltaBaselineRefusal) error {
	return errors.New("pass fence never refuses")
}

// MarkProjectionWriteStarted makes the pass fence double as a #7389 write
// marker that always marks.
func (passBootstrapBaselineFence) MarkProjectionWriteStarted(context.Context, projector.ScopeGenerationWork) error {
	return nil
}

// scriptedBootstrapBaselineFence refuses every delta, or fails its read.
type scriptedBootstrapBaselineFence struct {
	mu       sync.Mutex
	readErr  error
	refusals []projector.DeltaBaselineRefusal
}

func (f *scriptedBootstrapBaselineFence) ReadDeltaBaseline(context.Context, projector.ScopeGenerationWork) (projector.DeltaBaselineState, error) {
	if f.readErr != nil {
		return projector.DeltaBaselineState{}, f.readErr
	}
	return projector.DeltaBaselineState{
		TargetFound: true, TargetStatus: scope.GenerationStatusPending, IsDelta: true,
		BaselineCommitSHA: "A", ActiveGenerationID: "gen-b", ActiveCommitSHA: "B",
	}, nil
}

func (f *scriptedBootstrapBaselineFence) RefuseDeltaBaseline(_ context.Context, _ projector.ScopeGenerationWork, r projector.DeltaBaselineRefusal) error {
	f.mu.Lock()
	f.refusals = append(f.refusals, r)
	f.mu.Unlock()
	return fmt.Errorf("refused: %w", failure.ErrWorkSuperseded)
}

// countingBootstrapRunner and countingBootstrapSink record whether a refused
// delta reached projection, Ack, or Fail.
type countingBootstrapRunner struct{ calls atomic.Int64 }

func (r *countingBootstrapRunner) Project(context.Context, scope.IngestionScope, scope.ScopeGeneration, []facts.Envelope) (runtime.Result, error) {
	r.calls.Add(1)
	return runtime.Result{}, nil
}

type countingBootstrapSink struct{ acked, failed atomic.Int64 }

func (s *countingBootstrapSink) Ack(context.Context, projector.ScopeGenerationWork, runtime.Result) error {
	s.acked.Add(1)
	return nil
}

func (s *countingBootstrapSink) Fail(context.Context, projector.ScopeGenerationWork, error) error {
	s.failed.Add(1)
	return nil
}

func bootstrapDeltaItems(n int) []projector.ScopeGenerationWork {
	items := make([]projector.ScopeGenerationWork, n)
	for i := range items {
		items[i] = projector.ScopeGenerationWork{
			Scope:      scope.IngestionScope{ScopeID: fmt.Sprintf("scope-%d", i)},
			Generation: scope.ScopeGeneration{GenerationID: fmt.Sprintf("gen-%d", i)},
		}
	}
	return items
}

// TestDrainProjectorPreflightRefusalProjectsNothing is the pre-Project proof
// for the bootstrap-index loop, sequential and concurrent.
func TestDrainProjectorPreflightRefusalProjectsNothing(t *testing.T) {
	t.Parallel()
	for _, workers := range []int{1, 4} {
		fence := &scriptedBootstrapBaselineFence{}
		runner := &countingBootstrapRunner{}
		sink := &countingBootstrapSink{}
		err := drainProjector(context.Background(), &concurrentWorkSource{items: bootstrapDeltaItems(3)},
			&fakeFactStore{}, runner, sink, fence, passBootstrapBaselineFence{}, nil, 0, workers, nil, nil, nil)
		if err != nil {
			t.Fatalf("workers=%d drainProjector() = %v, want nil", workers, err)
		}
		if len(fence.refusals) != 3 || runner.calls.Load() != 0 || sink.acked.Load() != 0 || sink.failed.Load() != 0 {
			t.Fatalf("workers=%d refusals=%d project=%d ack=%d fail=%d, want 3 0 0 0", workers,
				len(fence.refusals), runner.calls.Load(), sink.acked.Load(), sink.failed.Load())
		}
	}
}

// TestDrainProjectorPreflightReadErrorFailsClosed proves a fence that cannot
// read routes the item to Fail and never projects it.
func TestDrainProjectorPreflightReadErrorFailsClosed(t *testing.T) {
	t.Parallel()
	runner := &countingBootstrapRunner{}
	sink := &countingBootstrapSink{}
	err := drainProjector(context.Background(), &concurrentWorkSource{items: bootstrapDeltaItems(1)},
		&fakeFactStore{}, runner, sink, &scriptedBootstrapBaselineFence{readErr: errors.New("connection reset")},
		passBootstrapBaselineFence{}, nil, 0, 1, nil, nil, nil)
	if err == nil {
		t.Fatal("drainProjector() = nil, want the incomplete-drain error")
	}
	if runner.calls.Load() != 0 || sink.failed.Load() != 1 || sink.acked.Load() != 0 {
		t.Fatalf("project=%d fail=%d ack=%d, want 0 1 0", runner.calls.Load(), sink.failed.Load(), sink.acked.Load())
	}
}

// TestDrainProjectorRequiresDeltaBaselineFence proves the fence is required.
func TestDrainProjectorRequiresDeltaBaselineFence(t *testing.T) {
	t.Parallel()
	source := &concurrentWorkSource{items: bootstrapDeltaItems(1)}
	err := drainProjector(context.Background(), source, &fakeFactStore{}, &fakeProjectionRunner{},
		&concurrentWorkSink{}, nil, passBootstrapBaselineFence{}, nil, 0, 1, nil, nil, nil)
	if err == nil {
		t.Fatal("drainProjector(nil fence) = nil, want error")
	}
	if source.index != 0 {
		t.Fatalf("claimed %d items without a fence, want 0", source.index)
	}
}

// TestBuildBootstrapProjectorWiresDeltaBaselineFence fails when bootstrap-index
// builds its projector without the #7319 fence.
func TestBuildBootstrapProjectorWiresDeltaBaselineFence(t *testing.T) {
	t.Parallel()
	deps, err := buildBootstrapProjector(context.Background(), &fakeBootstrapSQLDB{}, &noopCanonicalWriter{},
		func(string) string { return "" }, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildBootstrapProjector() = %v", err)
	}
	if deps.baselineFence == nil {
		t.Fatal("baselineFence = nil, want the projector queue")
	}
	if _, ok := deps.baselineFence.(postgres.ProjectorQueue); !ok {
		t.Fatalf("baselineFence type = %T, want postgres.ProjectorQueue", deps.baselineFence)
	}
}
