// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomruntime

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/sbomdocument"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// DocumentProvider fetches one bounded SBOM or attestation document.
type DocumentProvider interface {
	FetchDocument(context.Context, TargetConfig) (Document, error)
}

// Document is the parser input returned by a hosted document provider.
type Document struct {
	Body           []byte
	SourceURI      string
	SourceRecordID string
	MediaType      string
	ObservedAt     time.Time
}

// ClaimedSource implements collector.ClaimedSource for hosted SBOM and
// attestation targets.
type ClaimedSource struct {
	collectorInstanceID string
	targets             map[string]TargetConfig
	provider            DocumentProvider
	now                 func() time.Time
}

// NewClaimedSource validates configuration and builds a claim-driven source.
func NewClaimedSource(config SourceConfig) (*ClaimedSource, error) {
	collectorID := strings.TrimSpace(config.CollectorInstanceID)
	if collectorID == "" {
		return nil, fmt.Errorf("SBOM attestation collector instance ID is required")
	}
	if config.Provider == nil {
		return nil, fmt.Errorf("SBOM attestation document provider is required")
	}
	targets := make(map[string]TargetConfig, len(config.Targets))
	for i, target := range config.Targets {
		validated, err := target.validate()
		if err != nil {
			return nil, fmt.Errorf("target %d: %w", i, err)
		}
		if _, exists := targets[validated.ScopeID]; exists {
			return nil, fmt.Errorf("duplicate SBOM attestation target scope_id %q", validated.ScopeID)
		}
		targets[validated.ScopeID] = validated
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("at least one SBOM attestation target is required")
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &ClaimedSource{
		collectorInstanceID: collectorID,
		targets:             targets,
		provider:            config.Provider,
		now:                 now,
	}, nil
}

// NextClaimed collects the hosted SBOM or attestation target named by
// item.ScopeID.
func (s *ClaimedSource) NextClaimed(
	ctx context.Context,
	item workflow.WorkItem,
) (collector.CollectedGeneration, bool, error) {
	if strings.TrimSpace(item.CollectorInstanceID) != s.collectorInstanceID {
		return collector.CollectedGeneration{}, false, fmt.Errorf(
			"SBOM attestation work item collector_instance_id %q does not match source %q",
			item.CollectorInstanceID,
			s.collectorInstanceID,
		)
	}
	if item.CollectorKind != "" && item.CollectorKind != scope.CollectorSBOMAttestation {
		return collector.CollectedGeneration{}, false, fmt.Errorf(
			"SBOM attestation source cannot collect %q work items",
			item.CollectorKind,
		)
	}
	if strings.TrimSpace(item.GenerationID) == "" {
		return collector.CollectedGeneration{}, false, fmt.Errorf("SBOM attestation work item generation_id is required")
	}
	target, ok := s.targets[strings.TrimSpace(item.ScopeID)]
	if !ok {
		return collector.CollectedGeneration{}, false, fmt.Errorf(
			"SBOM attestation target scope_id %q is not configured for collector instance %q",
			item.ScopeID,
			s.collectorInstanceID,
		)
	}
	document, err := s.provider.FetchDocument(ctx, target)
	if err != nil {
		return collector.CollectedGeneration{}, false, fmt.Errorf("fetch SBOM attestation document: %w", err)
	}
	envs, observedAt, err := s.envelopes(item, target, document)
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	return collector.FactsFromSlice(
		ingestionScope(target),
		scope.ScopeGeneration{
			GenerationID: item.GenerationID,
			ScopeID:      target.ScopeID,
			ObservedAt:   observedAt,
			IngestedAt:   observedAt,
			Status:       scope.GenerationStatusPending,
			TriggerKind:  scope.TriggerKindSnapshot,
		},
		envs,
	), true, nil
}

func (s *ClaimedSource) envelopes(
	item workflow.WorkItem,
	target TargetConfig,
	document Document,
) ([]facts.Envelope, time.Time, error) {
	observedAt := document.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = s.now().UTC()
	}
	sourceURI := safeSourceURI(firstNonBlank(document.SourceURI, target.SourceURI, target.DocumentURL, target.Registry))
	sourceRecordID := firstNonBlank(document.SourceRecordID, target.SourceRecordID, target.ReferrerDigest, target.DocumentURL, target.ScopeID)
	switch target.ArtifactKind {
	case ArtifactKindSBOM:
		envs, err := sbomEnvelopes(item, target, document.Body, observedAt, sourceURI, sourceRecordID)
		return envs, observedAt, err
	case ArtifactKindAttestation:
		envs, err := attestationEnvelopes(item, target, document.Body, observedAt, sourceURI, sourceRecordID)
		return envs, observedAt, err
	default:
		return nil, time.Time{}, fmt.Errorf("unsupported artifact_kind %q", target.ArtifactKind)
	}
}

func sbomEnvelopes(
	item workflow.WorkItem,
	target TargetConfig,
	raw []byte,
	observedAt time.Time,
	sourceURI string,
	sourceRecordID string,
) ([]facts.Envelope, error) {
	ctx := sbomdocument.FixtureContext{
		ScopeID:             target.ScopeID,
		GenerationID:        item.GenerationID,
		CollectorInstanceID: strings.TrimSpace(item.CollectorInstanceID),
		FencingToken:        item.CurrentFencingToken,
		ObservedAt:          observedAt,
		SourceURI:           sourceURI,
		SourceRecordID:      sourceRecordID,
	}
	switch target.DocumentFormat {
	case DocumentFormatCycloneDX:
		return sbomdocument.CycloneDXFixtureEnvelopes(raw, ctx)
	case DocumentFormatSPDX:
		return sbomdocument.SPDXFixtureEnvelopes(raw, ctx)
	default:
		return nil, fmt.Errorf("unsupported SBOM document_format %q", target.DocumentFormat)
	}
}

func ingestionScope(target TargetConfig) scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       target.ScopeID,
		SourceSystem:  string(scope.CollectorSBOMAttestation),
		ScopeKind:     scope.KindSBOMAttestation,
		CollectorKind: scope.CollectorSBOMAttestation,
		PartitionKey:  partitionKey(target),
		Metadata: map[string]string{
			"artifact_kind":   string(target.ArtifactKind),
			"document_format": string(target.DocumentFormat),
			"source_type":     string(target.SourceType),
		},
	}
}

func partitionKey(target TargetConfig) string {
	return fmt.Sprintf("%s:%s", target.SourceType, target.ArtifactKind)
}

func safeSourceURI(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String()
}
