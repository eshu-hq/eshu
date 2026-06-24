// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const defaultIncidentLookback = 6 * time.Hour

var pagerDutyStatusPolicy = sdk.StatusPolicy{
	AuthDeniedClass: sdk.FailureAuthDenied,
	NotFoundClass:   sdk.FailureNotFound,
}

type targetRuntime struct {
	config TargetConfig
	client EvidenceClient
}

// ClaimedSource resolves PagerDuty workflow claims into incident source facts.
type ClaimedSource struct {
	collectorInstanceID string
	targets             map[string]targetRuntime
	now                 func() time.Time
	tracer              trace.Tracer
	instruments         *telemetry.Instruments
}

// NewClaimedSource validates configuration and builds a claim-driven PagerDuty
// source.
func NewClaimedSource(config SourceConfig) (*ClaimedSource, error) {
	collectorID := strings.TrimSpace(config.CollectorInstanceID)
	if collectorID == "" {
		return nil, fmt.Errorf("pagerduty collector instance ID is required")
	}
	factory := config.ClientFactory
	if factory == nil {
		factory = defaultClientFactory
	}
	targets := make(map[string]targetRuntime, len(config.Targets))
	for i, target := range config.Targets {
		validated, err := validateTarget(target)
		if err != nil {
			return nil, fmt.Errorf("target %d: %w", i, err)
		}
		if _, exists := targets[validated.ScopeID]; exists {
			return nil, fmt.Errorf("duplicate pagerduty target scope_id %q", validated.ScopeID)
		}
		client, err := factory(validated)
		if err != nil {
			return nil, fmt.Errorf("target %d client: %w", i, err)
		}
		targets[validated.ScopeID] = targetRuntime{config: validated, client: client}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("at least one pagerduty target is required")
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &ClaimedSource{
		collectorInstanceID: collectorID,
		targets:             targets,
		now:                 now,
		tracer:              config.Tracer,
		instruments:         config.Instruments,
	}, nil
}

// NextClaimed collects the PagerDuty target named by item.ScopeID.
func (s *ClaimedSource) NextClaimed(
	ctx context.Context,
	item workflow.WorkItem,
) (collector.CollectedGeneration, bool, error) {
	if strings.TrimSpace(item.CollectorInstanceID) != s.collectorInstanceID {
		return collector.CollectedGeneration{}, false, fmt.Errorf(
			"pagerduty work item collector_instance_id %q does not match source %q",
			item.CollectorInstanceID,
			s.collectorInstanceID,
		)
	}
	if item.CollectorKind != "" && item.CollectorKind != scope.CollectorPagerDuty {
		return collector.CollectedGeneration{}, false, fmt.Errorf("pagerduty source cannot collect %q work items", item.CollectorKind)
	}
	if strings.TrimSpace(item.GenerationID) == "" {
		return collector.CollectedGeneration{}, false, fmt.Errorf("pagerduty work item generation_id is required")
	}
	target, ok := s.targets[strings.TrimSpace(item.ScopeID)]
	if !ok {
		err := fmt.Errorf("pagerduty target scope_id is not configured")
		return collector.CollectedGeneration{}, false, sdk.NewProviderFailure("pagerduty", sdk.FailureRetryable, false, err)
	}

	window := collectionWindow(target.config, s.now())
	startedAt := time.Now()
	observeCtx, observeSpan := s.startObserve(ctx, target.config)
	defer observeSpan.End()
	fetchCtx, fetchSpan := s.startFetch(observeCtx)
	result, err := target.client.CollectIncidentEvidence(fetchCtx, target.config, window)
	if err != nil {
		failure := classifiedProviderFailure(err)
		s.recordFetch(observeCtx, target.config, failure.FailureClass(), startedAt)
		s.recordRateLimit(observeCtx, target.config, failure)
		recordSpanError(fetchSpan, failure)
		recordSpanError(observeSpan, failure)
		fetchSpan.End()
		return collector.CollectedGeneration{}, false, failure
	}
	configResult, err := s.collectConfigEvidence(fetchCtx, target)
	if err != nil {
		failure := classifiedProviderFailure(err)
		s.recordFetch(observeCtx, target.config, failure.FailureClass(), startedAt)
		s.recordRateLimit(observeCtx, target.config, failure)
		recordSpanError(fetchSpan, failure)
		recordSpanError(observeSpan, failure)
		fetchSpan.End()
		return collector.CollectedGeneration{}, false, failure
	}
	fetchSpan.End()
	observedAt := collectionObservedAt(result, configResult, s.now())
	if observedAt.IsZero() {
		observedAt = s.now().UTC()
	}
	envs, err := s.envelopes(item, target.config, result, configResult, observedAt)
	if err != nil {
		recordSpanError(observeSpan, err)
		return collector.CollectedGeneration{}, false, err
	}
	s.recordFacts(observeCtx, target.config, envs)
	s.recordConfigTelemetry(observeCtx, target.config, configResult)
	s.recordFetch(observeCtx, target.config, fetchStatusClass(result, configResult), startedAt)
	s.recordGenerationLag(observeCtx, target.config, observedAt)
	return collector.FactsFromSlice(
		ingestionScope(target.config),
		scope.ScopeGeneration{
			GenerationID:  item.GenerationID,
			ScopeID:       target.config.ScopeID,
			ObservedAt:    observedAt,
			IngestedAt:    observedAt,
			Status:        scope.GenerationStatusPending,
			TriggerKind:   scope.TriggerKindSnapshot,
			FreshnessHint: freshnessHint(envs),
		},
		envs,
	), true, nil
}

func classifiedProviderFailure(err error) ProviderFailure {
	return sdk.ClassifyProviderFailure("pagerduty", err, pagerDutyStatusPolicy, sdk.FailureRetryable)
}

func validateTarget(target TargetConfig) (TargetConfig, error) {
	target.Provider = strings.TrimSpace(target.Provider)
	target.ScopeID = strings.TrimSpace(target.ScopeID)
	target.AccountID = strings.TrimSpace(target.AccountID)
	target.Token = strings.TrimSpace(target.Token)
	target.APIBaseURL = strings.TrimRight(strings.TrimSpace(target.APIBaseURL), "/")
	target.SourceURI = strings.TrimSpace(target.SourceURI)
	target.AllowedServiceIDs = cleanStrings(target.AllowedServiceIDs)
	if target.Provider != ProviderPagerDuty {
		if target.Provider == "" {
			return TargetConfig{}, fmt.Errorf("provider is required")
		}
		return TargetConfig{}, fmt.Errorf("unsupported pagerduty provider %q", target.Provider)
	}
	if target.ScopeID == "" {
		return TargetConfig{}, fmt.Errorf("scope_id is required")
	}
	if target.AccountID == "" {
		return TargetConfig{}, fmt.Errorf("account_id is required")
	}
	if target.Token == "" {
		return TargetConfig{}, fmt.Errorf("token is required")
	}
	if target.IncidentLookback <= 0 {
		target.IncidentLookback = defaultIncidentLookback
	}
	if target.IncidentLimit < 0 || target.IncidentLimit > 100 {
		return TargetConfig{}, fmt.Errorf("incident_limit must be between 0 and 100")
	}
	if target.LogEntryLimit < 0 || target.LogEntryLimit > 100 {
		return TargetConfig{}, fmt.Errorf("log_entry_limit must be between 0 and 100")
	}
	if target.ChangeEventLimit < 0 || target.ChangeEventLimit > 100 {
		return TargetConfig{}, fmt.Errorf("change_event_limit must be between 0 and 100")
	}
	if target.ConfigResourceLimit < 0 || target.ConfigResourceLimit > 100 {
		return TargetConfig{}, fmt.Errorf("config_resource_limit must be between 0 and 100")
	}
	if target.ConfigValidationEnabled && target.ConfigResourceLimit == 0 {
		target.ConfigResourceLimit = 100
	}
	return target, nil
}

func (s *ClaimedSource) envelopes(
	item workflow.WorkItem,
	target TargetConfig,
	result CollectionResult,
	configResult ConfigCollectionResult,
	observedAt time.Time,
) ([]facts.Envelope, error) {
	ctx := EnvelopeContext{
		ScopeID:             target.ScopeID,
		GenerationID:        item.GenerationID,
		CollectorInstanceID: s.collectorInstanceID,
		FencingToken:        item.CurrentFencingToken,
		ObservedAt:          observedAt,
		SourceURI:           target.SourceURI,
	}
	envs := make(
		[]facts.Envelope,
		0,
		len(result.Incidents)+len(configResult.Services)+len(configResult.Integrations)+len(result.Warnings)+len(configResult.Warnings),
	)
	for _, incident := range result.Incidents {
		incidentCtx := ctx
		incidentCtx.SourceURI = firstNonBlank(incident.HTMLURL, target.SourceURI)
		env, err := NewIncidentRecordEnvelope(incidentCtx, incident)
		if err != nil {
			return nil, err
		}
		envs = append(envs, env)
		for _, event := range result.LifecycleEvents[incident.ID] {
			eventCtx := ctx
			eventCtx.SourceURI = firstNonBlank(event.HTMLURL, incident.HTMLURL, target.SourceURI)
			env, err := NewLifecycleEventEnvelope(eventCtx, event)
			if err != nil {
				return nil, err
			}
			envs = append(envs, env)
		}
		for _, change := range result.RelatedChangeEvents[incident.ID] {
			changeCtx := ctx
			changeCtx.SourceURI = firstNonBlank(change.HTMLURL, incident.HTMLURL, target.SourceURI)
			env, err := NewChangeRecordEnvelope(changeCtx, change)
			if err != nil {
				return nil, err
			}
			envs = append(envs, env)
		}
	}
	for _, service := range configResult.Services {
		env, err := NewObservedPagerDutyServiceEnvelope(ctx, service)
		if err != nil {
			return nil, err
		}
		envs = append(envs, env)
	}
	for _, integration := range configResult.Integrations {
		env, err := NewObservedPagerDutyIntegrationEnvelope(ctx, integration)
		if err != nil {
			return nil, err
		}
		envs = append(envs, env)
	}
	for _, warning := range result.Warnings {
		env, err := NewPagerDutyConfigCoverageWarningEnvelope(ctx, warning)
		if err != nil {
			return nil, err
		}
		envs = append(envs, env)
	}
	for _, warning := range configResult.Warnings {
		env, err := NewPagerDutyConfigCoverageWarningEnvelope(ctx, warning)
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
		SourceSystem:  ProviderPagerDuty,
		ScopeKind:     scope.KindPagerDutyAccount,
		CollectorKind: scope.CollectorPagerDuty,
		PartitionKey:  target.AccountID,
		Metadata: map[string]string{
			"provider":   ProviderPagerDuty,
			"account_id": target.AccountID,
		},
	}
}

func collectionWindow(target TargetConfig, now time.Time) CollectionWindow {
	until := now.UTC()
	return CollectionWindow{
		Since: until.Add(-target.IncidentLookback),
		Until: until,
	}
}

func fetchStatusClass(result CollectionResult, configResult ConfigCollectionResult) string {
	if len(result.Warnings) > 0 || len(configResult.Warnings) > 0 || configResult.Partial {
		return "partial"
	}
	return "success"
}

func freshnessHint(envs []facts.Envelope) string {
	keys := make([]string, 0, len(envs))
	for _, env := range envs {
		keys = append(keys, env.StableFactKey)
	}
	sort.Strings(keys)
	return facts.StableID("pagerduty.collection.freshness", map[string]any{"stable_fact_keys": keys})
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func (s *ClaimedSource) startObserve(ctx context.Context, target TargetConfig) (context.Context, trace.Span) {
	if s.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return s.tracer.Start(ctx, telemetry.SpanPagerDutyObserve, trace.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, target.Provider),
	))
}

func (s *ClaimedSource) startFetch(ctx context.Context) (context.Context, trace.Span) {
	if s.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return s.tracer.Start(ctx, telemetry.SpanPagerDutyFetch)
}

func (s *ClaimedSource) recordFetch(ctx context.Context, target TargetConfig, statusClass string, startedAt time.Time) {
	if s.instruments == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, target.Provider),
		attribute.String(telemetry.MetricDimensionStatusClass, statusClass),
	)
	s.instruments.PagerDutyProviderRequests.Add(ctx, 1, attrs)
	s.instruments.PagerDutyFetchDuration.Record(ctx, time.Since(startedAt).Seconds(), attrs)
}

func (s *ClaimedSource) recordRateLimit(ctx context.Context, target TargetConfig, failure ProviderFailure) {
	if s.instruments == nil || failure.FailureClass() != FailureRateLimited {
		return
	}
	s.instruments.PagerDutyRateLimited.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, target.Provider),
	))
}

func (s *ClaimedSource) recordFacts(ctx context.Context, target TargetConfig, envs []facts.Envelope) {
	if s.instruments == nil {
		return
	}
	counts := map[string]int64{}
	for _, env := range envs {
		counts[env.FactKind]++
	}
	for kind, count := range counts {
		s.instruments.PagerDutyFactsEmitted.Add(ctx, count, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionProvider, target.Provider),
			attribute.String(telemetry.MetricDimensionFactKind, kind),
		))
	}
}

func (s *ClaimedSource) recordGenerationLag(ctx context.Context, target TargetConfig, observedAt time.Time) {
	if s.instruments == nil || observedAt.IsZero() {
		return
	}
	lag := s.now().UTC().Sub(observedAt.UTC()).Seconds()
	if lag < 0 {
		lag = 0
	}
	s.instruments.PagerDutyGenerationLag.Record(ctx, lag, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, target.Provider),
	))
}

func recordSpanError(span trace.Span, err error) {
	if span == nil || err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
