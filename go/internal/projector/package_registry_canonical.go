// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// PackageRegistryPackageRow carries one stable package identity for canonical
// graph projection. Source hints are intentionally not represented here because
// registry metadata alone is provenance, not repository ownership truth.
type PackageRegistryPackageRow struct {
	UID                 string
	Ecosystem           string
	Registry            string
	RawName             string
	NormalizedName      string
	Namespace           string
	Classifier          string
	PURL                string
	BOMRef              string
	PackageManager      string
	SourcePath          string
	SourceSpecificID    string
	Visibility          string
	SourceFactID        string
	StableFactKey       string
	SourceSystem        string
	SourceRecordID      string
	SourceConfidence    string
	CollectorKind       string
	CorrelationAnchors  []string
	CollectorInstanceID string
	ObservedAt          time.Time
}

// PackageRegistryVersionRow carries one stable package version identity for
// canonical graph projection.
type PackageRegistryVersionRow struct {
	UID                 string
	PackageID           string
	Ecosystem           string
	Registry            string
	Version             string
	PURL                string
	BOMRef              string
	PackageManager      string
	PublishedAt         time.Time
	IsYanked            bool
	IsUnlisted          bool
	IsDeprecated        bool
	IsRetracted         bool
	ArtifactURLs        []string
	Checksums           map[string]string
	SourceFactID        string
	StableFactKey       string
	SourceSystem        string
	SourceRecordID      string
	SourceConfidence    string
	CollectorKind       string
	CorrelationAnchors  []string
	CollectorInstanceID string
	ObservedAt          time.Time
}

// PackageRegistryDependencyRow carries one package-native dependency edge with
// ecosystem-specific scope, type, marker, and optional/exclusion semantics.
type PackageRegistryDependencyRow struct {
	UID                  string
	PackageID            string
	VersionID            string
	Version              string
	DependencyPackageID  string
	DependencyEcosystem  string
	DependencyRegistry   string
	DependencyNamespace  string
	DependencyNormalized string
	DependencyPURL       string
	DependencyBOMRef     string
	DependencyManager    string
	DependencyRange      string
	DependencyType       string
	TargetFramework      string
	Marker               string
	Optional             bool
	Excluded             bool
	SourceFactID         string
	StableFactKey        string
	SourceSystem         string
	SourceRecordID       string
	SourceConfidence     string
	CollectorKind        string
	CorrelationAnchors   []string
	CollectorInstanceID  string
	ObservedAt           time.Time
}

// packageRegistryCanonicalStage is the bounded telemetry stage label the
// projector's package_registry canonical extractor reports on
// eshu_dp_projector_input_invalid_facts_total.
const packageRegistryCanonicalStage = "package_registry_canonical"

// extractPackageRegistryRows projects committed package_registry fact
// envelopes into canonical package/version/dependency rows on mat, decoding
// each fact through the typed factschema seam. A fact missing a required
// identity field is QUARANTINED per-fact (returned in the []quarantinedFact
// slice) rather than producing a graph identity from an empty-string segment:
// that one fact is skipped while every valid fact — package_registry and
// non-package_registry — still projects. The caller
// (buildCanonicalMaterialization) records the quarantined facts as visible
// input_invalid dead-letters via recordProjectorQuarantinedFacts. A
// present-but-empty identity field is a valid decode that the row builders'
// own identity gate still drops, byte-identical to the pre-typing behavior.
//
// package_registry.source_hint, .package_artifact, .vulnerability_hint,
// .registry_event, .repository_hosting, and .warning are intentionally not
// consumed here (typed-but-deferred, no projector read site today), so no case
// handles them.
func extractPackageRegistryRows(mat *CanonicalMaterialization, envelopes []facts.Envelope) []quarantinedFact {
	if mat == nil || len(envelopes) == 0 {
		return nil
	}
	var quarantined []quarantinedFact
	for _, envelope := range envelopes {
		var err error
		switch envelope.FactKind {
		case facts.PackageRegistryPackageFactKind:
			var row PackageRegistryPackageRow
			var ok bool
			row, ok, err = packageRegistryPackageRow(envelope)
			if ok {
				mat.PackageRegistryPackages = append(mat.PackageRegistryPackages, row)
			}
		case facts.PackageRegistryPackageVersionFactKind:
			var row PackageRegistryVersionRow
			var ok bool
			row, ok, err = packageRegistryVersionRow(envelope)
			if ok {
				mat.PackageRegistryVersions = append(mat.PackageRegistryVersions, row)
			}
		case facts.PackageRegistryPackageDependencyFactKind:
			var row PackageRegistryDependencyRow
			var ok bool
			row, ok, err = packageRegistryDependencyRow(envelope)
			if ok {
				mat.PackageRegistryDependencies = append(mat.PackageRegistryDependencies, row)
			}
		default:
			continue
		}
		if err == nil {
			continue
		}
		q, isQuarantine, fatal := partitionProjectorDecodeFailures(envelope, err)
		if fatal != nil {
			// The only fatal decode error is an unsupported schema major, which
			// the projector's schema-version admission (validateFactSchemaVersion
			// in runtime.go) already rejects for the whole work item BEFORE this
			// extractor runs, so a fatal here is unreachable on the production
			// path. Dropping it matches the pre-typing extractor's behavior for a
			// fact it could not read, and never fails the whole repository
			// projection over one fact.
			continue
		}
		if isQuarantine {
			quarantined = append(quarantined, q)
		}
	}
	return quarantined
}

// packageRegistryPackageRow decodes one package_registry.package envelope
// through the typed factschema seam and builds its canonical row. A missing
// required package_id dead-letters via the returned error (routed through
// partitionProjectorDecodeFailures by the caller); a present-but-empty
// package_id is a valid decode the row builder's own identity gate still
// drops, matching the pre-typing payloadString("") behavior.
func packageRegistryPackageRow(envelope facts.Envelope) (PackageRegistryPackageRow, bool, error) {
	if envelope.IsTombstone {
		return PackageRegistryPackageRow{}, false, nil
	}
	pkg, err := decodePackageRegistryPackage(envelope)
	if err != nil {
		return PackageRegistryPackageRow{}, false, err
	}
	packageID := strings.TrimSpace(pkg.PackageID)
	if packageID == "" {
		// Present-but-empty (or whitespace-only) package_id is a valid decode,
		// distinct from an absent required key (which the decode seam already
		// dead-lettered). Trim before the gate so a whitespace-only identity is
		// dropped as non-materializable exactly as the pre-typing payloadString
		// path did, never keying a row on an empty-after-trim graph identity.
		return PackageRegistryPackageRow{}, false, nil
	}
	return PackageRegistryPackageRow{
		UID:                 packageID,
		Ecosystem:           packageRegistryDerefString(pkg.Ecosystem),
		Registry:            packageRegistryDerefString(pkg.Registry),
		RawName:             packageRegistryDerefString(pkg.RawName),
		NormalizedName:      packageRegistryDerefString(pkg.NormalizedName),
		Namespace:           packageRegistryDerefString(pkg.Namespace),
		Classifier:          packageRegistryDerefString(pkg.Classifier),
		PURL:                packageRegistryDerefString(pkg.PURL),
		BOMRef:              packageRegistryDerefString(pkg.BOMRef),
		PackageManager:      packageRegistryDerefString(pkg.PackageManager),
		SourcePath:          packageRegistryDerefString(pkg.SourcePath),
		SourceSpecificID:    packageRegistryDerefString(pkg.SourceSpecificID),
		Visibility:          packageRegistryDerefString(pkg.Visibility),
		SourceFactID:        envelope.FactID,
		StableFactKey:       envelope.StableFactKey,
		SourceSystem:        packageRegistrySourceSystem(envelope),
		SourceRecordID:      envelope.SourceRef.SourceRecordID,
		SourceConfidence:    envelope.SourceConfidence,
		CollectorKind:       envelope.CollectorKind,
		CorrelationAnchors:  packageRegistrySortedStrings(pkg.CorrelationAnchors),
		CollectorInstanceID: packageRegistryDerefString(pkg.CollectorInstanceID),
		ObservedAt:          envelope.ObservedAt,
	}, true, nil
}

// packageRegistryVersionRow decodes one package_registry.package_version
// envelope through the typed factschema seam and builds its canonical row. A
// missing required package_id, version_id, or version dead-letters via the
// returned error; a present-but-empty value for any of them is a valid decode
// the row builder's own identity gate still drops.
func packageRegistryVersionRow(envelope facts.Envelope) (PackageRegistryVersionRow, bool, error) {
	if envelope.IsTombstone {
		return PackageRegistryVersionRow{}, false, nil
	}
	version, err := decodePackageRegistryPackageVersion(envelope)
	if err != nil {
		return PackageRegistryVersionRow{}, false, err
	}
	packageID := strings.TrimSpace(version.PackageID)
	versionID := strings.TrimSpace(version.VersionID)
	rawVersion := strings.TrimSpace(version.Version)
	if packageID == "" || versionID == "" || rawVersion == "" {
		// Present-but-empty (or whitespace-only) identity is a valid decode,
		// distinct from an absent required key. See packageRegistryPackageRow.
		return PackageRegistryVersionRow{}, false, nil
	}
	return PackageRegistryVersionRow{
		UID:                 versionID,
		PackageID:           packageID,
		Ecosystem:           packageRegistryDerefString(version.Ecosystem),
		Registry:            packageRegistryDerefString(version.Registry),
		Version:             rawVersion,
		PURL:                packageRegistryDerefString(version.PURL),
		BOMRef:              packageRegistryDerefString(version.BOMRef),
		PackageManager:      packageRegistryDerefString(version.PackageManager),
		PublishedAt:         packageRegistryParsedPublishedAt(version.PublishedAt),
		IsYanked:            packageRegistryDerefBool(version.IsYanked),
		IsUnlisted:          packageRegistryDerefBool(version.IsUnlisted),
		IsDeprecated:        packageRegistryDerefBool(version.IsDeprecated),
		IsRetracted:         packageRegistryDerefBool(version.IsRetracted),
		ArtifactURLs:        packageRegistrySortedStrings(version.ArtifactURLs),
		Checksums:           packageRegistryTrimmedStringMap(version.Checksums),
		SourceFactID:        envelope.FactID,
		StableFactKey:       envelope.StableFactKey,
		SourceSystem:        packageRegistrySourceSystem(envelope),
		SourceRecordID:      envelope.SourceRef.SourceRecordID,
		SourceConfidence:    envelope.SourceConfidence,
		CollectorKind:       envelope.CollectorKind,
		CorrelationAnchors:  packageRegistrySortedStrings(version.CorrelationAnchors),
		CollectorInstanceID: packageRegistryDerefString(version.CollectorInstanceID),
		ObservedAt:          envelope.ObservedAt,
	}, true, nil
}

// packageRegistryDependencyRow decodes one
// package_registry.package_dependency envelope through the typed factschema
// seam and builds its canonical edge row. A missing required package_id,
// version_id, or dependency_package_id dead-letters via the returned error; a
// present-but-empty value for any of them is a valid decode the row builder's
// own identity gate still drops. A blank StableFactKey drops the row exactly
// as pre-typing (the edge's own uid), independent of the payload decode.
func packageRegistryDependencyRow(envelope facts.Envelope) (PackageRegistryDependencyRow, bool, error) {
	if envelope.IsTombstone {
		return PackageRegistryDependencyRow{}, false, nil
	}
	dependency, err := decodePackageRegistryPackageDependency(envelope)
	if err != nil {
		return PackageRegistryDependencyRow{}, false, err
	}
	packageID := strings.TrimSpace(dependency.PackageID)
	versionID := strings.TrimSpace(dependency.VersionID)
	dependencyPackageID := strings.TrimSpace(dependency.DependencyPackageID)
	if packageID == "" || versionID == "" || dependencyPackageID == "" {
		// Present-but-empty (or whitespace-only) identity is a valid decode,
		// distinct from an absent required key. See packageRegistryPackageRow.
		return PackageRegistryDependencyRow{}, false, nil
	}
	stableFactKey := strings.TrimSpace(envelope.StableFactKey)
	if stableFactKey == "" {
		return PackageRegistryDependencyRow{}, false, nil
	}
	return PackageRegistryDependencyRow{
		UID:                  stableFactKey,
		PackageID:            packageID,
		VersionID:            versionID,
		Version:              packageRegistryDerefString(dependency.Version),
		DependencyPackageID:  dependencyPackageID,
		DependencyEcosystem:  packageRegistryDerefString(dependency.DependencyEcosystem),
		DependencyRegistry:   packageRegistryDerefString(dependency.DependencyRegistry),
		DependencyNamespace:  packageRegistryDerefString(dependency.DependencyNamespace),
		DependencyNormalized: packageRegistryDerefString(dependency.DependencyNormalized),
		DependencyPURL:       packageRegistryDerefString(dependency.DependencyPURL),
		DependencyBOMRef:     packageRegistryDerefString(dependency.DependencyBOMRef),
		DependencyManager:    packageRegistryDerefString(dependency.DependencyManager),
		DependencyRange:      packageRegistryDerefString(dependency.DependencyRange),
		DependencyType:       packageRegistryDerefString(dependency.DependencyType),
		TargetFramework:      packageRegistryDerefString(dependency.TargetFramework),
		Marker:               packageRegistryDerefString(dependency.Marker),
		Optional:             packageRegistryDerefBool(dependency.Optional),
		Excluded:             packageRegistryDerefBool(dependency.Excluded),
		SourceFactID:         envelope.FactID,
		StableFactKey:        stableFactKey,
		SourceSystem:         packageRegistrySourceSystem(envelope),
		SourceRecordID:       envelope.SourceRef.SourceRecordID,
		SourceConfidence:     envelope.SourceConfidence,
		CollectorKind:        envelope.CollectorKind,
		CorrelationAnchors:   packageRegistrySortedStrings(dependency.CorrelationAnchors),
		CollectorInstanceID:  packageRegistryDerefString(dependency.CollectorInstanceID),
		ObservedAt:           envelope.ObservedAt,
	}, true, nil
}

// packageRegistryParsedPublishedAt parses an RFC 3339 published_at string into
// a UTC time.Time, matching the pre-typing
// packageRegistryPublishedAtFromPayload behavior byte-for-byte: an absent or
// unparseable value yields the zero time.Time rather than an error, because
// PublishedAt is descriptive metadata, not an identity field.
func packageRegistryParsedPublishedAt(raw *string) time.Time {
	if raw == nil {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, *raw)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

// packageRegistrySortedStrings returns a sorted copy of values, or nil when
// values is empty, matching the pre-typing packageRegistryStringSlice/
// packageRegistryCorrelationAnchors behavior for a []string payload field.
func packageRegistrySortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	sorted := make([]string, len(values))
	copy(sorted, values)
	sort.Strings(sorted)
	return sorted
}

// packageRegistryTrimmedStringMap returns a copy of checksums with each key and
// value trimmed, dropping any entry whose key is blank after trimming, or nil
// when the result is empty. This matches the pre-typing
// packageRegistryStringMap behavior for the checksums payload field.
func packageRegistryTrimmedStringMap(checksums map[string]string) map[string]string {
	if len(checksums) == 0 {
		return nil
	}
	out := make(map[string]string, len(checksums))
	for key, value := range checksums {
		if trimmedKey := strings.TrimSpace(key); trimmedKey != "" {
			out[trimmedKey] = strings.TrimSpace(value)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// packageRegistrySourceSystem returns the envelope's source system, falling
// back to its collector kind, matching the pre-typing behavior.
func packageRegistrySourceSystem(envelope facts.Envelope) string {
	if sourceSystem := strings.TrimSpace(envelope.SourceRef.SourceSystem); sourceSystem != "" {
		return sourceSystem
	}
	return strings.TrimSpace(envelope.CollectorKind)
}
