// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import "go.opentelemetry.io/otel/attribute"

const (
	// MetricDimensionSourceTool labels graph-provenance metrics with the bounded
	// source-tool token that wrote an edge. The token set is closed by the
	// canonical vocabulary in go/internal/sourcetool; values outside that set are
	// coerced to "unknown" before reaching the metric label to prevent cardinality
	// explosion.
	MetricDimensionSourceTool = "source_tool"
)

// AttrSourceTool returns a source_tool attribute for graph-provenance metrics.
func AttrSourceTool(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionSourceTool, v)
}

func init() {
	metricDimensionKeys = append(metricDimensionKeys, MetricDimensionSourceTool)
}
