package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// blockingGroupExecutor records concurrent ExecuteGroup invocations and
// blocks them until the test releases the gate. Used to prove the
// nornicDBPhaseGroupExecutor entity phase fans grouped chunks out across
// the configured number of workers instead of dispatching them serially.
type blockingGroupExecutor struct {
	release      chan struct{}
	inFlight     int64
	maxInFlight  int64
	callCount    int64
	executeCalls int64
}

func (b *blockingGroupExecutor) Execute(_ context.Context, _ sourcecypher.Statement) error {
	atomic.AddInt64(&b.executeCalls, 1)
	return nil
}

func (b *blockingGroupExecutor) ExecuteGroup(ctx context.Context, _ []sourcecypher.Statement) error {
	atomic.AddInt64(&b.callCount, 1)
	cur := atomic.AddInt64(&b.inFlight, 1)
	defer atomic.AddInt64(&b.inFlight, -1)
	for {
		curMax := atomic.LoadInt64(&b.maxInFlight)
		if cur <= curMax {
			break
		}
		if atomic.CompareAndSwapInt64(&b.maxInFlight, curMax, cur) {
			break
		}
	}
	select {
	case <-b.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestNornicDBPhaseGroupExecutorDispatchesEntityChunksConcurrently(t *testing.T) {
	t.Parallel()

	const workers = 4
	inner := &blockingGroupExecutor{release: make(chan struct{})}
	executor := nornicDBPhaseGroupExecutor{
		inner:                  inner,
		maxStatements:          5,
		entityMaxStatements:    1,
		entityPhaseConcurrency: workers,
	}

	// Eight Function entity statements with entityMaxStatements=1 produces
	// eight chunks; a parallel dispatcher with four workers should reach
	// four in-flight ExecuteGroup calls before any of them release.
	stmts := make([]sourcecypher.Statement, 8)
	for i := range stmts {
		stmts[i] = sourcecypher.Statement{
			Cypher: "RETURN x",
			Parameters: map[string]any{
				sourcecypher.StatementMetadataPhaseKey:       sourcecypher.CanonicalPhaseEntities,
				sourcecypher.StatementMetadataEntityLabelKey: "Function",
			},
		}
	}

	done := make(chan error, 1)
	go func() {
		done <- executor.ExecutePhaseGroup(context.Background(), stmts)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt64(&inner.maxInFlight) < int64(workers) {
		if time.Now().After(deadline) {
			close(inner.release)
			<-done
			t.Fatalf(
				"max in-flight ExecuteGroup = %d, want >= %d (parallel dispatch did not happen)",
				atomic.LoadInt64(&inner.maxInFlight), workers,
			)
		}
		time.Sleep(10 * time.Millisecond)
	}

	close(inner.release)
	if err := <-done; err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	if got := atomic.LoadInt64(&inner.callCount); got != int64(len(stmts)) {
		t.Fatalf("ExecuteGroup call count = %d, want %d (chunks lost or duplicated)", got, len(stmts))
	}
}

func TestNornicDBPhaseGroupExecutorEntityPhaseFallsBackToSerialWhenConcurrencyUnset(t *testing.T) {
	t.Parallel()

	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:               inner,
		maxStatements:       5,
		entityMaxStatements: 1,
		// entityPhaseConcurrency unset (zero) keeps the existing serial
		// chunk loop so callers without an opt-in see no behavior change.
	}

	stmts := []sourcecypher.Statement{
		{Cypher: "RETURN 1", Parameters: entityFunctionParams()},
		{Cypher: "RETURN 2", Parameters: entityFunctionParams()},
		{Cypher: "RETURN 3", Parameters: entityFunctionParams()},
	}

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	if got, want := inner.groupSizes, []int{1, 1, 1}; !equalIntSlices(got, want) {
		t.Fatalf("group sizes = %v, want %v (serial path)", got, want)
	}
}

func TestNornicDBPhaseGroupExecutorEntityPhaseConcurrencyOneStaysSerial(t *testing.T) {
	t.Parallel()

	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:                  inner,
		maxStatements:          5,
		entityMaxStatements:    1,
		entityPhaseConcurrency: 1,
	}

	stmts := []sourcecypher.Statement{
		{Cypher: "RETURN 1", Parameters: entityFunctionParams()},
		{Cypher: "RETURN 2", Parameters: entityFunctionParams()},
	}

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	if got, want := inner.groupSizes, []int{1, 1}; !equalIntSlices(got, want) {
		t.Fatalf("group sizes = %v, want %v (serial path when workers=1)", got, want)
	}
}

func TestNornicDBPhaseGroupExecutorEntityPhaseConcurrencyPropagatesFirstError(t *testing.T) {
	t.Parallel()

	failErr := errors.New("simulated NornicDB write failure")
	inner := newFailingGroupChunkExecutor(2, failErr)
	executor := nornicDBPhaseGroupExecutor{
		inner:                  inner,
		maxStatements:          5,
		entityMaxStatements:    1,
		entityPhaseConcurrency: 4,
	}

	stmts := make([]sourcecypher.Statement, 8)
	for i := range stmts {
		stmts[i] = sourcecypher.Statement{
			Cypher:     "RETURN x",
			Parameters: entityFunctionParams(),
		}
	}

	err := executor.ExecutePhaseGroup(context.Background(), stmts)
	if err == nil {
		t.Fatal("ExecutePhaseGroup() error = nil, want non-nil")
	}
	if !errors.Is(err, failErr) {
		t.Fatalf("ExecutePhaseGroup() error = %v, want errors.Is failErr", err)
	}
}

// failingGroupChunkExecutor returns an error on the Nth ExecuteGroup call.
// Thread-safe: parallel workers may call ExecuteGroup concurrently.
type failingGroupChunkExecutor struct {
	mu       sync.Mutex
	count    int
	failAt   int
	err      error
}

func newFailingGroupChunkExecutor(failAt int, err error) *failingGroupChunkExecutor {
	return &failingGroupChunkExecutor{failAt: failAt, err: err}
}

func (f *failingGroupChunkExecutor) Execute(_ context.Context, _ sourcecypher.Statement) error {
	return nil
}

func (f *failingGroupChunkExecutor) ExecuteGroup(_ context.Context, _ []sourcecypher.Statement) error {
	f.mu.Lock()
	f.count++
	hit := f.count == f.failAt
	f.mu.Unlock()
	if hit {
		return f.err
	}
	return nil
}

func TestCanonicalExecutorForGraphBackendUsesConfiguredEntityPhaseConcurrency(t *testing.T) {
	t.Parallel()

	inner := &groupCapableIngesterExecutor{}
	executor := canonicalExecutorForGraphBackend(
		inner,
		runtimecfg.GraphBackendNornicDB,
		0,
		false,
		defaultNornicDBPhaseGroupStatements,
		defaultNornicDBFilePhaseStatements,
		defaultNornicDBEntityPhaseStatements,
		nil,
		6,
		nil,
		nil,
	)
	pge, ok := executor.(nornicDBPhaseGroupExecutor)
	if !ok {
		t.Fatalf("executor type = %T, want nornicDBPhaseGroupExecutor", executor)
	}
	if got, want := pge.entityPhaseConcurrency, 6; got != want {
		t.Fatalf("entity phase concurrency = %d, want %d", got, want)
	}
}

// entityFunctionParams returns the canonical-phase parameters that flag a
// statement as belonging to the Function entity phase, so the phase-group
// executor routes it through the entity-label path.
func entityFunctionParams() map[string]any {
	return map[string]any{
		sourcecypher.StatementMetadataPhaseKey:       sourcecypher.CanonicalPhaseEntities,
		sourcecypher.StatementMetadataEntityLabelKey: "Function",
	}
}
