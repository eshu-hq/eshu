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
	// AggregateGateName labels the legacy-only-mode outer permit pool that
	// bounds the COMBINED canonical+semantic total to the legacy
	// MaxInFlightEnv ceiling (issue #4448 P1). It only appears in telemetry
	// while AnyClassMaxInFlightSet is false; once an operator sets either
	// per-class env, the aggregate gate is disabled and only "canonical" and
	// "semantic" appear.
	//
	// In legacy-only mode a single write acquires BOTH the aggregate gate and
	// its class gate (aggregate first, then class), so it can independently
	// wait on, and independently report a wait sample for, each layer. This is
	// intentional, not double-counting the same event: the two "gate" values
	// measure two different questions for the same write — "did the combined
	// legacy-shaped budget saturate" (aggregate) versus "did this class's own
	// share of that budget saturate" (canonical/semantic) — so an operator can
	// tell which layer to size before opting into independent per-class
	// ceilings.
	AggregateGateName = "aggregate"
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
// identical PER-CLASS ceiling shape (issue #4448 back-compat). A non-positive
// result even after the fallback disables backpressure for that class.
//
// IMPORTANT: this function alone does not bound the combined total across
// classes. When neither class env is set, both classes call this with the
// same fallback and each gets its own N-permit gate, which would allow up to
// 2N combined in-flight writes — a concurrency increase over the pre-#4448
// single shared N-permit pool that is unmeasured and not safe to ship for
// existing deployments. AggregateMaxInFlight (see below) is the guard: when
// no per-class env is set, callers must also wrap both class gates in a
// shared N-permit aggregate gate so the combined total stays == N.
func ClassMaxInFlight(getenv func(string) string, classEnv string) int {
	if n := parseMaxInFlight(getenv(classEnv)); n > 0 {
		return n
	}
	return MaxInFlight(getenv)
}

// AnyClassMaxInFlightSet reports whether at least one of the reducer's
// per-class ceilings (CanonicalMaxInFlightEnv, SemanticMaxInFlightEnv) is
// explicitly configured to a positive value. It is the discriminator for
// AggregateMaxInFlight: once an operator opts into per-class sizing for
// EITHER class, the two gates are fully independent (the #4448 fix) and no
// aggregate ceiling applies. While neither is set, the reducer is still on
// the legacy single-knob shape and must keep the combined total bounded by
// MaxInFlightEnv (issue #4448 P1: without this, a legacy-only deployment that
// set ESHU_GRAPH_WRITE_MAX_IN_FLIGHT=N would silently get up to 2N combined
// in-flight writes once the pool was split by class, an unmeasured
// concurrency increase that can overload the graph backend under mixed
// canonical+semantic load).
func AnyClassMaxInFlightSet(getenv func(string) string) bool {
	return parseMaxInFlight(getenv(CanonicalMaxInFlightEnv)) > 0 ||
		parseMaxInFlight(getenv(SemanticMaxInFlightEnv)) > 0
}

// AggregateMaxInFlight returns the legacy MaxInFlightEnv ceiling when neither
// per-class env is set (AnyClassMaxInFlightSet is false), and 0 (disabled)
// once an operator has opted into per-class sizing for at least one class.
// The returned value sizes the outer aggregate gate that both the canonical
// and semantic class gates draw a SECOND permit from in legacy-only mode, so
// the combined in-flight total across both classes never exceeds the single
// legacy ceiling an existing deployment already sized to backend headroom
// (issue #4448 P1 fix). Once any per-class env is set, the aggregate is
// disabled and each class's own gate is the sole bound for that class,
// matching the documented opt-in per-class behavior.
func AggregateMaxInFlight(getenv func(string) string) int {
	if AnyClassMaxInFlightSet(getenv) {
		return 0
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
