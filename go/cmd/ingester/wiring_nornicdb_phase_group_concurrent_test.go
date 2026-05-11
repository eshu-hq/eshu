package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestNornicDBDefaultEntityPhaseConcurrencyTracksNumCPU pins the default
// worker count for canonical entity-phase dispatch to NumCPU clamped only
// at the env-override cap. The prior auto-cap of 4 left multi-core hosts
// with idle workers during canonical_write; lifting it to the env cap
// matches NornicDB's measured sub-linear scaling on the K8s dogfood lane.
func TestNornicDBDefaultEntityPhaseConcurrencyTracksNumCPU(t *testing.T) {
	t.Parallel()

	got := nornicDBDefaultEntityPhaseConcurrency()
	want := runtime.NumCPU()
	if want > nornicDBEntityPhaseConcurrencyCap {
		want = nornicDBEntityPhaseConcurrencyCap
	}
	if want < 1 {
		want = 1
	}
	if got != want {
		t.Fatalf("default entity phase concurrency = %d, want %d (NumCPU=%d, cap=%d)",
			got, want, runtime.NumCPU(), nornicDBEntityPhaseConcurrencyCap)
	}
}

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

// chunksSeenGroupExecutor blocks the first chunk that enters ExecuteGroup
// until `blockUntilSeen` chunks have entered. Used to assert the entity-phase
// executor streams chunks across what would be flush boundaries under the
// pre-streaming sync-barrier design — every chunk has to enter ExecuteGroup
// before the blocker unblocks, which only happens if the producer keeps
// pushing chunks while an earlier chunk is still in flight.
type chunksSeenGroupExecutor struct {
	count          int64
	blockUntilSeen int64
	deadline       time.Duration
}

func (c *chunksSeenGroupExecutor) Execute(_ context.Context, _ sourcecypher.Statement) error {
	return nil
}

func (c *chunksSeenGroupExecutor) ExecuteGroup(ctx context.Context, _ []sourcecypher.Statement) error {
	idx := atomic.AddInt64(&c.count, 1)
	if idx != 1 {
		return nil
	}
	deadline := time.Now().Add(c.deadline)
	for atomic.LoadInt64(&c.count) < c.blockUntilSeen {
		if time.Now().After(deadline) {
			return fmt.Errorf(
				"only %d chunks entered ExecuteGroup before the first chunk's deadline, want %d (producer is stalled inside an earlier flush instead of streaming chunks to a persistent worker pool)",
				atomic.LoadInt64(&c.count), c.blockUntilSeen,
			)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
	return nil
}

// TestNornicDBPhaseGroupExecutorEntityPhaseStreamsAcrossFlushBoundaries proves
// that the entity-phase executor pushes chunks to its worker pool as they are
// buffered, without per-flush sync barriers between batches. The K8s dogfood
// canonical_write evidence showed Variable label wall-clock at 208s for 673
// chunks × concurrency 8 — far above the 95s ideal floor. Per-flush
// `wg.Wait()` calls inside `flushGrouped` were leaving workers idle while the
// producer prepared the next batch, which is the structural waste this test
// pins down: a chunk arriving early enough must keep the pool saturated even
// when an earlier chunk is still running.
//
// Total chunks: 16 (twice the prior per-flush wave under
// `entityFlushTrigger = limit*concurrency = 1*4 = 4`). Under the legacy
// design, the first wave of 4 chunks runs, the blocker holds, and the
// producer cannot push waves 2–4 until the first wave drains; the blocker
// times out at 4 chunks and the test fails with a clear "stalled inside an
// earlier flush" message. The streaming design pushes all 16 chunks into a
// single long-lived channel; chunks 2–16 enter ExecuteGroup while the
// blocker is still running, the counter reaches 16, and the test passes.
func TestNornicDBPhaseGroupExecutorEntityPhaseStreamsAcrossFlushBoundaries(t *testing.T) {
	t.Parallel()

	const (
		workers    = 4
		totalStmts = 16
	)

	inner := &chunksSeenGroupExecutor{
		blockUntilSeen: totalStmts,
		deadline:       2 * time.Second,
	}
	executor := nornicDBPhaseGroupExecutor{
		inner:                  inner,
		maxStatements:          5,
		entityMaxStatements:    1,
		entityPhaseConcurrency: workers,
	}

	stmts := make([]sourcecypher.Statement, totalStmts)
	for i := range stmts {
		stmts[i] = sourcecypher.Statement{
			Cypher:     "RETURN x",
			Parameters: entityFunctionParams(),
		}
	}

	done := make(chan error, 1)
	go func() {
		done <- executor.ExecutePhaseGroup(context.Background(), stmts)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ExecutePhaseGroup() error = %v, want nil (the entity-phase executor did not stream chunks across what would be flush boundaries)", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("ExecutePhaseGroup did not complete within 5s — the executor is stuck inside the legacy per-flush sync barrier and never dispatched chunks 5-16 while the first chunk was still in flight")
	}
	if got := atomic.LoadInt64(&inner.count); got != int64(totalStmts) {
		t.Fatalf("ExecuteGroup call count = %d, want %d (some chunks were not dispatched)", got, totalStmts)
	}
}
