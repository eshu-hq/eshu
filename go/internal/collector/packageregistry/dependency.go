package packageregistry

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// NewPackageDependencyEnvelope builds the durable dependency fact for one
// source-reported package version dependency.
func NewPackageDependencyEnvelope(observation PackageDependencyObservation) (facts.Envelope, error) {
	pkg, version, versionID, err := packageVersionID(observation.Package, observation.Version, "package dependency")
	if err != nil {
		return facts.Envelope{}, err
	}
	dependency, err := NormalizePackageIdentity(observation.Dependency)
	if err != nil {
		return facts.Envelope{}, err
	}
	if err := validateObservationBoundary(
		observation.ScopeID,
		observation.GenerationID,
		observation.CollectorInstanceID,
		"package dependency observation",
	); err != nil {
		return facts.Envelope{}, err
	}

	dependencyType := strings.TrimSpace(observation.DependencyType)
	stableFactKey := facts.StableID(facts.PackageRegistryPackageDependencyFactKind, map[string]any{
		"dependency_package_id": dependency.PackageID,
		"dependency_range":      strings.TrimSpace(observation.Range),
		"dependency_type":       dependencyType,
		"excluded":              observation.Excluded,
		"marker":                strings.TrimSpace(observation.Marker),
		"optional":              observation.Optional,
		"target_framework":      strings.TrimSpace(observation.TargetFramework),
		"version_id":            versionID,
	})
	payload := map[string]any{
		"collector_instance_id":  observation.CollectorInstanceID,
		"ecosystem":              string(pkg.Ecosystem),
		"registry":               pkg.Registry,
		"package_id":             pkg.PackageID,
		"version_id":             versionID,
		"version":                version,
		"dependency_package_id":  dependency.PackageID,
		"dependency_ecosystem":   string(dependency.Ecosystem),
		"dependency_registry":    dependency.Registry,
		"dependency_namespace":   dependency.Namespace,
		"dependency_normalized":  dependency.NormalizedName,
		"dependency_range":       strings.TrimSpace(observation.Range),
		"dependency_type":        dependencyType,
		"target_framework":       strings.TrimSpace(observation.TargetFramework),
		"marker":                 strings.TrimSpace(observation.Marker),
		"optional":               observation.Optional,
		"excluded":               observation.Excluded,
		"source_record_package":  pkg.PackageID,
		"source_record_version":  version,
		"source_record_dep_kind": dependencyType,
	}

	envelope := newEnvelope(envelopeInput{
		factKind:            facts.PackageRegistryPackageDependencyFactKind,
		stableFactKey:       stableFactKey,
		schemaVersion:       facts.PackageRegistryPackageDependencySchemaVersion,
		scopeID:             observation.ScopeID,
		generationID:        observation.GenerationID,
		collectorInstanceID: observation.CollectorInstanceID,
		fencingToken:        observation.FencingToken,
		sourceURI:           observation.SourceURI,
		sourceRecordID:      versionID + "->" + dependency.PackageID,
		payload:             payload,
	})
	envelope.ObservedAt = normalizedObservedAt(observation.ObservedAt)
	return envelope, nil
}
