// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func (s Service) recordClaimStart(ctx context.Context, input ClaimInput, item workflow.WorkItem) {
	if s.Instruments == nil {
		return
	}
	s.Instruments.ScannerWorkerClaims.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrAnalyzer(string(input.Analyzer)),
		telemetry.AttrTargetKind(string(input.Target.Kind)),
		telemetry.AttrOutcome("started"),
	))
	reference := item.VisibleAt
	if reference.IsZero() {
		reference = item.CreatedAt
	}
	if reference.IsZero() {
		return
	}
	wait := s.now().Sub(reference.UTC()).Seconds()
	if wait < 0 {
		wait = 0
	}
	s.Instruments.ScannerWorkerQueueWaitDuration.Record(ctx, wait, metric.WithAttributes(
		telemetry.AttrAnalyzer(string(input.Analyzer)),
		telemetry.AttrTargetKind(string(input.Target.Kind)),
	))
}

func (s Service) recordScanDuration(ctx context.Context, input ClaimInput, seconds float64, success bool) {
	if s.Instruments == nil {
		return
	}
	result := "failed"
	if success {
		result = "succeeded"
	}
	s.Instruments.ScannerWorkerScanDuration.Record(ctx, seconds, metric.WithAttributes(
		telemetry.AttrAnalyzer(string(input.Analyzer)),
		telemetry.AttrTargetKind(string(input.Target.Kind)),
		telemetry.AttrResult(result),
	))
}

func (s Service) recordSuccess(ctx context.Context, input ClaimInput, result AnalyzerResult) {
	if s.Instruments == nil {
		return
	}
	attrs := metric.WithAttributes(
		telemetry.AttrAnalyzer(string(input.Analyzer)),
		telemetry.AttrTargetKind(string(input.Target.Kind)),
		telemetry.AttrResult("succeeded"),
	)
	s.Instruments.ScannerWorkerClaims.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrAnalyzer(string(input.Analyzer)),
		telemetry.AttrTargetKind(string(input.Target.Kind)),
		telemetry.AttrOutcome("completed"),
	))
	s.Instruments.ScannerWorkerTargetCount.Record(ctx, int64(result.Output.TargetCount), attrs)
	s.Instruments.ScannerWorkerResultCount.Record(ctx, int64(result.Output.ResultCount), attrs)
	s.Instruments.ScannerWorkerCPUSeconds.Record(ctx, result.Usage.CPUSeconds, attrs)
	s.Instruments.ScannerWorkerMemoryBytes.Record(ctx, result.Usage.PeakMemoryBytes, attrs)
	for factKind, count := range factKindCounts(result.Output.Facts) {
		s.Instruments.ScannerWorkerFactsEmitted.Add(ctx, count, metric.WithAttributes(
			telemetry.AttrAnalyzer(string(input.Analyzer)),
			telemetry.AttrTargetKind(string(input.Target.Kind)),
			attribute.String(telemetry.MetricDimensionFactKind, factKind),
		))
	}
}

func factKindCounts(values []facts.Envelope) map[string]int64 {
	counts := make(map[string]int64)
	for _, value := range values {
		counts[value.FactKind]++
	}
	return counts
}

func (s Service) recordRetry(ctx context.Context, input ClaimInput, failureClass FailureClass) {
	if s.Instruments == nil {
		return
	}
	s.Instruments.ScannerWorkerClaims.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrAnalyzer(string(input.Analyzer)),
		telemetry.AttrTargetKind(string(input.Target.Kind)),
		telemetry.AttrOutcome("retryable"),
	))
	s.Instruments.ScannerWorkerRetries.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrAnalyzer(string(input.Analyzer)),
		telemetry.AttrTargetKind(string(input.Target.Kind)),
		telemetry.AttrFailureClass(string(failureClass)),
	))
}

func (s Service) recordDeadLetter(ctx context.Context, input ClaimInput, failureClass FailureClass) {
	if s.Instruments == nil {
		return
	}
	s.Instruments.ScannerWorkerClaims.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrAnalyzer(string(input.Analyzer)),
		telemetry.AttrTargetKind(string(input.Target.Kind)),
		telemetry.AttrOutcome("dead_letter"),
	))
	s.Instruments.ScannerWorkerDeadLetters.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrAnalyzer(string(input.Analyzer)),
		telemetry.AttrTargetKind(string(input.Target.Kind)),
		telemetry.AttrFailureClass(string(failureClass)),
	))
}
