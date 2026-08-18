// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// probeCapableExecutor implements both Executor and ProbeExecutor for testing
// RetryingExecutor.ExecuteProbe forwarding (#5998).
type probeCapableExecutor struct {
	executeCalls atomic.Int32
	probeCalls   atomic.Int32
	probeStmt    Statement
	probeFound   bool
	probeErr     error
	failFor      int // fail ExecuteProbe this many times then succeed
}

func (p *probeCapableExecutor) Execute(_ context.Context, _ Statement) error {
	p.executeCalls.Add(1)
	return nil
}

func (p *probeCapableExecutor) ExecuteProbe(_ context.Context, stmt Statement) (bool, error) {
	n := int(p.probeCalls.Add(1))
	p.probeStmt = stmt
	if n <= p.failFor {
		return false, p.probeErr
	}
	return p.probeFound, nil
}

// TestRetryingExecutorForwardsExecuteProbe proves ExecuteProbe delegates to
// Inner.ExecuteProbe (not Execute) and returns its found result unchanged when
// Inner implements ProbeExecutor.
func TestRetryingExecutorForwardsExecuteProbe(t *testing.T) {
	t.Parallel()

	inner := &probeCapableExecutor{probeFound: true}
	r := &RetryingExecutor{Inner: inner, MaxRetries: 3, BaseDelay: time.Millisecond}

	stmt := Statement{Operation: OperationCanonicalRetract, Cypher: "MATCH (r) RETURN r LIMIT 1"}
	found, err := r.ExecuteProbe(context.Background(), stmt)
	if err != nil {
		t.Fatalf("ExecuteProbe() error = %v", err)
	}
	if !found {
		t.Fatal("ExecuteProbe() found = false, want true")
	}
	if got := int(inner.probeCalls.Load()); got != 1 {
		t.Errorf("probeCalls = %d, want 1", got)
	}
	if got := int(inner.executeCalls.Load()); got != 0 {
		t.Errorf("executeCalls = %d, want 0 (must not fall back to Execute)", got)
	}
	if inner.probeStmt.Cypher != stmt.Cypher {
		t.Errorf("forwarded statement cypher = %q, want %q", inner.probeStmt.Cypher, stmt.Cypher)
	}
}

// TestRetryingExecutorExecuteProbeErrorsWithoutProbeExecutor proves
// ExecuteProbe fails closed with a clear error, never a silent "not found",
// when Inner does not implement ProbeExecutor.
func TestRetryingExecutorExecuteProbeErrorsWithoutProbeExecutor(t *testing.T) {
	t.Parallel()

	inner := &failingExecutor{failFor: 0} // only implements Executor, not ProbeExecutor
	r := &RetryingExecutor{Inner: inner, MaxRetries: 3, BaseDelay: time.Millisecond}

	found, err := r.ExecuteProbe(context.Background(), Statement{Cypher: "RETURN 1"})
	if err == nil {
		t.Fatal("expected error when Inner does not implement ProbeExecutor")
	}
	if found {
		t.Fatal("found = true on an unsupported probe, want false")
	}
}

// TestRetryingExecutorExecuteProbeDoesNotRetryTransientError proves a probe
// does NOT inherit the write retry budget (#5998 review). A transient error
// that Execute would retry is returned to the caller on the first attempt, so
// the guard degrades immediately to its fail-safe DELETE instead of holding
// the partition lease across four attempts of up to
// ESHU_CANONICAL_WRITE_TIMEOUT each. The classifier would call this error
// retryable -- the point of the test is that ExecuteProbe never asks it.
func TestRetryingExecutorExecuteProbeDoesNotRetryTransientError(t *testing.T) {
	t.Parallel()

	transient := errors.New("TransientError: deadlock")
	if classifyTransientNeo4jError(transient) == "" {
		t.Fatal("precondition: this error must be one Execute would retry, else the test proves nothing")
	}

	inner := &probeCapableExecutor{
		probeFound: true,
		probeErr:   transient,
		failFor:    2,
	}
	r := &RetryingExecutor{Inner: inner, MaxRetries: 3, BaseDelay: time.Millisecond}

	found, err := r.ExecuteProbe(context.Background(), Statement{Cypher: "RETURN 1"})
	if err == nil {
		t.Fatal("ExecuteProbe() error = nil, want the transient error returned without retrying")
	}
	if found {
		t.Fatal("ExecuteProbe() found = true, want false alongside the error")
	}
	if got := int(inner.probeCalls.Load()); got != 1 {
		t.Fatalf("probeCalls = %d, want 1 (no retry)", got)
	}
}
