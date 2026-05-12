package packageregistry

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
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
		"visibility":            string(visibility),
	}

	return facts.Envelope{
		FactID: facts.StableID("PackageRegistryFact", map[string]any{
			"fact_kind":       facts.PackageRegistryPackageFactKind,
			"stable_fact_key": stableFactKey,
		}),
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
