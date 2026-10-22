// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifafaultinjection

package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/replay/faultreplay"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// holdScript builds a validated-script-shaped hold-statement-before-delegation
// FaultOp set; validation itself is proven in faultreplay, so tests here go
// through NewFaultingExecutor directly.
func holdScript(match string) faultreplay.Script {
	return faultreplay.Script{
		Version: faultreplay.CurrentVersion,
		Faults: []faultreplay.FaultOp{
			{
				Kind:    faultreplay.KindHoldStatementBeforeDelegation,
				Trigger: faultreplay.Trigger{OperationMatch: &match},
			},
		},
	}
}

// holdSentinel creates the sentinel file the hold blocks on and returns its
// path. The hold releases when this path stops existing.
func holdSentinel(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hold.sentinel")
	if err := os.WriteFile(path, []byte("held\n"), 0o600); err != nil {
		t.Fatalf("create hold sentinel: %v", err)
	}
	return path
}

// TestNewFaultingExecutorRejectsSecondHoldFault proves one-per-run: two hold
// faults in one script fail closed at construction instead of the second
// silently never firing.
func TestNewFaultingExecutorRejectsSecondHoldFault(t *testing.T) {
	t.Parallel()

	match := "MERGE (v)"
	script := faultreplay.Script{
		Version: faultreplay.CurrentVersion,
		Faults: []faultreplay.FaultOp{
			{Kind: faultreplay.KindHoldStatementBeforeDelegation, Trigger: faultreplay.Trigger{OperationMatch: &match}},
			{Kind: faultreplay.KindHoldStatementBeforeDelegation, Trigger: faultreplay.Trigger{OperationMatch: &match}},
		},
	}
	if _, err := NewFaultingExecutor(&faultRecordingExecutor{}, script, holdSentinel(t)); err == nil {
		t.Fatal("NewFaultingExecutor(two hold faults) = nil error, want one-per-run rejection")
	} else if !strings.Contains(err.Error(), faultreplay.KindHoldStatementBeforeDelegation) {
		t.Errorf("NewFaultingExecutor() error = %q, want it to name the hold kind", err.Error())
	}
}

// TestNewFaultingExecutorRejectsHoldWithoutSentinel proves sentinel-required:
// a hold with no sentinel path to block on fails closed at construction
// instead of firing-and-immediately-delegating (a hold that never holds).
func TestNewFaultingExecutorRejectsHoldWithoutSentinel(t *testing.T) {
	t.Parallel()

	inner := &faultRecordingExecutor{}
	if _, err := NewFaultingExecutor(inner, holdScript("MERGE (v)"), ""); err == nil {
		t.Fatal("NewFaultingExecutor(hold, empty sentinel) = nil error, want sentinel-required rejection")
	}
	if _, err := NewFaultingExecutor(inner, holdScript("MERGE (v)"), "   "); err == nil {
		t.Fatal("NewFaultingExecutor(hold, blank sentinel) = nil error, want sentinel-required rejection")
	}
}

// TestFaultingExecutorHoldBlocksExecuteUntilSentinelRemoval proves the core
// hold: a matching Execute does NOT delegate while the sentinel exists, and
// delegates once it is removed. The marker must already name the matched
// statement while held (the rendezvous proof the kill cell reads).
func TestFaultingExecutorHoldBlocksExecuteUntilSentinelRemoval(t *testing.T) {
	t.Parallel()

	inner := &faultRecordingExecutor{}
	sentinel := holdSentinel(t)
	fe := mustFaultingExecutor(t, inner, holdScript("USES_VAULT_POLICY"), sentinel)

	const matched = "MERGE (v)-[rel:SECRETS_IAM_USES_VAULT_POLICY]->(p)"
	released := make(chan error, 1)
	go func() {
		released <- fe.Execute(context.Background(), cypher.Statement{Cypher: matched})
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if fe.HoldFired() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("hold never fired on the matching Execute within 2s")
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case err := <-released:
		t.Fatalf("matching Execute returned %v while the sentinel still exists; the hold must not delegate before release", err)
	default:
	}
	if got := inner.executeCount(); got != 0 {
		t.Fatalf("inner.Execute called %d times while held, want 0 (hold-before-delegation)", got)
	}
	marker, err := os.ReadFile(sentinel + holdFiredMarkerSuffix)
	if err != nil {
		t.Fatalf("read hold marker while held: %v", err)
	}
	if !strings.Contains(string(marker), matched) {
		t.Errorf("hold marker %q does not name the matched statement %q", string(marker), matched)
	}

	if err := os.Remove(sentinel); err != nil {
		t.Fatalf("remove sentinel: %v", err)
	}
	select {
	case err := <-released:
		if err != nil {
			t.Fatalf("matching Execute after sentinel removal = %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("matching Execute did not return within 10s of sentinel removal")
	}
	if got := inner.executeCount(); got != 1 {
		t.Fatalf("inner.Execute called %d times after release, want 1", got)
	}
}

// TestFaultingExecutorHoldFiresOnExecuteGroupExactlyOnce proves the hold
// engages at the ExecuteGroup seam too -- the atomic path the secrets writer's
// dispatch actually takes when inner supports GroupExecutor -- and exactly
// once under concurrency: of N concurrent matching callers, exactly one
// holds; the rest delegate immediately.
func TestFaultingExecutorHoldFiresOnExecuteGroupExactlyOnce(t *testing.T) {
	t.Parallel()

	inner := &faultRecordingExecutor{supportsGrp: true}
	sentinel := holdSentinel(t)
	fe := mustFaultingExecutor(t, inner, holdScript("USES_VAULT_POLICY"), sentinel)

	const matched = "MERGE (v)-[rel:SECRETS_IAM_USES_VAULT_POLICY]->(p)"
	const callers = 8
	var wg sync.WaitGroup
	errs := make([]error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = fe.ExecuteGroup(context.Background(), []cypher.Statement{{Cypher: "MERGE (n:Other) RETURN n"}, {Cypher: matched}})
		}(i)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	// The one holder must still be blocked while the sentinel exists; the
	// CAS losers delegate immediately.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if fe.HoldFired() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("hold never fired on the matching ExecuteGroup within 2s")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	select {
	case <-done:
		t.Fatalf("all %d ExecuteGroup callers returned while the sentinel still exists; exactly one must stay held", callers)
	default:
	}

	if err := os.Remove(sentinel); err != nil {
		t.Fatalf("remove sentinel: %v", err)
	}
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("ExecuteGroup callers did not all return within 15s of sentinel removal")
	}
	for i, err := range errs {
		if err != nil {
			t.Errorf("caller %d: ExecuteGroup = %v, want nil", i, err)
		}
	}
	if got := inner.groupCount(); got != callers {
		t.Errorf("inner ExecuteGroup called %d times, want %d (every caller delegates exactly once)", got, callers)
	}
	marker, err := os.ReadFile(sentinel + holdFiredMarkerSuffix)
	if err != nil {
		t.Fatalf("read hold marker after release: %v", err)
	}
	if !strings.Contains(string(marker), matched) {
		t.Errorf("hold marker %q does not name the matched statement %q", string(marker), matched)
	}
}

// TestFaultingExecutorHoldContextCancelReturnsWithoutDelegating proves a
// canceled hold errors without ever reaching inner: a kill-cell harness that
// gives up waiting must not leave the statement half-delegated.
func TestFaultingExecutorHoldContextCancelReturnsWithoutDelegating(t *testing.T) {
	t.Parallel()

	inner := &faultRecordingExecutor{}
	sentinel := holdSentinel(t)
	fe := mustFaultingExecutor(t, inner, holdScript("USES_VAULT_POLICY"), sentinel)

	ctx, cancel := context.WithCancel(context.Background())
	released := make(chan error, 1)
	go func() {
		released <- fe.Execute(ctx, cypher.Statement{Cypher: "MERGE (v)-[rel:SECRETS_IAM_USES_VAULT_POLICY]->(p)"})
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if fe.HoldFired() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("hold never fired within 2s")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-released:
		if err == nil {
			t.Fatal("canceled hold returned nil, want a context-cancellation error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("canceled hold did not return within 10s")
	}
	if got := inner.executeCount(); got != 0 {
		t.Fatalf("inner.Execute called %d times after ctx cancel, want 0", got)
	}
}

// TestFaultingExecutorHoldIgnoresNonMatchingStatement proves a non-matching
// statement never holds, even with the sentinel present: the hold engages a
// write only it performs.
func TestFaultingExecutorHoldIgnoresNonMatchingStatement(t *testing.T) {
	t.Parallel()

	inner := &faultRecordingExecutor{}
	sentinel := holdSentinel(t)
	fe := mustFaultingExecutor(t, inner, holdScript("USES_VAULT_POLICY"), sentinel)

	ctx := context.Background()
	if err := fe.Execute(ctx, cypher.Statement{Cypher: "MERGE (a:AUTHENTICATES) RETURN a"}); err != nil {
		t.Fatalf("non-matching Execute = %v, want nil (no hold)", err)
	}
	if fe.HoldFired() {
		t.Fatal("HoldFired() = true after a non-matching statement, want false")
	}
	if got := inner.executeCount(); got != 1 {
		t.Fatalf("inner.Execute called %d times, want 1 (direct delegation, no hold)", got)
	}
	if _, err := os.Stat(sentinel + holdFiredMarkerSuffix); !os.IsNotExist(err) {
		t.Fatalf("hold marker exists after a non-matching statement (err = %v), want absent", err)
	}
}

// TestFaultingExecutorHoldFiresOnlyOnce proves the second matching call
// delegates without blocking: the hold is a one-shot rendezvous, not a
// permanent gate on the statement.
func TestFaultingExecutorHoldFiresOnlyOnce(t *testing.T) {
	t.Parallel()

	inner := &faultRecordingExecutor{}
	sentinel := holdSentinel(t)
	fe := mustFaultingExecutor(t, inner, holdScript("USES_VAULT_POLICY"), sentinel)

	const matched = "MERGE (v)-[rel:SECRETS_IAM_USES_VAULT_POLICY]->(p)"
	first := make(chan error, 1)
	go func() {
		first <- fe.Execute(context.Background(), cypher.Statement{Cypher: matched})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if fe.HoldFired() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("hold never fired within 2s")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := os.Remove(sentinel); err != nil {
		t.Fatalf("remove sentinel: %v", err)
	}
	select {
	case err := <-first:
		if err != nil {
			t.Fatalf("first matching Execute = %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("first matching Execute did not return within 10s of sentinel removal")
	}

	// Sentinel is gone, but even if it were recreated the hold must not
	// re-fire: the CAS already flipped.
	if err := fe.Execute(context.Background(), cypher.Statement{Cypher: matched}); err != nil {
		t.Fatalf("second matching Execute = %v, want nil (hold fires only once)", err)
	}
	if got := inner.executeCount(); got != 2 {
		t.Fatalf("inner.Execute called %d times, want 2 (both calls delegated exactly once)", got)
	}
}
