// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type targetRuntime struct {
	config TargetConfig
	client EvidenceClient
}

// ClaimedSource resolves Tempo workflow claims into observability source facts.
type ClaimedSource struct {
	collectorInstanceID string
	targets             map[string]targetRuntime
	now                 func() time.Time
	tracer              trace.Tracer
	instruments         *telemetry.Instruments
}

// NewClaimedSource validates configuration and builds a claim-driven Tempo
// source. Disabled targets are ignored before client construction.
func NewClaimedSource(config SourceConfig) (*ClaimedSource, error) {
	collectorID := strings.TrimSpace(config.CollectorInstanceID)
	if collectorID == "" {
		return nil, fmt.Errorf("tempo collector instance ID is required")
	}
	factory := config.ClientFactory
	if factory == nil {
		factory = defaultClientFactory
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	targets := make(map[string]targetRuntime, len(config.Targets))
	for i, target := range config.Targets {
		if !target.Enabled {
			continue
		}
		validated, err := validateTarget(target, now)
		if err != nil {
			return nil, fmt.Errorf("target %d: %w", i, err)
		}
		if _, exists := targets[validated.ScopeID]; exists {
			return nil, fmt.Errorf("duplicate tempo target scope_id %q", validated.ScopeID)
		}
		client, err := factory(validated)
		if err != nil {
			return nil, fmt.Errorf("target %d client: %w", i, err)
		}
		targets[validated.ScopeID] = targetRuntime{config: validated, client: client}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("at least one enabled tempo target is required")
	}
	return &ClaimedSource{
		collectorInstanceID: collectorID,
		targets:             targets,
		now:                 now,
		tracer:              config.Tracer,
		instruments:         config.Instruments,
	}, nil
}

// NextClaimed collects the Tempo target named by item.ScopeID.
func (s *ClaimedSource) NextClaimed(
	ctx context.Context,
	item workflow.WorkItem,
) (collector.CollectedGeneration, bool, error) {
	if strings.TrimSpace(item.CollectorInstanceID) != s.collectorInstanceID {
		return collector.CollectedGeneration{}, false, fmt.Errorf(
			"tempo work item collector_instance_id %q does not match source %q",
			item.CollectorInstanceID,
			s.collectorInstanceID,
		)
	}
	if item.CollectorKind != "" && item.CollectorKind != scope.CollectorKind(CollectorKind) {
		return collector.CollectedGeneration{}, false, fmt.Errorf("tempo source cannot collect %q work items", item.CollectorKind)
	}
	if strings.TrimSpace(item.GenerationID) == "" {
		return collector.CollectedGeneration{}, false, fmt.Errorf("tempo work item generation_id is required")
	}
	target, ok := s.targets[strings.TrimSpace(item.ScopeID)]
	if !ok {
		err := fmt.Errorf("tempo target scope_id is not configured")
		return collector.CollectedGeneration{}, false, sdk.NewProviderFailure("tempo", sdk.FailureRetryable, false, err)
	}

	startedAt := time.Now()
	observeCtx, observeSpan := s.startObserve(ctx)
	defer observeSpan.End()
	fetchCtx, fetchSpan := s.startFetch(observeCtx)
	result, err := target.client.CollectObservedMetadata(fetchCtx, target.config)
	if err != nil {
		failure := classifiedProviderFailure(err)
		s.recordFetch(observeCtx, string(failure.FailureClass()), startedAt)
		s.recordRateLimit(observeCtx, failure)
		s.recordRetries(observeCtx, result.Stats.Retries)
		recordSpanError(fetchSpan, failure)
		recordSpanError(observeSpan, failure)
		fetchSpan.End()
		return collector.CollectedGeneration{}, false, failure
	}
	fetchSpan.End()

	observedAt := result.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = s.now().UTC()
	}
	envs, err := s.envelopes(item, target.config, result, observedAt)
	if err != nil {
		recordSpanError(observeSpan, err)
		return collector.CollectedGeneration{}, false, err
	}
	s.recordFacts(observeCtx, envs)
	s.recordRedactions(observeCtx, result.Stats.Redactions)
	s.recordRateLimits(observeCtx, result.Stats.RateLimits)
	s.recordRetries(observeCtx, result.Stats.Retries)
	s.recordStale(observeCtx, result.Stats.Stale)
	s.recordHighCardinality(observeCtx, result.Stats.HighCardinalityRejected)
	s.recordFetch(observeCtx, "success", startedAt)
	return collector.FactsFromSlice(
		ingestionScope(target.config),
		scope.ScopeGeneration{
			GenerationID:  item.GenerationID,
			ScopeID:       target.config.ScopeID,
			ObservedAt:    observedAt,
			IngestedAt:    observedAt,
			Status:        scope.GenerationStatusPending,
			TriggerKind:   scope.TriggerKindSnapshot,
			FreshnessHint: "tempo_observed_metadata",
		},
		envs,
	), true, nil
}

func defaultClientFactory(target TargetConfig) (EvidenceClient, error) {
	return NewHTTPClient(HTTPClientConfig{BaseURL: target.BaseURL})
}

func classifiedProviderFailure(err error) ProviderFailure {
	return sdk.ClassifyProviderFailure("tempo", err, sdk.StatusPolicy{
		AuthDeniedClass: sdk.FailureAuthDenied,
		NotFoundClass:   sdk.FailureTerminal,
	}, sdk.FailureTerminal)
}

func validateTarget(target TargetConfig, now func() time.Time) (TargetConfig, error) {
	target.ScopeID = strings.TrimSpace(target.ScopeID)
	target.InstanceID = strings.TrimSpace(target.InstanceID)
	target.BaseURL = strings.TrimRight(strings.TrimSpace(target.BaseURL), "/")
	target.PathPrefix = "/" + strings.Trim(strings.TrimSpace(target.PathPrefix), "/")
	if target.PathPrefix == "/" {
		target.PathPrefix = ""
	}
	target.Token = strings.TrimSpace(target.Token)
	target.TenantID = strings.TrimSpace(target.TenantID)
	target.TagValueNames = cleanStringSlice(target.TagValueNames)
	target.Now = now
	if target.ScopeID == "" {
		return TargetConfig{}, fmt.Errorf("scope_id is required")
	}
	if target.InstanceID == "" {
		return TargetConfig{}, fmt.Errorf("instance_id is required")
	}
	if target.BaseURL == "" {
		return TargetConfig{}, fmt.Errorf("base_url is required")
	}
	if target.ResourceLimit < 0 || target.ResourceLimit > maxResourceLimit {
		return TargetConfig{}, fmt.Errorf("resource_limit must be between 0 and %d", maxResourceLimit)
	}
	if target.ResourceLimit == 0 {
		target.ResourceLimit = defaultResourceLimit
	}
	if target.MaxTagValuesPerTag < 0 || target.MaxTagValuesPerTag > maxTagValueLimit {
		return TargetConfig{}, fmt.Errorf("max_tag_values_per_tag must be between 0 and %d", maxTagValueLimit)
	}
	if target.MaxTagValuesPerTag == 0 {
		target.MaxTagValuesPerTag = defaultTagValueLimit
	}
	if target.Lookback < 0 {
		return TargetConfig{}, fmt.Errorf("lookback must be positive")
	}
	if target.Lookback == 0 {
		target.Lookback = defaultLookback
	}
	return target, nil
}

func (s *ClaimedSource) envelopes(
	item workflow.WorkItem,
	target TargetConfig,
	result CollectionResult,
	observedAt time.Time,
) ([]facts.Envelope, error) {
	ctx := EnvelopeContext{
		ScopeID:             target.ScopeID,
		GenerationID:        item.GenerationID,
		CollectorInstanceID: s.collectorInstanceID,
		FencingToken:        item.CurrentFencingToken,
		ObservedAt:          observedAt,
		SourceInstanceID:    target.InstanceID,
	}
	stats := result.Stats
	stats.Signals = len(result.Signals)
	stats.Warnings = len(result.Warnings)
	envs := make([]facts.Envelope, 0, 1+len(result.Signals)+len(result.Warnings))
	source, err := NewSourceInstanceEnvelope(ctx, result.Source, stats)
	if err != nil {
		return nil, err
	}
	envs = append(envs, source)
	for _, signal := range result.Signals {
		env, err := NewObservedTraceSignalEnvelope(ctx, signal)
		if err != nil {
			return nil, err
		}
		envs = append(envs, env)
	}
	for _, warning := range result.Warnings {
		env, err := NewCoverageWarningEnvelope(ctx, warning)
		if err != nil {
			return nil, err
		}
		envs = append(envs, env)
	}
	return envs, nil
}

func ingestionScope(target TargetConfig) scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       target.ScopeID,
		SourceSystem:  CollectorKind,
		ScopeKind:     scope.ScopeKind(ScopeKindTraceSource),
		CollectorKind: scope.CollectorKind(CollectorKind),
		PartitionKey:  firstNonBlank(target.InstanceID, ProviderTempo),
		Metadata: map[string]string{
			"provider":    ProviderTempo,
			"instance_id": target.InstanceID,
		},
	}
}

func (s *ClaimedSource) startObserve(ctx context.Context) (context.Context, trace.Span) {
	if s.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return s.tracer.Start(ctx, telemetry.SpanTempoObserve, trace.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, ProviderTempo),
	))
}

func (s *ClaimedSource) startFetch(ctx context.Context) (context.Context, trace.Span) {
	if s.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return s.tracer.Start(ctx, telemetry.SpanTempoFetch)
}

func recordSpanError(span trace.Span, err error) {
	if span == nil || err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
