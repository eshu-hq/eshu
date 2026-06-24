// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package accuracygate is the continuous golden accuracy gate that fails CI on
// regressions across three measured dimensions: cyclomatic complexity
// correctness, cross-repo call-edge precision/recall plus resolver coverage, and
// correlation admission precision/recall.
//
// The package owns only aggregation, scoring rollup, the checked-in baseline
// contract, the threshold gate, and the published metrics snapshot. It does not
// parse source, resolve calls, or run the reducer; the gate's test feeds it real
// measurements taken from the parser, resolutionparity, and admissionaudit
// harnesses, so the gate measures shipped behavior rather than re-asserting
// hand-written numbers.
//
// A Baseline holds a per-dimension Threshold floor and is loaded from a tracked
// JSON file (LoadBaseline). Evaluate scores a Measurement against the baseline
// and returns a GateResult whose Pass, Summary, and FailureMessage drive a Go
// test and a CI verify script. Floors are minimums: accuracy may improve freely,
// but a drop below the published floor or a missing measurement fails the gate.
//
// ScorePredictions and CoverageMetric build dimension Metrics from a confusion
// matrix or a scored-item count using the same div-by-zero convention as
// goldenaudit (an empty denominator scores 1.0 only when its counterpart is also
// empty). Publish renders a deterministic PublishedMetrics snapshot whose git
// history is how per-dimension metrics are tracked over time.
package accuracygate
