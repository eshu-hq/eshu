// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttrace

import (
	"context"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// recordScopedGrantDenied increments telemetry.Instruments.QueryScopedGrantDenied
// for ResolveWorkloadSelector, this family's #6786 Go-side
// grant-decision seam for the deployment-trace route. instruments is nil in
// every non-live test in this package and in any caller that has not wired
// the full telemetry stack; emission is skipped rather than panicking, the
// same nil-tolerant shape go/internal/query/entity's sibling emission seam
// (scoped_grant_telemetry.go) follows.
//
// operation and reason MUST both be bounded, low-cardinality values (never a
// request-scoped id, entity id, or repository id) -- see
// go/internal/telemetry's "No high-cardinality metric labels" rule.
func recordScopedGrantDenied(ctx context.Context, instruments *telemetry.Instruments, operation, reason string) {
	if instruments == nil || instruments.QueryScopedGrantDenied == nil {
		return
	}
	instruments.QueryScopedGrantDenied.Add(ctx, 1,
		metric.WithAttributes(telemetry.AttrOperation(operation), telemetry.AttrReason(reason)),
	)
}
