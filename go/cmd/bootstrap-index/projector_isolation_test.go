// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// countingProjectorSink tracks Ack vs Fail so tests can assert a failed work
// item was routed to the queue Fail path (retry/dead-letter) rather than left
// claimed and orphaned.
type countingProjectorSink struct {
	acked  atomic.Int64
	failed atomic.Int64
}

func (s *countingProjectorSink) Ack(context.Context, projector.ScopeGenerationWork, projector.Result) error {
	s.acked.Add(1)
	return nil
}

func (s *countingProjectorSink) Fail(context.Context, projector.ScopeGenerationWork, error) error {
	s.failed.Add(1)
	return nil
}

// ackErrorSink returns an error from Ack and records whether Fail was called,
// so tests can prove an Ack failure is treated as fatal (not routed to Fail,
// which could dead-letter already-committed projection work).
type ackErrorSink struct {
	ackErr     error
	failCalled atomic.Bool
}

func (s *ackErrorSink) Ack(context.Context, projector.ScopeGenerationWork, projector.Result) error {
	return s.ackErr
}

func (s *ackErrorSink) Fail(context.Context, projector.ScopeGenerationWork, error) error {
	s.failCalled.Store(true)
	return nil
}

// TestDrainProjectorWorkItemRoutesFailureToFailPath proves the #4464 fix: a
// projection error routes the item to WorkSink.Fail (retry/dead-letter) and
// returns the errProjectorItemFailed sentinel — the worker counts it and
// continues rather than canceling the shared context and aborting siblings.
func TestDrainProjectorWorkItemRoutesFailureToFailPath(t *testing.T) {
	t.Parallel()

	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "scope-timeout", SourceSystem: "git"},
		Generation: scope.ScopeGeneration{GenerationID: "generation-1"},
	}
	sink := &countingProjectorSink{}
	var completed atomic.Int64

	err := drainProjectorWorkItem(
		context.Background(),
		&fakeWorkSource{items: []projector.ScopeGenerationWork{work}},
		&fakeFactStore{},
		&failingProjectionRunner{failAfter: 0, err: errors.New("canonical phase-group write (structural_edges): neo4j execute group timed out after 30s")},
		sink,
		nil, // no heartbeater
		time.Millisecond,
		0, // workerID
		&completed,
		nil, nil, nil,
	)

	if !errors.Is(err, errProjectorItemFailed) {
		t.Fatalf("drainProjectorWorkItem() error = %v, want errProjectorItemFailed (isolated, not propagated as fatal)", err)
	}
	if got := sink.failed.Load(); got != 1 {
		t.Fatalf("sink.Fail called %d times, want 1 (failed item must route to the queue Fail path)", got)
	}
	if got := sink.acked.Load(); got != 0 {
		t.Fatalf("sink.Ack called %d times, want 0", got)
	}
	if got := completed.Load(); got != 0 {
		t.Fatalf("completed = %d, want 0", got)
	}
}

// TestDrainProjectorWorkItemAckErrorIsFatal proves Ack failures are NOT routed
// to Fail: Project already committed graph/content/reducer writes, so failing
// the item could dead-letter successful work and mark the scope failed. The
// steady-state projector treats Ack failure as fatal; bootstrap must match.
func TestDrainProjectorWorkItemAckErrorIsFatal(t *testing.T) {
	t.Parallel()

	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "scope-ack", SourceSystem: "git"},
		Generation: scope.ScopeGeneration{GenerationID: "generation-1"},
	}
	sink := &ackErrorSink{ackErr: errors.New("ack: connection reset")}
	var completed atomic.Int64

	err := drainProjectorWorkItem(
		context.Background(),
		&fakeWorkSource{items: []projector.ScopeGenerationWork{work}},
		&fakeFactStore{},
		&fakeProjectionRunner{}, // Project succeeds
		sink,
		nil,
		time.Millisecond,
		0,
		&completed,
		nil, nil, nil,
	)

	if err == nil {
		t.Fatal("drainProjectorWorkItem() error = nil, want a fatal ack error")
	}
	if errors.Is(err, errProjectorItemFailed) || errors.Is(err, errProjectorDrained) {
		t.Fatalf("ack error must be fatal, not a sentinel; got %v", err)
	}
	if sink.failCalled.Load() {
		t.Fatal("Fail was called for an Ack error; successful projection must not be dead-lettered")
	}
}
