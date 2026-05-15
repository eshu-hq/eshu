package cloudruntime

import (
	"context"
	"sort"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Summary captures admitted AWS cloud-runtime drift finding counts.
type Summary struct {
	OrphanedResources  int
	UnmanagedResources int
	AmbiguousResources int
	UnknownResources   int
}

// RecordEvaluation emits bounded metrics for one AWS cloud-runtime drift
// evaluation and returns the same admitted finding counts for status callers.
func RecordEvaluation(ctx context.Context, instruments *telemetry.Instruments, evaluation engine.Evaluation) Summary {
	var summary Summary
	for _, result := range evaluation.Results {
		if result.Candidate.State != model.CandidateStateAdmitted {
			continue
		}
		recordRuleMatches(ctx, instruments, result)
		switch FindingKind(readFindingKindAtom(result.Candidate)) {
		case FindingKindOrphanedCloudResource:
			summary.OrphanedResources++
			recordFindingCounter(ctx, instruments, instrumentsCounterOrphan)
		case FindingKindUnmanagedCloudResource:
			summary.UnmanagedResources++
			recordFindingCounter(ctx, instruments, instrumentsCounterUnmanaged)
		case FindingKindAmbiguousCloudResource:
			summary.AmbiguousResources++
		case FindingKindUnknownCloudResource:
			summary.UnknownResources++
		}
	}
	return summary
}

type instrumentsCounterKind int

const (
	instrumentsCounterOrphan instrumentsCounterKind = iota
	instrumentsCounterUnmanaged
)

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
			attribute.String(telemetry.MetricDimensionPack, rules.AWSCloudRuntimeDriftPackName),
			attribute.String(telemetry.MetricDimensionRule, ruleName),
		))
	}
}

func recordFindingCounter(ctx context.Context, instruments *telemetry.Instruments, kind instrumentsCounterKind) {
	if instruments == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionPack, rules.AWSCloudRuntimeDriftPackName),
		attribute.String(telemetry.MetricDimensionRule, rules.AWSCloudRuntimeDriftRuleAdmitFinding),
	)
	switch kind {
	case instrumentsCounterOrphan:
		if instruments.CorrelationOrphanDetected != nil {
			instruments.CorrelationOrphanDetected.Add(ctx, 1, attrs)
		}
	case instrumentsCounterUnmanaged:
		if instruments.CorrelationUnmanagedDetected != nil {
			instruments.CorrelationUnmanagedDetected.Add(ctx, 1, attrs)
		}
	}
}

func readFindingKindAtom(candidate model.Candidate) string {
	for _, atom := range candidate.Evidence {
		if atom.EvidenceType == EvidenceTypeFindingKind {
			return atom.Value
		}
	}
	return ""
}
