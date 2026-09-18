// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// recordScopedGrantDenied increments telemetry.Instruments.QueryScopedGrantDenied
// for one of this family's #6786 Go-side grant-decision seams
// (GetEntityContext, FetchWorkloadContextForOperation). h.Instruments is nil
// in many tests that construct a Handler directly; emission is skipped
// rather than panicking, the same nil-tolerant shape every other Instruments
// use in this package follows (see the Handler.Instruments field doc
// comment).
//
// operation and reason MUST both be bounded, low-cardinality values (never a
// request-scoped id, entity id, or repository id) -- see
// go/internal/telemetry's "No high-cardinality metric labels" rule.
func (h *Handler) recordScopedGrantDenied(ctx context.Context, operation, reason string) {
	if h == nil || h.Instruments == nil || h.Instruments.QueryScopedGrantDenied == nil {
		return
	}
	h.Instruments.QueryScopedGrantDenied.Add(ctx, 1,
		metric.WithAttributes(telemetry.AttrOperation(operation), telemetry.AttrReason(reason)),
	)
}
