package packageregistry

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// NewRegistryEventEnvelope builds the durable source-reported registry event
// fact for one package or package version.
func NewRegistryEventEnvelope(observation RegistryEventObservation) (facts.Envelope, error) {
	pkg, version, versionID, err := optionalPackageVersionID(
		observation.Package,
		observation.Version,
	)
	if err != nil {
		return facts.Envelope{}, err
	}
	if err := validateObservationBoundary(
		observation.ScopeID,
		observation.GenerationID,
		observation.CollectorInstanceID,
		"registry event observation",
	); err != nil {
		return facts.Envelope{}, err
	}
	eventKey := strings.TrimSpace(observation.EventKey)
	if eventKey == "" {
		return facts.Envelope{}, fmt.Errorf("registry event key must not be blank")
	}
	eventType := strings.TrimSpace(observation.EventType)
	if eventType == "" {
		return facts.Envelope{}, fmt.Errorf("registry event type must not be blank")
	}
	artifactKey := strings.TrimSpace(observation.ArtifactKey)

	stableFactKey := facts.StableID(facts.PackageRegistryRegistryEventFactKind, map[string]any{
		"artifact_key": artifactKey,
		"event_key":    eventKey,
		"event_type":   eventType,
		"package_id":   pkg.PackageID,
		"version":      version,
	})
	payload := map[string]any{
		"collector_instance_id": observation.CollectorInstanceID,
		"ecosystem":             string(pkg.Ecosystem),
		"registry":              pkg.Registry,
		"package_id":            pkg.PackageID,
		"version_id":            versionID,
		"version":               version,
		"event_key":             eventKey,
		"event_type":            eventType,
		"artifact_key":          artifactKey,
		"actor":                 strings.TrimSpace(observation.Actor),
		"message":               sanitizeText(observation.Message),
		"occurred_at":           observation.OccurredAt,
		"correlation_anchors":   correlationAnchors(pkg.PackageID, versionID, eventKey, artifactKey),
	}

	envelope := newEnvelope(envelopeInput{
		factKind:            facts.PackageRegistryRegistryEventFactKind,
		stableFactKey:       stableFactKey,
		schemaVersion:       facts.PackageRegistryRegistryEventSchemaVersion,
		scopeID:             observation.ScopeID,
		generationID:        observation.GenerationID,
		collectorInstanceID: observation.CollectorInstanceID,
		fencingToken:        observation.FencingToken,
		sourceURI:           observation.SourceURI,
		sourceRecordID:      eventKey,
		payload:             payload,
	})
	envelope.ObservedAt = normalizedObservedAt(observation.ObservedAt)
	return envelope, nil
}
