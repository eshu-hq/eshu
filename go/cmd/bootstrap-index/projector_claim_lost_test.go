// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// claimLostSink returns a lost-claim error from Ack or Fail, the way the
// Postgres queue rejects an attempt that another worker reclaimed.
type claimLostSink struct {
	ackErr  error
	failErr error
	acked   atomic.Int64
	failed  atomic.Int64
}

func (s *claimLostSink) Ack(context.Context, projector.ScopeGenerationWork, runtime.Result) error {
	s.acked.Add(1)
	return s.ackErr
}

func (s *claimLostSink) Fail(context.Context, projector.ScopeGenerationWork, error) error {
	s.failed.Add(1)
	return s.failErr
}

// TestDrainProjectorWorkItemDropsLostClaim proves a bootstrap worker whose
// claim was taken by another attempt drops the item instead of failing it or
// aborting the whole drain. The current owner acks or fails the item.
func TestDrainProjectorWorkItemDropsLostClaim(t *testing.T) {
	t.Parallel()

	claimLost := fmt.Errorf("queue rejected stale attempt: %w", failure.ErrWorkClaimLost)
	tests := []struct {
		name        string
		runner      projector.ProjectionRunner
		heartbeater projector.ProjectorWorkHeartbeater
		sink        *claimLostSink
		wantAcks    int64
		wantFails   int64
	}{
		{
			name:   "heartbeat",
			runner: &blockingProjectionRunner{started: make(chan struct{})},
			heartbeater: projectorHeartbeaterFunc(func(context.Context, projector.ScopeGenerationWork) error {
				return claimLost
			}),
			sink: &claimLostSink{},
		},
		{
			name:     "ack",
			runner:   &fakeProjectionRunner{},
			sink:     &claimLostSink{ackErr: claimLost},
			wantAcks: 1,
		},
		{
			name:      "fail",
			runner:    &failingProjectionRunner{failAfter: 0, err: errors.New("projection failed")},
			sink:      &claimLostSink{failErr: claimLost},
			wantFails: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			work := projector.ScopeGenerationWork{
				Scope:        scope.IngestionScope{ScopeID: "scope-lost", SourceSystem: "git"},
				Generation:   scope.ScopeGeneration{GenerationID: "generation-1"},
				AttemptCount: 1,
			}
			var completed atomic.Int64
			err := drainProjectorWorkItem(
				context.Background(),
				&fakeWorkSource{items: []projector.ScopeGenerationWork{work}},
				&fakeFactStore{},
				tt.runner,
				tt.sink,
				tt.heartbeater,
				time.Millisecond,
				0,
				&completed,
				nil, nil, nil,
			)
			if err != nil {
				t.Fatalf("drainProjectorWorkItem() error = %v, want nil (drop the lost claim)", err)
			}
			if got := tt.sink.acked.Load(); got != tt.wantAcks {
				t.Fatalf("Ack calls = %d, want %d", got, tt.wantAcks)
			}
			if got := tt.sink.failed.Load(); got != tt.wantFails {
				t.Fatalf("Fail calls = %d, want %d", got, tt.wantFails)
			}
			if got := completed.Load(); got != 0 {
				t.Fatalf("completed = %d, want 0", got)
			}
		})
	}
}

// deferThenAckSink defers the first Ack the way the Postgres queue does while
// an ingestion commit holds the scope, then accepts it.
type deferThenAckSink struct {
	acked atomic.Int64
}

func (s *deferThenAckSink) Ack(context.Context, projector.ScopeGenerationWork, runtime.Result) error {
	if s.acked.Add(1) == 1 {
		return fmt.Errorf("lock timeout: %w", failure.ErrWorkAckDeferred)
	}
	return nil
}

func (s *deferThenAckSink) Fail(context.Context, projector.ScopeGenerationWork, error) error {
	return errors.New("unexpected Fail")
}

// TestDrainProjectorWorkItemRetriesDeferredAck proves a bootstrap worker keeps
// its claim and retries Ack while the scope is busy, instead of aborting the
// whole drain on the first lock timeout.
func TestDrainProjectorWorkItemRetriesDeferredAck(t *testing.T) {
	t.Parallel()

	work := projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: "scope-busy", SourceSystem: "git"},
		Generation:   scope.ScopeGeneration{GenerationID: "generation-1"},
		AttemptCount: 1,
	}
	sink := &deferThenAckSink{}
	var heartbeats atomic.Int64
	var completed atomic.Int64
	err := drainProjectorWorkItem(
		context.Background(),
		&fakeWorkSource{items: []projector.ScopeGenerationWork{work}},
		&fakeFactStore{},
		&fakeProjectionRunner{},
		sink,
		projectorHeartbeaterFunc(func(context.Context, projector.ScopeGenerationWork) error {
			heartbeats.Add(1)
			return nil
		}),
		time.Hour,
		0,
		&completed,
		nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("drainProjectorWorkItem() error = %v, want nil", err)
	}
	if got := sink.acked.Load(); got != 2 {
		t.Fatalf("Ack calls = %d, want 2", got)
	}
	if got := heartbeats.Load(); got != 1 {
		t.Fatalf("lease renewals = %d, want 1", got)
	}
	if got := completed.Load(); got != 1 {
		t.Fatalf("completed = %d, want 1", got)
	}
}
