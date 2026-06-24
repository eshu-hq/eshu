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
const MaxInFlightEnv = "ESHU_GRAPH_WRITE_MAX_IN_FLIGHT"

// MaxInFlight reads the configured concurrent-write ceiling from the
// environment. A blank, non-numeric, or non-positive value returns 0, which
// disables backpressure (passthrough). The value is not clamped to a maximum on
// purpose: an operator sizing it to backend headroom must be able to set it
// above the default worker count.
func MaxInFlight(getenv func(string) string) int {
	raw := strings.TrimSpace(getenv(MaxInFlightEnv))
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
// gets a working bound, just no metrics.
type observer struct {
	instruments *telemetry.Instruments
}

// NewObserver returns a cypher.BackpressureObserver that records engaged-count
// and wait-duration metrics, or nil when instruments is nil so the executor
// skips observation entirely.
func NewObserver(instruments *telemetry.Instruments) cypher.BackpressureObserver {
	if instruments == nil {
		return nil
	}
	return observer{instruments: instruments}
}

// ObserveBackpressureWait records one write that blocked for a permit. The
// engaged counter and wait histogram share the operation label and are recorded
// together so their counts stay equal, which is what lets an operator read the
// wait distribution as "of the writes that hit backpressure, how long they
// waited".
func (o observer) ObserveBackpressureWait(ctx context.Context, operation string, wait time.Duration) {
	if o.instruments == nil {
		return
	}
	attrs := metric.WithAttributes(telemetry.AttrOperation(operation))
	if o.instruments.GraphWriteBackpressureEngaged != nil {
		o.instruments.GraphWriteBackpressureEngaged.Add(ctx, 1, attrs)
	}
	if o.instruments.GraphWriteBackpressureWaitDuration != nil {
		o.instruments.GraphWriteBackpressureWaitDuration.Record(ctx, wait.Seconds(), attrs)
	}
}

// Wrap wraps inner with a BackpressureExecutor bounded to maxInFlight concurrent
// writes, wired to the telemetry observer. It is the single helper both the
// reducer and projector wiring call so the executor sits at the same outermost
// position (above retry/timeout) in every write path.
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
	return WrapExecutorWithGate(inner, NewGate(maxInFlight, instruments))
}

// NewGate builds a shared backpressure permit pool bounded to maxInFlight,
// wired to the telemetry observer. Share one gate across every wrapper that must
// draw from the same pool — the single-statement Executor path, the grouped
// path, and the reducer.CypherExecutor materializer path — so the
// ESHU_GRAPH_WRITE_MAX_IN_FLIGHT ceiling bounds every reducer graph write as one
// pool rather than per-wrapper sub-pools (#3652).
//
// A non-positive maxInFlight returns nil so callers can treat a disabled bound
// as a passthrough: WrapExecutorWithGate and WrapCypherExecutorWithGate return
// their inner executor unchanged when the gate is nil.
func NewGate(maxInFlight int, instruments *telemetry.Instruments) *cypher.BackpressureGate {
	if maxInFlight <= 0 {
		return nil
	}
	return cypher.NewBackpressureGate(maxInFlight, NewObserver(instruments))
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
