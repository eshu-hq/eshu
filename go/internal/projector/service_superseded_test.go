// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestServiceRunStopsGracefullyWhenHeartbeatSupersedesWork(t *testing.T) {
	t.Parallel()

	work := ScopeGenerationWork{
		Scope: scope.IngestionScope{
			ScopeID:       "scope-123",
			SourceSystem:  "git",
			ScopeKind:     scope.KindRepository,
			CollectorKind: scope.CollectorGit,
			PartitionKey:  "repo-123",
		},
		Generation: scope.ScopeGeneration{
			ScopeID:      "scope-123",
			GenerationID: "generation-old",
			ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
			IngestedAt:   time.Date(2026, time.April, 12, 11, 31, 0, 0, time.UTC),
			Status:       scope.GenerationStatusPending,
			TriggerKind:  scope.TriggerKindSnapshot,
		},
	}
	runner := &stubProjectionRunner{
		waitForContextCancellation: true,
	}
	heartbeater := &stubProjectorWorkHeartbeater{
		failAfter: 1,
		err:       ErrWorkSuperseded,
	}
	sink := &stubProjectorWorkSink{}
	service := Service{
		PollInterval:      10 * time.Millisecond,
		WorkSource:        &stubProjectorWorkSource{workItems: []ScopeGenerationWork{work}},
		FactStore:         &stubFactStore{},
		Runner:            runner,
		WorkSink:          sink,
		Heartbeater:       heartbeater,
		HeartbeatInterval: 5 * time.Millisecond,
		Wait:              func(context.Context, time.Duration) error { return context.Canceled },
	}

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := sink.ackCalls, 0; got != want {
		t.Fatalf("ack calls = %d, want %d", got, want)
	}
	if got, want := sink.failCalls, 0; got != want {
		t.Fatalf("fail calls = %d, want %d", got, want)
	}
}

func TestServiceRunDropsWorkWhoseClaimWasLost(t *testing.T) {
	t.Parallel()

	claimLost := fmt.Errorf("storage rejected stale attempt: %w", ErrWorkClaimLost)
	tests := []struct {
		name        string
		runner      *stubProjectionRunner
		heartbeater *stubProjectorWorkHeartbeater
		sink        *stubProjectorWorkSink
		wantAcks    int
		wantFails   int
	}{
		{
			name:        "heartbeat",
			runner:      &stubProjectionRunner{waitForContextCancellation: true},
			heartbeater: &stubProjectorWorkHeartbeater{failAfter: 1, err: claimLost},
			sink:        &stubProjectorWorkSink{},
		},
		{
			name:        "ack",
			runner:      &stubProjectionRunner{},
			heartbeater: &stubProjectorWorkHeartbeater{},
			sink:        &stubProjectorWorkSink{ackErr: claimLost},
			wantAcks:    1,
		},
		{
			name:        "fail",
			runner:      &stubProjectionRunner{runErr: errors.New("projection failed")},
			heartbeater: &stubProjectorWorkHeartbeater{},
			sink:        &stubProjectorWorkSink{failErr: claimLost},
			wantFails:   1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			work := ScopeGenerationWork{
				Scope:        scope.IngestionScope{ScopeID: "scope-123", ScopeKind: scope.KindRepository},
				Generation:   scope.ScopeGeneration{ScopeID: "scope-123", GenerationID: "generation-1"},
				AttemptCount: 1,
			}
			service := Service{
				PollInterval:      10 * time.Millisecond,
				WorkSource:        &stubProjectorWorkSource{workItems: []ScopeGenerationWork{work}},
				FactStore:         &stubFactStore{},
				Runner:            tt.runner,
				WorkSink:          tt.sink,
				Heartbeater:       tt.heartbeater,
				HeartbeatInterval: 5 * time.Millisecond,
				Wait:              func(context.Context, time.Duration) error { return context.Canceled },
			}

			// Another attempt owns the work now; the stale worker must drop
			// it rather than stop every projector worker.
			if err := service.Run(context.Background()); err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}
			if got := tt.sink.ackCalls; got != tt.wantAcks {
				t.Fatalf("ack calls = %d, want %d", got, tt.wantAcks)
			}
			if got := tt.sink.failCalls; got != tt.wantFails {
				t.Fatalf("fail calls = %d, want %d", got, tt.wantFails)
			}
		})
	}
}

// sequencedAckSink returns ackErrs in order, then nil.
type sequencedAckSink struct {
	mu      sync.Mutex
	ackErrs []error
	acks    int
	fails   int
}

func (s *sequencedAckSink) Ack(context.Context, ScopeGenerationWork, Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acks++
	if len(s.ackErrs) == 0 {
		return nil
	}
	err := s.ackErrs[0]
	s.ackErrs = s.ackErrs[1:]
	return err
}

func (s *sequencedAckSink) Fail(context.Context, ScopeGenerationWork, error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fails++
	return nil
}

func TestServiceRunRetriesAckWhileScopeIsBusy(t *testing.T) {
	t.Parallel()

	deferred := fmt.Errorf("storage lock timeout: %w", ErrWorkAckDeferred)
	tests := []struct {
		name           string
		heartbeater    *stubProjectorWorkHeartbeater
		wantAcks       int
		wantHeartbeats int
	}{
		{
			// Each deferral renews the lease, then Ack is retried until the
			// ingestion commit releases the scope.
			name:           "retries until the scope is free",
			heartbeater:    &stubProjectorWorkHeartbeater{},
			wantAcks:       3,
			wantHeartbeats: 2,
		},
		{
			// A newer generation committed while Ack waited: stop without
			// acking stale work.
			name:           "stops when the renewal reports supersession",
			heartbeater:    &stubProjectorWorkHeartbeater{failAfter: 1, err: ErrWorkSuperseded},
			wantAcks:       1,
			wantHeartbeats: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sink := &sequencedAckSink{ackErrs: []error{deferred, deferred}}
			work := ScopeGenerationWork{
				Scope:        scope.IngestionScope{ScopeID: "scope-123", ScopeKind: scope.KindRepository},
				Generation:   scope.ScopeGeneration{ScopeID: "scope-123", GenerationID: "generation-1"},
				AttemptCount: 1,
			}
			service := Service{
				PollInterval:      10 * time.Millisecond,
				WorkSource:        &stubProjectorWorkSource{workItems: []ScopeGenerationWork{work}},
				FactStore:         &stubFactStore{},
				Runner:            &stubProjectionRunner{},
				WorkSink:          sink,
				Heartbeater:       tt.heartbeater,
				HeartbeatInterval: time.Hour, // only the deferral loop heartbeats
				Wait:              func(context.Context, time.Duration) error { return context.Canceled },
			}

			if err := service.Run(context.Background()); err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}
			if sink.acks != tt.wantAcks || sink.fails != 0 {
				t.Fatalf("acks=%d fails=%d, want acks=%d fails=0", sink.acks, sink.fails, tt.wantAcks)
			}
			if got := tt.heartbeater.calls; got != tt.wantHeartbeats {
				t.Fatalf("heartbeats = %d, want %d", got, tt.wantHeartbeats)
			}
		})
	}
}
