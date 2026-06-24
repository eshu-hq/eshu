// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Claim status values are bounded enums for the claim lifecycle counter. They
// are safe as telemetry labels.
const (
	// ClaimStatusStarted marks a claim the collector began.
	ClaimStatusStarted = "started"
	// ClaimStatusSucceeded marks a claim that completed with full coverage.
	ClaimStatusSucceeded = "succeeded"
	// ClaimStatusPartial marks a claim that completed with durable warning
	// evidence for incomplete coverage.
	ClaimStatusPartial = "partial"
	// ClaimStatusFailed marks a claim that failed before producing complete
	// evidence.
	ClaimStatusFailed = "failed"
	// ClaimStatusRetried marks a claim retry attempt.
	ClaimStatusRetried = "retried"
)

// Metrics holds the scoped OTEL instruments for the GCP cloud collector. Every
// label is a bounded enum: collector kind, claim status, CAI operation, parent
// scope kind, asset family, content family, status class, fact kind, warning
// kind, and outcome. No instrument ever carries a full resource name, project
// id, label value, IAM member, URL, or credential name.
type Metrics struct {
	claims        metric.Int64Counter
	apiCalls      metric.Int64Counter
	pages         metric.Int64Counter
	pageResumes   metric.Int64Counter
	factsEmitted  metric.Int64Counter
	warnings      metric.Int64Counter
	freshnessLag  metric.Float64Histogram
	collectorKind string
}

// NewMetrics registers the GCP cloud collector instruments on a meter. It
// returns an error when the meter is nil or instrument registration fails.
func NewMetrics(meter metric.Meter) (*Metrics, error) {
	if meter == nil {
		return nil, errors.New("meter is required")
	}
	claims, err := meter.Int64Counter(
		"eshu_dp_gcp_cloud_claims_total",
		metric.WithDescription("GCP cloud collector claim lifecycle events by status"),
	)
	if err != nil {
		return nil, err
	}
	apiCalls, err := meter.Int64Counter(
		"eshu_dp_gcp_cloud_api_calls_total",
		metric.WithDescription("GCP Cloud Asset Inventory API calls by operation, scope kind, family, and status class"),
	)
	if err != nil {
		return nil, err
	}
	pages, err := meter.Int64Counter(
		"eshu_dp_gcp_cloud_pages_total",
		metric.WithDescription("GCP cloud collector response pages observed by parent scope kind"),
	)
	if err != nil {
		return nil, err
	}
	pageResumes, err := meter.Int64Counter(
		"eshu_dp_gcp_cloud_page_token_resumes_total",
		metric.WithDescription("GCP cloud collector continuation-token resumes by parent scope kind"),
	)
	if err != nil {
		return nil, err
	}
	factsEmitted, err := meter.Int64Counter(
		"eshu_dp_gcp_cloud_facts_emitted_total",
		metric.WithDescription("GCP cloud collector facts emitted by fact kind and parent scope kind"),
	)
	if err != nil {
		return nil, err
	}
	warnings, err := meter.Int64Counter(
		"eshu_dp_gcp_cloud_warnings_total",
		metric.WithDescription("GCP cloud collector partial, unsupported, stale, quota, and redaction warnings by kind and outcome"),
	)
	if err != nil {
		return nil, err
	}
	freshnessLag, err := meter.Float64Histogram(
		"eshu_dp_gcp_cloud_freshness_lag_seconds",
		metric.WithDescription("GCP cloud collector freshness lag from provider update/read time to Eshu observation time"),
	)
	if err != nil {
		return nil, err
	}
	return &Metrics{
		claims:        claims,
		apiCalls:      apiCalls,
		pages:         pages,
		pageResumes:   pageResumes,
		factsEmitted:  factsEmitted,
		warnings:      warnings,
		freshnessLag:  freshnessLag,
		collectorKind: CollectorKind,
	}, nil
}

// RecordClaim records one claim lifecycle event by bounded status.
func (m *Metrics) RecordClaim(ctx context.Context, status string) {
	m.claims.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrCollectorKind(m.collectorKind),
		telemetry.AttrStatus(status),
	))
}

// RecordAPICall records one Cloud Asset Inventory API call. operation, scope
// kind, asset family, content family, and statusClass must all be bounded enums.
func (m *Metrics) RecordAPICall(ctx context.Context, operation string, scope ParentScopeKind, assetFamily, contentFamily, statusClass string) {
	m.apiCalls.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrCollectorKind(m.collectorKind),
		telemetry.AttrOperation(operation),
		telemetry.AttrScopeKind(string(scope)),
		telemetry.AttrKind(assetFamily),
		telemetry.AttrResourceScope(contentFamily),
		telemetry.AttrStatusClass(statusClass),
	))
}

// RecordPage records one observed response page for a parent scope kind.
func (m *Metrics) RecordPage(ctx context.Context, scope ParentScopeKind) {
	m.pages.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrCollectorKind(m.collectorKind),
		telemetry.AttrScopeKind(string(scope)),
	))
}

// RecordPageTokenResume records one continuation-token resume for a parent scope
// kind.
func (m *Metrics) RecordPageTokenResume(ctx context.Context, scope ParentScopeKind) {
	m.pageResumes.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrCollectorKind(m.collectorKind),
		telemetry.AttrScopeKind(string(scope)),
	))
}

// RecordFactsEmitted records facts emitted for a fact kind and parent scope kind.
func (m *Metrics) RecordFactsEmitted(ctx context.Context, factKind string, scope ParentScopeKind, count int) {
	if count <= 0 {
		return
	}
	m.factsEmitted.Add(ctx, int64(count), metric.WithAttributes(
		telemetry.AttrCollectorKind(m.collectorKind),
		telemetry.AttrFactKind(factKind),
		telemetry.AttrScopeKind(string(scope)),
	))
}

// RecordWarning records one collection warning by bounded kind and outcome.
func (m *Metrics) RecordWarning(ctx context.Context, warningKind, outcome string) {
	m.warnings.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrCollectorKind(m.collectorKind),
		telemetry.AttrKind(warningKind),
		telemetry.AttrOutcome(outcome),
	))
}

// RecordFreshnessLag records the freshness lag in seconds for a parent scope
// kind. Negative lags are ignored to keep the histogram meaningful.
func (m *Metrics) RecordFreshnessLag(ctx context.Context, scope ParentScopeKind, seconds float64) {
	if seconds < 0 {
		return
	}
	m.freshnessLag.Record(ctx, seconds, metric.WithAttributes(
		telemetry.AttrCollectorKind(m.collectorKind),
		telemetry.AttrScopeKind(string(scope)),
	))
}
