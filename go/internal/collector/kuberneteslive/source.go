// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kuberneteslive

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
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// Config configures the snapshot Source with one or more cluster targets.
type Config struct {
	// CollectorInstanceID is the durable collector instance identity stamped on
	// every emitted fact.
	CollectorInstanceID string
	// Clusters is the set of configured read-only cluster targets.
	Clusters []ClusterTarget
}

func (c Config) validated() (Config, error) {
	if strings.TrimSpace(c.CollectorInstanceID) == "" {
		return Config{}, fmt.Errorf("kubernetes live collector instance id is required")
	}
	if len(c.Clusters) == 0 {
		return Config{}, fmt.Errorf("kubernetes live collector requires at least one cluster target")
	}
	seen := make(map[string]struct{}, len(c.Clusters))
	for i, cluster := range c.Clusters {
		id := strings.TrimSpace(cluster.ClusterID)
		if id == "" {
			return Config{}, fmt.Errorf("cluster[%d] cluster_id must not be blank", i)
		}
		if _, dup := seen[id]; dup {
			return Config{}, fmt.Errorf("duplicate cluster_id %q", id)
		}
		seen[id] = struct{}{}
	}
	return c, nil
}

// Source lists a configured core resource set from each cluster target and
// yields one snapshot generation per cluster. It is read-only and metadata-only.
//
// Concurrency note: a single Source instance is a serial producer driven by
// collector.Service.Next; the durable conflict domain is the per-cluster scope
// id, and each cluster yields exactly one generation. Listing multiple clusters
// is intentionally one-cluster-per-Next so each generation commits atomically
// through the shared committer; parallel multi-cluster collection is a
// follow-up that must partition by the per-cluster scope id (its natural
// conflict key) rather than serialize, per the repo's concurrency contract.
type Source struct {
	Config        Config
	ClientFactory ClientFactory
	Tracer        trace.Tracer
	Instruments   *telemetry.Instruments
	Logger        *slog.Logger
	Clock         func() time.Time

	next int
}

// Next returns the next configured cluster snapshot generation. It returns
// ok=false to signal the batch is drained, resetting for the next poll.
func (s *Source) Next(ctx context.Context) (collector.CollectedGeneration, bool, error) {
	config, err := s.Config.validated()
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	if s.ClientFactory == nil {
		return collector.CollectedGeneration{}, false, fmt.Errorf("kubernetes live client factory is required")
	}
	if s.next >= len(config.Clusters) {
		s.next = 0
		return collector.CollectedGeneration{}, false, nil
	}
	target := config.Clusters[s.next]
	s.next++
	collected, err := s.collectCluster(ctx, config, target)
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	return collected, true, nil
}

func (s *Source) collectCluster(ctx context.Context, config Config, target ClusterTarget) (collector.CollectedGeneration, error) {
	start := s.now()
	if s.Tracer != nil {
		var span trace.Span
		ctx, span = s.Tracer.Start(ctx, telemetry.SpanKubernetesLiveSnapshot)
		span.SetAttributes(attribute.String("cluster_id", target.ClusterID))
		defer span.End()
	}

	client, err := s.ClientFactory.Client(ctx, target)
	if err != nil {
		return collector.CollectedGeneration{}, fmt.Errorf("create kubernetes client: %w", err)
	}
	if err := s.recordAPICall(ctx, "ping", func(context.Context) error { return client.PingReadOnly(ctx) }); err != nil {
		return collector.CollectedGeneration{}, fmt.Errorf("ping kubernetes cluster: %w", err)
	}

	observedAt := s.now()
	builder := &generationBuilder{
		source:              s,
		target:              target,
		collectorInstanceID: config.CollectorInstanceID,
		observedAt:          observedAt,
	}
	if err := builder.run(ctx, client); err != nil {
		return collector.CollectedGeneration{}, err
	}

	scopeValue, generationValue, err := s.scopeAndGeneration(target, observedAt, builder.partial)
	if err != nil {
		return collector.CollectedGeneration{}, err
	}
	envelopes := builder.envelopes
	if s.Instruments != nil {
		s.Instruments.KubernetesLiveListDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
			telemetry.AttrResourceScope("cluster"),
		))
	}
	if s.Logger != nil {
		s.Logger.InfoContext(
			ctx, "kubernetes live snapshot completed",
			telemetry.PhaseAttr(telemetry.PhaseDiscovery),
			log.ScopeID(scopeValue.ScopeID),
			log.GenerationID(generationValue.GenerationID),
			log.ClusterID(target.ClusterID),
			slog.Bool("partial", builder.partial),
			slog.Int("fact_count", len(envelopes)),
			slog.Float64("duration_seconds", time.Since(start).Seconds()),
		)
	}
	return collector.FactsFromSlice(scopeValue, generationValue, envelopes), nil
}

func (s *Source) scopeAndGeneration(target ClusterTarget, observedAt time.Time, partial bool) (scope.IngestionScope, scope.ScopeGeneration, error) {
	scopeID, err := ClusterScopeID(target.ClusterID)
	if err != nil {
		return scope.IngestionScope{}, scope.ScopeGeneration{}, err
	}
	freshness := "complete"
	if partial {
		freshness = "partial"
	}
	generationID := clusterGenerationID(target.ClusterID, observedAt)
	metadata := map[string]string{"cluster_id": target.ClusterID}
	if provider := strings.TrimSpace(target.Provider); provider != "" {
		metadata["provider"] = provider
	}
	if env := strings.TrimSpace(target.Environment); env != "" {
		metadata["environment"] = env
	}
	scopeValue := scope.IngestionScope{
		ScopeID:       scopeID,
		SourceSystem:  CollectorKind,
		ScopeKind:     scope.KindCluster,
		CollectorKind: scope.CollectorKubernetesLive,
		PartitionKey:  target.ClusterID,
		Metadata:      metadata,
	}
	generationValue := scope.ScopeGeneration{
		ScopeID:       scopeID,
		GenerationID:  generationID,
		Status:        scope.GenerationStatusPending,
		ObservedAt:    observedAt,
		IngestedAt:    observedAt,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: freshness,
	}
	return scopeValue, generationValue, generationValue.ValidateForScope(scopeValue)
}

func (s *Source) recordAPICall(ctx context.Context, operation string, call func(context.Context) error) error {
	if s.Tracer != nil {
		var span trace.Span
		ctx, span = s.Tracer.Start(ctx, telemetry.SpanKubernetesLiveAPICall)
		span.SetAttributes(attribute.String("operation", operation))
		defer span.End()
	}
	err := call(ctx)
	result := "success"
	if err != nil {
		result = "error"
	}
	if s.Instruments != nil {
		s.Instruments.KubernetesLiveAPICalls.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrOperation(operation),
			telemetry.AttrResult(result),
		))
	}
	return err
}

func (s *Source) recordResourcesListed(ctx context.Context, resourceScope string, count int, partial bool) {
	if s.Instruments == nil {
		return
	}
	result := "success"
	if partial {
		result = "partial"
	}
	s.Instruments.KubernetesLiveResourcesListed.Add(ctx, int64(count), metric.WithAttributes(
		telemetry.AttrResourceScope(resourceScope),
		telemetry.AttrResult(result),
	))
}

func (s *Source) recordFactEmitted(ctx context.Context, factKind string) {
	if s.Instruments == nil {
		return
	}
	s.Instruments.KubernetesLiveFactsEmitted.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrFactKind(factKind),
	))
}

func (s *Source) recordWarning(ctx context.Context, reason string) {
	if s.Instruments == nil {
		return
	}
	s.Instruments.KubernetesLiveWarnings.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrReason(reason),
	))
}

func (s *Source) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}
