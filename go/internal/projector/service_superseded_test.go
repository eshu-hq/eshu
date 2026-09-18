// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"errors"
	"fmt"
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
