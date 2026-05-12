package packageregistry

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// NewPackageVersionEnvelope builds the durable package version fact for one
// registry observation.
func NewPackageVersionEnvelope(observation PackageVersionObservation) (facts.Envelope, error) {
	normalized, err := NormalizePackageIdentity(observation.Package)
	if err != nil {
		return facts.Envelope{}, err
	}
	version := strings.TrimSpace(observation.Version)
	if version == "" {
		return facts.Envelope{}, fmt.Errorf("package version must not be blank")
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
	payload := map[string]any{
		"collector_instance_id": observation.CollectorInstanceID,
		"ecosystem":             string(normalized.Ecosystem),
		"registry":              normalized.Registry,
		"package_id":            normalized.PackageID,
		"version_id":            versionID,
		"version":               version,
		"is_yanked":             observation.Yanked,
		"is_unlisted":           observation.Unlisted,
		"is_deprecated":         observation.Deprecated,
		"is_retracted":          observation.Retracted,
		"artifact_urls":         cloneStrings(observation.ArtifactURLs),
		"checksums":             cloneStringMap(observation.Checksums),
	}
	if !observation.PublishedAt.IsZero() {
		payload["published_at"] = observation.PublishedAt.UTC().Format(time.RFC3339)
	}

	return facts.Envelope{
		FactID: facts.StableID("PackageRegistryFact", map[string]any{
			"fact_kind":       facts.PackageRegistryPackageVersionFactKind,
			"stable_fact_key": stableFactKey,
		}),
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
