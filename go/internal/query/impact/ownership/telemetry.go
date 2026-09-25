// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ownership

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Closed reason values for RecordWithheld. Every value is a fixed string;
// never pass an id, name, or request-derived value.
const (
	// ReasonUngrantedNode: a path crossed a node the grant does not own.
	ReasonUngrantedNode = "ungranted_node"
	// ReasonUncheckedOverCap: a path crossed a key left unchecked past
	// CheckedKeyCap.
	ReasonUncheckedOverCap = "unchecked_over_cap"
	// ReasonWithheldSinkClass: an exposure path ended on a sink class that
	// has no owner a grant can bind (SecretsIAMSecretMetadataPath, CidrBlock).
	ReasonWithheldSinkClass = "withheld_sink_class"
	// ReasonAnchorUngranted: the resolved anchor (or an explain endpoint) is
	// not owned by the grant, so the whole answer renders as not found.
	ReasonAnchorUngranted = "anchor_ungranted"
)

// RecordWithheld adds n to eshu_dp_query_impact_scoped_paths_withheld_total
// for route and reason. A zero n or nil instruments records nothing.
func RecordWithheld(ctx context.Context, instruments *telemetry.Instruments, route, reason string, n int) {
	if n <= 0 || instruments == nil || instruments.QueryImpactScopedPathsWithheld == nil {
		return
	}
	instruments.QueryImpactScopedPathsWithheld.Add(ctx, int64(n),
		metric.WithAttributes(telemetry.AttrRoute(route), telemetry.AttrReason(reason)))
}

// recordDuration observes one ownership statement's wall time, labelled by
// route, owned node label, and outcome (ok or error).
func (c Checker) recordDuration(ctx context.Context, class Class, elapsed time.Duration, err error) {
	if c.Instruments == nil || c.Instruments.QueryImpactOwnershipCheckDuration == nil {
		return
	}
	outcome := "ok"
	if err != nil {
		outcome = "error"
	}
	c.Instruments.QueryImpactOwnershipCheckDuration.Record(ctx, elapsed.Seconds(),
		metric.WithAttributes(
			telemetry.AttrRoute(c.Route),
			telemetry.AttrNodeLabel(classMetricLabel(class)),
			telemetry.AttrOutcome(outcome),
		))
}

// logCapped warns once per request that the page held more statement-checked
// keys than the budget allows, so the tail was treated as ungranted.
func (c Checker) logCapped(ctx context.Context, grantSize, limit int) {
	logger := c.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.WarnContext(ctx, "impact ownership check capped; unchecked nodes treated as ungranted",
		slog.String("route", c.Route),
		slog.Int("grant_size", grantSize),
		slog.Int("checked_key_cap", limit),
		slog.String("degradation", "fail_closed_truncated"),
	)
}
