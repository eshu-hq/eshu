// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphbackpressure

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
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

// TestAnyClassMaxInFlightSet is the regression for the P1 finding on issue
// #4448: this is the discriminator that decides whether the legacy-only
// aggregate ceiling applies. It must report true as soon as EITHER per-class
// env is set to a positive value, and false only when both are unset, blank,
// non-numeric, or non-positive.
func TestAnyClassMaxInFlightSet(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		canonicalVal string
		semanticVal  string
		want         bool
	}{
		"both unset":                           {canonicalVal: "", semanticVal: "", want: false},
		"both non-positive":                    {canonicalVal: "0", semanticVal: "-1", want: false},
		"both non-numeric":                     {canonicalVal: "lots", semanticVal: "nope", want: false},
		"canonical set only":                   {canonicalVal: "3", semanticVal: "", want: true},
		"semantic set only":                    {canonicalVal: "", semanticVal: "2", want: true},
		"both set":                             {canonicalVal: "3", semanticVal: "2", want: true},
		"canonical non-positive, semantic set": {canonicalVal: "0", semanticVal: "2", want: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			getenv := func(name string) string {
				switch name {
				case CanonicalMaxInFlightEnv:
					return tc.canonicalVal
				case SemanticMaxInFlightEnv:
					return tc.semanticVal
				default:
					return ""
				}
			}
			if got := AnyClassMaxInFlightSet(getenv); got != tc.want {
				t.Fatalf("AnyClassMaxInFlightSet(canonical=%q, semantic=%q) = %v, want %v", tc.canonicalVal, tc.semanticVal, got, tc.want)
			}
		})
	}
}

// TestAggregateMaxInFlight is the direct regression for the P1 finding on
// issue #4448: AggregateMaxInFlight must return the legacy ceiling ONLY while
// neither per-class env is set, and 0 (disabled) as soon as an operator opts
// into per-class sizing for either class. This is the function that prevents
// a legacy-only deployment from silently getting 2x its configured
// concurrency budget once the pool is split by class.
func TestAggregateMaxInFlight(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		legacyVal    string
		canonicalVal string
		semanticVal  string
		want         int
	}{
		"legacy only set enables aggregate at legacy value":     {legacyVal: "5", canonicalVal: "", semanticVal: "", want: 5},
		"nothing set disables aggregate":                        {legacyVal: "", canonicalVal: "", semanticVal: "", want: 0},
		"legacy set but canonical also set disables aggregate":  {legacyVal: "5", canonicalVal: "3", semanticVal: "", want: 0},
		"legacy set but semantic also set disables aggregate":   {legacyVal: "5", canonicalVal: "", semanticVal: "2", want: 0},
		"legacy set and both class envs set disables aggregate": {legacyVal: "5", canonicalVal: "3", semanticVal: "2", want: 0},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			getenv := func(name string) string {
				switch name {
				case MaxInFlightEnv:
					return tc.legacyVal
				case CanonicalMaxInFlightEnv:
					return tc.canonicalVal
				case SemanticMaxInFlightEnv:
					return tc.semanticVal
				default:
					return ""
				}
			}
			if got := AggregateMaxInFlight(getenv); got != tc.want {
				t.Fatalf("AggregateMaxInFlight(legacy=%q, canonical=%q, semantic=%q) = %d, want %d", tc.legacyVal, tc.canonicalVal, tc.semanticVal, got, tc.want)
			}
		})
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

// TestIsValidGateName is the direct regression for the cardinality-safety
// finding on issue #4448: IsValidGateName must accept exactly the closed
// three-member vocabulary (CanonicalGateName, SemanticGateName,
// AggregateGateName) and reject everything else, including empty strings,
// case variants, and arbitrary operation/statement-like strings a future
// call-site mistake might pass.
func TestIsValidGateName(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		CanonicalGateName:     true,
		SemanticGateName:      true,
		AggregateGateName:     true,
		"":                    false,
		"Canonical":           false, // case-sensitive: not a silent alias
		"unknown":             false, // "unknown" is the coercion TARGET, not itself valid input
		"materialize_cypher":  false, // an operation label, the exact mistake this guards against
		"CREATE (n) RETURN n": false, // a raw statement/Cypher string, the exact mistake this guards against
	}

	for name, want := range cases {
		if got := IsValidGateName(name); got != want {
			t.Fatalf("IsValidGateName(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestNewObserverCoercesUnknownGateName is the regression for the
// cardinality-safety finding on issue #4448: NewObserver must not trust an
// arbitrary caller-supplied gateName. A call site that accidentally passes an
// operation name, a raw Cypher statement, or any other out-of-vocabulary
// string must have it coerced to "unknown" before it is stored, so the "gate"
// telemetry label's cardinality stays bounded to the closed vocabulary plus
// one escape value no matter what a future caller passes in.
func TestNewObserverCoercesUnknownGateName(t *testing.T) {
	t.Parallel()

	inst := &telemetry.Instruments{}

	cases := []string{
		"",
		"materialize_cypher",
		"CREATE (n) RETURN n",
		"Canonical", // wrong case is also out-of-vocabulary
	}

	for _, bad := range cases {
		obs := NewObserver(inst, bad)
		got, ok := obs.(observer)
		if !ok {
			t.Fatalf("NewObserver(_, %q) returned %T, want observer", bad, obs)
		}
		if got.gateName != unknownGateName {
			t.Fatalf("NewObserver(_, %q).gateName = %q, want %q", bad, got.gateName, unknownGateName)
		}
	}

	// Valid gate names must pass through unchanged.
	for _, valid := range []string{CanonicalGateName, SemanticGateName, AggregateGateName} {
		obs := NewObserver(inst, valid)
		got, ok := obs.(observer)
		if !ok {
			t.Fatalf("NewObserver(_, %q) returned %T, want observer", valid, obs)
		}
		if got.gateName != valid {
			t.Fatalf("NewObserver(_, %q).gateName = %q, want %q (valid names must not be coerced)", valid, got.gateName, valid)
		}
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
	// InFlight() must actually REACH maxInFlight, not merely stay at or below
	// it: with 10 blocked callers racing for 2 permits, a wait loop that timed
	// out below the ceiling would mean writes never reached the configured
	// concurrency budget (an accidental serialization regression the
	// project's "Serialization Is Not A Fix" rule bars), which a
	// got<=maxInFlight-only assertion would silently accept.
	if got := bp.InFlight(); got != maxInFlight {
		t.Fatalf("InFlight() = %d, want exactly %d (bound breached or writes were serialized below the ceiling)", got, maxInFlight)
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
