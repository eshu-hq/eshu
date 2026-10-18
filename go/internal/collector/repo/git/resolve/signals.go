// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package resolve

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The metric helpers read the late-bound instruments and no-op until Bind
// runs, mirroring the deadline package: an unwired cache (tests, tooling)
// still resolves and pins, it just counts nothing. The host label is the
// provider host only, never a URL or repository identity.

func (c *Cache) recordLookup(ctx context.Context, host, result string, elapsed time.Duration) {
	instruments := c.instruments.Load()
	if instruments == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionHost, host),
		attribute.String(telemetry.MetricDimensionResult, result),
	)
	if instruments.GitResolveCacheLookups != nil {
		instruments.GitResolveCacheLookups.Add(ctx, 1, attrs)
	}
	if instruments.GitResolveCacheLookupDuration != nil {
		instruments.GitResolveCacheLookupDuration.Record(ctx, elapsed.Seconds(), attrs)
	}
}

func (c *Cache) recordRefreshRequest(ctx context.Context, host, reason string) {
	instruments := c.instruments.Load()
	if instruments == nil || instruments.GitResolveCacheRefreshRequests == nil {
		return
	}
	instruments.GitResolveCacheRefreshRequests.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionHost, host),
		attribute.String(telemetry.MetricDimensionReason, reason),
	))
}

func (c *Cache) recordFallback(ctx context.Context, host, reason string) {
	instruments := c.instruments.Load()
	if instruments == nil || instruments.GitResolveCacheFallback == nil {
		return
	}
	instruments.GitResolveCacheFallback.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionHost, host),
		attribute.String(telemetry.MetricDimensionReason, reason),
	))
}
