package reducer

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestFirstReducerPartitionKeyUsesStableSortedKey(t *testing.T) {
	t.Parallel()

	intent := Intent{
		EntityKeys: []string{"repo:zeta", "repo:alpha", "repo:beta"},
	}

	if got, want := firstReducerPartitionKey(intent), "repo:alpha"; got != want {
		t.Fatalf("firstReducerPartitionKey() = %q, want %q", got, want)
	}
}

func TestServiceRunHeartbeatsLongRunningReducerWork(t *testing.T) {
	t.Parallel()

	intent := Intent{
		IntentID:        "intent-heartbeat",
		ScopeID:         "scope-123",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Domain:          DomainSemanticEntityMaterialization,
		Cause:           "projector emitted semantic entity work",
		EntityKeys:      []string{"repo:eshu"},
		RelatedScopeIDs: []string{"scope-123"},
		EnqueuedAt:      time.Date(2026, time.April, 23, 17, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 23, 17, 0, 0, 0, time.UTC),
		Status:          IntentStatusPending,
	}

	release := make(chan struct{})
	heartbeater := &stubReducerHeartbeater{
		afterHeartbeat: func(count int) {
			if count == 2 {
				close(release)
			}
		},
	}
	executor := &blockingReducerExecutor{
		release: release,
		result: Result{
			IntentID: intent.IntentID,
			Domain:   intent.Domain,
			Status:   ResultStatusSucceeded,
		},
	}
	sink := &stubReducerWorkSink{}

	service := Service{
		PollInterval:      10 * time.Millisecond,
		WorkSource:        &stubReducerWorkSource{intents: []Intent{intent}},
		Executor:          executor,
		WorkSink:          sink,
		Heartbeater:       heartbeater,
		HeartbeatInterval: 5 * time.Millisecond,
		Wait:              func(context.Context, time.Duration) error { return context.Canceled },
	}

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := heartbeater.calls(), 2; got < want {
		t.Fatalf("heartbeat calls = %d, want at least %d", got, want)
	}
	if got, want := sink.ackCalls, 1; got != want {
		t.Fatalf("ack calls = %d, want %d", got, want)
	}
	if got, want := sink.failCalls, 0; got != want {
		t.Fatalf("fail calls = %d, want %d", got, want)
	}
}

type stubReducerHeartbeater struct {
	mu             sync.Mutex
	heartbeatCalls int
	afterHeartbeat func(int)
}

func (s *stubReducerHeartbeater) Heartbeat(context.Context, Intent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heartbeatCalls++
	if s.afterHeartbeat != nil {
		s.afterHeartbeat(s.heartbeatCalls)
	}
	return nil
}

func (s *stubReducerHeartbeater) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.heartbeatCalls
}

type blockingReducerExecutor struct {
	release <-chan struct{}
	result  Result
}

func (b *blockingReducerExecutor) Execute(ctx context.Context, _ Intent) (Result, error) {
	select {
	case <-b.release:
		return b.result, nil
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}
