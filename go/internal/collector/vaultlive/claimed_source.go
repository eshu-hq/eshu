package vaultlive

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ClaimedSourceConfig configures the claim-driven Vault metadata source.
type ClaimedSourceConfig struct {
	Config        Config
	ClientFactory ClientFactory
	Tracer        trace.Tracer
	Instruments   *telemetry.Instruments
	Logger        *slog.Logger
	Clock         func() time.Time
}

// ClaimedSource resolves workflow work items into Vault metadata generations.
type ClaimedSource struct {
	config        Config
	redactionKey  redact.Key
	clientFactory ClientFactory
	targets       map[string]ClusterTarget
	tracer        trace.Tracer
	instruments   *telemetry.Instruments
	logger        *slog.Logger
	clock         func() time.Time
}

// NewClaimedSource validates configuration and builds a claim-driven Vault
// metadata source. Targets are keyed by the deterministic Vault scope ID so the
// workflow item remains the durable source of generation and fencing identity.
func NewClaimedSource(config ClaimedSourceConfig) (*ClaimedSource, error) {
	validated, err := config.Config.validated()
	if err != nil {
		return nil, err
	}
	if config.ClientFactory == nil {
		return nil, fmt.Errorf("vault live client factory is required")
	}
	targets := make(map[string]ClusterTarget, len(validated.Targets))
	for _, target := range validated.Targets {
		scopeID, err := VaultScopeID(target.VaultClusterID, target.Namespace)
		if err != nil {
			return nil, err
		}
		if _, ok := targets[scopeID]; ok {
			return nil, fmt.Errorf("duplicate vault target scope_id %q", scopeID)
		}
		targets[scopeID] = target
	}
	return &ClaimedSource{
		config:        validated,
		redactionKey:  validated.RedactionKey,
		clientFactory: config.ClientFactory,
		targets:       targets,
		tracer:        config.Tracer,
		instruments:   config.Instruments,
		logger:        config.Logger,
		clock:         config.Clock,
	}, nil
}

// NextClaimed collects the Vault target named by item.ScopeID.
func (s *ClaimedSource) NextClaimed(
	ctx context.Context,
	item workflow.WorkItem,
) (collector.CollectedGeneration, bool, error) {
	if strings.TrimSpace(item.CollectorInstanceID) != s.config.CollectorInstanceID {
		return collector.CollectedGeneration{}, false, fmt.Errorf(
			"vault live work item collector_instance_id %q does not match source %q",
			item.CollectorInstanceID,
			s.config.CollectorInstanceID,
		)
	}
	if item.CollectorKind != "" && item.CollectorKind != scope.CollectorVaultLive {
		return collector.CollectedGeneration{}, false, fmt.Errorf(
			"vault live source cannot collect %q work items",
			item.CollectorKind,
		)
	}
	if sourceSystem := strings.TrimSpace(item.SourceSystem); sourceSystem != "" && sourceSystem != CollectorKind {
		return collector.CollectedGeneration{}, false, fmt.Errorf(
			"vault live source cannot collect source_system %q",
			item.SourceSystem,
		)
	}
	if strings.TrimSpace(item.GenerationID) == "" {
		return collector.CollectedGeneration{}, false, fmt.Errorf("vault live work item generation_id is required")
	}
	if item.CurrentFencingToken <= 0 {
		return collector.CollectedGeneration{}, false, fmt.Errorf("vault live work item fencing token must be positive")
	}
	target, ok := s.targets[strings.TrimSpace(item.ScopeID)]
	if !ok {
		return collector.CollectedGeneration{}, false, fmt.Errorf("vault live target scope_id is not configured")
	}
	return s.collectClaimedTarget(ctx, item, target)
}

func (s *ClaimedSource) collectClaimedTarget(
	ctx context.Context,
	item workflow.WorkItem,
	target ClusterTarget,
) (collector.CollectedGeneration, bool, error) {
	start := s.now()
	if s.tracer != nil {
		var span trace.Span
		ctx, span = s.tracer.Start(ctx, telemetry.SpanVaultLiveSnapshot)
		span.SetAttributes(attribute.String("scope_kind", string(scope.KindVaultCluster)))
		defer span.End()
	}

	client, err := s.clientFactory.Client(ctx, target)
	if err != nil {
		return collector.CollectedGeneration{}, false, fmt.Errorf("create vault client: %w", err)
	}
	observedAt := s.now()
	scopeValue, err := s.claimedScope(target)
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	generationValue := scope.ScopeGeneration{
		ScopeID:       scopeValue.ScopeID,
		GenerationID:  strings.TrimSpace(item.GenerationID),
		Status:        scope.GenerationStatusPending,
		ObservedAt:    observedAt,
		IngestedAt:    observedAt,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: "complete",
	}
	if err := generationValue.ValidateForScope(scopeValue); err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	envelopes, err := Source{
		CollectorInstanceID: s.config.CollectorInstanceID,
		RedactionKey:        s.redactionKey,
		Instruments:         s.instruments,
	}.Collect(ctx, VaultTarget{
		VaultClusterID: target.VaultClusterID,
		Namespace:      target.Namespace,
		ScopeID:        scopeValue.ScopeID,
		GenerationID:   generationValue.GenerationID,
		FencingToken:   item.CurrentFencingToken,
		ObservedAt:     observedAt,
		SourceURI:      target.SourceURI,
	}, client)
	if err != nil {
		return collector.CollectedGeneration{}, false, fmt.Errorf("collect vault metadata: %w", err)
	}
	if hasCoverageWarning(envelopes) {
		generationValue.FreshnessHint = "partial"
	}
	s.recordClaimedTelemetry(ctx, scopeValue, observedAt, envelopes)
	if s.logger != nil {
		s.logger.InfoContext(
			ctx, "vault live claimed snapshot completed",
			telemetry.PhaseAttr(telemetry.PhaseDiscovery),
			slog.String(telemetry.LogKeyScopeID, scopeValue.ScopeID),
			slog.String(telemetry.LogKeyGenerationID, generationValue.GenerationID),
			slog.Int("fact_count", len(envelopes)),
			slog.Float64("duration_seconds", time.Since(start).Seconds()),
		)
	}
	return collector.FactsFromSlice(scopeValue, generationValue, envelopes), true, nil
}

func (s *ClaimedSource) claimedScope(target ClusterTarget) (scope.IngestionScope, error) {
	scopeID, err := VaultScopeID(target.VaultClusterID, target.Namespace)
	if err != nil {
		return scope.IngestionScope{}, err
	}
	value := scope.IngestionScope{
		ScopeID:       scopeID,
		SourceSystem:  CollectorKind,
		ScopeKind:     scope.KindVaultCluster,
		CollectorKind: scope.CollectorVaultLive,
		PartitionKey:  target.VaultClusterID,
		Metadata:      vaultScopeMetadata(target),
	}
	return value, value.Validate()
}

func (s *ClaimedSource) recordClaimedTelemetry(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	observedAt time.Time,
	envelopes []facts.Envelope,
) {
	if s.instruments == nil {
		return
	}
	for _, env := range envelopes {
		s.instruments.SecretsIAMSourceFactsEmitted.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrSource(secretsIAMSourceVault),
			telemetry.AttrFactKind(env.FactKind),
		))
		if env.FactKind == facts.SecretsIAMCoverageWarningFactKind {
			s.instruments.SecretsIAMSourcePartialScope.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrSource(secretsIAMSourceVault),
				telemetry.AttrReason(payloadStringValue(env.Payload, "resource_scope")),
			))
		}
	}
	s.instruments.SecretsIAMSourceScopeFreshness.Record(ctx, scopeFreshnessSeconds(s.now(), observedAt), metric.WithAttributes(
		telemetry.AttrSource(secretsIAMSourceVault),
		telemetry.AttrScopeKind(string(scopeValue.ScopeKind)),
	))
}

func (s *ClaimedSource) now() time.Time {
	if s.clock != nil {
		return s.clock().UTC()
	}
	return time.Now().UTC()
}
