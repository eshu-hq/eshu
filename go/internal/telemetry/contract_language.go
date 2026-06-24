// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import "go.opentelemetry.io/otel/attribute"

const (
	// MetricDimensionLanguage labels parser and SCIP metrics with the bounded
	// source language selected by Eshu's parser registry, never file paths or
	// repository identifiers.
	MetricDimensionLanguage = "language"
)

// AttrLanguage returns a language attribute for parser and SCIP metrics.
func AttrLanguage(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionLanguage, v)
}
