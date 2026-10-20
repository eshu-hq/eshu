// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
)

// CountProbeExecutor is an optional capability an Executor implementation
// can add to answer how many rows a read-only COUNT statement matches,
// without mutating the graph. It exists for guards that must NAME the rows
// they refuse to delete: an existence probe answers whether to fail, but
// the failure telemetry must name the prior count (issue #8657's
// writer-side conditional retract probes the prior File count before
// issuing the unbounded file retract).
//
// The fail-safe direction matches ProbeExecutor: the absence of this
// interface, and any error ExecuteCountProbe returns, mean "unknown", never
// "zero rows". Callers proceed with the guarded write and count the
// outcome, so a probe-only failure cannot fail a projection whose write
// would succeed. Like ExecuteProbe it does not retry: the write that
// follows still retries, so no transient-error resilience is lost.
//
// The all-or-nothing forwarding rule for ProbeExecutor applies here too:
// every wrapper in this package that forwards GroupExecutor MUST forward
// CountProbeExecutor the same way, and the ExecuteOnly* wrappers MUST hide
// it, or a mid-chain gap silently downgrades an enforced guard to
// probe_unsupported with all tests green. probe_follows_group_test.go pins
// the column.
type CountProbeExecutor interface {
	// ExecuteCountProbe runs stmt (a read-only COUNT query) and reports
	// its count. count is meaningful only when err is nil; a negative
	// count is treated as "unknown", never as zero.
	ExecuteCountProbe(ctx context.Context, stmt Statement) (int64, error)
}

// ErrCountProbeUnsupported is the shared sentinel every wrapper's
// ExecuteCountProbe fail-closed path reports (wrapping its own specific
// error) when the inner executor lacks the CountProbeExecutor capability.
// Guards map it with errors.Is to the probe_unsupported outcome, so an
// operator triaging WARNs can tell a permanent missing seam from a
// transient probe failure; both proceed identically.
var ErrCountProbeUnsupported = errors.New("executor chain does not support ExecuteCountProbe")

// CountProbeIntValue converts the Bolt integer types a COUNT query can
// return. Neo4j integers arrive as int64; the float64 and int cases keep
// the seam honest against drivers that widen or narrow the value. It lives
// here (not in the cmd wirings) so the projector and ingester executors
// share one implementation instead of drifting copies.
func CountProbeIntValue(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case float64:
		return int64(n), true
	default:
		return 0, false
	}
}
