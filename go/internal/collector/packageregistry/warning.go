package packageregistry

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// NewWarningEnvelope builds the durable warning fact for one non-fatal
// package-registry collection issue.
func NewWarningEnvelope(observation WarningObservation) (facts.Envelope, error) {
	if err := validateObservationBoundary(
		observation.ScopeID,
		observation.GenerationID,
		observation.CollectorInstanceID,
		"warning observation",
	); err != nil {
		return facts.Envelope{}, err
	}
	warningKey := strings.TrimSpace(observation.WarningKey)
	if warningKey == "" {
		return facts.Envelope{}, fmt.Errorf("warning key must not be blank")
	}
	warningCode := strings.TrimSpace(observation.WarningCode)
	if warningCode == "" {
		return facts.Envelope{}, fmt.Errorf("warning code must not be blank")
	}

	payload := map[string]any{
		"collector_instance_id": observation.CollectorInstanceID,
		"warning_key":           warningKey,
		"warning_code":          warningCode,
		"severity":              strings.TrimSpace(observation.Severity),
		"message":               sanitizeText(observation.Message),
		"correlation_anchors":   correlationAnchors(observation.ScopeID),
	}
	stableIdentity := map[string]any{
		"scope_id":     observation.ScopeID,
		"warning_code": warningCode,
		"warning_key":  warningKey,
	}
	if observation.Package != nil {
		normalized, err := NormalizePackageIdentity(*observation.Package)
		if err != nil {
			return facts.Envelope{}, err
		}
		version := strings.TrimSpace(observation.Version)
		versionID := ""
		if version != "" {
			versionID = normalized.PackageID + "@" + version
		}
		payload["ecosystem"] = string(normalized.Ecosystem)
		payload["registry"] = normalized.Registry
		payload["package_id"] = normalized.PackageID
		payload["version"] = version
		payload["version_id"] = versionID
		payload["correlation_anchors"] = correlationAnchors(observation.ScopeID, normalized.PackageID, versionID)
		stableIdentity["package_id"] = normalized.PackageID
		stableIdentity["version"] = version
	}

	stableFactKey := facts.StableID(facts.PackageRegistryWarningFactKind, stableIdentity)
	envelope := newEnvelope(envelopeInput{
		factKind:            facts.PackageRegistryWarningFactKind,
		stableFactKey:       stableFactKey,
		schemaVersion:       facts.PackageRegistryWarningSchemaVersion,
		scopeID:             observation.ScopeID,
		generationID:        observation.GenerationID,
		collectorInstanceID: observation.CollectorInstanceID,
		fencingToken:        observation.FencingToken,
		sourceURI:           observation.SourceURI,
		sourceRecordID:      warningKey,
		payload:             payload,
	})
	envelope.ObservedAt = normalizedObservedAt(observation.ObservedAt)
	return envelope, nil
}
