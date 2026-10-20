// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
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
