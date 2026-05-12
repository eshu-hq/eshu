package packageregistry

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// NewPackageArtifactEnvelope builds the durable artifact fact for one package
// version artifact.
func NewPackageArtifactEnvelope(observation PackageArtifactObservation) (facts.Envelope, error) {
	pkg, version, versionID, err := packageVersionID(observation.Package, observation.Version, "package artifact")
	if err != nil {
		return facts.Envelope{}, err
	}
	if err := validateObservationBoundary(
		observation.ScopeID,
		observation.GenerationID,
		observation.CollectorInstanceID,
		"package artifact observation",
	); err != nil {
		return facts.Envelope{}, err
	}
	artifactKey := strings.TrimSpace(observation.ArtifactKey)
	if artifactKey == "" {
		return facts.Envelope{}, fmt.Errorf("package artifact key must not be blank")
	}

	stableFactKey := facts.StableID(facts.PackageRegistryPackageArtifactFactKind, map[string]any{
		"artifact_key": artifactKey,
		"version_id":   versionID,
	})
	payload := map[string]any{
		"collector_instance_id": observation.CollectorInstanceID,
		"ecosystem":             string(pkg.Ecosystem),
		"registry":              pkg.Registry,
		"package_id":            pkg.PackageID,
		"version_id":            versionID,
		"version":               version,
		"artifact_key":          artifactKey,
		"artifact_type":         strings.TrimSpace(observation.ArtifactType),
		"artifact_url":          sanitizeURL(observation.ArtifactURL),
		"artifact_path":         strings.TrimSpace(observation.ArtifactPath),
		"size_bytes":            observation.SizeBytes,
		"hashes":                cloneStringMap(observation.Hashes),
		"classifier":            strings.TrimSpace(observation.Classifier),
		"platform_tags":         cloneStrings(observation.PlatformTags),
	}

	envelope := newEnvelope(envelopeInput{
		factKind:            facts.PackageRegistryPackageArtifactFactKind,
		stableFactKey:       stableFactKey,
		schemaVersion:       facts.PackageRegistryPackageArtifactSchemaVersion,
		scopeID:             observation.ScopeID,
		generationID:        observation.GenerationID,
		collectorInstanceID: observation.CollectorInstanceID,
		fencingToken:        observation.FencingToken,
		sourceURI:           observation.SourceURI,
		sourceRecordID:      versionID + "#" + artifactKey,
		payload:             payload,
	})
	envelope.ObservedAt = normalizedObservedAt(observation.ObservedAt)
	return envelope, nil
}
