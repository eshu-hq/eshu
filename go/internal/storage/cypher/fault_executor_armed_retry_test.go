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

func (a *fakeExecutorRetryArmer) Arm(ctx context.Context) context.Context {
	a.armCount.Add(1)
	return ctx
}

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

// TestFaultingExecutorExecuteGroupExecutorRetryLaneArmsAndDelegates proves the
// group path reaches the below-RetryingExecutor armer introduced for #5086.
func TestFaultingExecutorExecuteGroupExecutorRetryLaneArmsAndDelegates(t *testing.T) {
	t.Parallel()

	inner := &faultRecordingExecutor{supportsGrp: true}
	script := onceThenSucceedScript(faultreplay.LaneExecutorRetry, intPtr(1), nil)
	fe := mustFaultingExecutor(t, inner, script, "")
	arm := &fakeExecutorRetryArmer{}
	fe.SetExecutorRetryArmer(arm)

	stmts := []Statement{{Cypher: "MERGE (a) RETURN a"}}
	if err := fe.ExecuteGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecuteGroup() error = %v, want nil after arming and delegation", err)
	}
	if got := arm.armCount.Load(); got != 1 {
		t.Fatalf("expected Arm() once for ExecuteGroup, got %d", got)
	}
	if got := inner.groupCount(); got != 1 {
		t.Fatalf("expected inner.ExecuteGroup once after arming, got %d", got)
	}
}
