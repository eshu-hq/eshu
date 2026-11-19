// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphbackpressure

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// wrapCountedFake is a GroupExecutor with an optional counted-write seam,
// recording how many counted calls reach the inner executor (#10938).
type wrapCountedFake struct {
	mu           sync.Mutex
	groupCalls   int
	countedCalls int
	countErr     error
	release      chan struct{}
}

func (f *wrapCountedFake) Execute(context.Context, cypher.Statement) error { return nil }

func (f *wrapCountedFake) ExecuteGroup(_ context.Context, stmts []cypher.Statement) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.groupCalls++
	return nil
}

func (f *wrapCountedFake) ExecuteCountedWrite(_ context.Context, _ cypher.Statement) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.countedCalls++
	if f.countErr != nil {
		return 0, f.countErr
	}
	return 2, nil
}

func (f *wrapCountedFake) ExecuteCountedWriteGroup(ctx context.Context, stmts []cypher.Statement) ([]int64, error) {
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.countedCalls++
	if f.countErr != nil {
		return nil, f.countErr
	}
	out := make([]int64, len(stmts))
	for i := range stmts {
		out[i] = 2
	}
	return out, nil
}

func (f *wrapCountedFake) counted() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.countedCalls
}

// wrapGroupOnlyFake implements GroupExecutor without the counted seam. It
// deliberately does not embed wrapCountedFake: embedding would promote the
// counted methods and silently satisfy the seam this fake must lack.
type wrapGroupOnlyFake struct{}

func (wrapGroupOnlyFake) Execute(context.Context, cypher.Statement) error { return nil }

func (wrapGroupOnlyFake) ExecuteGroup(context.Context, []cypher.Statement) error { return nil }

// TestWrapExecutorWithGatePreservesCountedWriteSeam proves the gate wrapper
// forwards counted writes to a seam-capable inner while holding one permit,
// and hides the seam on the execute-only branch (#10938).
func TestWrapExecutorWithGatePreservesCountedWriteSeam(t *testing.T) {
	t.Parallel()

	inner := &wrapCountedFake{release: make(chan struct{})}
	gate := NewGate(1, nil, "test-counted")
	wrapped := WrapExecutorWithGate(inner, gate)
	grouped, ok := wrapped.(cypher.CountedWriteGroupExecutor)
	if !ok {
		t.Fatal("gated group executor does not expose CountedWriteGroupExecutor")
	}
	single, ok := wrapped.(cypher.CountedWriteExecutor)
	if !ok {
		t.Fatal("gated group executor does not expose CountedWriteExecutor")
	}
	stmts := []cypher.Statement{{Cypher: "RETURN 1"}, {Cypher: "RETURN 2"}}
	type result struct {
		counts []int64
		err    error
	}
	done := make(chan result, 1)
	go func() {
		counts, err := grouped.ExecuteCountedWriteGroup(context.Background(), stmts)
		done <- result{counts, err}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for gate.InFlight() != 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := gate.InFlight(); got != 1 {
		close(inner.release)
		t.Fatalf("gate InFlight() = %d while the counted group is blocked inside, want 1", got)
	}
	close(inner.release)
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("ExecuteCountedWriteGroup() error = %v, want nil", r.err)
		}
		if len(r.counts) != 2 || r.counts[0] != 2 || r.counts[1] != 2 {
			t.Fatalf("counts = %v, want [2 2]", r.counts)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("counted group call did not return after release")
	}
	if got := inner.counted(); got != 1 {
		t.Fatalf("inner counted calls = %d, want 1 (one logical call, retries owned below)", got)
	}
	count, err := single.ExecuteCountedWrite(context.Background(), cypher.Statement{Cypher: "RETURN 1"})
	if err != nil {
		t.Fatalf("ExecuteCountedWrite() error = %v, want nil", err)
	}
	if count != 2 {
		t.Fatalf("ExecuteCountedWrite() = %d, want 2", count)
	}

	plain := WrapExecutorWithGate(inner, nil)
	if _, ok := plain.(cypher.CountedWriteGroupExecutor); !ok {
		t.Fatal("passthrough (nil gate) lost CountedWriteGroupExecutor")
	}
}

// TestWrapExecutorWithGateHidesCountedSeamOnExecuteOnlyBranch proves an
// inner without GroupExecutor hides the whole bundle including counted
// writes, and a Group inner without the seam fails counted calls closed
// with the permit released.
func TestWrapExecutorWithGateHidesCountedSeamOnExecuteOnlyBranch(t *testing.T) {
	t.Parallel()

	plain := WrapExecutorWithGate(executeOnlyStub{}, NewGate(1, nil, "test-execute-only"))
	if _, ok := plain.(cypher.CountedWriteGroupExecutor); ok {
		t.Fatal("execute-only branch exposes CountedWriteGroupExecutor")
	}
	if _, ok := plain.(cypher.CountedWriteExecutor); ok {
		t.Fatal("execute-only branch exposes CountedWriteExecutor")
	}

	inner := &wrapGroupOnlyFake{}
	gate := NewGate(1, nil, "test-group-no-seam")
	wrapped := WrapExecutorWithGate(inner, gate)
	if _, ok := wrapped.(cypher.CountedWriteGroupExecutor); !ok {
		t.Fatal("grouped wrapper must advertise the counted bundle (fail-closed at call time)")
	}
	_, err := wrapped.(cypher.CountedWriteGroupExecutor).ExecuteCountedWriteGroup(
		context.Background(), []cypher.Statement{{Cypher: "RETURN 1"}})
	if !errors.Is(err, cypher.ErrCountedWriteUnsupported) {
		t.Fatalf("counted group on a seam-less inner err = %v, want ErrCountedWriteUnsupported", err)
	}
	if got := gate.InFlight(); got != 0 {
		t.Fatalf("gate InFlight() = %d after a failed counted call, want 0 (no leaked permit)", got)
	}
}

type executeOnlyStub struct{}

func (executeOnlyStub) Execute(context.Context, cypher.Statement) error { return nil }
