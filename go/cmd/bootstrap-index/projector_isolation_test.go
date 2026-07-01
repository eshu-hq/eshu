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

// TestDrainProjectorWorkItemFailsItemInsteadOfPropagating proves the #4464 fix:
// a projection error on one work item must Fail that item (so it retries /
// dead-letters instead of orphaning its claim) and return nil, so the concurrent
// drainProjector loop does NOT cancel the shared context and abort sibling
// workers. Before the fix, drainProjectorWorkItem returned the error (triggering
// cancel-all) and never called Fail.
func TestDrainProjectorWorkItemFailsItemInsteadOfPropagating(t *testing.T) {
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
	if err != nil {
		t.Fatalf("drainProjectorWorkItem() error = %v, want nil (item failure must be isolated, not propagated)", err)
	}
	if got := sink.failed.Load(); got != 1 {
		t.Fatalf("sink.Fail called %d times, want 1 (failed item must be routed to the queue Fail path, not orphaned)", got)
	}
	if got := sink.acked.Load(); got != 0 {
		t.Fatalf("sink.Ack called %d times, want 0", got)
	}
	if got := completed.Load(); got != 0 {
		t.Fatalf("completed = %d, want 0", got)
	}
}
