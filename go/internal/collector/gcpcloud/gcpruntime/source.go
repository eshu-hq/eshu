// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// Source implements collector.Source for the GCP Cloud Asset Inventory
// collector. Each Next call yields one CollectedGeneration for the next
// configured scope by draining pages through the PageProvider seam, accumulating
// the parsed gcp_cloud_resource, gcp_cloud_relationship, gcp_tag_observation,
// gcp_iam_policy_observation, gcp_dns_record, gcp_image_reference, and
// gcp_collection_warning facts, and fencing the generation so a stale scan
// cannot replace current facts.
//
// Source performs no Google Cloud calls itself; the PageProvider owns transport.
// Source is single-goroutine per collector.Service; it is not safe for concurrent
// Next calls.
type Source struct {
	// Config is the declarative runtime configuration. It is required.
	Config Config
	// Provider is the page transport seam. It is required.
	Provider PageProvider
	// TagProvider is the optional direct/effective Resource Manager tag API seam.
	// It is required only for scopes that explicitly opt in to tag APIs.
	TagProvider TagProvider
	// RedactionKey keys label/member fingerprinting in emitted facts. It is
	// required so facts are never emitted with unkeyed redaction markers.
	RedactionKey redact.Key
	// Tracker fences generations per scope. When nil a process-local tracker is
	// created on first use.
	Tracker *gcpcloud.GenerationTracker
	// Metrics records bounded-label collector telemetry. Optional; nil disables
	// metrics.
	Metrics *gcpcloud.Metrics
	// Logger emits structured diagnostics. Optional; nil disables logging.
	Logger *slog.Logger
	// Clock supplies the observation time. Optional; nil uses time.Now.
	Clock func() time.Time

	scopeIndex int
	drained    bool
}

// Next collects the next configured scope generation. It returns ok=false when
// the configured scope batch is exhausted so collector.Service can wait for the
// next poll, then restarts the batch on the following poll.
func (s *Source) Next(ctx context.Context) (collector.CollectedGeneration, bool, error) {
	if err := s.validate(); err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	if s.drained {
		s.drained = false
		s.scopeIndex = 0
		return collector.CollectedGeneration{}, false, nil
	}

	scopes := s.Config.resolvedScopes()
	if s.scopeIndex >= len(scopes) {
		s.drained = true
		return collector.CollectedGeneration{}, false, nil
	}
	scopeCfg := scopes[s.scopeIndex]
	s.scopeIndex++
	if s.scopeIndex >= len(scopes) {
		s.drained = true
	}

	collected, err := s.collectScope(ctx, scopeCfg)
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	return collected, true, nil
}

// collectScope drains every page for one scope, fences the generation, and
// builds the deterministic envelope set for durable commit.
func (s *Source) collectScope(ctx context.Context, scopeCfg ScopeConfig) (collector.CollectedGeneration, error) {
	observedAt := s.now()
	generationID := scopeCfg.GenerationID
	if generationID == "" {
		generationID = deriveGenerationID(scopeCfg.ScopeID, observedAt)
	}

	tracker := s.tracker()
	if err := tracker.Accept(scopeCfg.ScopeID, generationID, scopeCfg.FencingToken); err != nil {
		if errors.Is(err, gcpcloud.ErrStaleGeneration) {
			return s.staleGeneration(ctx, scopeCfg, generationID, observedAt)
		}
		return collector.CollectedGeneration{}, fmt.Errorf("fence gcp generation for parent scope kind %q: %w", scopeCfg.ParentScopeKind, err)
	}

	boundary := s.boundary(scopeCfg, generationID, observedAt)
	generation := gcpcloud.NewGeneration(boundary, s.RedactionKey)

	resources, err := s.drainPages(ctx, scopeCfg, generation)
	if err != nil {
		s.recordClaim(ctx, gcpcloud.ClaimStatusFailed)
		return collector.CollectedGeneration{}, err
	}
	if err := s.drainTagPages(ctx, scopeCfg, generation, resources); err != nil {
		s.recordClaim(ctx, gcpcloud.ClaimStatusFailed)
		return collector.CollectedGeneration{}, err
	}

	envelopes, err := generation.Build()
	if err != nil {
		s.recordClaim(ctx, gcpcloud.ClaimStatusFailed)
		return collector.CollectedGeneration{}, fmt.Errorf("build gcp generation for parent scope kind %q: %w", scopeCfg.ParentScopeKind, err)
	}

	s.recordEmission(ctx, scopeCfg, envelopes, generation.Boundary(), observedAt)
	if generation.WarningCount() > 0 {
		s.recordClaim(ctx, gcpcloud.ClaimStatusPartial)
	} else {
		s.recordClaim(ctx, gcpcloud.ClaimStatusSucceeded)
	}
	s.logScope(ctx, scopeCfg, generation)

	scopeValue, generationValue := s.scopeAndGeneration(scopeCfg, generationID, observedAt)
	return collector.FactsFromSlice(scopeValue, generationValue, envelopes), nil
}

// drainPages walks every page for a scope through the provider, resuming from
// each page's continuation token until the scope is drained. A continuation
// token that the provider cannot resume becomes a page_token_expired warning so
// partial coverage is durable evidence, not silent truncation.
func (s *Source) drainPages(
	ctx context.Context,
	scopeCfg ScopeConfig,
	generation *gcpcloud.Generation,
) ([]gcpcloud.ResourceObservation, error) {
	token := ""
	resources := map[string]gcpcloud.ResourceObservation{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if token != "" {
			s.recordPageTokenResume(ctx, scopeCfg.ParentScopeKind)
		}
		page, err := s.Provider.FetchPage(ctx, PageRequest{Scope: scopeCfg, PageToken: token})
		if err != nil {
			if errors.Is(err, errPageTokenNotFound) {
				generation.AddWarning(s.expiredTokenWarning(scopeCfg, generation))
				s.recordWarning(ctx, gcpcloud.WarningKindPageTokenExpired, gcpcloud.OutcomePartial)
				return resourceList(resources), nil
			}
			var warning ProviderWarning
			if errors.As(err, &warning) {
				generation.AddWarning(s.providerWarning(scopeCfg, generation, warning))
				s.recordWarning(ctx, warning.WarningKind, warning.Outcome)
				return resourceList(resources), nil
			}
			return nil, fmt.Errorf("fetch gcp page for parent scope kind %q: %w", scopeCfg.ParentScopeKind, err)
		}
		s.recordPage(ctx, scopeCfg.ParentScopeKind)
		generation.ObserveReadTime(page.ReadTime)
		if addErr := generation.AddPage(page.Resources); addErr != nil {
			return nil, fmt.Errorf("accumulate gcp page for parent scope kind %q: %w", scopeCfg.ParentScopeKind, addErr)
		}
		s.recordAttributeExtractions(ctx, page.Resources)
		for _, resource := range page.Resources {
			if key := resourceCollectionKey(resource); key != "" {
				resources[key] = resource
			}
		}
		token = page.NextPageToken
		if token == "" {
			return resourceList(resources), nil
		}
	}
}

// staleGeneration emits a single stale warning fact for a fenced-out scan so a
// rejected generation produces durable partial-coverage evidence instead of
// silently replacing or dropping current facts.
func (s *Source) staleGeneration(
	ctx context.Context,
	scopeCfg ScopeConfig,
	generationID string,
	observedAt time.Time,
) (collector.CollectedGeneration, error) {
	boundary := s.boundary(scopeCfg, generationID, observedAt)
	warning := gcpcloud.WarningObservation{
		Boundary:    boundary,
		WarningKind: gcpcloud.WarningKindStale,
		Outcome:     gcpcloud.OutcomeStale,
		Reason:      "generation rejected by newer fencing token",
	}
	envelope, err := gcpcloud.NewCollectionWarningEnvelope(warning)
	if err != nil {
		return collector.CollectedGeneration{}, fmt.Errorf("build gcp stale warning for parent scope kind %q: %w", scopeCfg.ParentScopeKind, err)
	}
	s.recordWarning(ctx, gcpcloud.WarningKindStale, gcpcloud.OutcomeStale)
	s.recordFactsEmitted(ctx, facts.GCPCollectionWarningFactKind, scopeCfg.ParentScopeKind, 1)
	s.recordClaim(ctx, gcpcloud.ClaimStatusPartial)

	scopeValue, generationValue := s.scopeAndGeneration(scopeCfg, generationID, observedAt)
	return collector.FactsFromSlice(scopeValue, generationValue, []facts.Envelope{envelope}), nil
}

func (s *Source) expiredTokenWarning(scopeCfg ScopeConfig, generation *gcpcloud.Generation) gcpcloud.WarningObservation {
	return gcpcloud.WarningObservation{
		Boundary:    generation.Boundary(),
		WarningKind: gcpcloud.WarningKindPageTokenExpired,
		Outcome:     gcpcloud.OutcomePartial,
		Reason:      "continuation token could not be resumed",
		Retryable:   true,
		SourceURI:   "cai://assets.list",
	}
}

func (s *Source) providerWarning(
	scopeCfg ScopeConfig,
	generation *gcpcloud.Generation,
	warning ProviderWarning,
) gcpcloud.WarningObservation {
	sourceURI := warning.SourceURI
	if sourceURI == "" {
		sourceURI = "cai://assets.list"
	}
	return gcpcloud.WarningObservation{
		Boundary:    generation.Boundary(),
		WarningKind: warning.WarningKind,
		Outcome:     warning.Outcome,
		Reason:      warning.Reason,
		Retryable:   warning.Retryable,
		HiddenCount: warning.HiddenCount,
		SourceURI:   sourceURI,
	}
}

func (s *Source) boundary(scopeCfg ScopeConfig, generationID string, observedAt time.Time) gcpcloud.Boundary {
	return gcpcloud.Boundary{
		CollectorInstanceID: s.Config.CollectorInstanceID,
		ParentScopeKind:     scopeCfg.ParentScopeKind,
		ParentScopeID:       scopeCfg.ParentScopeID,
		AssetTypeFamily:     scopeCfg.AssetTypeFamily,
		ContentFamily:       scopeCfg.ContentFamily,
		LocationBucket:      scopeCfg.LocationBucket,
		ScopeID:             scopeCfg.ScopeID,
		GenerationID:        generationID,
		FencingToken:        scopeCfg.FencingToken,
		ObservedAt:          observedAt,
	}
}

func (s *Source) scopeAndGeneration(
	scopeCfg ScopeConfig,
	generationID string,
	observedAt time.Time,
) (scope.IngestionScope, scope.ScopeGeneration) {
	scopeValue := scope.IngestionScope{
		ScopeID:       scopeCfg.ScopeID,
		SourceSystem:  gcpcloud.CollectorKind,
		ScopeKind:     scope.KindAccount,
		CollectorKind: scope.CollectorGCP,
		PartitionKey:  scopeCfg.ScopeID,
		Metadata: map[string]string{
			"collector_instance_id": s.Config.CollectorInstanceID,
			"parent_scope_kind":     string(scopeCfg.ParentScopeKind),
			"asset_type_family":     scopeCfg.AssetTypeFamily,
			"content_family":        scopeCfg.ContentFamily,
			"location_bucket":       scopeCfg.LocationBucket,
		},
	}
	generationValue := scope.ScopeGeneration{
		GenerationID: generationID,
		ScopeID:      scopeCfg.ScopeID,
		ObservedAt:   observedAt,
		IngestedAt:   observedAt,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	return scopeValue, generationValue
}

func (s *Source) tracker() *gcpcloud.GenerationTracker {
	if s.Tracker == nil {
		s.Tracker = gcpcloud.NewGenerationTracker()
	}
	return s.Tracker
}

func (s *Source) validate() error {
	if err := s.Config.Validate(); err != nil {
		return err
	}
	if s.Provider == nil {
		return errors.New("gcp collector page provider is required")
	}
	if s.RedactionKey.IsZero() {
		return errors.New("gcp collector redaction key is required")
	}
	for _, scopeCfg := range s.Config.resolvedScopes() {
		if len(tagSourceKinds(scopeCfg)) > 0 && s.TagProvider == nil {
			return errors.New("gcp collector tag provider is required when direct or effective tags are enabled")
		}
	}
	return nil
}

func (s *Source) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func deriveGenerationID(scopeID string, observedAt time.Time) string {
	return facts.StableID("GCPGeneration", map[string]any{
		"observed_at": observedAt.UTC().Format(time.RFC3339Nano),
		"scope_id":    scopeID,
	})
}
