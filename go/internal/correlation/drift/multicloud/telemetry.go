// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package multicloud

import (
	"context"
	"sort"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Summary captures admitted multi-cloud runtime drift finding counts. The
// counters mirror the AWS summary so one operator vocabulary spans providers.
type Summary struct {
	OrphanedResources  int
	UnmanagedResources int
	AmbiguousResources int
	UnknownResources   int
	// ImageVersionDriftResources counts admitted image_version_drift findings
	// (#5453), mirroring cloudruntime.Summary. No dedicated counter: every
	// admitted result already increments eshu_dp_correlation_rule_matches_total
	// via recordRuleMatches below.
	ImageVersionDriftResources int
}

// RecordEvaluation emits bounded metrics for one multi-cloud runtime drift
// evaluation and returns the admitted finding counts for status callers. Metric
// labels stay bounded: only the pack name and rule name are attached, never the
// canonical uid, raw identity, provider scope, tags, or addresses.
func RecordEvaluation(ctx context.Context, instruments *telemetry.Instruments, evaluation engine.Evaluation) Summary {
	var summary Summary
	for _, result := range evaluation.Results {
		if result.Candidate.State != model.CandidateStateAdmitted {
			continue
		}
		recordRuleMatches(ctx, instruments, result)
		switch cloudruntime.FindingKind(FindingKindFromCandidate(result.Candidate)) {
		case cloudruntime.FindingKindOrphanedCloudResource:
			summary.OrphanedResources++
			recordOrphan(ctx, instruments)
		case cloudruntime.FindingKindUnmanagedCloudResource:
			summary.UnmanagedResources++
			recordUnmanaged(ctx, instruments)
		case cloudruntime.FindingKindAmbiguousCloudResource:
			summary.AmbiguousResources++
		case cloudruntime.FindingKindUnknownCloudResource:
			summary.UnknownResources++
		case cloudruntime.FindingKindImageVersionDrift:
			summary.ImageVersionDriftResources++
		}
	}
	return summary
}

func recordRuleMatches(ctx context.Context, instruments *telemetry.Instruments, result engine.Result) {
	if instruments == nil || instruments.CorrelationRuleMatches == nil {
		return
	}
	matchRuleNames := make([]string, 0, len(result.MatchCounts))
	for ruleName := range result.MatchCounts {
		matchRuleNames = append(matchRuleNames, ruleName)
	}
	sort.Strings(matchRuleNames)
	for _, ruleName := range matchRuleNames {
		count := result.MatchCounts[ruleName]
		if count <= 0 {
			continue
		}
		instruments.CorrelationRuleMatches.Add(ctx, int64(count), metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionPack, rules.MultiCloudRuntimeDriftPackName),
			attribute.String(telemetry.MetricDimensionRule, ruleName),
		))
	}
}

func admitAttrs() metric.MeasurementOption {
	return metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionPack, rules.MultiCloudRuntimeDriftPackName),
		attribute.String(telemetry.MetricDimensionRule, rules.MultiCloudRuntimeDriftRuleAdmitFinding),
	)
}

func recordOrphan(ctx context.Context, instruments *telemetry.Instruments) {
	if instruments == nil || instruments.CorrelationOrphanDetected == nil {
		return
	}
	instruments.CorrelationOrphanDetected.Add(ctx, 1, admitAttrs())
}

func recordUnmanaged(ctx context.Context, instruments *telemetry.Instruments) {
	if instruments == nil || instruments.CorrelationUnmanagedDetected == nil {
		return
	}
	instruments.CorrelationUnmanagedDetected.Add(ctx, 1, admitAttrs())
}
