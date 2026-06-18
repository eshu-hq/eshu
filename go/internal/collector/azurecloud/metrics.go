package azurecloud

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Status classes bound the API-call outcome label. They are coarse enums, never
// raw HTTP codes or provider error strings.
const (
	// StatusClassSuccess marks a successful provider read.
	StatusClassSuccess = "success"
	// StatusClassThrottled marks a throttled provider read.
	StatusClassThrottled = "throttled"
	// StatusClassError marks a failed provider read.
	StatusClassError = "error"
)

// Claim status values are bounded enums for the claim lifecycle counter. They
// are safe as telemetry labels and mirror the GCP cloud collector claim states.
const (
	// ClaimStatusSucceeded marks a claim that committed with full coverage.
	ClaimStatusSucceeded = "succeeded"
	// ClaimStatusPartial marks a claim that committed with durable partial-scope
	// warning evidence.
	ClaimStatusPartial = "partial"
	// ClaimStatusFailed marks a claim that failed before committing complete
	// evidence.
	ClaimStatusFailed = "failed"
)

// metricSourceLaneKey is the bounded source_lane metric dimension. The Azure
// collector owns it because the shared telemetry package has no source_lane
// helper yet; the value is always a SourceLane* enum.
const metricSourceLaneKey = "source_lane"

// Metrics records bounded Azure collector telemetry. Every label is a bounded
// enum (collector kind, scope kind, source lane, operation, status class, fact
// kind, warning reason). Implementations must never accept ARM IDs,
// subscription IDs, tenant IDs, resource group or resource names, locations,
// tags, KQL text, URLs, or credential names as labels.
type Metrics interface {
	// RecordAPICall records one Resource Graph or ARM call by operation and
	// status class.
	RecordAPICall(ctx context.Context, boundary Boundary, operation, statusClass string)
	// RecordSkipTokenResume records one $skipToken continuation fetch.
	RecordSkipTokenResume(ctx context.Context, boundary Boundary)
	// RecordPartialScope records one partial-scope outcome by bounded reason.
	RecordPartialScope(ctx context.Context, boundary Boundary, reason string)
	// RecordFactsEmitted records facts emitted by fact kind.
	RecordFactsEmitted(ctx context.Context, boundary Boundary, factKind string, count int)
	// RecordFreshnessLag records provider-to-Eshu freshness lag in seconds.
	RecordFreshnessLag(ctx context.Context, boundary Boundary, lagSeconds float64)
	// RecordClaim records one claim lifecycle outcome by bounded status enum. It
	// carries collector kind and status only, never scope, credential, or
	// provider identity.
	RecordClaim(ctx context.Context, status string)
}

// NopMetrics is a Metrics that records nothing. It lets callers run the
// collector without an OTel meter.
type NopMetrics struct{}

// RecordAPICall does nothing.
func (NopMetrics) RecordAPICall(context.Context, Boundary, string, string) {}

// RecordSkipTokenResume does nothing.
func (NopMetrics) RecordSkipTokenResume(context.Context, Boundary) {}

// RecordPartialScope does nothing.
func (NopMetrics) RecordPartialScope(context.Context, Boundary, string) {}

// RecordFactsEmitted does nothing.
func (NopMetrics) RecordFactsEmitted(context.Context, Boundary, string, int) {}

// RecordFreshnessLag does nothing.
func (NopMetrics) RecordFreshnessLag(context.Context, Boundary, float64) {}

// RecordClaim does nothing.
func (NopMetrics) RecordClaim(context.Context, string) {}

type otelMetrics struct {
	apiCalls     metric.Int64Counter
	skipResumes  metric.Int64Counter
	partialScope metric.Int64Counter
	factsEmitted metric.Int64Counter
	freshnessLag metric.Float64Histogram
	claims       metric.Int64Counter
}

// NewMetrics registers the Azure collector OTel instruments. The returned
// Metrics records bounded-label series only. A nil meter is rejected.
func NewMetrics(meter metric.Meter) (Metrics, error) {
	if meter == nil {
		return nil, errors.New("meter is required")
	}
	apiCalls, err := meter.Int64Counter(
		"eshu_dp_azure_api_calls_total",
		metric.WithDescription("Total Azure Resource Graph and ARM calls by collector kind, scope kind, source lane, operation, and status class"),
	)
	if err != nil {
		return nil, err
	}
	skipResumes, err := meter.Int64Counter(
		"eshu_dp_azure_skip_token_resumes_total",
		metric.WithDescription("Total Azure Resource Graph $skipToken continuation fetches by collector kind, scope kind, and source lane"),
	)
	if err != nil {
		return nil, err
	}
	partialScope, err := meter.Int64Counter(
		"eshu_dp_azure_partial_scope_total",
		metric.WithDescription("Total Azure partial-scope outcomes by collector kind, scope kind, source lane, and bounded reason"),
	)
	if err != nil {
		return nil, err
	}
	factsEmitted, err := meter.Int64Counter(
		"eshu_dp_azure_facts_emitted_total",
		metric.WithDescription("Total Azure facts emitted by collector kind, scope kind, source lane, and fact kind"),
	)
	if err != nil {
		return nil, err
	}
	freshnessLag, err := meter.Float64Histogram(
		"eshu_dp_azure_freshness_lag_seconds",
		metric.WithDescription("Azure provider-to-Eshu freshness lag in seconds by collector kind, scope kind, and source lane"),
	)
	if err != nil {
		return nil, err
	}
	claims, err := meter.Int64Counter(
		"eshu_dp_azure_claims_total",
		metric.WithDescription("Azure cloud collector claim lifecycle events by collector kind and status"),
	)
	if err != nil {
		return nil, err
	}
	return otelMetrics{
		apiCalls:     apiCalls,
		skipResumes:  skipResumes,
		partialScope: partialScope,
		factsEmitted: factsEmitted,
		freshnessLag: freshnessLag,
		claims:       claims,
	}, nil
}

func (m otelMetrics) RecordAPICall(ctx context.Context, boundary Boundary, operation, statusClass string) {
	m.apiCalls.Add(ctx, 1, metric.WithAttributes(
		append(boundaryAttrs(boundary),
			telemetry.AttrOperation(operation),
			telemetry.AttrStatusClass(statusClass),
		)...,
	))
}

func (m otelMetrics) RecordSkipTokenResume(ctx context.Context, boundary Boundary) {
	m.skipResumes.Add(ctx, 1, metric.WithAttributes(boundaryAttrs(boundary)...))
}

func (m otelMetrics) RecordPartialScope(ctx context.Context, boundary Boundary, reason string) {
	m.partialScope.Add(ctx, 1, metric.WithAttributes(
		append(boundaryAttrs(boundary), telemetry.AttrOutcome(reason))...,
	))
}

func (m otelMetrics) RecordFactsEmitted(ctx context.Context, boundary Boundary, factKind string, count int) {
	if count <= 0 {
		return
	}
	m.factsEmitted.Add(ctx, int64(count), metric.WithAttributes(
		append(boundaryAttrs(boundary), telemetry.AttrFactKind(factKind))...,
	))
}

func (m otelMetrics) RecordFreshnessLag(ctx context.Context, boundary Boundary, lagSeconds float64) {
	m.freshnessLag.Record(ctx, lagSeconds, metric.WithAttributes(boundaryAttrs(boundary)...))
}

func (m otelMetrics) RecordClaim(ctx context.Context, status string) {
	m.claims.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrCollectorKind(CollectorKind),
		telemetry.AttrStatus(status),
	))
}

// boundaryAttrs returns the bounded-enum attribute set shared by every Azure
// metric: collector kind, scope kind, and source lane. It never includes
// tenant, subscription, resource group, resource name, ARM ID, location, tags,
// query text, URLs, or credential names.
func boundaryAttrs(boundary Boundary) []attribute.KeyValue {
	return []attribute.KeyValue{
		telemetry.AttrCollectorKind(CollectorKind),
		telemetry.AttrScopeKind(boundary.ScopeKind),
		attribute.String(metricSourceLaneKey, boundary.SourceLane),
	}
}
