package packageregistry

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// NewSourceHintEnvelope builds the durable source-hint fact for one package
// metadata source, homepage, SCM, or provenance hint.
func NewSourceHintEnvelope(observation SourceHintObservation) (facts.Envelope, error) {
	pkg, err := NormalizePackageIdentity(observation.Package)
	if err != nil {
		return facts.Envelope{}, err
	}
	if err := validateObservationBoundary(
		observation.ScopeID,
		observation.GenerationID,
		observation.CollectorInstanceID,
		"source hint observation",
	); err != nil {
		return facts.Envelope{}, err
	}
	hintKind := strings.TrimSpace(observation.HintKind)
	if hintKind == "" {
		return facts.Envelope{}, fmt.Errorf("source hint kind must not be blank")
	}
	rawURL := sanitizeURL(observation.RawURL)
	normalizedURL := sanitizeURL(observation.NormalizedURL)
	stableURL := normalizedURL
	if stableURL == "" {
		stableURL = rawURL
	}
	if stableURL == "" {
		return facts.Envelope{}, fmt.Errorf("source hint URL must not be blank")
	}
	version := strings.TrimSpace(observation.Version)
	versionID := ""
	if version != "" {
		versionID = pkg.PackageID + "@" + version
	}

	stableFactKey := facts.StableID(facts.PackageRegistrySourceHintFactKind, map[string]any{
		"hint_kind":      hintKind,
		"package_id":     pkg.PackageID,
		"source_url":     stableURL,
		"source_version": version,
	})
	payload := map[string]any{
		"collector_instance_id": observation.CollectorInstanceID,
		"ecosystem":             string(pkg.Ecosystem),
		"registry":              pkg.Registry,
		"package_id":            pkg.PackageID,
		"version_id":            versionID,
		"version":               version,
		"hint_kind":             hintKind,
		"raw_url":               rawURL,
		"normalized_url":        normalizedURL,
		"confidence_reason":     strings.TrimSpace(observation.ConfidenceReason),
	}

	envelope := newEnvelope(envelopeInput{
		factKind:            facts.PackageRegistrySourceHintFactKind,
		stableFactKey:       stableFactKey,
		schemaVersion:       facts.PackageRegistrySourceHintSchemaVersion,
		scopeID:             observation.ScopeID,
		generationID:        observation.GenerationID,
		collectorInstanceID: observation.CollectorInstanceID,
		fencingToken:        observation.FencingToken,
		sourceURI:           observation.SourceURI,
		sourceRecordID:      pkg.PackageID + "#" + hintKind + "#" + stableURL,
		payload:             payload,
	})
	envelope.ObservedAt = normalizedObservedAt(observation.ObservedAt)
	return envelope, nil
}
