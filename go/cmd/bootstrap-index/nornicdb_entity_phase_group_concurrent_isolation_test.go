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

// bootstrapTimeoutThenBlockGroupExecutor mirrors the ingester reproduction for
// issue #4464 Bug 1: one chunk (matched by its first statement's Cypher text)
// fails immediately, as a real canonical write does when it exceeds
// ESHU_CANONICAL_WRITE_TIMEOUT and TimeoutExecutor returns a
// GraphWriteTimeoutError. A sibling chunk is healthy but slower — it blocks
// on a release gate until the test signals it. Before the fix, both chunks'
// ExecuteGroup calls ran against the same runCtx that recordErr canceled on
// the first error, so the timeout chunk's failure tore down the healthy
// chunk's in-flight write too.
type bootstrapTimeoutThenBlockGroupExecutor struct {
	failCypher string
	failErr    error

	release chan struct{}
	// failGate, when non-nil, is waited on before the failing chunk returns
	// failErr. Tests with several healthy sibling chunks use this to let the
	// dispatcher hand every chunk to a worker before the failure fires and
	// triggers cancelDispatch(), so "never dispatched" (an unbuffered jobs
	// channel race inherent to the dispatch loop, unrelated to this fix) and
	// "wrongly canceled after dispatch" (the actual regression) cannot be
	// confused with each other.
	failGate     chan struct{}
	blockedCalls atomic.Int64
	blockedErr   atomic.Value // error
}

func (b *bootstrapTimeoutThenBlockGroupExecutor) Execute(context.Context, sourcecypher.Statement) error {
	return nil
}

func (b *bootstrapTimeoutThenBlockGroupExecutor) ExecuteGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	if len(stmts) > 0 && stmts[0].Cypher == b.failCypher {
		if b.failGate != nil {
			<-b.failGate
		}
		return b.failErr
	}
	b.blockedCalls.Add(1)
	select {
	case <-b.release:
		return nil
	case <-ctx.Done():
		b.blockedErr.Store(ctx.Err())
		return ctx.Err()
	}
}

// TestExecuteGroupedChunksConcurrentlyIsolatesPerChunkTimeout proves
// bootstrapNornicDBPhaseGroupExecutor.executeGroupedChunksConcurrently does
// not cancel a sibling chunk's in-flight, otherwise-healthy ExecuteGroup call
// when a different chunk fails with a graph-write timeout (#4464 Bug 1). This
// is the bootstrap-index analogue of the same fix applied to the ingester's
// executeGroupedChunksConcurrentlyObserved and
// executeEntityPhaseGroupStreaming: #4474 isolated per-work-item failures
// across the 8 projector-stage workers, but this intra-materialization
// entity-phase chunk fan-out still shared one cancelable runCtx that a single
// chunk's timeout tore down for every sibling.
func TestExecuteGroupedChunksConcurrentlyIsolatesPerChunkTimeout(t *testing.T) {
	t.Parallel()

	failCypher := "MERGE (u:HelmTemplateValueUsage) RETURN u"
	timeoutErr := sourcecypher.GraphWriteTimeoutError{
		Operation: "neo4j execute group timed out",
		Timeout:   30 * time.Second,
		Cause:     context.DeadlineExceeded,
	}

	inner := &bootstrapTimeoutThenBlockGroupExecutor{
		failCypher: failCypher,
		failErr:    timeoutErr,
		release:    make(chan struct{}),
		failGate:   make(chan struct{}),
	}
	const healthyChunks = 7
	executor := bootstrapNornicDBPhaseGroupExecutor{
		inner:                  inner,
		entityPhaseConcurrency: healthyChunks + 1,
	}

	stmts := make([]sourcecypher.Statement, 0, healthyChunks+1)
	stmts = append(stmts, sourcecypher.Statement{Cypher: failCypher})
	for i := 0; i < healthyChunks; i++ {
		stmts = append(stmts, sourcecypher.Statement{Cypher: "MERGE (f:Function) RETURN f"})
	}

	done := make(chan error, 1)
	go func() {
		done <- executor.executeGroupedChunksConcurrently(
			context.Background(), inner, stmts, "Function", 1,
		)
	}()

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

	close(inner.failGate)
	time.Sleep(50 * time.Millisecond)
	close(inner.release)
	<-done

	if stored := inner.blockedErr.Load(); stored != nil {
		err, _ := stored.(error)
		t.Fatalf(
			"healthy sibling chunk observed ctx error = %v, want nil — a sibling chunk's timeout must not cancel this chunk's in-flight ExecuteGroup call (#4464 Bug 1)",
			err,
		)
	}
}
