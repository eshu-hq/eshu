package packageregistry

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// NewRepositoryHostingEnvelope builds the durable provider topology fact for a
// package repository or feed.
func NewRepositoryHostingEnvelope(observation RepositoryHostingObservation) (facts.Envelope, error) {
	if err := validateObservationBoundary(
		observation.ScopeID,
		observation.GenerationID,
		observation.CollectorInstanceID,
		"repository hosting observation",
	); err != nil {
		return facts.Envelope{}, err
	}
	provider := strings.TrimSpace(observation.Provider)
	if provider == "" {
		return facts.Envelope{}, fmt.Errorf("repository hosting provider must not be blank")
	}
	registry := normalizeRegistry(observation.Registry)
	if registry == "" {
		return facts.Envelope{}, fmt.Errorf("repository hosting registry must not be blank")
	}
	repository := strings.TrimSpace(observation.Repository)
	if repository == "" {
		return facts.Envelope{}, fmt.Errorf("repository hosting repository must not be blank")
	}
	repositoryType := strings.TrimSpace(observation.RepositoryType)
	repositoryID := provider + "://" + registry + "/" + repository

	stableFactKey := facts.StableID(facts.PackageRegistryRepositoryHostingFactKind, map[string]any{
		"provider":        provider,
		"registry":        registry,
		"repository":      repository,
		"repository_type": repositoryType,
	})
	payload := map[string]any{
		"collector_instance_id": observation.CollectorInstanceID,
		"provider":              provider,
		"registry":              registry,
		"repository":            repository,
		"repository_type":       repositoryType,
		"ecosystem":             string(observation.Ecosystem),
		"upstream_id":           strings.TrimSpace(observation.UpstreamID),
		"upstream_url":          sanitizeURL(observation.UpstreamURL),
		"correlation_anchors":   correlationAnchors(repositoryID, strings.TrimSpace(observation.UpstreamID), sanitizeURL(observation.UpstreamURL)),
	}

	envelope := newEnvelope(envelopeInput{
		factKind:            facts.PackageRegistryRepositoryHostingFactKind,
		stableFactKey:       stableFactKey,
		schemaVersion:       facts.PackageRegistryRepositoryHostingSchemaVersion,
		scopeID:             observation.ScopeID,
		generationID:        observation.GenerationID,
		collectorInstanceID: observation.CollectorInstanceID,
		fencingToken:        observation.FencingToken,
		sourceURI:           observation.SourceURI,
		sourceRecordID:      repositoryID,
		payload:             payload,
	})
	envelope.ObservedAt = normalizedObservedAt(observation.ObservedAt)
	return envelope, nil
}
