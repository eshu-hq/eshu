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
	bp := cypher.NewBackpressureExecutor(inner, maxInFlight, NewObserver(instruments))
	// Only expose GroupExecutor if the inner executor supports it. When inner is
	// ExecuteOnlyExecutor (ESHU_NORNICDB_CANONICAL_GROUPED_WRITES=false), the
	// wrapper must not advertise ExecuteGroup or callers that type-assert
	// GroupExecutor will hit errInnerNoExecuteGroup instead of falling through to
	// sequential execution.
	if _, ok := inner.(cypher.GroupExecutor); ok {
		return bp // full interface: Executor + GroupExecutor
	}
	return cypher.ExecuteOnlyBackpressureExecutor(bp) // Executor only
}
