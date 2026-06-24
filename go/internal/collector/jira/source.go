// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

import (
	"context"
	"errors"
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

const defaultUpdatedLookback = 24 * time.Hour

// ErrArchivedIssue marks Jira archived issue or project responses when the
// provider exposes the state without a distinct status code.
var ErrArchivedIssue = errors.New("jira issue is archived")

var jiraStatusPolicy = sdk.StatusPolicy{
	AuthDeniedClass: sdk.FailurePermissionHidden,
	NotFoundClass:   sdk.FailureDeleted,
	GoneClass:       sdk.FailureArchived,
}

type targetRuntime struct {
	config TargetConfig
	client EvidenceClient
}

// ClaimedSource resolves Jira workflow claims into work-item source facts.
type ClaimedSource struct {
	collectorInstanceID string
	targets             map[string]targetRuntime
	now                 func() time.Time
	tracer              trace.Tracer
	instruments         *telemetry.Instruments
}

// NewClaimedSource validates configuration and builds a claim-driven Jira
// source.
func NewClaimedSource(config SourceConfig) (*ClaimedSource, error) {
	collectorID := strings.TrimSpace(config.CollectorInstanceID)
	if collectorID == "" {
		return nil, fmt.Errorf("jira collector instance ID is required")
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
			return nil, fmt.Errorf("duplicate jira target scope_id %q", validated.ScopeID)
		}
		client, err := factory(validated)
		if err != nil {
			return nil, fmt.Errorf("target %d client: %w", i, err)
		}
		targets[validated.ScopeID] = targetRuntime{config: validated, client: client}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("at least one jira target is required")
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

// NextClaimed collects the Jira target named by item.ScopeID.
func (s *ClaimedSource) NextClaimed(
	ctx context.Context,
	item workflow.WorkItem,
) (collector.CollectedGeneration, bool, error) {
	if strings.TrimSpace(item.CollectorInstanceID) != s.collectorInstanceID {
		return collector.CollectedGeneration{}, false, fmt.Errorf(
			"jira work item collector_instance_id %q does not match source %q",
			item.CollectorInstanceID,
			s.collectorInstanceID,
		)
	}
	if item.CollectorKind != "" && item.CollectorKind != scope.CollectorJira {
		return collector.CollectedGeneration{}, false, fmt.Errorf("jira source cannot collect %q work items", item.CollectorKind)
	}
	if strings.TrimSpace(item.GenerationID) == "" {
		return collector.CollectedGeneration{}, false, fmt.Errorf("jira work item generation_id is required")
	}
	target, ok := s.targets[strings.TrimSpace(item.ScopeID)]
	if !ok {
		err := fmt.Errorf("jira target scope_id is not configured")
		return collector.CollectedGeneration{}, false, sdk.NewProviderFailure("jira", sdk.FailureRetryable, false, err)
	}

	startedAt := time.Now().UTC()
	observeCtx, observeSpan := s.startObserve(ctx, target.config)
	defer observeSpan.End()
	fetchCtx, fetchSpan := s.startFetch(observeCtx)
	window := s.window(target.config)
	windowStats := collectionWindowStats(window, startedAt, target.config.UpdatedLookback)
	result, err := target.client.CollectWorkItemEvidence(fetchCtx, target.config, window)
	if err != nil {
		failure := classifiedProviderFailure(err)
		recordFailureStats(fetchSpan, failure, windowStats)
		s.recordFetch(observeCtx, target.config, failure.FailureClass(), startedAt)
		s.recordRateLimit(observeCtx, target.config, failure)
		recordSpanError(fetchSpan, failure)
		recordSpanError(observeSpan, failure)
		fetchSpan.End()
		return collector.CollectedGeneration{}, false, failure
	}
	result.Stats.StaleWindows += windowStats.StaleWindows
	recordFetchStats(fetchSpan, result.Stats)
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
	s.recordFacts(observeCtx, target.config, envs)
	s.recordFetch(observeCtx, target.config, "success", startedAt)
	return collector.FactsFromSlice(
		ingestionScope(target.config),
		scope.ScopeGeneration{
			GenerationID:  item.GenerationID,
			ScopeID:       target.config.ScopeID,
			ObservedAt:    observedAt,
			IngestedAt:    observedAt,
			Status:        scope.GenerationStatusPending,
			TriggerKind:   scope.TriggerKindSnapshot,
			FreshnessHint: "jira_updated_window",
		},
		envs,
	), true, nil
}

func defaultClientFactory(target TargetConfig) (EvidenceClient, error) {
	return NewHTTPClient(HTTPClientConfig{
		BaseURL: target.BaseURL,
		Email:   target.Email,
		Token:   target.Token,
	})
}

func classifiedProviderFailure(err error) ProviderFailure {
	if errors.Is(err, ErrArchivedIssue) {
		return sdk.NewProviderFailure("jira", sdk.FailureArchived, true, err)
	}
	return sdk.ClassifyProviderFailure("jira", err, jiraStatusPolicy, sdk.FailureRetryable)
}

func validateTarget(target TargetConfig) (TargetConfig, error) {
	target.Provider = strings.TrimSpace(target.Provider)
	if target.Provider == "" {
		target.Provider = ProviderJiraCloud
	}
	if target.Provider != ProviderJiraCloud {
		return TargetConfig{}, fmt.Errorf("unsupported jira provider %q", target.Provider)
	}
	target.ScopeID = strings.TrimSpace(target.ScopeID)
	target.SiteID = strings.TrimSpace(target.SiteID)
	target.BaseURL = strings.TrimRight(strings.TrimSpace(target.BaseURL), "/")
	target.Email = strings.TrimSpace(target.Email)
	target.Token = strings.TrimSpace(target.Token)
	target.JQL = strings.TrimSpace(target.JQL)
	if target.ScopeID == "" {
		return TargetConfig{}, fmt.Errorf("scope_id is required")
	}
	if target.SiteID == "" {
		return TargetConfig{}, fmt.Errorf("site_id is required")
	}
	if target.BaseURL == "" {
		target.BaseURL = "https://" + target.SiteID
	}
	if target.Token == "" {
		return TargetConfig{}, fmt.Errorf("token is required")
	}
	if target.UpdatedLookback == 0 {
		target.UpdatedLookback = defaultUpdatedLookback
	}
	if target.UpdatedLookback < 0 {
		return TargetConfig{}, fmt.Errorf("updated lookback must be positive")
	}
	return target, nil
}

func (s *ClaimedSource) window(target TargetConfig) CollectionWindow {
	until := s.now().UTC()
	return CollectionWindow{
		Since: until.Add(-target.UpdatedLookback),
		Until: until,
	}
}

func (s *ClaimedSource) envelopes(
	item workflow.WorkItem,
	target TargetConfig,
	result CollectionResult,
	observedAt time.Time,
) ([]facts.Envelope, error) {
	envs := make([]facts.Envelope, 0, len(result.Issues)+len(result.Projects)+len(result.IssueTypes)+len(result.Statuses)+len(result.Workflows)+len(result.Fields)+len(result.MetadataWarnings))
	ctx := EnvelopeContext{
		ScopeID:             target.ScopeID,
		GenerationID:        item.GenerationID,
		CollectorInstanceID: s.collectorInstanceID,
		FencingToken:        item.CurrentFencingToken,
		ObservedAt:          observedAt,
		SourceURI:           target.BaseURL,
	}
	for _, project := range result.Projects {
		env, err := NewWorkItemProjectMetadataEnvelope(ctx, project)
		if err != nil {
			return nil, err
		}
		envs = append(envs, env)
	}
	for _, issueType := range result.IssueTypes {
		env, err := NewWorkItemIssueTypeMetadataEnvelope(ctx, issueType)
		if err != nil {
			return nil, err
		}
		envs = append(envs, env)
	}
	for _, status := range result.Statuses {
		env, err := NewWorkItemStatusMetadataEnvelope(ctx, status)
		if err != nil {
			return nil, err
		}
		envs = append(envs, env)
	}
	for _, workflow := range result.Workflows {
		env, err := NewWorkItemWorkflowMetadataEnvelope(ctx, workflow)
		if err != nil {
			return nil, err
		}
		envs = append(envs, env)
	}
	for _, field := range result.Fields {
		env, err := NewWorkItemFieldMetadataEnvelope(ctx, field)
		if err != nil {
			return nil, err
		}
		envs = append(envs, env)
	}
	for _, warning := range result.MetadataWarnings {
		env, err := NewWorkItemMetadataWarningEnvelope(ctx, warning)
		if err != nil {
			return nil, err
		}
		envs = append(envs, env)
	}
	for _, issue := range result.Issues {
		record, err := NewWorkItemRecordEnvelope(ctx, issue)
		if err != nil {
			return nil, err
		}
		envs = append(envs, record)
		for _, transition := range result.Transitions[issue.ID] {
			transitionEnv, err := NewWorkItemTransitionEnvelope(ctx, transition)
			if err != nil {
				return nil, err
			}
			envs = append(envs, transitionEnv)
		}
		for _, link := range result.ExternalLinks[issue.ID] {
			linkEnv, err := NewWorkItemExternalLinkEnvelope(ctx, link)
			if err != nil {
				return nil, err
			}
			envs = append(envs, linkEnv)
		}
	}
	return envs, nil
}

func ingestionScope(target TargetConfig) scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       target.ScopeID,
		SourceSystem:  string(scope.CollectorJira),
		ScopeKind:     scope.KindJiraSite,
		CollectorKind: scope.CollectorJira,
		PartitionKey:  firstNonBlank(target.SiteID, target.Provider),
		Metadata: map[string]string{
			"provider": target.Provider,
			"site_id":  target.SiteID,
		},
	}
}

func (s *ClaimedSource) startObserve(ctx context.Context, target TargetConfig) (context.Context, trace.Span) {
	if s.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return s.tracer.Start(ctx, telemetry.SpanJiraObserve, trace.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, target.Provider),
	))
}

func (s *ClaimedSource) startFetch(ctx context.Context) (context.Context, trace.Span) {
	if s.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return s.tracer.Start(ctx, telemetry.SpanJiraFetch)
}

func recordSpanError(span trace.Span, err error) {
	if span == nil || err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

func recordFetchStats(span trace.Span, stats CollectionStats) {
	if span == nil {
		return
	}
	span.SetAttributes(
		attribute.Int(telemetry.SpanAttrJiraSearchPages, stats.SearchPages),
		attribute.Int(telemetry.SpanAttrJiraChangelogPages, stats.ChangelogPages),
		attribute.Int(telemetry.SpanAttrJiraRemoteLinkPages, stats.RemoteLinkPages),
		attribute.Int(telemetry.SpanAttrJiraIssuesEmitted, stats.IssuesEmitted),
		attribute.Int(telemetry.SpanAttrJiraChangelogEventsEmitted, stats.ChangelogEventsEmitted),
		attribute.Int(telemetry.SpanAttrJiraRemoteLinksEmitted, stats.RemoteLinksEmitted),
		attribute.Int(telemetry.SpanAttrJiraRemoteLinksRejected, stats.RemoteLinksRejected),
		attribute.Int(telemetry.SpanAttrJiraUnsupportedProviderLinks, stats.UnsupportedProviderLinks),
		attribute.Int(telemetry.SpanAttrJiraMetadataPages, stats.MetadataPages),
		attribute.Int(telemetry.SpanAttrJiraMetadataObjectsScanned, stats.MetadataObjectsScanned),
		attribute.Int(telemetry.SpanAttrJiraMetadataObjectsEmitted, stats.MetadataObjectsEmitted),
		attribute.Int(telemetry.SpanAttrJiraUnsupportedMetadata, stats.UnsupportedMetadata),
		attribute.Int(telemetry.SpanAttrJiraPermissionHiddenMetadata, stats.PermissionHiddenMetadata),
		attribute.Int(telemetry.SpanAttrJiraStaleMetadata, stats.StaleMetadata),
		attribute.Int(telemetry.SpanAttrJiraMetadataRedactions, stats.MetadataRedactions),
		attribute.Int(telemetry.SpanAttrJiraPartialFailures, stats.PartialFailures),
		attribute.Int(telemetry.SpanAttrJiraRateLimits, stats.RateLimits),
		attribute.Int(telemetry.SpanAttrJiraRetryAfterSeconds, stats.RetryAfterSeconds),
		attribute.Int(telemetry.SpanAttrJiraStaleWindows, stats.StaleWindows),
	)
}

func recordFailureStats(span trace.Span, failure ProviderFailure, base CollectionStats) {
	if span == nil {
		return
	}
	stats := base
	var partial PartialCollectionError
	if errors.As(failure, &partial) {
		stats = partial.Stats
		stats.StaleWindows += base.StaleWindows
	}
	var jiraErr JiraError
	if errors.As(failure, &jiraErr) {
		if failure.FailureClass() == FailureRateLimited {
			stats.RateLimits++
		}
		if jiraErr.RetryAfter > 0 {
			stats.RetryAfterSeconds = int(jiraErr.RetryAfter / time.Second)
		}
	}
	recordFetchStats(span, stats)
}

func collectionWindowStats(window CollectionWindow, startedAt time.Time, lookback time.Duration) CollectionStats {
	if !staleCollectionWindow(window, startedAt, lookback) {
		return CollectionStats{}
	}
	return CollectionStats{StaleWindows: 1, StaleMetadata: 1}
}

func staleCollectionWindow(window CollectionWindow, startedAt time.Time, lookback time.Duration) bool {
	if window.Since.IsZero() || window.Until.IsZero() || window.Until.Before(window.Since) {
		return true
	}
	if lookback <= 0 {
		return false
	}
	return window.Until.Before(startedAt.UTC().Add(-lookback))
}
