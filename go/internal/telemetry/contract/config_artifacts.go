// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// Span name and metric dimension of the config-artifact derive (#8155, ADR
// 7126 section 3.5). They live in this sibling file so the frozen root
// contract.go does not grow. Both are registered now and first used by
// PR3b-2c-2, which wires the derive into the content Write; nothing emits
// them before that change.
const (
	// SpanConfigArtifactsDerive wraps one per-repository config-artifact
	// derive: one span per derive, a child of the caller's span (the content
	// Write today; the reducer walker later). Its attributes may carry the
	// repository id; metrics never do. The sibling request span of the read
	// path is named config_artifacts.load in the ADR.
	SpanConfigArtifactsDerive = "config_artifacts.derive"

	// MetricDimensionTrigger labels the config-artifact derive metrics with
	// what started the derive. The set is closed: write (the content Write),
	// backfill and repair (the reducer walker).
	MetricDimensionTrigger = "trigger"
)
