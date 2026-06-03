package vaultlive

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// CollectorKind is the source-system / scope collector-kind identifier for the
// live Vault metadata collector.
const CollectorKind = "vault_live"

// ClusterTarget is one configured Vault collection boundary: a Vault cluster
// and namespace. It carries durable, non-secret identity only; connection
// details and the read-only token live in the client factory, never in the
// scope.
type ClusterTarget struct {
	VaultClusterID string
	Namespace      string
	DisplayName    string
	Environment    string
	FencingToken   int64
	SourceURI      string
}

// Config configures the snapshot source with one or more Vault targets.
type Config struct {
	CollectorInstanceID string
	Targets             []ClusterTarget
}

func (c Config) validated() (Config, error) {
	if strings.TrimSpace(c.CollectorInstanceID) == "" {
		return Config{}, fmt.Errorf("vault live collector instance id is required")
	}
	if len(c.Targets) == 0 {
		return Config{}, fmt.Errorf("vault live collector requires at least one target")
	}
	seen := make(map[string]struct{}, len(c.Targets))
	for i, target := range c.Targets {
		id := strings.TrimSpace(target.VaultClusterID)
		if id == "" {
			return Config{}, fmt.Errorf("target[%d] vault_cluster_id must not be blank", i)
		}
		key := id + "\x00" + strings.TrimSpace(target.Namespace)
		if _, dup := seen[key]; dup {
			return Config{}, fmt.Errorf("duplicate vault target %q (namespace %q)", id, target.Namespace)
		}
		seen[key] = struct{}{}
	}
	return c, nil
}

// ClientFactory creates a read-only, metadata-only Vault Client for one target.
// Implementations own credentials; the source never sees the token.
type ClientFactory interface {
	Client(context.Context, ClusterTarget) (Client, error)
}

// SnapshotSource lists the configured Vault metadata families from each target
// and yields one snapshot generation per target. It is read-only and
// metadata-only, mirroring the kuberneteslive snapshot model: a serial producer
// driven by collector.Service.Next, with the per-target scope id as the durable
// conflict domain.
type SnapshotSource struct {
	Config        Config
	ClientFactory ClientFactory
	Tracer        trace.Tracer
	Instruments   *telemetry.Instruments
	Logger        *slog.Logger
	Clock         func() time.Time

	next int
}

// Next returns the next configured Vault snapshot generation. It returns
// ok=false to signal the batch is drained, resetting for the next poll.
func (s *SnapshotSource) Next(ctx context.Context) (collector.CollectedGeneration, bool, error) {
	config, err := s.Config.validated()
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	if s.ClientFactory == nil {
		return collector.CollectedGeneration{}, false, fmt.Errorf("vault live client factory is required")
	}
	if s.next >= len(config.Targets) {
		s.next = 0
		return collector.CollectedGeneration{}, false, nil
	}
	target := config.Targets[s.next]
	s.next++
	collected, err := s.collectTarget(ctx, config, target)
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	return collected, true, nil
}

func (s *SnapshotSource) collectTarget(ctx context.Context, config Config, target ClusterTarget) (collector.CollectedGeneration, error) {
	start := s.now()
	if s.Tracer != nil {
		var span trace.Span
		ctx, span = s.Tracer.Start(ctx, telemetry.SpanVaultLiveSnapshot)
		span.SetAttributes(attribute.String("vault_cluster_id", target.VaultClusterID))
		defer span.End()
	}

	client, err := s.ClientFactory.Client(ctx, target)
	if err != nil {
		return collector.CollectedGeneration{}, fmt.Errorf("create vault client: %w", err)
	}

	observedAt := s.now()
	scopeValue, generationValue, err := s.scopeAndGeneration(target, observedAt)
	if err != nil {
		return collector.CollectedGeneration{}, err
	}

	envelopes, err := Source{CollectorInstanceID: config.CollectorInstanceID}.Collect(ctx, VaultTarget{
		VaultClusterID: target.VaultClusterID,
		Namespace:      target.Namespace,
		ScopeID:        scopeValue.ScopeID,
		GenerationID:   generationValue.GenerationID,
		FencingToken:   target.FencingToken,
		ObservedAt:     observedAt,
		SourceURI:      target.SourceURI,
	}, client)
	if err != nil {
		return collector.CollectedGeneration{}, fmt.Errorf("collect vault metadata: %w", err)
	}

	if s.Logger != nil {
		s.Logger.InfoContext(ctx, "vault live snapshot completed",
			telemetry.PhaseAttr(telemetry.PhaseDiscovery),
			slog.String(telemetry.LogKeyScopeID, scopeValue.ScopeID),
			slog.String(telemetry.LogKeyGenerationID, generationValue.GenerationID),
			slog.String("vault_cluster_id", target.VaultClusterID),
			slog.Int("fact_count", len(envelopes)),
			slog.Float64("duration_seconds", time.Since(start).Seconds()),
		)
	}
	return collector.FactsFromSlice(scopeValue, generationValue, envelopes), nil
}

func (s *SnapshotSource) scopeAndGeneration(target ClusterTarget, observedAt time.Time) (scope.IngestionScope, scope.ScopeGeneration, error) {
	scopeID, err := VaultScopeID(target.VaultClusterID, target.Namespace)
	if err != nil {
		return scope.IngestionScope{}, scope.ScopeGeneration{}, err
	}
	metadata := map[string]string{"vault_cluster_id": target.VaultClusterID}
	if ns := strings.TrimSpace(target.Namespace); ns != "" {
		metadata["namespace"] = ns
	}
	if env := strings.TrimSpace(target.Environment); env != "" {
		metadata["environment"] = env
	}
	scopeValue := scope.IngestionScope{
		ScopeID:       scopeID,
		SourceSystem:  CollectorKind,
		ScopeKind:     scope.KindVaultCluster,
		CollectorKind: scope.CollectorVaultLive,
		PartitionKey:  target.VaultClusterID,
		Metadata:      metadata,
	}
	generationValue := scope.ScopeGeneration{
		ScopeID:       scopeID,
		GenerationID:  vaultGenerationID(target.VaultClusterID, target.Namespace, observedAt),
		Status:        scope.GenerationStatusPending,
		ObservedAt:    observedAt,
		IngestedAt:    observedAt,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: "complete",
	}
	return scopeValue, generationValue, generationValue.ValidateForScope(scopeValue)
}

func (s *SnapshotSource) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

// VaultScopeID returns the deterministic durable scope id for one Vault cluster
// and namespace. The namespace is part of the scope identity per the contract
// ({vault_cluster_id, namespace}).
func VaultScopeID(clusterID, namespace string) (string, error) {
	clusterID = strings.TrimSpace(clusterID)
	if clusterID == "" {
		return "", fmt.Errorf("vault_cluster_id must not be blank")
	}
	return CollectorKind + ":" + facts.StableID("VaultLiveScope", map[string]any{
		"vault_cluster_id": clusterID,
		"namespace":        strings.TrimSpace(namespace),
	}), nil
}

// vaultGenerationID returns the deterministic generation id for one Vault
// snapshot, so every fact in the snapshot shares one generation id.
func vaultGenerationID(clusterID, namespace string, observedAt time.Time) string {
	return CollectorKind + ":" + facts.StableID("VaultLiveGeneration", map[string]any{
		"vault_cluster_id": strings.TrimSpace(clusterID),
		"namespace":        strings.TrimSpace(namespace),
		"observed":         observedAt.UTC().Format(time.RFC3339Nano),
	})
}
