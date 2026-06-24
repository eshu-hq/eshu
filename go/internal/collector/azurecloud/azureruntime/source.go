package azureruntime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// spanAzureScopeScan names the per-target collection span. It is a bounded
// constant, never derived from scope identity.
const spanAzureScopeScan = "collector.azure.scope_scan"

// Source reads each configured Azure scope target through the PageProvider seam
// and yields one collector.CollectedGeneration per target. It implements
// collector.Source for the non-claimed Azure cloud runtime. The live Resource
// Graph/ARM client stays behind ProviderFactory, so the same logic runs under
// fixtures and in production. Source mutates no Azure state and commits no
// facts; the collector.Service owns the durable commit boundary.
type Source struct {
	// Config declares the collector instance and bounded scope targets.
	Config Config
	// ProviderFactory builds the read-only PageProvider for each target. A
	// fixture factory is used in tests; LiveProviderFactory is the gated
	// production seam.
	ProviderFactory PageProviderFactory
	// Metrics records bounded-label Azure collector telemetry. A nil value
	// disables Azure-specific metrics.
	Metrics azurecloud.Metrics
	// RedactionKey keys azure_tag_observation value fingerprinting. A zero key
	// (the default) disables tag observation emission entirely, so tag values
	// are never fingerprinted or carried without an operator-provided key.
	RedactionKey redact.Key
	// Tracer optionally traces per-target scans. Nil disables tracing.
	Tracer trace.Tracer
	// Logger optionally emits structured per-target diagnostics. Nil disables
	// logging.
	Logger *slog.Logger
	// Clock supplies the observation time. Nil uses time.Now in UTC.
	Clock func() time.Time
	// GenerationIDFunc assigns the generation identity for one target sweep. Nil
	// derives a deterministic fingerprint from scope identity and observed time
	// so a replayed sweep at the same instant converges.
	GenerationIDFunc func(scopeID string, observedAt time.Time) string

	next int
}

// Next returns the next configured Azure scope target's collected generation.
// It advances through Config.Targets one target per call and reports exhaustion
// (ok=false) once the batch is drained, resetting for the next poll. A provider
// read error aborts the sweep instead of committing silently incomplete
// evidence.
func (s *Source) Next(ctx context.Context) (collector.CollectedGeneration, bool, error) {
	config, err := s.Config.validated()
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	if s.ProviderFactory == nil {
		return collector.CollectedGeneration{}, false, fmt.Errorf("azure page provider factory is required")
	}
	if s.next >= len(config.Targets) {
		s.next = 0
		return collector.CollectedGeneration{}, false, nil
	}
	target := config.Targets[s.next]
	s.next++

	collected, err := s.scanTarget(ctx, config.CollectorInstanceID, target, "")
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	return collected, true, nil
}

// scanTarget collects one bounded Azure scope target. A non-empty generationID
// pins the durable generation identity (claim-driven collection supplies the
// coordinator-assigned id); an empty value derives a deterministic per-poll id
// so an unclaimed sweep at the same instant converges.
func (s *Source) scanTarget(
	ctx context.Context,
	collectorInstanceID string,
	target TargetConfig,
	generationID string,
) (collector.CollectedGeneration, error) {
	observedAt := s.now()
	boundary := s.boundary(collectorInstanceID, target, generationID, observedAt)

	if s.Tracer != nil {
		var span trace.Span
		ctx, span = s.Tracer.Start(ctx, spanAzureScopeScan)
		span.SetAttributes(
			telemetry.AttrCollectorKind(azurecloud.CollectorKind),
			telemetry.AttrScopeKind(target.ScopeKind),
			attribute.String("source_lane", boundary.SourceLane),
		)
		defer span.End()
	}

	scopeValue, generationValue, err := s.scopeAndGeneration(target, boundary, observedAt)
	if err != nil {
		return collector.CollectedGeneration{}, err
	}

	provider, err := s.ProviderFactory.PageProvider(ctx, boundary, target)
	if err != nil {
		return collector.CollectedGeneration{}, fmt.Errorf("build azure page provider: %w", err)
	}
	if provider == nil {
		return collector.CollectedGeneration{}, fmt.Errorf("azure page provider factory returned nil provider")
	}

	result, err := azurecloud.NewCollector(provider, s.Metrics, azurecloud.WithRedactionKey(s.RedactionKey)).Collect(ctx, boundary)
	if err != nil {
		s.recordClaim(ctx, azurecloud.ClaimStatusFailed)
		return collector.CollectedGeneration{}, fmt.Errorf("collect azure scope generation: %w", err)
	}

	// Record the coverage-aware claim outcome: partial scope access or any
	// collection warning is a partial claim, not a full success. The status
	// committer separately records the commit outcome.
	if result.Partial || result.WarningCount > 0 {
		s.recordClaim(ctx, azurecloud.ClaimStatusPartial)
	} else {
		s.recordClaim(ctx, azurecloud.ClaimStatusSucceeded)
	}

	s.logScan(ctx, target, result, observedAt)
	return collector.FactsFromSlice(scopeValue, generationValue, result.Facts), nil
}

// recordClaim records one claim lifecycle outcome on the bounded claim counter.
// A nil Metrics disables recording so the source runs without a meter.
func (s *Source) recordClaim(ctx context.Context, status string) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordClaim(ctx, status)
}

func (s *Source) boundary(
	collectorInstanceID string,
	target TargetConfig,
	generationID string,
	observedAt time.Time,
) azurecloud.Boundary {
	scopeID := scopeIDForTarget(target)
	resolvedGenerationID := strings.TrimSpace(generationID)
	if resolvedGenerationID == "" {
		resolvedGenerationID = s.generationID(scopeID, observedAt)
	}
	return azurecloud.Boundary{
		CollectorInstanceID: collectorInstanceID,
		TenantID:            target.TenantID,
		ScopeKind:           target.ScopeKind,
		ProviderScopeID:     target.ProviderScopeID,
		ResourceTypeFamily:  target.ResourceTypeFamily,
		LocationBucket:      target.LocationBucket,
		SourceLane:          target.SourceLane,
		ScopeID:             scopeID,
		GenerationID:        resolvedGenerationID,
		FencingToken:        target.FencingToken,
		ObservedAt:          observedAt,
	}
}

func (s *Source) scopeAndGeneration(
	target TargetConfig,
	boundary azurecloud.Boundary,
	observedAt time.Time,
) (scope.IngestionScope, scope.ScopeGeneration, error) {
	scopeValue := scope.IngestionScope{
		ScopeID:       boundary.ScopeID,
		SourceSystem:  azurecloud.CollectorKind,
		ScopeKind:     scope.KindAccount,
		CollectorKind: scope.CollectorAzure,
		PartitionKey:  partitionKeyForTarget(target),
		Metadata: map[string]string{
			"tenant_id":  target.TenantID,
			"scope_kind": target.ScopeKind,
		},
	}
	generationValue := scope.ScopeGeneration{
		ScopeID:      boundary.ScopeID,
		GenerationID: boundary.GenerationID,
		Status:       scope.GenerationStatusPending,
		ObservedAt:   observedAt,
		IngestedAt:   observedAt,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	if err := generationValue.ValidateForScope(scopeValue); err != nil {
		return scope.IngestionScope{}, scope.ScopeGeneration{}, err
	}
	return scopeValue, generationValue, nil
}

func (s *Source) generationID(scopeID string, observedAt time.Time) string {
	if s.GenerationIDFunc != nil {
		if id := strings.TrimSpace(s.GenerationIDFunc(scopeID, observedAt)); id != "" {
			return id
		}
	}
	fingerprint := facts.StableID("AzureCloudGeneration", map[string]any{
		"scope_id":    scopeID,
		"observed_at": observedAt.UTC().Format(time.RFC3339Nano),
	})
	return "azure:" + fingerprint
}

func (s *Source) logScan(
	ctx context.Context,
	target TargetConfig,
	result azurecloud.ScanResult,
	observedAt time.Time,
) {
	if s.Logger == nil {
		return
	}
	s.Logger.InfoContext(
		ctx, "azure scope scan completed",
		telemetry.PhaseAttr(telemetry.PhaseDiscovery),
		slog.String("scope_kind", target.ScopeKind),
		slog.String("source_lane", target.SourceLane),
		slog.Int("resource_count", result.ResourceCount),
		slog.Int("resource_change_count", result.ResourceChangeCount),
		slog.Int("warning_count", result.WarningCount),
		slog.Int("page_count", result.PageCount),
		slog.Int("skip_token_resumes", result.SkipTokenResumes),
		slog.Bool("partial_scope", result.Partial),
		slog.Bool("truncated", result.Truncated),
		slog.Float64("duration_seconds", time.Since(observedAt).Seconds()),
	)
}

func (s *Source) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

// scopeIDForTarget builds the durable Eshu scope ID for one Azure shard,
// following the contract layout
// azure:<tenant>:<scope_kind>:<provider_scope>:<resource_family>:<location>:<source_lane>.
// Empty narrowing buckets collapse to "all" so the scope stays stable and
// readable.
func scopeIDForTarget(target TargetConfig) string {
	return strings.Join([]string{
		azurecloud.CollectorKind,
		target.TenantID,
		target.ScopeKind,
		target.ProviderScopeID,
		orAll(target.ResourceTypeFamily),
		orAll(target.LocationBucket),
		target.SourceLane,
	}, ":")
}

func partitionKeyForTarget(target TargetConfig) string {
	return strings.Join([]string{
		target.TenantID,
		target.ScopeKind,
		target.ProviderScopeID,
	}, ":")
}

func orAll(value string) string {
	if strings.TrimSpace(value) == "" {
		return "all"
	}
	return value
}
