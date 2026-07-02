// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphbackpressure

import (
	"context"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// MaxInFlightEnv bounds concurrent graph writes through the
// BackpressureExecutor. It is the operator knob for issue #3560: a positive
// value caps in-flight writes so a slow graph backend slows intake instead of
// dead-lettering recoverable work, while an empty or non-positive value leaves
// backpressure disabled (passthrough) so the wrapper is a safe no-op until an
// operator opts in.
//
// It remains the projector's single knob (the projector has only one write
// class) and the reducer's fallback ceiling: ClassMaxInFlight reads a
// class-specific env var first and falls back to this one when the class var
// is unset, so an operator who already tuned this value keeps identical
// behavior on both reducer gates until they opt into per-class ceilings
// (issue #4448).
const MaxInFlightEnv = "ESHU_GRAPH_WRITE_MAX_IN_FLIGHT"

// CanonicalGateName and SemanticGateName are the closed set of gate-class
// values used as the "gate" telemetry label and as the discriminator for
// ClassMaxInFlight. CanonicalGateName covers canonical, handler-edge,
// shared-projection, secrets/IAM, orphan-sweep, and materializer writes;
// SemanticGateName covers the semantic entity write path. Splitting these into
// independent permit pools is issue #4448: before the split, a slow write on
// one class could exhaust the single shared pool and starve the other
// (head-of-line blocking), even though the pool was never full for the starved
// class's own workload.
const (
	CanonicalGateName = "canonical"
	SemanticGateName  = "semantic"
)

// CanonicalMaxInFlightEnv and SemanticMaxInFlightEnv are the per-class operator
// knobs for issue #4448. Either may be left unset, in which case
// ClassMaxInFlight falls back to MaxInFlightEnv so a single legacy setting
// keeps bounding both classes identically until an operator opts into
// per-class tuning.
const (
	CanonicalMaxInFlightEnv = "ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT"
	SemanticMaxInFlightEnv  = "ESHU_GRAPH_WRITE_SEMANTIC_MAX_IN_FLIGHT"
)

// MaxInFlight reads the configured concurrent-write ceiling from the
// environment. A blank, non-numeric, or non-positive value returns 0, which
// disables backpressure (passthrough). The value is not clamped to a maximum on
// purpose: an operator sizing it to backend headroom must be able to set it
// above the default worker count.
func MaxInFlight(getenv func(string) string) int {
	return parseMaxInFlight(getenv(MaxInFlightEnv))
}

// ClassMaxInFlight reads the per-class concurrent-write ceiling for classEnv
// (CanonicalMaxInFlightEnv or SemanticMaxInFlightEnv). When classEnv is unset,
// blank, non-numeric, or non-positive, it falls back to MaxInFlightEnv so an
// operator who has only configured the legacy shared knob still gets an
// identical bound on both classes (issue #4448 back-compat). A non-positive
// result even after the fallback disables backpressure for that class.
func ClassMaxInFlight(getenv func(string) string, classEnv string) int {
	if n := parseMaxInFlight(getenv(classEnv)); n > 0 {
		return n
	}
	return MaxInFlight(getenv)
}

// parseMaxInFlight parses a raw env value into a concurrent-write ceiling. A
// blank, non-numeric, or non-positive value returns 0 (disabled).
func parseMaxInFlight(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// observer bridges cypher.BackpressureExecutor signals to telemetry.Instruments
// so an operator sees backpressure engage without coupling the cypher package to
// the meter. It is nil-instrument tolerant: a runtime without telemetry still
// gets a working bound, just no metrics. gateName labels every metric this
// observer records so an operator can distinguish which permit pool a write
// waited on (issue #4448).
type observer struct {
	instruments *telemetry.Instruments
	gateName    string
}

// NewObserver returns a cypher.BackpressureObserver that records engaged-count
// and wait-duration metrics labeled with gateName, or nil when instruments is
// nil so the executor skips observation entirely. gateName must be
// CanonicalGateName or SemanticGateName.
func NewObserver(instruments *telemetry.Instruments, gateName string) cypher.BackpressureObserver {
	if instruments == nil {
		return nil
	}
	return observer{instruments: instruments, gateName: gateName}
}

// ObserveBackpressureWait records one write that blocked for a permit. The
// engaged counter and wait histogram share the operation and gate labels and
// are recorded together so their counts stay equal, which is what lets an
// operator read the wait distribution as "of the writes that hit backpressure
// on this gate, how long they waited".
func (o observer) ObserveBackpressureWait(ctx context.Context, operation string, wait time.Duration) {
	if o.instruments == nil {
		return
	}
	attrs := metric.WithAttributes(telemetry.AttrOperation(operation), telemetry.AttrGate(o.gateName))
	if o.instruments.GraphWriteBackpressureEngaged != nil {
		o.instruments.GraphWriteBackpressureEngaged.Add(ctx, 1, attrs)
	}
	if o.instruments.GraphWriteBackpressureWaitDuration != nil {
		o.instruments.GraphWriteBackpressureWaitDuration.Record(ctx, wait.Seconds(), attrs)
	}
}

// Wrap wraps inner with a BackpressureExecutor bounded to maxInFlight concurrent
// writes, wired to the telemetry observer under CanonicalGateName. It is the
// helper the projector wiring calls, since the projector has only one write
// class; the reducer, which has independent canonical and semantic classes,
// calls NewGate directly per class (issue #4448).
//
// A non-positive maxInFlight returns inner unchanged so a disabled bound adds no
// wrapper, no indirection, and preserves any type the inner executor exposes
// (GroupExecutor, TimeoutExecutor). Callers can therefore wire Wrap
// unconditionally and let the env knob decide whether the bound is active.
//
// When inner does NOT implement GroupExecutor (e.g. ExecuteOnlyExecutor with
// ESHU_NORNICDB_CANONICAL_GROUPED_WRITES=false), Wrap returns an
// ExecuteOnlyBackpressureExecutor that intentionally strips ExecuteGroup from
// the returned type. This ensures callers that type-assert GroupExecutor take
// the sequential fallback path instead of hitting errInnerNoExecuteGroup inside
// BackpressureExecutor.ExecuteGroup.
func Wrap(inner cypher.Executor, maxInFlight int, instruments *telemetry.Instruments) cypher.Executor {
	if maxInFlight <= 0 {
		return inner
	}
	return WrapExecutorWithGate(inner, NewGate(maxInFlight, instruments, CanonicalGateName))
}

// NewGate builds a backpressure permit pool bounded to maxInFlight, wired to the
// telemetry observer under gateName. Share one gate across every wrapper that
// must draw from the SAME pool — for example the reducer's canonical
// single-statement Executor path, the grouped path, and the
// reducer.CypherExecutor materializer path all share one canonical gate — but
// build an INDEPENDENT gate per write class so one class's permits cannot be
// exhausted by another class's slow writes (issue #4448; originally every
// class shared one gate per #3652, which caused head-of-line blocking between
// canonical and semantic writes).
//
// A non-positive maxInFlight returns nil so callers can treat a disabled bound
// as a passthrough: WrapExecutorWithGate and WrapCypherExecutorWithGate return
// their inner executor unchanged when the gate is nil.
func NewGate(maxInFlight int, instruments *telemetry.Instruments, gateName string) *cypher.BackpressureGate {
	if maxInFlight <= 0 {
		return nil
	}
	return cypher.NewBackpressureGate(maxInFlight, NewObserver(instruments, gateName))
}

// WrapExecutorWithGate wraps inner so it draws from the shared gate's permit
// pool. The wrapper acquires a permit, then delegates to inner, so it must be
// composed as the OUTERMOST layer of a write path: a permit then covers the
// whole inner attempt (timeout, retry, backend write). Composing it inside a
// TimeoutExecutor would charge permit-wait time against that timeout, turning a
// saturated pool into spurious graph_write_timeout dead letters (#3652 P1).
//
// A nil gate returns inner unchanged (passthrough), preserving any interface it
// exposes. When the gate is set but inner does not implement GroupExecutor (the
// ExecuteOnlyExecutor path), the returned value intentionally does not expose
// ExecuteGroup so callers that type-assert GroupExecutor fall through to
// sequential execution instead of hitting errInnerNoExecuteGroup.
func WrapExecutorWithGate(inner cypher.Executor, gate *cypher.BackpressureGate) cypher.Executor {
	if gate == nil {
		return inner
	}
	bp := cypher.NewBackpressureExecutorWithGate(inner, gate)
	if _, ok := inner.(cypher.GroupExecutor); ok {
		return bp // full interface: Executor + GroupExecutor
	}
	return cypher.ExecuteOnlyBackpressureExecutor(bp) // Executor only
}
