// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// CollectorKind is the source-system / scope collector-kind identifier for the
// live Vault metadata collector.
const CollectorKind = "vault_live"

// secretsIAMSourceVault is the bounded `source` label value for the Vault lane's
// secrets/IAM source telemetry.
const secretsIAMSourceVault = "vault"

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
	RedactionKey        redact.Key
	Targets             []ClusterTarget
}

func (c Config) validated() (Config, error) {
	if strings.TrimSpace(c.CollectorInstanceID) == "" {
		return Config{}, fmt.Errorf("vault live collector instance id is required")
	}
	if len(c.Targets) == 0 {
		return Config{}, fmt.Errorf("vault live collector requires at least one target")
	}
	if c.RedactionKey.IsZero() {
		return Config{}, fmt.Errorf("vault live collector redaction key is required")
	}
	seen := make(map[string]struct{}, len(c.Targets))
	for i, target := range c.Targets {
		id := strings.TrimSpace(target.VaultClusterID)
		if id == "" {
			return Config{}, fmt.Errorf("target[%d] vault_cluster_id must not be blank", i)
		}
		key := id + "\x00" + strings.TrimSpace(target.Namespace)
		if _, dup := seen[key]; dup {
			return Config{}, fmt.Errorf("duplicate vault target scope")
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
		span.SetAttributes(attribute.String("scope_kind", string(scope.KindVaultCluster)))
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

	envelopes, err := Source{
		CollectorInstanceID: config.CollectorInstanceID,
		RedactionKey:        config.RedactionKey,
		Instruments:         s.Instruments,
	}.Collect(ctx, VaultTarget{
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

	// A coverage-warning fact means at least one Vault family list failed and the
	// generation covers only part of the cluster. Surface that as a partial
	// freshness hint (mirroring the kuberneteslive lane) so status surfaces never
	// read a partial snapshot as complete. Detection is independent of Instruments
	// so the hint is correct whether or not metrics are wired.
	if hasCoverageWarning(envelopes) {
		generationValue.FreshnessHint = "partial"
	}

	if s.Instruments != nil {
		for _, env := range envelopes {
			s.Instruments.SecretsIAMSourceFactsEmitted.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrSource(secretsIAMSourceVault),
				telemetry.AttrFactKind(env.FactKind),
			))
			// A coverage warning marks a family with partial coverage; the
			// resource_scope (a bounded family enum) is the partial reason.
			if env.FactKind == facts.SecretsIAMCoverageWarningFactKind {
				s.Instruments.SecretsIAMSourcePartialScope.Add(ctx, 1, metric.WithAttributes(
					telemetry.AttrSource(secretsIAMSourceVault),
					telemetry.AttrReason(payloadStringValue(env.Payload, "resource_scope")),
				))
			}
		}
		// Record generation freshness age at finalization (now minus the
		// observed-at the generation was stamped with). The scope kind is a
		// bounded enum; never a cluster id, namespace, or path. A clock skew that
		// yields a negative age is clamped to 0 so the gauge stays non-negative.
		s.Instruments.SecretsIAMSourceScopeFreshness.Record(ctx, scopeFreshnessSeconds(s.now(), observedAt), metric.WithAttributes(
			telemetry.AttrSource(secretsIAMSourceVault),
			telemetry.AttrScopeKind(string(scopeValue.ScopeKind)),
		))
	}

	if s.Logger != nil {
		s.Logger.InfoContext(
			ctx, "vault live snapshot completed",
			telemetry.PhaseAttr(telemetry.PhaseDiscovery),
			log.ScopeID(scopeValue.ScopeID),
			log.GenerationID(generationValue.GenerationID),
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
	scopeValue := scope.IngestionScope{
		ScopeID:       scopeID,
		SourceSystem:  CollectorKind,
		ScopeKind:     scope.KindVaultCluster,
		CollectorKind: scope.CollectorVaultLive,
		PartitionKey:  target.VaultClusterID,
		Metadata:      vaultScopeMetadata(target),
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

// scopeFreshnessSeconds returns the non-negative age in seconds of a generation
// stamped at observedAt as measured at now. A negative result (clock skew or a
// fixed test clock) is clamped to 0 so the freshness gauge never reports a
// nonsensical negative age.
func scopeFreshnessSeconds(now, observedAt time.Time) float64 {
	age := now.Sub(observedAt).Seconds()
	if age < 0 {
		return 0
	}
	return age
}

// hasCoverageWarning reports whether the collected envelopes include a
// secrets/IAM coverage-warning fact, which marks the generation as covering only
// part of the cluster (one or more Vault family lists failed).
func hasCoverageWarning(envelopes []facts.Envelope) bool {
	for _, env := range envelopes {
		if env.FactKind == facts.SecretsIAMCoverageWarningFactKind {
			return true
		}
	}
	return false
}

func vaultScopeMetadata(target ClusterTarget) map[string]string {
	metadata := map[string]string{"vault_cluster_id": strings.TrimSpace(target.VaultClusterID)}
	if namespace := strings.TrimSpace(target.Namespace); namespace != "" {
		metadata["namespace_present"] = "true"
		metadata["namespace_depth"] = fmt.Sprintf("%d", vaultNamespaceDepth(namespace))
	}
	if env := strings.TrimSpace(target.Environment); env != "" {
		metadata["environment"] = env
	}
	return metadata
}

func vaultNamespaceDepth(namespace string) int {
	namespace = strings.Trim(strings.TrimSpace(namespace), "/")
	if namespace == "" {
		return 0
	}
	depth := 0
	for _, part := range strings.Split(namespace, "/") {
		if strings.TrimSpace(part) != "" {
			depth++
		}
	}
	return depth
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
