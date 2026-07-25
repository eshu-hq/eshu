// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitlabciruntime

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
// of source.go to keep that file under the repo's 500-line cap, mirroring
// ghactionsruntime/source_telemetry.go. It reuses the SAME shared
// eshu_dp_ci_cd_run_* instruments GitHub Actions records to, labeled with
// provider=gitlab_ci instead of provider=github_actions -- see
// docs/public/observability/telemetry-coverage.md.

func (s ClaimedSource) startObserve(ctx context.Context) (context.Context, trace.Span) {
	if s.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return s.tracer.Start(ctx, telemetry.SpanCICDRunObserve, trace.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, string(cicdrun.ProviderGitLabCI)),
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
		telemetry.AttrProvider(string(cicdrun.ProviderGitLabCI)),
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
		telemetry.AttrProvider(string(cicdrun.ProviderGitLabCI)),
	))
}

func (s ClaimedSource) recordFacts(ctx context.Context, envelopes []facts.Envelope) {
	if s.instruments == nil {
		return
	}
	for _, envelope := range envelopes {
		s.instruments.CICDRunFactsEmitted.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrProvider(string(cicdrun.ProviderGitLabCI)),
			telemetry.AttrFactKind(envelope.FactKind),
		))
	}
}

// recordPartialGeneration reports two independent partial-generation
// reasons across the whole fetched page: jobs_truncated (summed across every
// pipeline in the window whose jobs page was itself truncated) and
// pipelines_truncated (one signal per generation when the pipeline-list page
// itself was full), mirroring ghactionsruntime's jobs_truncated/runs_truncated
// pair. GitLab's Jobs API reports artifacts inline on the job with no
// separate pagination, so there is no artifacts_truncated counterpart, and
// this v1 client has no run-watermark gap detection (see doc.go), so there is
// no runs_backfill_gap counterpart either.
func (s ClaimedSource) recordPartialGeneration(ctx context.Context, page PipelinePage) {
	if s.instruments == nil {
		return
	}
	jobsPartialCount := 0
	for _, snapshot := range page.Snapshots {
		if snapshot.JobsPartial {
			jobsPartialCount++
		}
	}
	if jobsPartialCount > 0 {
		s.instruments.CICDRunPartialGenerations.Add(ctx, int64(jobsPartialCount), metric.WithAttributes(
			telemetry.AttrProvider(string(cicdrun.ProviderGitLabCI)),
			telemetry.AttrReason("jobs_truncated"),
		))
	}
	if page.Truncated {
		s.instruments.CICDRunPartialGenerations.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrProvider(string(cicdrun.ProviderGitLabCI)),
			telemetry.AttrReason("pipelines_truncated"),
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
