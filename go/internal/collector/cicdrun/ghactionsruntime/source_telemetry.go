// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"context"
	"errors"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun"
	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/github/app"
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
	var providerFailure sdk.ProviderFailure
	if errors.As(err, &providerFailure) && providerFailure.FailureClass() == string(sdk.FailureAuthDenied) {
		return string(sdk.FailureAuthDenied)
	}
	var retryable *app.RetryableError
	if errors.As(err, &retryable) && retryable.Class == app.ClassRateLimited {
		return "rate_limited"
	}
	var denied *app.AuthDeniedError
	var missing *app.NotFoundError
	var config *app.ConfigError
	if errors.As(err, &denied) || errors.As(err, &missing) || errors.As(err, &config) {
		return "auth_error"
	}
	var expired appReadExpiredError
	if errors.As(err, &expired) {
		return "auth_error"
	}
	return "error"
}

// classifyProviderStatusForMode marks direct-App provider permission failures
// while preserving the static token path's original generic error class.
func classifyProviderStatusForMode(err error, appMode bool) string {
	class := classifyProviderStatus(err)
	if class != "error" || !appMode {
		return class
	}
	var provider sdk.HTTPError
	if errors.As(err, &provider) && (provider.StatusCode == http.StatusUnauthorized ||
		provider.StatusCode == http.StatusForbidden || provider.StatusCode == http.StatusNotFound) {
		return "auth_error"
	}
	return class
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

// recordPartialGeneration reports six independent partial-generation
// reasons across the whole fetched page: jobs_truncated (summed across every
// run in the window whose jobs page was itself truncated), artifacts_truncated
// (summed across every run in the window whose artifact page was itself
// truncated, mirroring jobs_truncated), provider_warning (summed across every
// run's own Warnings), runs_truncated (one signal per generation when the
// runs page itself was full, mirroring the jobs_partial/jobs_truncated
// pattern at the run-window level), runs_backfill_gap (one signal per
// generation when detectRunBackfillGap found that runs between a prior
// cycle's watermark and this cycle's window floor were never fetched by
// either cycle, #5429), and deployments_truncated (one signal per generation
// when the GitHub Deployments API window was full, #5425 STEP 3, mirroring
// runs_truncated at the deployment-window level). It reads the original,
// unmutated page rather than the warning-attached copy buildRunEnvelopes
// emits, so the runs_truncated and runs_backfill_gap facts (counted here
// under provider_warning once they land in a snapshot's Warnings) are not
// double-counted against their own dedicated reasons below.
func (s ClaimedSource) recordPartialGeneration(ctx context.Context, page RunPage, gapDetected bool, deploymentsTruncated bool) {
	if s.instruments == nil {
		return
	}
	jobsPartialCount := 0
	artifactsPartialCount := 0
	warningsCount := 0
	for _, snapshot := range page.Snapshots {
		if snapshot.JobsPartial {
			jobsPartialCount++
		}
		if snapshot.ArtifactsPartial {
			artifactsPartialCount++
		}
		warningsCount += len(snapshot.Warnings)
	}
	if jobsPartialCount > 0 {
		s.instruments.CICDRunPartialGenerations.Add(ctx, int64(jobsPartialCount), metric.WithAttributes(
			telemetry.AttrProvider(string(cicdrun.ProviderGitHubActions)),
			telemetry.AttrReason("jobs_truncated"),
		))
	}
	if artifactsPartialCount > 0 {
		s.instruments.CICDRunPartialGenerations.Add(ctx, int64(artifactsPartialCount), metric.WithAttributes(
			telemetry.AttrProvider(string(cicdrun.ProviderGitHubActions)),
			telemetry.AttrReason("artifacts_truncated"),
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
	if gapDetected {
		s.instruments.CICDRunPartialGenerations.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrProvider(string(cicdrun.ProviderGitHubActions)),
			telemetry.AttrReason("runs_backfill_gap"),
		))
	}
	if deploymentsTruncated {
		s.instruments.CICDRunPartialGenerations.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrProvider(string(cicdrun.ProviderGitHubActions)),
			telemetry.AttrReason("deployments_truncated"),
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
