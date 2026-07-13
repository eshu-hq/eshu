// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestExecuteGroupedChunksConcurrentlyObservedIsolatesPerChunkTimeout proves
// executeGroupedChunksConcurrentlyObserved does not cancel a sibling chunk's
// in-flight, otherwise-healthy ExecuteGroup call when a different chunk fails
// (#4464 Bug 1). Before the fix, the dispatch-control context returned by
// context.WithCancel was reused as the execution context passed to
// ge.ExecuteGroup, so cancel() (called on the first worker error) tore down
// every other worker's in-flight write too.
func TestExecuteGroupedChunksConcurrentlyObservedIsolatesPerChunkTimeout(t *testing.T) {
	t.Parallel()

	failCypher := "MERGE (u:HelmTemplateValueUsage) RETURN u"
	timeoutErr := sourcecypher.GraphWriteTimeoutError{
		Operation: "neo4j execute group timed out",
		Timeout:   30 * time.Second,
		Cause:     context.DeadlineExceeded,
	}

	// The failing chunk waits on failGate before returning its error. This
	// gives the (unbuffered-channel) dispatch loop time to hand every healthy
	// chunk to a worker before cancelDispatch() fires — without the gate, the
	// failing chunk (dispatched first, since it is stmts[0]) can fail and
	// cancel dispatch before the dispatcher's next `jobs <-` send for a
	// healthy chunk completes, which would make "the sibling chunk was never
	// even dispatched" indistinguishable from "the sibling chunk was
	// dispatched and then wrongly canceled" — the exact ambiguity this test
	// must not have.
	inner := &timeoutThenBlockGroupExecutor{
		failCypher: failCypher,
		failErr:    timeoutErr,
		release:    make(chan struct{}),
		failGate:   make(chan struct{}),
	}
	executor := nornicDBPhaseGroupExecutor{Inner: inner}

	const healthyChunks = 7
	stmts := make([]sourcecypher.Statement, 0, healthyChunks+1)
	stmts = append(stmts, sourcecypher.Statement{Cypher: failCypher})
	for i := 0; i < healthyChunks; i++ {
		stmts = append(stmts, sourcecypher.Statement{Cypher: "MERGE (f:Function) RETURN f"})
	}

	done := make(chan error, 1)
	go func() {
		done <- executor.ExecuteGroupedChunksConcurrentlyObserved(
			context.Background(), inner, stmts, 1, healthyChunks+1, nil,
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

	// All healthy chunks are now blocked inside ExecuteGroup. Release the
	// failing chunk so it returns its error and triggers cancelDispatch(),
	// then release the healthy chunks so they can observe (or not) the
	// cancellation.
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
