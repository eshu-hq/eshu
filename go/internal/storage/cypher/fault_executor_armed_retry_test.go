// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifafaultinjection

package cypher

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replay/faultreplay"
)

// fakeExecutorRetryArmer is a minimal ExecutorRetryArmer test double that
// records how many times Arm() was called, so tests can assert
// FaultingExecutor calls it exactly once for the fired attempt.
type fakeExecutorRetryArmer struct {
	armCount atomic.Int64
}

func (a *fakeExecutorRetryArmer) Arm() { a.armCount.Add(1) }

// TestFaultingExecutorExecutorRetryLaneArmsBelowTheSeamAndDelegatesWhenWired
// proves the #5048 fix at the FaultingExecutor level in isolation: when an
// ExecutorRetryArmer is wired via SetExecutorRetryArmer, the fired
// executor-retry attempt calls Arm() exactly once and DELEGATES to inner
// (rather than returning the shaped error itself), so whatever retry loop
// lives inside inner gets a real chance to observe and retry the armed
// failure. The full below-the-seam retry-in-place proof against the real
// reducer wiring lives in go/cmd/reducer's
// TestWrapIfaFaultExecutorExecutorRetryLaneRetriesInPlaceBelowTheRetryingExecutor.
func TestFaultingExecutorExecutorRetryLaneArmsBelowTheSeamAndDelegatesWhenWired(t *testing.T) {
	t.Parallel()

	inner := &faultRecordingExecutor{}
	script := onceThenSucceedScript(faultreplay.LaneExecutorRetry, intPtr(1), nil)
	fe := mustFaultingExecutor(t, inner, script, "")
	arm := &fakeExecutorRetryArmer{}
	fe.SetExecutorRetryArmer(arm)

	if err := fe.Execute(context.Background(), Statement{Cypher: "MERGE (a) RETURN a"}); err != nil {
		t.Fatalf("expected Execute to delegate to inner and succeed when armed, got %v", err)
	}
	if got := arm.armCount.Load(); got != 1 {
		t.Fatalf("expected Arm() called exactly once, got %d", got)
	}
	if got := inner.executeCount(); got != 1 {
		t.Fatalf("expected inner.Execute called once (armed attempt delegates instead of short-circuiting), got %d", got)
	}
	if !fe.OnceThenSucceedFired() {
		t.Fatal("expected OnceThenSucceedFired() to report true")
	}

	// A second call must not re-arm or re-fire; the once-fault already
	// consumed itself via the CompareAndSwap gate.
	if err := fe.Execute(context.Background(), Statement{Cypher: "MERGE (b) RETURN b"}); err != nil {
		t.Fatalf("call 2: expected success, got %v", err)
	}
	if got := arm.armCount.Load(); got != 1 {
		t.Fatalf("expected Arm() still called exactly once after a second call, got %d", got)
	}
}

// TestFaultingExecutorExecuteGroupExecutorRetryLaneIgnoresArmerReturnsShapedError
// proves the arming scope boundary: even with an ExecutorRetryArmer wired,
// ExecuteGroup for the executor-retry lane still returns the shaped error
// directly rather than arming and delegating. go/cmd/reducer's grouped
// canonical writers bypass RetryingExecutor entirely, so arming here would
// silently swallow the scripted fault (Arm() would set a flag nothing ever
// checks, and the real group write would proceed as if nothing had fired).
// Group-write retry parity is tracked separately, out of #5048's scope.
func TestFaultingExecutorExecuteGroupExecutorRetryLaneIgnoresArmerReturnsShapedError(t *testing.T) {
	t.Parallel()

	inner := &faultRecordingExecutor{supportsGrp: true}
	script := onceThenSucceedScript(faultreplay.LaneExecutorRetry, intPtr(1), nil)
	fe := mustFaultingExecutor(t, inner, script, "")
	arm := &fakeExecutorRetryArmer{}
	fe.SetExecutorRetryArmer(arm)

	stmts := []Statement{{Cypher: "MERGE (a) RETURN a"}}
	err := fe.ExecuteGroup(context.Background(), stmts)
	if err == nil {
		t.Fatal("expected the scripted fault to fire on the first ExecuteGroup call")
	}
	if !isTransientNeo4jError(err) {
		t.Fatalf("executor-retry lane error must be shaped transient, got %q", err.Error())
	}
	if got := arm.armCount.Load(); got != 0 {
		t.Fatalf("expected Arm() never called for ExecuteGroup, got %d", got)
	}
	if got := inner.groupCount(); got != 0 {
		t.Fatalf("expected inner.ExecuteGroup never called for the fired attempt, got %d", got)
	}
}
