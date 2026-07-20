// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// This file holds NextClaimed's tracing/metrics recording helpers, split out
// of source.go to keep that file under the repo's 500-line cap.

func (s ClaimedSource) startObserve(ctx context.Context) (context.Context, trace.Span) {
	if s.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return s.tracer.Start(ctx, telemetry.SpanCICDRunObserve, trace.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, string(cicdrun.ProviderGitHubActions)),
	))
}

func (s ClaimedSource) startFetch(ctx context.Context) (context.Context, trace.Span) {
	if s.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return s.tracer.Start(ctx, telemetry.SpanCICDRunFetch)
}

func classifyProviderStatus(err error) string {
	if errors.Is(err, ErrRateLimited) {
		return "rate_limited"
	}
	return "error"
}

func (s ClaimedSource) recordFetch(ctx context.Context, statusClass string, startedAt time.Time) {
	if s.instruments == nil {
		return
	}
	attrs := []attribute.KeyValue{
		telemetry.AttrProvider(string(cicdrun.ProviderGitHubActions)),
		telemetry.AttrStatusClass(statusClass),
	}
	s.instruments.CICDRunProviderRequests.Add(ctx, 1, metric.WithAttributes(attrs...))
	s.instruments.CICDRunFetchDuration.Record(ctx, time.Since(startedAt).Seconds(), metric.WithAttributes(attrs...))
}

func (s ClaimedSource) recordRateLimit(ctx context.Context, statusClass string) {
	if s.instruments == nil || statusClass != "rate_limited" {
		return
	}
	s.instruments.CICDRunRateLimited.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrProvider(string(cicdrun.ProviderGitHubActions)),
	))
}

func (s ClaimedSource) recordFacts(ctx context.Context, envelopes []facts.Envelope) {
	if s.instruments == nil {
		return
	}
	for _, envelope := range envelopes {
		s.instruments.CICDRunFactsEmitted.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrProvider(string(cicdrun.ProviderGitHubActions)),
			telemetry.AttrFactKind(envelope.FactKind),
		))
	}
}

// recordPartialGeneration reports three independent partial-generation
// reasons across the whole fetched page: jobs_truncated (summed across every
// run in the window whose jobs page was itself truncated), provider_warning
// (summed across every run's own Warnings), and runs_truncated (one signal
// per generation when the runs page itself was full, mirroring the
// jobs_partial/jobs_truncated pattern at the run-window level). It reads the
// original, unmutated page rather than the runs_truncated-warning-attached
// copy buildRunEnvelopes emits, so the runs_truncated fact (counted here
// under provider_warning once it lands in a snapshot's Warnings) is not
// double-counted against the dedicated runs_truncated reason below.
func (s ClaimedSource) recordPartialGeneration(ctx context.Context, page RunPage) {
	if s.instruments == nil {
		return
	}
	jobsPartialCount := 0
	warningsCount := 0
	for _, snapshot := range page.Snapshots {
		if snapshot.JobsPartial {
			jobsPartialCount++
		}
		warningsCount += len(snapshot.Warnings)
	}
	if jobsPartialCount > 0 {
		s.instruments.CICDRunPartialGenerations.Add(ctx, int64(jobsPartialCount), metric.WithAttributes(
			telemetry.AttrProvider(string(cicdrun.ProviderGitHubActions)),
			telemetry.AttrReason("jobs_truncated"),
		))
	}
	if warningsCount > 0 {
		s.instruments.CICDRunPartialGenerations.Add(ctx, int64(warningsCount), metric.WithAttributes(
			telemetry.AttrProvider(string(cicdrun.ProviderGitHubActions)),
			telemetry.AttrReason("provider_warning"),
		))
	}
	if page.Truncated {
		s.instruments.CICDRunPartialGenerations.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrProvider(string(cicdrun.ProviderGitHubActions)),
			telemetry.AttrReason("runs_truncated"),
		))
	}
}

func recordSpanError(span trace.Span, err error) {
	if span == nil || err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
