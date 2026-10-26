// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// Closed label values for the runtime declaration read surface (#8817).
const (
	// RuntimeDeclarationOperationList is the page read.
	RuntimeDeclarationOperationList = "list"
	// RuntimeDeclarationOperationExcludedCount is the count of declarations a
	// version comparison could not place.
	RuntimeDeclarationOperationExcludedCount = "excluded_count"
	// RuntimeDeclarationOperationRepositoryNames is the display-name read for
	// the rows of a page.
	RuntimeDeclarationOperationRepositoryNames = "repository_names"
	// RuntimeDeclarationOperationContextSection is the get_repo_context
	// section read.
	RuntimeDeclarationOperationContextSection = "context_section"

	// RuntimeDeclarationOutcomeOK and RuntimeDeclarationOutcomeError close the
	// outcome label.
	RuntimeDeclarationOutcomeOK    = "ok"
	RuntimeDeclarationOutcomeError = "error"
)

// registerRuntimeDeclarationQuery registers the runtime declaration read
// surface instruments (#8817) on inst.
func registerRuntimeDeclarationQuery(meter metric.Meter, inst *Instruments) error {
	var err error
	// The buckets cover the measured warm reads (single-digit to tens of
	// milliseconds at 50,000 declarations) and the declared production
	// capability budget (1.5 s) with headroom for a cold page cache.
	buckets := []float64{0.002, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 1.5, 2.5, 5, 10}
	if inst.RuntimeDeclarationQueryDuration, err = meter.Float64Histogram(
		"eshu_dp_runtime_declaration_query_duration_seconds",
		metric.WithDescription("Graph read duration behind the runtime declaration read surface, by closed operation and outcome (#8817)"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(buckets...),
	); err != nil {
		return fmt.Errorf("register RuntimeDeclarationQueryDuration histogram: %w", err)
	}
	if inst.RuntimeDeclarationQueryExcluded, err = meter.Int64Counter(
		"eshu_dp_runtime_declaration_query_excluded_total",
		metric.WithDescription("Declarations a version comparison could not place, by closed reason (range, alias, unresolved, partial, other) (#8817)"),
		metric.WithUnit("{declaration}"),
	); err != nil {
		return fmt.Errorf("register RuntimeDeclarationQueryExcluded counter: %w", err)
	}
	return nil
}
