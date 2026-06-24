// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package accuracygate

import (
	"fmt"
	"sort"
	"strings"
)

// schemaVersion is the on-disk contract version for a checked-in accuracy
// baseline. Bump it only with a documented migration of every baseline file.
const schemaVersion = "accuracy_golden_gate.v1"

// Dimension names the three accuracy axes the continuous golden gate measures.
// They are stable JSON keys: published metrics and baselines index by them, so
// renaming a dimension is a breaking baseline change.
type Dimension string

const (
	// DimensionComplexity covers per-language cyclomatic complexity correctness:
	// real McCabe values instead of a fabricated constant.
	DimensionComplexity Dimension = "complexity"
	// DimensionResolvers covers cross-repo call-edge precision/recall and
	// resolver language coverage.
	DimensionResolvers Dimension = "resolvers"
	// DimensionCorrelation covers admission-decision precision/recall against the
	// golden correlation suite.
	DimensionCorrelation Dimension = "correlation"
)

// orderedDimensions is the deterministic dimension order used for report and
// failure rendering so output never depends on map iteration order.
var orderedDimensions = []Dimension{
	DimensionComplexity,
	DimensionResolvers,
	DimensionCorrelation,
}

// Metric is one measured accuracy data point for a dimension. Precision and
// recall are in [0,1]; CoveredItems counts the languages or evidence kinds the
// dimension scored at measurement time (e.g. complexity-scored languages or
// resolver-covered languages). Labels carries per-language / per-evidence-kind
// detail for the published report and is not gated directly.
type Metric struct {
	Precision    float64           `json:"precision"`
	Recall       float64           `json:"recall"`
	CoveredItems int               `json:"covered_items"`
	Labels       map[string]string `json:"labels,omitempty"`
}

// Threshold is the regression floor for one dimension. A measured metric passes
// only when precision and recall meet or exceed the floor AND CoveredItems is at
// least MinCoveredItems. The floors are minimums, not equalities: accuracy may
// improve freely, but a drop below the published floor fails the gate.
type Threshold struct {
	MinPrecision    float64 `json:"min_precision"`
	MinRecall       float64 `json:"min_recall"`
	MinCoveredItems int     `json:"min_covered_items"`
}

// Baseline is the checked-in published accuracy contract. It is the source of
// truth a reviewer reads to see what accuracy the repo guarantees, and the floor
// the gate enforces. Tracking this file in git is how per-dimension metrics are
// "tracked over time".
type Baseline struct {
	SchemaVersion string                  `json:"schema_version"`
	Thresholds    map[Dimension]Threshold `json:"thresholds"`
}

// Measurement bundles the live metrics for all three dimensions, produced by the
// gate's test from the real complexity, resolver, and correlation harnesses.
type Measurement struct {
	Metrics map[Dimension]Metric
}

// DimensionResult is the gate outcome for a single dimension.
type DimensionResult struct {
	Dimension Dimension
	Metric    Metric
	Threshold Threshold
	Pass      bool
	Reason    string
}

// GateResult is the aggregate outcome across all three dimensions plus the
// per-dimension breakdown, ordered deterministically.
type GateResult struct {
	Results []DimensionResult
}

// Pass reports whether every measured dimension met its regression floor.
func (g GateResult) Pass() bool {
	for _, result := range g.Results {
		if !result.Pass {
			return false
		}
	}
	return true
}

// Summary returns a stable one-line per-dimension summary for CI logs and test
// failures. Dimensions render in orderedDimensions order.
func (g GateResult) Summary() string {
	parts := make([]string, 0, len(g.Results))
	for _, result := range g.Results {
		parts = append(parts, fmt.Sprintf(
			"%s{precision=%.3f recall=%.3f covered=%d pass=%t}",
			result.Dimension,
			result.Metric.Precision,
			result.Metric.Recall,
			result.Metric.CoveredItems,
			result.Pass,
		))
	}
	return strings.Join(parts, " ")
}

// FailureMessage returns a bounded, debuggable block naming each dimension that
// fell below its floor with measured vs required values. It is empty when the
// gate passes, so a test can fail with exactly the regressions and nothing else.
func (g GateResult) FailureMessage() string {
	var b strings.Builder
	for _, result := range g.Results {
		if result.Pass {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s: %s", result.Dimension, result.Reason)
	}
	return b.String()
}

// Evaluate scores a measurement against a baseline and returns the aggregate
// gate result. A dimension that has a threshold but no measured metric fails as
// a missing measurement, so deleting a measurement cannot silently disable a
// floor. A measured dimension absent from the baseline is ignored for gating but
// still reported, so new dimensions surface before they are enforced.
//
// Evaluate performs comparison only: it never mutates the baseline or the
// measurement, and it uses plain >= with no epsilon, matching goldenaudit's
// exact-ratio comparison convention.
func Evaluate(baseline Baseline, measurement Measurement) GateResult {
	result := GateResult{Results: make([]DimensionResult, 0, len(orderedDimensions))}
	seen := make(map[Dimension]struct{}, len(orderedDimensions))

	for _, dimension := range orderedDimensions {
		threshold, gated := baseline.Thresholds[dimension]
		metric, measured := measurement.Metrics[dimension]
		if !gated && !measured {
			continue
		}
		seen[dimension] = struct{}{}
		result.Results = append(result.Results, evaluateDimension(dimension, threshold, gated, metric, measured))
	}

	// Surface any baseline or measured dimension outside the known order so a
	// typo'd dimension key never silently disappears.
	extra := extraDimensions(baseline, measurement, seen)
	for _, dimension := range extra {
		threshold, gated := baseline.Thresholds[dimension]
		metric, measured := measurement.Metrics[dimension]
		result.Results = append(result.Results, evaluateDimension(dimension, threshold, gated, metric, measured))
	}
	return result
}

// evaluateDimension applies one threshold to one metric, producing a pass/fail
// with a stable reason string.
func evaluateDimension(
	dimension Dimension,
	threshold Threshold,
	gated bool,
	metric Metric,
	measured bool,
) DimensionResult {
	out := DimensionResult{
		Dimension: dimension,
		Metric:    metric,
		Threshold: threshold,
	}
	switch {
	case gated && !measured:
		out.Pass = false
		out.Reason = "no measured metric for gated dimension"
	case !gated && measured:
		out.Pass = true
		out.Reason = "measured but not gated by baseline"
	default:
		out.Pass, out.Reason = meetsThreshold(metric, threshold)
	}
	return out
}

// meetsThreshold returns whether a metric clears its floor plus a reason string
// listing every floor it missed.
func meetsThreshold(metric Metric, threshold Threshold) (bool, string) {
	var failures []string
	if metric.Precision < threshold.MinPrecision {
		failures = append(failures, fmt.Sprintf(
			"precision=%.3f below min %.3f", metric.Precision, threshold.MinPrecision,
		))
	}
	if metric.Recall < threshold.MinRecall {
		failures = append(failures, fmt.Sprintf(
			"recall=%.3f below min %.3f", metric.Recall, threshold.MinRecall,
		))
	}
	if metric.CoveredItems < threshold.MinCoveredItems {
		failures = append(failures, fmt.Sprintf(
			"covered=%d below min %d", metric.CoveredItems, threshold.MinCoveredItems,
		))
	}
	if len(failures) == 0 {
		return true, "ok"
	}
	return false, strings.Join(failures, "; ")
}

// extraDimensions returns dimensions present in the baseline or measurement but
// not in orderedDimensions, sorted for deterministic output.
func extraDimensions(baseline Baseline, measurement Measurement, seen map[Dimension]struct{}) []Dimension {
	extras := make(map[Dimension]struct{})
	for dimension := range baseline.Thresholds {
		if _, ok := seen[dimension]; !ok {
			extras[dimension] = struct{}{}
		}
	}
	for dimension := range measurement.Metrics {
		if _, ok := seen[dimension]; !ok {
			extras[dimension] = struct{}{}
		}
	}
	ordered := make([]Dimension, 0, len(extras))
	for dimension := range extras {
		ordered = append(ordered, dimension)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	return ordered
}
