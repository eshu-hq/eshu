// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
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

// recordScopeGrantInlineCapped emits the #5408 SHAPE-A inline-cap signal
// (telemetry.Instruments.QueryScopeGrantInlineCapped, reason = surface) and a
// Warn log with the grant-set sizes when a scoped token's grants overflow
// querycontract.MaxScopeGrantInlineTerms, and does nothing otherwise. Past the
// cap the scoped name read's DEFINES terms for the overflow grants are dropped,
// so a workload admitted only through one of those grants goes missing (fail
// closed). This is the only signal an operator gets for that. surface is a
// fixed, low-cardinality string chosen by the caller. Call it once per read.
func recordScopeGrantInlineCapped(ctx context.Context, logger *slog.Logger, instruments *telemetry.Instruments, access querycontract.RepositoryAccessFilter, surface string) {
	if !access.GrantInlineCapExceeded() {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	logger.WarnContext(ctx, "scoped token grant set exceeded the inline-map cap; DEFINES-collision admission truncated",
		slog.String("surface", surface),
		slog.Int("granted_repositories", len(access.AllowedRepositoryIDs)),
		slog.Int("granted_scopes", len(access.AllowedScopeIDs)),
		slog.Int("inline_term_cap", querycontract.MaxScopeGrantInlineTerms),
		slog.String("degradation", "fail_closed_missing_rows"),
	)
	if instruments != nil && instruments.QueryScopeGrantInlineCapped != nil {
		instruments.QueryScopeGrantInlineCapped.Add(ctx, 1, metric.WithAttributes(telemetry.AttrReason(surface)))
	}
}
