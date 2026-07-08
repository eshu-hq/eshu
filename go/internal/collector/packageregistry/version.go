// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageregistry

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	packageregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/packageregistry/v1"
)

// NewPackageVersionEnvelope builds the durable package version fact for one
// registry observation.
func NewPackageVersionEnvelope(observation PackageVersionObservation) (facts.Envelope, error) {
	version := strings.TrimSpace(observation.Version)
	if version == "" {
		return facts.Envelope{}, fmt.Errorf("package version must not be blank")
	}
	identity := observation.Package
	identity.Version = version
	normalized, err := NormalizePackageIdentity(identity)
	if err != nil {
		return facts.Envelope{}, err
	}
	if observation.ScopeID == "" {
		return facts.Envelope{}, fmt.Errorf("package version observation scope_id must not be blank")
	}
	if observation.GenerationID == "" {
		return facts.Envelope{}, fmt.Errorf("package version observation generation_id must not be blank")
	}
	if observation.CollectorInstanceID == "" {
		return facts.Envelope{}, fmt.Errorf("package version observation collector_instance_id must not be blank")
	}

	observedAt := normalizedObservedAt(observation.ObservedAt)
	versionID := normalized.PackageID + "@" + version
	stableFactKey := facts.StableID(facts.PackageRegistryPackageVersionFactKind, map[string]any{
		"version_id": versionID,
	})
	publishedAt := ""
	if !observation.PublishedAt.IsZero() {
		publishedAt = observation.PublishedAt.UTC().Format(time.RFC3339)
	}
	payload, err := factschema.EncodePackageRegistryPackageVersion(packageregistryv1.PackageVersion{
		PackageID:           normalized.PackageID,
		VersionID:           versionID,
		Version:             version,
		Ecosystem:           stringPtr(string(normalized.Ecosystem)),
		Registry:            stringPtr(normalized.Registry),
		PURL:                stringPtr(normalized.PURL),
		BOMRef:              stringPtr(normalized.BOMRef),
		PackageManager:      stringPtr(normalized.PackageManager),
		PublishedAt:         optionalStringPtr(publishedAt),
		IsYanked:            boolPtr(observation.Yanked),
		IsUnlisted:          boolPtr(observation.Unlisted),
		IsDeprecated:        boolPtr(observation.Deprecated),
		IsRetracted:         boolPtr(observation.Retracted),
		ArtifactURLs:        cloneStrings(observation.ArtifactURLs),
		Checksums:           cloneStringMap(observation.Checksums),
		CollectorInstanceID: stringPtr(observation.CollectorInstanceID),
		CorrelationAnchors:  correlationAnchors(normalized.PackageID, versionID, normalized.PURL, normalized.BOMRef),
	})
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("encode package version payload: %w", err)
	}

	return facts.Envelope{
		FactID:           packageRegistryFactID(facts.PackageRegistryPackageVersionFactKind, stableFactKey, observation.ScopeID, observation.GenerationID),
		ScopeID:          observation.ScopeID,
		GenerationID:     observation.GenerationID,
		FactKind:         facts.PackageRegistryPackageVersionFactKind,
		StableFactKey:    stableFactKey,
		SchemaVersion:    facts.PackageRegistryPackageVersionSchemaVersion,
		CollectorKind:    CollectorKind,
		FencingToken:     observation.FencingToken,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       observedAt,
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   CollectorKind,
			ScopeID:        observation.ScopeID,
			GenerationID:   observation.GenerationID,
			FactKey:        stableFactKey,
			SourceURI:      observation.SourceURI,
			SourceRecordID: versionID,
		},
	}, nil
}

func normalizedObservedAt(observedAt time.Time) time.Time {
	if observedAt.IsZero() {
		return time.Now().UTC()
	}
	return observedAt.UTC()
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	cloned := make([]string, len(input))
	copy(cloned, input)
	return cloned
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}
