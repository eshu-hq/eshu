// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// timeoutThenBlockGroupExecutor reproduces the #4464 Bug 1 cascade at the
// intra-materialization entity-phase fan-out level: one chunk (identified by
// its first statement's Cypher text) fails immediately with a
// GraphWriteTimeoutError-shaped error, matching a real canonical write that
// exceeded ESHU_CANONICAL_WRITE_TIMEOUT. A second, unrelated chunk is healthy
// but slower — it blocks on a release gate until the test signals it, then
// returns success. In production this models two sibling chunks in the same
// canonical entity-phase batch: one MERGE that is genuinely slow enough to
// time out, and one that is merely a little slower than its siblings but
// would otherwise commit fine.
//
// Before the fix, both chunks shared one cancelable pool context
// (wiring_nornicdb_phase_group_streaming.go's poolCtx / raiseErr): the
// failing chunk's error triggered poolCancel(), which canceled the healthy
// chunk's in-flight ExecuteGroup(poolCtx, ...) call too, turning one slow
// write into a whole-batch failure. This is the reproduction referenced in
// issue #4464: "one write exceeding the per-write timeout cancels a context
// shared by all concurrent workers".
type timeoutThenBlockGroupExecutor struct {
	failCypher string
	failErr    error

	release chan struct{}
	// failGate, when non-nil, is waited on before the failing chunk returns
	// failErr. Tests with several healthy sibling chunks use this to let the
	// dispatcher hand every chunk to a worker before the failure fires and
	// triggers cancelDispatch(), so "never dispatched" and "wrongly canceled
	// after dispatch" cannot be confused with each other.
	failGate     chan struct{}
	blockedCalls atomic.Int64
	blockedErr   atomic.Value // error
}

func (t *timeoutThenBlockGroupExecutor) Execute(context.Context, sourcecypher.Statement) error {
	return nil
}

func (t *timeoutThenBlockGroupExecutor) ExecuteGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	if len(stmts) > 0 && stmts[0].Cypher == t.failCypher {
		if t.failGate != nil {
			<-t.failGate
		}
		return t.failErr
	}
	t.blockedCalls.Add(1)
	select {
	case <-t.release:
		return nil
	case <-ctx.Done():
		t.blockedErr.Store(ctx.Err())
		return ctx.Err()
	}
}

// TestExecuteEntityPhaseGroupStreamingIsolatesPerChunkTimeout proves a single
// chunk's write-timeout failure does not cancel a sibling chunk's in-flight,
// otherwise-healthy ExecuteGroup call. This is the intra-materialization
// analogue of the #4474 stage-level fix (bootstrap_projector.go): that PR
// isolated per-work-item failures across the 8 projector-stage workers, but
// left this entity-phase chunk fan-out (used by both bootstrap-index and the
// continuous ingester) sharing one poolCtx that a single chunk's timeout
// still tears down for every sibling.
func TestExecuteEntityPhaseGroupStreamingIsolatesPerChunkTimeout(t *testing.T) {
	t.Parallel()

	failCypher := "MERGE (u:HelmTemplateValueUsage) RETURN u"
	timeoutErr := sourcecypher.GraphWriteTimeoutError{
		Operation: "neo4j execute group timed out",
		Timeout:   30 * time.Second,
		Cause:     context.DeadlineExceeded,
	}

	inner := &timeoutThenBlockGroupExecutor{
		failCypher: failCypher,
		failErr:    timeoutErr,
		release:    make(chan struct{}),
		failGate:   make(chan struct{}),
	}
	const healthyChunks = 7
	executor := nornicDBPhaseGroupExecutor{
		inner:                  inner,
		maxStatements:          5,
		entityMaxStatements:    1,
		entityPhaseConcurrency: healthyChunks + 1,
	}

	stmts := make([]sourcecypher.Statement, 0, healthyChunks+1)
	stmts = append(stmts, sourcecypher.Statement{Cypher: failCypher, Parameters: entityFunctionParams()})
	for i := 0; i < healthyChunks; i++ {
		stmts = append(stmts, sourcecypher.Statement{Cypher: "MERGE (f:Function) RETURN f", Parameters: entityFunctionParams()})
	}

	done := make(chan error, 1)
	go func() {
		done <- executor.ExecutePhaseGroup(context.Background(), stmts)
	}()

	// Give every healthy chunk time to enter ExecuteGroup and start blocking
	// on the release gate before the failing chunk (held on failGate) is
	// allowed to return its error. If the pool context is canceled by the
	// sibling's timeout error, a healthy chunk observes ctx.Done() here
	// instead of reaching the release gate.
	deadline := time.Now().Add(5 * time.Second)
	for inner.blockedCalls.Load() < int64(healthyChunks) {
		if time.Now().After(deadline) {
			close(inner.failGate)
			close(inner.release)
			<-done
			t.Fatalf(
				"only %d/%d healthy sibling chunks entered ExecuteGroup before the deadline",
				inner.blockedCalls.Load(), healthyChunks,
			)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Release the failing chunk so it returns its error and triggers
	// poolCancel(), then give the (buggy) shared-cancel path a moment to
	// propagate before releasing the healthy chunks, so a regression
	// reliably observes ctx.Done() rather than winning a race against the
	// release channel.
	close(inner.failGate)
	time.Sleep(50 * time.Millisecond)
	close(inner.release)

	<-done // drain the phase-group result; assertions below are on the sibling chunks

	if stored := inner.blockedErr.Load(); stored != nil {
		err, _ := stored.(error)
		t.Fatalf(
			"healthy sibling chunk observed ctx error = %v, want nil — a sibling chunk's timeout must not cancel this chunk's in-flight ExecuteGroup call (#4464 Bug 1)",
			err,
		)
	}
	if !errors.Is(timeoutErr, timeoutErr) {
		t.Fatal("sanity: timeoutErr must be comparable via errors.Is")
	}
}
