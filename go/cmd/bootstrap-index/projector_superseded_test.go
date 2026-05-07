package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestDrainProjectorWorkItemStopsGracefullyWhenSuperseded(t *testing.T) {
	t.Parallel()

	work := projector.ScopeGenerationWork{
		Scope: scope.IngestionScope{
			ScopeID:      "scope-superseded",
			SourceSystem: "git",
		},
		Generation: scope.ScopeGeneration{GenerationID: "generation-old"},
	}
	started := make(chan struct{})
	completed := atomic.Int64{}
	sink := &concurrentWorkSink{}

	err := drainProjectorWorkItem(
		context.Background(),
		&fakeWorkSource{items: []projector.ScopeGenerationWork{work}},
		&fakeFactStore{},
		&blockingProjectionRunner{started: started},
		sink,
		projectorHeartbeaterFunc(func(context.Context, projector.ScopeGenerationWork) error {
			return projector.ErrWorkSuperseded
		}),
		time.Millisecond,
		0,
		&completed,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("drainProjectorWorkItem() error = %v, want nil", err)
	}
	if got, want := sink.acked.Load(), int64(0); got != want {
		t.Fatalf("acked = %d, want %d", got, want)
	}
	if got, want := completed.Load(), int64(0); got != want {
		t.Fatalf("completed = %d, want %d", got, want)
	}
	select {
	case <-started:
	default:
		t.Fatal("projection did not start before superseded heartbeat")
	}
}

type projectorHeartbeaterFunc func(context.Context, projector.ScopeGenerationWork) error

func (f projectorHeartbeaterFunc) Heartbeat(ctx context.Context, work projector.ScopeGenerationWork) error {
	return f(ctx, work)
}
