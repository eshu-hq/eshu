// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// blockingIngesterDrainExecutor is a raw executor whose drain RunWrite blocks
// until its context is canceled — standing in for a NornicDB drain iteration
// whose Bolt response is lost, so the only thing that can unblock it is a
// client-side deadline.
type blockingIngesterDrainExecutor struct{}

func (blockingIngesterDrainExecutor) Execute(context.Context, sourcecypher.Statement) error {
	return nil
}

func (blockingIngesterDrainExecutor) RunWrite(
	ctx context.Context,
	_ string,
	_ map[string]any,
) (DrainWriteResult, error) {
	<-ctx.Done()
	return DrainWriteResult{}, ctx.Err()
}

func newTestNornicDBCanonicalExecutorWithTimeout(
	raw sourcecypher.Executor,
	timeout time.Duration,
) sourcecypher.Executor {
	return canonicalExecutorForGraphBackend(
		raw,
		runtimecfg.GraphBackendNornicDB,
		timeout,
		false,
		defaultNornicDBPhaseGroupStatements,
		defaultNornicDBFilePhaseStatements,
		defaultNornicDBEntityPhaseStatements,
		nil,
		4,
		defaultNornicDBCanonicalRetractBatchSize,
		nil,
		nil,
		nil,
	)
}

// TestIngesterNornicDBDrainUsesPerIterationClientTimeout is the #5198 regression:
// each raw drain iteration must get its own client deadline so a lost Bolt
// response cannot hold the iteration open past the per-iteration budget. Before
// the fix the ingester wired the raw reader with no per-iteration deadline, so a
// blocked drain iteration outlived the budget and returned only when the outer
// context expired.
func TestIngesterNornicDBDrainUsesPerIterationClientTimeout(t *testing.T) {
	t.Parallel()

	executor := newTestNornicDBCanonicalExecutorWithTimeout(blockingIngesterDrainExecutor{}, 10*time.Millisecond)
	phase, ok := executor.(nornicDBPhaseGroupExecutor)
	if !ok {
		t.Fatalf("executor type = %T, want nornicDBPhaseGroupExecutor", executor)
	}
	if phase.DrainReader == nil {
		t.Fatal("NornicDB phase executor has no drain reader")
	}

	outerCtx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := phase.DrainReader.RunWrite(outerCtx, "RETURN 0 AS __drained", nil)
	if err == nil || !strings.Contains(err.Error(), "drain timed out after 10ms") {
		t.Fatalf("RunWrite() error = %v, want per-iteration client-timeout error", err)
	}
	var timeoutErr sourcecypher.GraphWriteTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("RunWrite() error = %T, want GraphWriteTimeoutError", err)
	}
	if got, want := timeoutErr.FailureClass(), sourcecypher.GraphWriteTimeoutFailureClass; got != want {
		t.Fatalf("FailureClass() = %q, want %q", got, want)
	}
	if !projector.IsRetryable(err) {
		t.Fatalf("projector.IsRetryable(%v) = false, want queue retry", err)
	}
	if elapsed := time.Since(started); elapsed >= 80*time.Millisecond {
		t.Fatalf("RunWrite() elapsed = %s, want client timeout well before the outer deadline", elapsed)
	}
}

// fakeDrainReader returns a fixed result/error and records the context it was
// called with, so the wrapper's branch behavior can be asserted directly.
type fakeDrainReader struct {
	result   DrainWriteResult
	err      error
	gotCtx   context.Context
	hadDeadl bool
}

func (r *fakeDrainReader) RunWrite(ctx context.Context, _ string, _ map[string]any) (DrainWriteResult, error) {
	r.gotCtx = ctx
	_, r.hadDeadl = ctx.Deadline()
	return r.result, r.err
}

// TestIngesterTimeoutDrainReaderPassthroughWhenTimeoutUnset proves a non-positive
// timeout is a passthrough: no child deadline is imposed and the inner result is
// returned verbatim (mirrors TimeoutExecutor).
func TestIngesterTimeoutDrainReaderPassthroughWhenTimeoutUnset(t *testing.T) {
	t.Parallel()

	inner := &fakeDrainReader{result: DrainWriteResult{NodesDeleted: 7}}
	reader := ingesterTimeoutDrainReader{inner: inner, timeout: 0, timeoutHint: canonicalWriteTimeoutEnv}
	got, err := reader.RunWrite(context.Background(), "RETURN 0 AS __drained", nil)
	if err != nil {
		t.Fatalf("RunWrite() error = %v, want nil", err)
	}
	if got.NodesDeleted != 7 {
		t.Fatalf("NodesDeleted = %d, want 7 (result not passed through)", got.NodesDeleted)
	}
	if inner.hadDeadl {
		t.Fatal("passthrough imposed a deadline on the inner reader, want none")
	}
}

// TestIngesterTimeoutDrainReaderWrapsNonTimeoutError proves a non-deadline error
// from the inner reader is wrapped (not silently reshaped into a timeout) and
// remains unwrappable to the original cause.
func TestIngesterTimeoutDrainReaderWrapsNonTimeoutError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("bolt boom")
	inner := &fakeDrainReader{err: sentinel}
	reader := ingesterTimeoutDrainReader{inner: inner, timeout: time.Second, timeoutHint: canonicalWriteTimeoutEnv}
	_, err := reader.RunWrite(context.Background(), "RETURN 0 AS __drained", nil)
	if err == nil || !strings.Contains(err.Error(), "run nornicdb drain") {
		t.Fatalf("RunWrite() error = %v, want wrapped non-timeout error", err)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("wrapped error does not unwrap to the cause: %v", err)
	}
	var timeoutErr sourcecypher.GraphWriteTimeoutError
	if errors.As(err, &timeoutErr) {
		t.Fatal("non-deadline error misclassified as GraphWriteTimeoutError")
	}
}

// TestIngesterTimeoutDrainReaderForwardsParentCancellation proves a
// parent-driven cancellation is forwarded unchanged, never reshaped into a
// retryable per-iteration timeout (the parent deadline is authoritative).
func TestIngesterTimeoutDrainReaderForwardsParentCancellation(t *testing.T) {
	t.Parallel()

	inner := &fakeDrainReader{err: context.Canceled}
	reader := ingesterTimeoutDrainReader{inner: inner, timeout: time.Hour, timeoutHint: canonicalWriteTimeoutEnv}
	parentCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := reader.RunWrite(parentCtx, "RETURN 0 AS __drained", nil)
	var timeoutErr sourcecypher.GraphWriteTimeoutError
	if errors.As(err, &timeoutErr) {
		t.Fatalf("parent cancellation misclassified as GraphWriteTimeoutError: %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunWrite() error = %v, want context.Canceled forwarded", err)
	}
}

// slowProgressDrainReader returns a decreasing sequence of __drained counts,
// sleeping a fixed duration on each call to model a drain iteration that makes
// steady progress but is individually slower than a single iteration budget.
type slowProgressDrainReader struct {
	counts  []int64
	perCall time.Duration
	callIdx int
}

func (r *slowProgressDrainReader) Execute(context.Context, sourcecypher.Statement) error {
	return nil
}

func (r *slowProgressDrainReader) RunWrite(
	ctx context.Context,
	_ string,
	_ map[string]any,
) (DrainWriteResult, error) {
	select {
	case <-ctx.Done():
		return DrainWriteResult{}, ctx.Err()
	case <-time.After(r.perCall):
	}
	idx := r.callIdx
	r.callIdx++
	var drained int64
	if idx < len(r.counts) {
		drained = r.counts[idx]
	}
	return DrainWriteResult{Rows: []map[string]any{{"__drained": drained}}}, nil
}

// TestIngesterNornicDBMultiIterationDrainNotCanceledByEarlierIteration proves the
// #5198 non-goal is honored: the per-iteration deadline must NOT be a phase-wide
// deadline. A drain that keeps making progress across several iterations — whose
// cumulative time far exceeds one iteration budget, while each single iteration
// stays within it — must complete, not be canceled partway.
func TestIngesterNornicDBMultiIterationDrainNotCanceledByEarlierIteration(t *testing.T) {
	t.Parallel()

	reader := &slowProgressDrainReader{
		counts:  []int64{2000, 2000, 2000, 500}, // then 0 terminates the loop
		perCall: 15 * time.Millisecond,
	}
	// Per-iteration budget (40ms) comfortably exceeds each 15ms iteration, but the
	// whole drain (>=75ms across 5 calls) far exceeds it. A phase-wide deadline
	// would cancel after the first iteration; a per-iteration reset must not.
	executor := newTestNornicDBCanonicalExecutorWithTimeout(reader, 40*time.Millisecond)
	phase, ok := executor.(nornicDBPhaseGroupExecutor)
	if !ok {
		t.Fatalf("executor type = %T, want nornicDBPhaseGroupExecutor", executor)
	}

	stmts := []sourcecypher.Statement{{
		Operation: sourcecypher.OperationCanonicalRetract,
		Cypher: `MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
WHERE f.repo_id = $repo_id AND f.generation_id <> $generation_id
DETACH DELETE f`,
		Parameters: map[string]any{"repo_id": "repo-1", "generation_id": "gen-2"},
		Drain:      true,
		DrainVar:   "f",
	}}

	if err := phase.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil (progressing drain must not be canceled)", err)
	}
	if reader.callIdx != 5 {
		t.Fatalf("drain iterations = %d, want 5 (2000, 2000, 2000, 500, 0)", reader.callIdx)
	}
}
