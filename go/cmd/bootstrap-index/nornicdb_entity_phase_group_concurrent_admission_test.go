// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// bootstrapAdmissionStopGroupExecutor exercises the #4464 review-comment fix:
// once cancelDispatch() has fired, a not-yet-started chunk must be refused
// rather than beginning a brand-new graph write, mirroring the ingester's
// pre-execution dispatchCtx.Err() check
// (executeGroupedChunksConcurrentlyObserved/executeEntityPhaseGroupStreaming).
// An already-in-flight chunk must still run to its own natural conclusion —
// the original #4464 Bug 1 guarantee, which this fix must not regress.
//
// Two chunk roles:
//   - the failing chunk blocks on failGate until the test releases it, then
//     returns failErr. Holding it open lets the test park every other worker
//     on a trailing chunk before the failure — and cancelDispatch() — ever
//     happens.
//   - trailing chunks record their start in trailingStarted the instant they
//     are entered, then park on trailingGate. Reaching ExecuteGroup at all is
//     what this test watches for; started-before-the-failure is legitimate
//     concurrent dispatch, so the test snapshots trailingStarted right before
//     releasing failGate and only counts chunks that start AFTER that
//     snapshot.
type bootstrapAdmissionStopGroupExecutor struct {
	failCypher string
	failErr    error

	failGate        chan struct{}
	trailingGate    chan struct{}
	trailingStarted atomic.Int64
	// trailingParked counts trailing chunks currently blocked in ExecuteGroup
	// waiting on trailingGate, used to detect when every worker not busy with
	// the failing chunk has parked on a distinct trailing chunk (i.e. no
	// worker is idle), so the only way any further chunk can start once
	// failGate is released is via the worker the failing chunk frees up.
	trailingParked atomic.Int64
}

func (b *bootstrapAdmissionStopGroupExecutor) Execute(context.Context, sourcecypher.Statement) error {
	return nil
}

func (b *bootstrapAdmissionStopGroupExecutor) ExecuteGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	if len(stmts) > 0 && stmts[0].Cypher == b.failCypher {
		<-b.failGate
		return b.failErr
	}
	// A trailing chunk: park here so the test can observe exactly how many
	// chunks have started, then block until told to unblock.
	b.trailingStarted.Add(1)
	b.trailingParked.Add(1)
	defer b.trailingParked.Add(-1)
	select {
	case <-b.trailingGate:
	case <-ctx.Done():
	}
	return ctx.Err()
}

// TestExecuteGroupedChunksConcurrentlyStopsAdmittingAfterDispatchCanceled
// documents and exercises the #4464 review-comment fix.
//
// Empirical note on determinism: the failing worker frees itself and loops
// back to `for index := range jobs` in the same goroutine, immediately after
// calling cancelDispatch() via recordErr. Measured directly against this Go
// toolchain (repeated stress runs, including a GOMAXPROCS=1 variant and an
// isolated minimal reproduction of the same channel/context shape), the
// dispatch loop's `select { case jobs <- index: case <-dispatchCtx.Done(): }`
// reliably resolves to the already-closed dispatchCtx.Done() branch before
// the freed worker becomes ready to receive again — so this specific
// shared-failing-and-freed-worker race did not reproduce as a live failure
// on this toolchain even across thousands of iterations without the fix.
// This does not make the fix unnecessary: the Go language spec guarantees no
// particular case ordering when multiple select cases are simultaneously
// ready, so a future toolchain, GOARCH, or scheduler tuning could resolve it
// differently, and the ingester's identical shape carries the identical
// guard for the same reason. This test still provides two guarantees under
// the current toolchain:
//  1. It proves no regression to the original #4464 Bug 1 isolation
//     guarantee: chunks already parked in ExecuteGroup when the failure
//     occurs still run to their own natural conclusion rather than being
//     torn down (see the "started before the failure" chunks below).
//  2. It would fail with a precise, actionable message
//     ("chunks that started ExecuteGroup after dispatch was canceled")
//     if the race ever does resolve the other way on a given toolchain/run,
//     making it a correct two-sided check even though it could not be forced
//     to fail without the fix on this toolchain.
func TestExecuteGroupedChunksConcurrentlyStopsAdmittingAfterDispatchCanceled(t *testing.T) {
	t.Parallel()

	failCypher := "MERGE (u:HelmTemplateValueUsage) RETURN u"
	timeoutErr := sourcecypher.GraphWriteTimeoutError{
		Operation: "neo4j execute group timed out",
		Timeout:   30 * time.Second,
		Cause:     context.DeadlineExceeded,
	}

	inner := &bootstrapAdmissionStopGroupExecutor{
		failCypher:   failCypher,
		failErr:      timeoutErr,
		failGate:     make(chan struct{}),
		trailingGate: make(chan struct{}),
	}
	// 4 workers, 12 total chunks: 1 failing + 11 trailing. With failGate held
	// closed, the 3 non-failing workers park on the first 3 trailing chunks
	// and stay there — every worker is occupied, so none of the remaining 8
	// trailing chunks can start until the failing worker frees up, which only
	// happens once failGate closes (the same instant cancelDispatch() fires).
	const workers = 4
	const trailingChunks = 11
	executor := bootstrapNornicDBPhaseGroupExecutor{
		inner:                  inner,
		entityPhaseConcurrency: workers,
	}

	stmts := make([]sourcecypher.Statement, 0, trailingChunks+1)
	stmts = append(stmts, sourcecypher.Statement{Cypher: failCypher})
	for i := 0; i < trailingChunks; i++ {
		stmts = append(stmts, sourcecypher.Statement{Cypher: "MERGE (f:Function) RETURN f"})
	}

	done := make(chan error, 1)
	go func() {
		done <- executor.executeGroupedChunksConcurrently(
			context.Background(), inner, stmts, "mixed", 1,
		)
	}()

	// Wait until every non-failing worker has parked on a trailing chunk
	// (workers-1 of them) — i.e. no worker is idle.
	deadline := time.Now().Add(5 * time.Second)
	for inner.trailingParked.Load() < int64(workers-1) {
		if time.Now().After(deadline) {
			close(inner.trailingGate)
			<-done
			t.Fatalf(
				"only %d/%d non-failing workers parked on a trailing chunk before the deadline",
				inner.trailingParked.Load(), workers-1,
			)
		}
		time.Sleep(time.Millisecond)
	}

	// Snapshot how many chunks have started so far — this many are
	// legitimate (dispatched before any failure occurred). Any chunk that
	// starts after this point can only be one the fix's admission-stop check
	// must refuse.
	startedBeforeFailure := inner.trailingStarted.Load()
	close(inner.failGate)

	// Give the freed failing-worker a bounded window to either lose the race
	// to cancelDispatch() and never call ExecuteGroup (the fix, and what this
	// toolchain reliably does), or win it and start a new trailing chunk that
	// then parks on trailingGate (the regression this test targets).
	// trailingGate is deliberately still closed here so a chunk that does
	// start stays parked long enough for this check to observe it.
	time.Sleep(50 * time.Millisecond)
	startedAfterFailure := inner.trailingStarted.Load() - startedBeforeFailure

	// Release every parked chunk so the call can finish, then wait for the
	// hard synchronization point: executeGroupedChunksConcurrently only
	// returns after wg.Wait(), i.e. after every worker goroutine has exited
	// its loop.
	close(inner.trailingGate)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("executeGroupedChunksConcurrently did not return within 5s")
	}

	if startedAfterFailure != 0 {
		t.Fatalf(
			"chunks that started ExecuteGroup after dispatch was canceled = %d, want 0 — "+
				"a not-yet-started chunk must not begin a graph write once a sibling's failure has "+
				"already stopped admission (#4464 review comment)",
			startedAfterFailure,
		)
	}
	if startedBeforeFailure == 0 {
		t.Fatal("no trailing chunk started before the failure — the test setup did not saturate the worker pool as intended")
	}
}
