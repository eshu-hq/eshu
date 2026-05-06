package reducer

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeBatchWorkSource implements both WorkSource (single) and BatchWorkSource.
type fakeBatchWorkSource struct {
	mu      sync.Mutex
	intents []Intent
	claimed int
	limits  []int
}

func (f *fakeBatchWorkSource) Claim(ctx context.Context) (Intent, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.intents) == 0 {
		return Intent{}, false, nil
	}
	intent := f.intents[0]
	f.intents = f.intents[1:]
	f.claimed++
	return intent, true, nil
}

func (f *fakeBatchWorkSource) ClaimBatch(_ context.Context, limit int) ([]Intent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.limits = append(f.limits, limit)
	if len(f.intents) == 0 {
		return nil, nil
	}
	end := limit
	if end > len(f.intents) {
		end = len(f.intents)
	}
	batch := make([]Intent, end)
	copy(batch, f.intents[:end])
	f.intents = f.intents[end:]
	f.claimed += end
	return batch, nil
}

func (f *fakeBatchWorkSource) claimedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.claimed
}

func (f *fakeBatchWorkSource) claimLimits() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.limits...)
}

// fakeBatchWorkSink implements both WorkSink and BatchWorkSink.
type fakeBatchWorkSink struct {
	mu       sync.Mutex
	acked    int
	failed   int
	ackIDs   []string
	failedBy []string
}

func (f *fakeBatchWorkSink) Ack(_ context.Context, intent Intent, _ Result) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acked++
	f.ackIDs = append(f.ackIDs, intent.IntentID)
	return nil
}

func (f *fakeBatchWorkSink) Fail(_ context.Context, intent Intent, _ error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failed++
	f.failedBy = append(f.failedBy, intent.IntentID)
	return nil
}

func (f *fakeBatchWorkSink) AckBatch(_ context.Context, intents []Intent, _ []Result) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acked += len(intents)
	for _, intent := range intents {
		f.ackIDs = append(f.ackIDs, intent.IntentID)
	}
	return nil
}

type countingExecutor struct {
	count atomic.Int64
}

func (e *countingExecutor) Execute(_ context.Context, intent Intent) (Result, error) {
	e.count.Add(1)
	return Result{
		IntentID: intent.IntentID,
		Domain:   intent.Domain,
		Status:   ResultStatusSucceeded,
	}, nil
}

func makeTestIntents(n int) []Intent {
	intents := make([]Intent, n)
	for i := range intents {
		intents[i] = Intent{
			IntentID:        fmt.Sprintf("intent-%d", i),
			ScopeID:         "scope-1",
			GenerationID:    "gen-1",
			SourceSystem:    "git",
			Domain:          DomainCodeCallMaterialization,
			Cause:           "test",
			EntityKeys:      []string{fmt.Sprintf("key-%d", i)},
			RelatedScopeIDs: []string{"scope-1"},
			Status:          IntentStatusClaimed,
			EnqueuedAt:      time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC),
			AvailableAt:     time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC),
		}
	}
	return intents
}

func TestServiceRunBatchConcurrentProcessesAllItems(t *testing.T) {
	t.Parallel()

	intents := makeTestIntents(20)
	source := &fakeBatchWorkSource{intents: intents}
	sink := &fakeBatchWorkSink{}
	executor := &countingExecutor{}

	svc := Service{
		PollInterval:   10 * time.Millisecond,
		WorkSource:     source,
		Executor:       executor,
		WorkSink:       sink,
		Workers:        4,
		BatchClaimSize: 8,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := svc.runMainLoop(ctx)
	if err != nil {
		t.Fatalf("runMainLoop() error = %v", err)
	}

	if got := executor.count.Load(); got != 20 {
		t.Fatalf("executor count = %d, want 20", got)
	}
	sink.mu.Lock()
	acked := sink.acked
	sink.mu.Unlock()
	if acked != 20 {
		t.Fatalf("sink acked = %d, want 20", acked)
	}
}

func TestServiceFallsBackToPerItemWhenNoBatchInterface(t *testing.T) {
	t.Parallel()

	// Use a WorkSource that does NOT implement BatchWorkSource.
	intents := makeTestIntents(5)
	source := &nonBatchWorkSource{intents: intents}
	sink := &nonBatchWorkSink{}
	executor := &countingExecutor{}

	svc := Service{
		PollInterval: 10 * time.Millisecond,
		WorkSource:   source,
		Executor:     executor,
		WorkSink:     sink,
		Workers:      2,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := svc.runMainLoop(ctx)
	if err != nil {
		t.Fatalf("runMainLoop() error = %v", err)
	}

	if got := executor.count.Load(); got != 5 {
		t.Fatalf("executor count = %d, want 5", got)
	}
}

func TestServiceRunBatchConcurrentHeartbeatsLongRunningReducerWork(t *testing.T) {
	t.Parallel()

	intent := Intent{
		IntentID:        "intent-batch-heartbeat",
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
	sink := &fakeBatchWorkSink{}

	service := Service{
		PollInterval:      10 * time.Millisecond,
		WorkSource:        &fakeBatchWorkSource{intents: []Intent{intent}},
		Executor:          executor,
		WorkSink:          sink,
		Heartbeater:       heartbeater,
		HeartbeatInterval: 5 * time.Millisecond,
		Workers:           2,
		BatchClaimSize:    2,
		Wait:              func(context.Context, time.Duration) error { return context.Canceled },
	}

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := heartbeater.calls(), 2; got < want {
		t.Fatalf("heartbeat calls = %d, want at least %d", got, want)
	}
	sink.mu.Lock()
	acked, failed := sink.acked, sink.failed
	sink.mu.Unlock()
	if got, want := acked, 1; got != want {
		t.Fatalf("sink acked = %d, want %d", got, want)
	}
	if got, want := failed, 0; got != want {
		t.Fatalf("sink failed = %d, want %d", got, want)
	}
}

func TestServiceRunBatchConcurrentDoesNotPreclaimBeyondReadyWorkers(t *testing.T) {
	t.Parallel()

	source := &fakeBatchWorkSource{intents: makeTestIntents(5)}
	sink := &fakeBatchWorkSink{}
	release := make(chan struct{})
	executor := &blockingManyReducerExecutor{
		release: release,
		started: make(chan string, 5),
	}

	service := Service{
		PollInterval:   10 * time.Millisecond,
		WorkSource:     source,
		Executor:       executor,
		WorkSink:       sink,
		Workers:        2,
		BatchClaimSize: 8,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errc := make(chan error, 1)
	go func() {
		errc <- service.runMainLoop(ctx)
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-executor.started:
		case <-ctx.Done():
			t.Fatalf("timed out waiting for worker %d to start", i)
		}
	}

	if got, want := source.claimedCount(), 2; got != want {
		t.Fatalf("claimed count while both workers are busy = %d, want %d", got, want)
	}
	for _, limit := range source.claimLimits() {
		if limit > 2 {
			t.Fatalf("ClaimBatch limit = %d, want at most ready worker count 2", limit)
		}
	}

	close(release)
	if err := <-errc; err != nil {
		t.Fatalf("runMainLoop() error = %v, want nil", err)
	}
	if got := executor.count.Load(); got != 5 {
		t.Fatalf("executor count = %d, want 5", got)
	}
}

type blockingManyReducerExecutor struct {
	count   atomic.Int64
	release <-chan struct{}
	started chan string
}

func (b *blockingManyReducerExecutor) Execute(ctx context.Context, intent Intent) (Result, error) {
	b.count.Add(1)
	select {
	case b.started <- intent.IntentID:
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	select {
	case <-b.release:
		return Result{
			IntentID: intent.IntentID,
			Domain:   intent.Domain,
			Status:   ResultStatusSucceeded,
		}, nil
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

// nonBatchWorkSource only implements WorkSource, not BatchWorkSource.
type nonBatchWorkSource struct {
	mu      sync.Mutex
	intents []Intent
}

func (f *nonBatchWorkSource) Claim(_ context.Context) (Intent, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.intents) == 0 {
		return Intent{}, false, nil
	}
	intent := f.intents[0]
	f.intents = f.intents[1:]
	return intent, true, nil
}

// nonBatchWorkSink only implements WorkSink, not BatchWorkSink.
type nonBatchWorkSink struct {
	mu    sync.Mutex
	acked int
}

func (f *nonBatchWorkSink) Ack(_ context.Context, _ Intent, _ Result) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acked++
	return nil
}

func (f *nonBatchWorkSink) Fail(_ context.Context, _ Intent, _ error) error {
	return nil
}
