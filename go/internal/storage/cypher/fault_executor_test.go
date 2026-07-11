// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifafaultinjection

package cypher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/replay/faultreplay"
)

func intPtr(v int) *int       { return &v }
func strPtr(v string) *string { return &v }

// faultRecordingExecutor is a minimal Executor (+ optional GroupExecutor) fake
// that records every call it receives, for asserting how many times
// FaultingExecutor actually delegated.
type faultRecordingExecutor struct {
	mu          sync.Mutex
	executes    []Statement
	groups      [][]Statement
	supportsGrp bool
	failNext    error
}

func (e *faultRecordingExecutor) Execute(_ context.Context, stmt Statement) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.executes = append(e.executes, stmt)
	if e.failNext != nil {
		err := e.failNext
		e.failNext = nil
		return err
	}
	return nil
}

func (e *faultRecordingExecutor) ExecuteGroup(_ context.Context, stmts []Statement) error {
	if !e.supportsGrp {
		return errors.New("faultRecordingExecutor: group support disabled for this test")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.groups = append(e.groups, stmts)
	return nil
}

func (e *faultRecordingExecutor) executeCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.executes)
}

func (e *faultRecordingExecutor) groupCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.groups)
}

// faultExecuteOnlyRecordingExecutor exposes only Execute (no ExecuteGroup),
// so tests can prove FaultingExecutor.ExecuteGroup reports
// errFaultingExecutorInnerNoGroup when inner does not support grouped
// writes. It deliberately does NOT embed *faultRecordingExecutor: Go
// promotes an embedded pointer's methods, which would silently satisfy
// GroupExecutor via promotion and defeat the point of this fake.
type faultExecuteOnlyRecordingExecutor struct {
	inner *faultRecordingExecutor
}

func newFaultExecuteOnlyRecordingExecutor() *faultExecuteOnlyRecordingExecutor {
	return &faultExecuteOnlyRecordingExecutor{inner: &faultRecordingExecutor{}}
}

func (e *faultExecuteOnlyRecordingExecutor) Execute(ctx context.Context, stmt Statement) error {
	return e.inner.Execute(ctx, stmt)
}

// mustFaultingExecutor constructs a FaultingExecutor and type-asserts the
// concrete type back out of NewFaultingExecutor's Executor return, so tests
// can reach the inspector methods (OnceThenSucceedFired, RestartFired) and
// the GroupExecutor/PhaseGroupExecutor surface directly.
func mustFaultingExecutor(t *testing.T, inner Executor, script faultreplay.Script, sentinelPath string) *FaultingExecutor {
	t.Helper()
	got, err := NewFaultingExecutor(inner, script, sentinelPath)
	if err != nil {
		t.Fatalf("NewFaultingExecutor: %v", err)
	}
	fe, ok := got.(*FaultingExecutor)
	if !ok {
		t.Fatalf("expected *FaultingExecutor, got %T", got)
	}
	return fe
}

func onceThenSucceedScript(lane string, ordinal *int, match *string) faultreplay.Script {
	return faultreplay.Script{
		Version: faultreplay.CurrentVersion,
		Faults: []faultreplay.FaultOp{
			{
				Kind: faultreplay.KindFailGraphWriteOnceThenSucceed,
				Trigger: faultreplay.Trigger{
					StatementOrdinal: ordinal,
					OperationMatch:   match,
				},
				Target: faultreplay.Target{Lane: lane},
			},
		},
	}
}

// TestFaultingExecutorQueueRetryLaneFiresOnceThenDelegates proves the
// queue-retry lane fails the scripted ordinal exactly once, without ever
// calling inner for that attempt, and delegates on every other call.
func TestFaultingExecutorQueueRetryLaneFiresOnceThenDelegates(t *testing.T) {
	t.Parallel()

	inner := &faultRecordingExecutor{}
	script := onceThenSucceedScript(faultreplay.LaneQueueRetry, intPtr(2), nil)
	fe := mustFaultingExecutor(t, inner, script, "")

	ctx := context.Background()
	if err := fe.Execute(ctx, Statement{Cypher: "MERGE (a) RETURN a"}); err != nil {
		t.Fatalf("call 1: expected success, got %v", err)
	}
	err := fe.Execute(ctx, Statement{Cypher: "MERGE (b) RETURN b"})
	if err == nil {
		t.Fatal("call 2: expected the scripted fault to fire, got nil error")
	}
	if isTransientNeo4jError(err) {
		t.Fatalf("queue-retry lane error must NOT classify as transient (isTransientNeo4jError), got %q", err.Error())
	}
	if err := fe.Execute(ctx, Statement{Cypher: "MERGE (c) RETURN c"}); err != nil {
		t.Fatalf("call 3: expected success (fault fires only once), got %v", err)
	}

	if got := inner.executeCount(); got != 2 {
		t.Fatalf("expected inner.Execute called twice (calls 1 and 3; call 2 short-circuited), got %d", got)
	}
	if !fe.OnceThenSucceedFired() {
		t.Fatal("expected OnceThenSucceedFired() to report true after the scripted call")
	}
}

// TestFaultingExecutorExecutorRetryLaneWithoutArmerFallsBackAboveTheSeam
// proves the pre-#5048 fallback still applies whenever no
// ExecutorRetryArmer is wired (SetExecutorRetryArmer never called, the zero
// value): this decorator wraps ABOVE any RetryingExecutor sitting below it,
// so even though the executor-retry lane's error is shaped to satisfy
// isTransientNeo4jError, wrapping a RetryingExecutor as inner here proves
// that RetryingExecutor never sees or retries it -- the fault
// short-circuits before inner.Execute is ever called. This is the honest
// remaining limitation for callers that never wire an armer; go/cmd/reducer's
// wrapIfaFaultExecutor does wire one for the real reducer binary. See
// fault_executor_armed_retry_test.go for the armed-path proofs and
// go/cmd/reducer's
// TestWrapIfaFaultExecutorExecutorRetryLaneRetriesInPlaceBelowTheRetryingExecutor
// for the full end-to-end proof.
func TestFaultingExecutorExecutorRetryLaneWithoutArmerFallsBackAboveTheSeam(t *testing.T) {
	t.Parallel()

	base := &faultRecordingExecutor{}
	retrying := &RetryingExecutor{Inner: base, MaxRetries: 3, BaseDelay: time.Millisecond}
	script := onceThenSucceedScript(faultreplay.LaneExecutorRetry, intPtr(1), nil)
	fe := mustFaultingExecutor(t, retrying, script, "")

	err := fe.Execute(context.Background(), Statement{Cypher: "MERGE (a) RETURN a"})
	if err == nil {
		t.Fatal("expected the scripted fault to fire on the first call")
	}
	if !isTransientNeo4jError(err) {
		t.Fatalf("executor-retry lane error must be shaped transient (isTransientNeo4jError), got %q", err.Error())
	}
	if got := base.executeCount(); got != 0 {
		t.Fatalf("RetryingExecutor's inner base executor must NEVER be called for the fired attempt "+
			"(proves this decorator sits above the retry, not below it); got %d calls", got)
	}
	if !fe.OnceThenSucceedFired() {
		t.Fatal("expected OnceThenSucceedFired() to report true")
	}
}

// TestFaultingExecutorMatchesByOperationSubstring proves the OperationMatch
// trigger fires on the statement whose Cypher text contains the substring,
// regardless of ordinal position.
func TestFaultingExecutorMatchesByOperationSubstring(t *testing.T) {
	t.Parallel()

	inner := &faultRecordingExecutor{}
	script := onceThenSucceedScript(faultreplay.LaneQueueRetry, nil, strPtr("CloudResource"))
	fe := mustFaultingExecutor(t, inner, script, "")

	ctx := context.Background()
	if err := fe.Execute(ctx, Statement{Cypher: "MERGE (f:File) RETURN f"}); err != nil {
		t.Fatalf("non-matching statement: expected success, got %v", err)
	}
	if err := fe.Execute(ctx, Statement{Cypher: "MERGE (r:CloudResource) RETURN r"}); err == nil {
		t.Fatal("matching statement: expected the scripted fault to fire")
	}
	if err := fe.Execute(ctx, Statement{Cypher: "MERGE (r:CloudResource) RETURN r"}); err != nil {
		t.Fatalf("second matching statement: expected success (fault fires only once), got %v", err)
	}
}

// TestFaultingExecutorExecuteGroupFiresOnceThenDelegates proves the fault
// applies at the ExecuteGroup seam too (the atomic path go/cmd/reducer's
// canonical writers actually take), not only single-statement Execute.
func TestFaultingExecutorExecuteGroupFiresOnceThenDelegates(t *testing.T) {
	t.Parallel()

	inner := &faultRecordingExecutor{supportsGrp: true}
	script := onceThenSucceedScript(faultreplay.LaneQueueRetry, intPtr(1), nil)
	fe := mustFaultingExecutor(t, inner, script, "")

	ctx := context.Background()
	stmts := []Statement{{Cypher: "MERGE (a) RETURN a"}, {Cypher: "MERGE (b) RETURN b"}}
	if err := fe.ExecuteGroup(ctx, stmts); err == nil {
		t.Fatal("expected the scripted fault to fire on the first ExecuteGroup call")
	}
	if err := fe.ExecuteGroup(ctx, stmts); err != nil {
		t.Fatalf("second call: expected success, got %v", err)
	}
	if got := inner.groupCount(); got != 1 {
		t.Fatalf("expected inner.ExecuteGroup called once (the fired call short-circuited), got %d", got)
	}
}

// TestFaultingExecutorExecuteGroupErrorsWhenInnerLacksGroupSupport proves
// ExecuteGroup fails closed (a clear error, not a silent no-op or panic) when
// the wrapped executor does not implement GroupExecutor.
func TestFaultingExecutorExecuteGroupErrorsWhenInnerLacksGroupSupport(t *testing.T) {
	t.Parallel()

	inner := newFaultExecuteOnlyRecordingExecutor()
	fe := mustFaultingExecutor(t, inner, faultreplay.Script{Version: faultreplay.CurrentVersion}, "")
	if err := fe.ExecuteGroup(context.Background(), []Statement{{Cypher: "MERGE (a) RETURN a"}}); !errors.Is(err, errFaultingExecutorInnerNoGroup) {
		t.Fatalf("expected errFaultingExecutorInnerNoGroup, got %v", err)
	}
}

// TestFaultingExecutorRestartBackendBlocksUntilSentinelRemoved proves the
// restart-backend-between-phase-groups fault writes a sentinel file after
// the scripted Nth phase group commits, then blocks until that file is
// removed -- the coordination point the (deferred) Docker gate script uses to
// restart the graph backend mid-run.
func TestFaultingExecutorRestartBackendBlocksUntilSentinelRemoved(t *testing.T) {
	t.Parallel()

	inner := &faultRecordingExecutor{supportsGrp: true}
	sentinel := filepath.Join(t.TempDir(), "restart.sentinel")
	script := faultreplay.Script{
		Version: faultreplay.CurrentVersion,
		Faults: []faultreplay.FaultOp{
			{
				Kind:    faultreplay.KindRestartBackendBetweenPhaseGroups,
				Trigger: faultreplay.Trigger{AfterPhaseGroups: intPtr(1)},
			},
		},
	}
	fe := mustFaultingExecutor(t, inner, script, sentinel)

	done := make(chan error, 1)
	go func() {
		done <- fe.ExecuteGroup(context.Background(), []Statement{{Cypher: "MERGE (a) RETURN a"}})
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, statErr := os.Stat(sentinel); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the fault to write the sentinel file")
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case err := <-done:
		t.Fatalf("ExecuteGroup returned before the sentinel was removed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	if err := os.Remove(sentinel); err != nil {
		t.Fatalf("remove sentinel: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected ExecuteGroup to succeed after sentinel removal, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ExecuteGroup to unblock after sentinel removal")
	}
	if !fe.RestartFired() {
		t.Fatal("expected RestartFired() to report true")
	}
	if got := inner.groupCount(); got != 1 {
		t.Fatalf("expected exactly one delegated ExecuteGroup call, got %d", got)
	}
}

// TestFaultingExecutorRestartBackendRespectsContextCancellation proves the
// block loop is not an unconditional deadlock: a canceled context releases
// it even if the sentinel is never removed (concurrency-deadlock-rigor).
func TestFaultingExecutorRestartBackendRespectsContextCancellation(t *testing.T) {
	t.Parallel()

	inner := &faultRecordingExecutor{supportsGrp: true}
	sentinel := filepath.Join(t.TempDir(), "restart.sentinel")
	script := faultreplay.Script{
		Version: faultreplay.CurrentVersion,
		Faults: []faultreplay.FaultOp{
			{
				Kind:    faultreplay.KindRestartBackendBetweenPhaseGroups,
				Trigger: faultreplay.Trigger{AfterPhaseGroups: intPtr(1)},
			},
		},
	}
	fe := mustFaultingExecutor(t, inner, script, sentinel)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := fe.ExecuteGroup(ctx, []Statement{{Cypher: "MERGE (a) RETURN a"}})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected an error once the context deadline expired while the sentinel still exists")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected a context.DeadlineExceeded-wrapped error, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("blocked far longer than the context deadline: %s", elapsed)
	}
}

// TestFaultingExecutorRestartRequiresSentinelPath proves construction fails
// closed when a restart-backend-between-phase-groups fault is scripted but no
// sentinel path is supplied -- an unusable fault must not silently no-op.
func TestFaultingExecutorRestartRequiresSentinelPath(t *testing.T) {
	t.Parallel()

	script := faultreplay.Script{
		Version: faultreplay.CurrentVersion,
		Faults: []faultreplay.FaultOp{
			{
				Kind:    faultreplay.KindRestartBackendBetweenPhaseGroups,
				Trigger: faultreplay.Trigger{AfterPhaseGroups: intPtr(1)},
			},
		},
	}
	if _, err := NewFaultingExecutor(&faultRecordingExecutor{}, script, ""); err == nil {
		t.Fatal("expected an error when sentinelPath is empty but the script requires it")
	}
}

// TestFaultingExecutorRejectsDuplicateFaultsOfTheSameKind proves the
// constructor fails closed rather than silently keeping only the last
// duplicate fault -- an ambiguous script must not resolve into hidden
// last-write-wins behavior.
func TestFaultingExecutorRejectsDuplicateFaultsOfTheSameKind(t *testing.T) {
	t.Parallel()

	script := faultreplay.Script{
		Version: faultreplay.CurrentVersion,
		Faults: []faultreplay.FaultOp{
			{
				Kind:    faultreplay.KindFailGraphWriteOnceThenSucceed,
				Trigger: faultreplay.Trigger{StatementOrdinal: intPtr(1)},
				Target:  faultreplay.Target{Lane: faultreplay.LaneQueueRetry},
			},
			{
				Kind:    faultreplay.KindFailGraphWriteOnceThenSucceed,
				Trigger: faultreplay.Trigger{StatementOrdinal: intPtr(2)},
				Target:  faultreplay.Target{Lane: faultreplay.LaneQueueRetry},
			},
		},
	}
	if _, err := NewFaultingExecutor(&faultRecordingExecutor{}, script, ""); err == nil {
		t.Fatal("expected an error when the script names more than one fail-graph-write-once-then-succeed fault")
	}
}

// TestFaultingExecutorIgnoresWorkSourceSeamFaultKinds proves a script mixing
// graph-executor faults with WorkSource/intent-seam fault kinds (owned by a
// different decorator) constructs cleanly -- this seam simply does not react
// to them.
func TestFaultingExecutorIgnoresWorkSourceSeamFaultKinds(t *testing.T) {
	t.Parallel()

	script := faultreplay.Script{
		Version: faultreplay.CurrentVersion,
		Faults: []faultreplay.FaultOp{
			{Kind: faultreplay.KindKillWorkerAfterClaim, Trigger: faultreplay.Trigger{AfterClaims: intPtr(1)}},
			{Kind: faultreplay.KindExpireLeaseMidHandler, Trigger: faultreplay.Trigger{IntentID: strPtr("intent-1")}},
			{Kind: faultreplay.KindFailTerminal, Trigger: faultreplay.Trigger{IntentID: strPtr("intent-2")}},
		},
	}
	fe, err := NewFaultingExecutor(&faultRecordingExecutor{}, script, "")
	if err != nil {
		t.Fatalf("NewFaultingExecutor: %v", err)
	}
	if err := fe.Execute(context.Background(), Statement{Cypher: "MERGE (a) RETURN a"}); err != nil {
		t.Fatalf("expected a pass-through Execute with no scripted graph-executor fault, got %v", err)
	}
}

// TestFaultingExecutorRejectsUnknownFaultKind proves an unrecognized kind is
// a construction-time error, not a silently ignored no-op.
func TestFaultingExecutorRejectsUnknownFaultKind(t *testing.T) {
	t.Parallel()

	script := faultreplay.Script{
		Version: faultreplay.CurrentVersion,
		Faults:  []faultreplay.FaultOp{{Kind: "not-a-real-fault-kind"}},
	}
	if _, err := NewFaultingExecutor(&faultRecordingExecutor{}, script, ""); err == nil {
		t.Fatal("expected an error for an unknown fault kind")
	}
}

// TestFaultingExecutorCallOrdinalIsSharedAcrossExecuteAndExecuteGroup proves
// the statement-ordinal counter advances across BOTH Execute and
// ExecuteGroup calls as one shared sequence, matching the design's "fail the
// Nth matching Execute/ExecuteGroup" wording precisely.
func TestFaultingExecutorCallOrdinalIsSharedAcrossExecuteAndExecuteGroup(t *testing.T) {
	t.Parallel()

	inner := &faultRecordingExecutor{supportsGrp: true}
	script := onceThenSucceedScript(faultreplay.LaneQueueRetry, intPtr(2), nil)
	fe := mustFaultingExecutor(t, inner, script, "")

	ctx := context.Background()
	if err := fe.Execute(ctx, Statement{Cypher: "MERGE (a) RETURN a"}); err != nil {
		t.Fatalf("call 1 (Execute): expected success, got %v", err)
	}
	if err := fe.ExecuteGroup(ctx, []Statement{{Cypher: "MERGE (b) RETURN b"}}); err == nil {
		t.Fatal("call 2 (ExecuteGroup): expected the scripted fault to fire")
	}
	if err := fe.Execute(ctx, Statement{Cypher: "MERGE (c) RETURN c"}); err != nil {
		t.Fatalf("call 3 (Execute): expected success, got %v", err)
	}
}

// TestFaultingExecutorConcurrentCallsFireExactlyOnce hammers Execute
// concurrently and proves the once-then-succeed fault still fires exactly
// one time, never zero and never more than one -- the atomic.Bool
// compare-and-swap gate must hold under real contention, not just
// sequential calls.
func TestFaultingExecutorConcurrentCallsFireExactlyOnce(t *testing.T) {
	const callers = 64

	inner := &faultRecordingExecutor{}
	// Match every statement, so every concurrent caller is a fault
	// candidate; only the CompareAndSwap winner may observe an error.
	script := onceThenSucceedScript(faultreplay.LaneQueueRetry, nil, strPtr("MATCHALL"))
	fe, err := NewFaultingExecutor(inner, script, "")
	if err != nil {
		t.Fatalf("NewFaultingExecutor: %v", err)
	}

	var failures atomic.Int64
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			if err := fe.Execute(context.Background(), Statement{Cypher: "MERGE (a:MATCHALL) RETURN a"}); err != nil {
				failures.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := failures.Load(); got != 1 {
		t.Fatalf("expected exactly 1 fired failure across %d concurrent callers, got %d", callers, got)
	}
	if got := inner.executeCount(); got != callers-1 {
		t.Fatalf("expected %d delegated calls (all but the one that fired), got %d", callers-1, got)
	}
}
