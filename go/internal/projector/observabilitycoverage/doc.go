// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package observabilitycoverage builds the observability-coverage-correlation
// reducer intent from one immutable scope generation: when the generation
// carries any observability source fact (a declared dashboard, alert, or
// log/trace source the facts.ObservabilitySchemaVersion registry recognizes,
// excluding observability_source.instance) or any AWS-native observability
// aws_resource fact (a CloudWatch alarm, composite alarm, dashboard, logs log
// group, or X-Ray sampling rule/group), it asks the reducer's
// observability_coverage_correlation domain to correlate that coverage
// against the monitored resources it covers versus the uncovered gaps
// (issue #391). The intent anchors to the earliest trigger fact in original
// input order so the reducer claim is stable across reprojections, and its
// source-system label keeps the family's literal third-tier "observability"
// fallback for a trigger fact with no source ref and no collector kind. Root
// projector assembly owns lookup construction and lifetime, invocation order,
// queue writes, retries, and telemetry.
package observabilitycoverage
