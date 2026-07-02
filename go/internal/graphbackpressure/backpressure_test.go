// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphbackpressure

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestMaxInFlight(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		raw  string
		want int
	}{
		"unset disables":       {raw: "", want: 0},
		"blank disables":       {raw: "   ", want: 0},
		"zero disables":        {raw: "0", want: 0},
		"negative disables":    {raw: "-4", want: 0},
		"non-numeric disables": {raw: "lots", want: 0},
		"positive bounds":      {raw: "6", want: 6},
		"above default ok":     {raw: "64", want: 64},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := MaxInFlight(func(string) string { return tc.raw })
			if got != tc.want {
				t.Fatalf("MaxInFlight(%q) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

// TestClassMaxInFlight is the regression for issue #4448's back-compat
// contract: ClassMaxInFlight must read the class-specific env var first and
// fall back to the legacy shared MaxInFlightEnv only when the class var is
// unset, blank, non-numeric, or non-positive. This is what lets an operator
// who has only configured ESHU_GRAPH_WRITE_MAX_IN_FLIGHT keep an identical
// bound on both the canonical and semantic gates until they opt into
// per-class tuning.
func TestClassMaxInFlight(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		classVal  string
		legacyVal string
		want      int
	}{
		"class set, legacy unset uses class":        {classVal: "3", legacyVal: "", want: 3},
		"class set, legacy also set uses class":     {classVal: "3", legacyVal: "9", want: 3},
		"class unset falls back to legacy":          {classVal: "", legacyVal: "9", want: 9},
		"class blank falls back to legacy":          {classVal: "   ", legacyVal: "9", want: 9},
		"class zero falls back to legacy":           {classVal: "0", legacyVal: "9", want: 9},
		"class negative falls back to legacy":       {classVal: "-2", legacyVal: "9", want: 9},
		"class non-numeric falls back to legacy":    {classVal: "lots", legacyVal: "9", want: 9},
		"class and legacy both unset disables":      {classVal: "", legacyVal: "", want: 0},
		"class unset, legacy non-positive disables": {classVal: "", legacyVal: "0", want: 0},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			getenv := func(name string) string {
				switch name {
				case CanonicalMaxInFlightEnv:
					return tc.classVal
				case MaxInFlightEnv:
					return tc.legacyVal
				default:
					return ""
				}
			}
			got := ClassMaxInFlight(getenv, CanonicalMaxInFlightEnv)
			if got != tc.want {
				t.Fatalf("ClassMaxInFlight(class=%q, legacy=%q) = %d, want %d", tc.classVal, tc.legacyVal, got, tc.want)
			}
		})
	}
}

// TestClassMaxInFlightSemanticEnvIndependent proves ClassMaxInFlight reads the
// SemanticMaxInFlightEnv var independently of CanonicalMaxInFlightEnv, so the
// two class gates can be sized differently from each other, not just
// differently from the legacy shared var.
func TestClassMaxInFlightSemanticEnvIndependent(t *testing.T) {
	t.Parallel()

	getenv := func(name string) string {
		switch name {
		case CanonicalMaxInFlightEnv:
			return "4"
		case SemanticMaxInFlightEnv:
			return "2"
		default:
			return ""
		}
	}

	if got := ClassMaxInFlight(getenv, CanonicalMaxInFlightEnv); got != 4 {
		t.Fatalf("ClassMaxInFlight(canonical) = %d, want 4", got)
	}
	if got := ClassMaxInFlight(getenv, SemanticMaxInFlightEnv); got != 2 {
		t.Fatalf("ClassMaxInFlight(semantic) = %d, want 2", got)
	}
}

// TestNewObserverNilInstruments proves a runtime without telemetry still gets a
// working bound (the observer is nil, not a panicking stub).
func TestNewObserverNilInstruments(t *testing.T) {
	t.Parallel()

	if obs := NewObserver(nil, CanonicalGateName); obs != nil {
		t.Fatalf("NewObserver(nil, _) = %v, want nil", obs)
	}
}

// TestWrapDisabledIsPassthrough proves a non-positive ceiling leaves write
// concurrency unbounded, so the wrapper is a safe no-op until an operator opts
// in.
func TestWrapDisabledIsPassthrough(t *testing.T) {
	t.Parallel()

	probe := &countingExecutor{}
	wrapped := Wrap(probe, 0, nil)

	// A disabled bound must return inner unchanged so it adds no wrapper and
	// preserves any interface the inner executor exposes.
	if wrapped != cypher.Executor(probe) {
		t.Fatalf("Wrap(_, 0, _) = %T, want the inner executor unchanged", wrapped)
	}
	if err := wrapped.Execute(context.Background(), cypher.Statement{Cypher: "RETURN 1"}); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
	if probe.calls != 1 {
		t.Fatalf("inner calls = %d, want 1", probe.calls)
	}
}

// TestWrapBoundsConcurrency proves the wired wrapper actually caps in-flight
// writes, the core #3560 control, end to end through the helper.
func TestWrapBoundsConcurrency(t *testing.T) {
	t.Parallel()

	const maxInFlight = 2
	const callers = 10

	probe := &blockingExecutor{release: make(chan struct{})}
	wrapped := Wrap(probe, maxInFlight, nil)
	bp, ok := wrapped.(*cypher.BackpressureExecutor)
	if !ok {
		t.Fatalf("Wrap returned %T, want *cypher.BackpressureExecutor", wrapped)
	}

	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = wrapped.Execute(context.Background(), cypher.Statement{Cypher: "RETURN 1"})
		}()
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && bp.InFlight() < maxInFlight {
		time.Sleep(time.Millisecond)
	}
	if got := bp.InFlight(); got > maxInFlight {
		t.Fatalf("InFlight() = %d, want <= %d", got, maxInFlight)
	}
	close(probe.release)
	wg.Wait()
	if got := bp.InFlight(); got != 0 {
		t.Fatalf("InFlight() = %d after drain, want 0", got)
	}
}

// TestWrapExecuteOnlyInnerDoesNotExposeGroup is the end-to-end regression for
// issue #3652 P1: when inner is ExecuteOnlyExecutor
// (ESHU_NORNICDB_CANONICAL_GROUPED_WRITES=false), Wrap must return a value that
// does NOT implement GroupExecutor so WriteSemanticEntities falls through to
// sequential execution.
func TestWrapExecuteOnlyInnerDoesNotExposeGroup(t *testing.T) {
	t.Parallel()

	inner := cypher.ExecuteOnlyExecutor{Inner: &countingExecutor{}}
	wrapped := Wrap(inner, 4, nil)

	if _, ok := wrapped.(cypher.GroupExecutor); ok {
		t.Fatal("Wrap with ExecuteOnlyExecutor inner exposes GroupExecutor, want no GroupExecutor so sequential fallback works")
	}
	// Execute must still work.
	if err := wrapped.Execute(context.Background(), cypher.Statement{Cypher: "RETURN 1"}); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
}

// TestWrapGroupExecutorInnerExposesGroup proves that when inner implements
// GroupExecutor, Wrap returns a value that also exposes GroupExecutor.
func TestWrapGroupExecutorInnerExposesGroup(t *testing.T) {
	t.Parallel()

	// concurrencyProbeExecutor (local blockingExecutor extended with ExecuteGroup)
	// implements both Execute and ExecuteGroup.
	inner := &groupCapableExecutor{}
	wrapped := Wrap(inner, 4, nil)

	if _, ok := wrapped.(cypher.GroupExecutor); !ok {
		t.Fatal("Wrap with GroupExecutor inner does not expose GroupExecutor, want GroupExecutor preserved")
	}
}

type countingExecutor struct {
	calls int
}

func (e *countingExecutor) Execute(context.Context, cypher.Statement) error {
	e.calls++
	return nil
}

// groupCapableExecutor is a no-op executor that also implements GroupExecutor,
// used in tests that exercise the grouped-write code path.
type groupCapableExecutor struct{}

func (e *groupCapableExecutor) Execute(context.Context, cypher.Statement) error { return nil }

func (e *groupCapableExecutor) ExecuteGroup(context.Context, []cypher.Statement) error { return nil }

// blockingExecutor blocks each call until release is closed. It implements
// GroupExecutor so Wrap returns the *cypher.BackpressureExecutor type whose
// InFlight() gauge the bound-proving test reads; this mirrors the real reducer
// executor, which is group-capable.
type blockingExecutor struct {
	release chan struct{}
}

func (e *blockingExecutor) Execute(ctx context.Context, _ cypher.Statement) error {
	return e.block(ctx)
}

func (e *blockingExecutor) ExecuteGroup(ctx context.Context, _ []cypher.Statement) error {
	return e.block(ctx)
}

func (e *blockingExecutor) block(ctx context.Context) error {
	select {
	case <-e.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
