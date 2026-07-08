// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageregistry

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	packageregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/packageregistry/v1"
)

// NewPackageEnvelope builds the durable package identity fact for one package
// observation.
func NewPackageEnvelope(observation PackageObservation) (facts.Envelope, error) {
	normalized, err := NormalizePackageIdentity(observation.Identity)
	if err != nil {
		return facts.Envelope{}, err
	}
	if observation.ScopeID == "" {
		return facts.Envelope{}, fmt.Errorf("package observation scope_id must not be blank")
	}
	if observation.GenerationID == "" {
		return facts.Envelope{}, fmt.Errorf("package observation generation_id must not be blank")
	}
	if observation.CollectorInstanceID == "" {
		return facts.Envelope{}, fmt.Errorf("package observation collector_instance_id must not be blank")
	}
	observedAt := normalizedObservedAt(observation.ObservedAt)
	visibility := observation.Visibility
	if visibility == "" {
		visibility = VisibilityUnknown
	}

	stableFactKey := facts.StableID(facts.PackageRegistryPackageFactKind, map[string]any{
		"package_id": normalized.PackageID,
	})
	payload := map[string]any{
		"collector_instance_id": observation.CollectorInstanceID,
		"ecosystem":             string(normalized.Ecosystem),
		"registry":              normalized.Registry,
		"raw_name":              normalized.RawName,
		"normalized_name":       normalized.NormalizedName,
		"namespace":             normalized.Namespace,
		"classifier":            normalized.Classifier,
		"package_id":            normalized.PackageID,
		"purl":                  normalized.PURL,
		"bom_ref":               normalized.BOMRef,
		"package_manager":       normalized.PackageManager,
		"source_path":           normalized.SourcePath,
		"source_specific_id":    normalized.SourceSpecificID,
		"visibility":            string(visibility),
		"correlation_anchors":   correlationAnchors(normalized.PackageID, normalized.PURL, normalized.BOMRef),
	}
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodePackageRegistryPackage(packageregistryv1.Package{
			PackageID:           normalized.PackageID,
			Ecosystem:           stringPtr(string(normalized.Ecosystem)),
			Registry:            stringPtr(normalized.Registry),
			RawName:             stringPtr(normalized.RawName),
			NormalizedName:      stringPtr(normalized.NormalizedName),
			Namespace:           stringPtr(normalized.Namespace),
			Classifier:          stringPtr(normalized.Classifier),
			PURL:                stringPtr(normalized.PURL),
			BOMRef:              stringPtr(normalized.BOMRef),
			PackageManager:      stringPtr(normalized.PackageManager),
			SourcePath:          stringPtr(normalized.SourcePath),
			SourceSpecificID:    stringPtr(normalized.SourceSpecificID),
			Visibility:          stringPtr(string(visibility)),
			CollectorInstanceID: stringPtr(observation.CollectorInstanceID),
			CorrelationAnchors:  correlationAnchors(normalized.PackageID, normalized.PURL, normalized.BOMRef),
		})
	}); err != nil {
		return facts.Envelope{}, err
	}

	return facts.Envelope{
		FactID:           packageRegistryFactID(facts.PackageRegistryPackageFactKind, stableFactKey, observation.ScopeID, observation.GenerationID),
		ScopeID:          observation.ScopeID,
		GenerationID:     observation.GenerationID,
		FactKind:         facts.PackageRegistryPackageFactKind,
		StableFactKey:    stableFactKey,
		SchemaVersion:    facts.PackageRegistryPackageSchemaVersion,
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
			SourceRecordID: normalized.PackageID,
		},
	}, nil
}
