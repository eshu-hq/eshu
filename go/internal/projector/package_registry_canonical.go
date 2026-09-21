// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector //nolint:dirgate // Root owns package-registry canonical projection; package/source owns only reducer-intent routing.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
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

// PackageRegistryCanonicalStage is the bounded telemetry stage label the
// projector's package_registry canonical extractor reports on
// eshu_dp_projector_input_invalid_facts_total.
const PackageRegistryCanonicalStage = "package_registry_canonical"

// extractPackageRegistryRows projects committed package_registry fact
// envelopes into canonical package/version/dependency rows on mat, decoding
// each fact through the typed factschema seam. A fact missing a required
// identity field is QUARANTINED per-fact (returned in the []QuarantinedFact
// slice) rather than producing a graph identity from an empty-string segment:
// that one fact is skipped while every valid fact — package_registry and
// non-package_registry — still projects. The caller
// (BuildMaterialization) records the quarantined facts as visible
// input_invalid dead-letters via RecordQuarantinedFacts. A
// present-but-empty identity field is a valid decode that the row builders'
// own identity gate still drops, byte-identical to the pre-typing behavior.
//
// package_registry.source_hint, .vulnerability_hint, .repository_hosting, and
// .warning are intentionally not consumed here (typed-but-deferred, no
// projector read site today), so no case handles them. .package_artifact and
// .registry_event both gained a real consumer in #5458: .package_artifact
// carries the per-artifact hash digests the version row's
// checksum_algorithms property drops; .registry_event carries the
// per-version yank/deprecate/publish lifecycle timeline the epic names.
// Their row types and decode/row-building helpers live in
// package_registry_canonical_artifact.go and
// package_registry_canonical_event.go respectively, split out from this file
// to stay under the package's 500-line-per-file convention (mirrors
// tfstate_canonical_types.go's split from tfstate_canonical.go).
func extractPackageRegistryRows(mat *CanonicalMaterialization, envelopes []facts.Envelope) []QuarantinedFact {
	if mat == nil || len(envelopes) == 0 {
		return nil
	}
	var quarantined []QuarantinedFact
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
		case facts.PackageRegistryPackageArtifactFactKind:
			var row PackageRegistryArtifactRow
			var ok bool
			row, ok, err = packageRegistryArtifactRow(envelope)
			if ok {
				mat.PackageRegistryArtifacts = append(mat.PackageRegistryArtifacts, row)
			}
		case facts.PackageRegistryRegistryEventFactKind:
			var row PackageRegistryEventRow
			var ok bool
			row, ok, err = packageRegistryEventRow(envelope)
			if ok {
				mat.PackageRegistryEvents = append(mat.PackageRegistryEvents, row)
			}
		default:
			continue
		}
		if err == nil {
			continue
		}
		q, isQuarantine, fatal := PartitionFailures(envelope, err)
		if fatal != nil {
			// The only fatal decode error is an unsupported schema major, which
			// the projector's schema-version admission (ValidateFactSchemaVersion
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
// PartitionFailures by the caller); a present-but-empty
// package_id is a valid decode the row builder's own identity gate still
// drops, matching the pre-typing PayloadString("") behavior.
func packageRegistryPackageRow(envelope facts.Envelope) (PackageRegistryPackageRow, bool, error) {
	if envelope.IsTombstone {
		return PackageRegistryPackageRow{}, false, nil
	}
	pkg, err := PackageRegistryPackage(envelope)
	if err != nil {
		return PackageRegistryPackageRow{}, false, err
	}
	packageID := strings.TrimSpace(pkg.PackageID)
	if packageID == "" {
		// Present-but-empty (or whitespace-only) package_id is a valid decode,
		// distinct from an absent required key (which the decode seam already
		// dead-lettered). Trim before the gate so a whitespace-only identity is
		// dropped as non-materializable exactly as the pre-typing PayloadString
		// path did, never keying a row on an empty-after-trim graph identity.
		return PackageRegistryPackageRow{}, false, nil
	}
	return PackageRegistryPackageRow{
		UID:                 packageID,
		Ecosystem:           PackageRegistryDerefString(pkg.Ecosystem),
		Registry:            PackageRegistryDerefString(pkg.Registry),
		RawName:             PackageRegistryDerefString(pkg.RawName),
		NormalizedName:      PackageRegistryDerefString(pkg.NormalizedName),
		Namespace:           PackageRegistryDerefString(pkg.Namespace),
		Classifier:          PackageRegistryDerefString(pkg.Classifier),
		PURL:                PackageRegistryDerefString(pkg.PURL),
		BOMRef:              PackageRegistryDerefString(pkg.BOMRef),
		PackageManager:      PackageRegistryDerefString(pkg.PackageManager),
		SourcePath:          PackageRegistryDerefString(pkg.SourcePath),
		SourceSpecificID:    PackageRegistryDerefString(pkg.SourceSpecificID),
		Visibility:          PackageRegistryDerefString(pkg.Visibility),
		SourceFactID:        envelope.FactID,
		StableFactKey:       envelope.StableFactKey,
		SourceSystem:        packageRegistrySourceSystem(envelope),
		SourceRecordID:      envelope.SourceRef.SourceRecordID,
		SourceConfidence:    envelope.SourceConfidence,
		CollectorKind:       envelope.CollectorKind,
		CorrelationAnchors:  packageRegistrySortedStrings(pkg.CorrelationAnchors),
		CollectorInstanceID: PackageRegistryDerefString(pkg.CollectorInstanceID),
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
	version, err := PackageRegistryPackageVersion(envelope)
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
	checksums, err := packageRegistryTrimmedStringMap(
		factschema.FactKindPackageRegistryPackageVersion, "checksums", version.Checksums,
	)
	if err != nil {
		return PackageRegistryVersionRow{}, false, err
	}
	return PackageRegistryVersionRow{
		UID:                 versionID,
		PackageID:           packageID,
		Ecosystem:           PackageRegistryDerefString(version.Ecosystem),
		Registry:            PackageRegistryDerefString(version.Registry),
		Version:             rawVersion,
		PURL:                PackageRegistryDerefString(version.PURL),
		BOMRef:              PackageRegistryDerefString(version.BOMRef),
		PackageManager:      PackageRegistryDerefString(version.PackageManager),
		PublishedAt:         packageRegistryParsedTimestamp(version.PublishedAt),
		IsYanked:            PackageRegistryDerefBool(version.IsYanked),
		IsUnlisted:          PackageRegistryDerefBool(version.IsUnlisted),
		IsDeprecated:        PackageRegistryDerefBool(version.IsDeprecated),
		IsRetracted:         PackageRegistryDerefBool(version.IsRetracted),
		ArtifactURLs:        packageRegistrySortedStrings(version.ArtifactURLs),
		Checksums:           checksums,
		SourceFactID:        envelope.FactID,
		StableFactKey:       envelope.StableFactKey,
		SourceSystem:        packageRegistrySourceSystem(envelope),
		SourceRecordID:      envelope.SourceRef.SourceRecordID,
		SourceConfidence:    envelope.SourceConfidence,
		CollectorKind:       envelope.CollectorKind,
		CorrelationAnchors:  packageRegistrySortedStrings(version.CorrelationAnchors),
		CollectorInstanceID: PackageRegistryDerefString(version.CollectorInstanceID),
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
	dependency, err := PackageRegistryPackageDependency(envelope)
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
		Version:              PackageRegistryDerefString(dependency.Version),
		DependencyPackageID:  dependencyPackageID,
		DependencyEcosystem:  PackageRegistryDerefString(dependency.DependencyEcosystem),
		DependencyRegistry:   PackageRegistryDerefString(dependency.DependencyRegistry),
		DependencyNamespace:  PackageRegistryDerefString(dependency.DependencyNamespace),
		DependencyNormalized: PackageRegistryDerefString(dependency.DependencyNormalized),
		DependencyPURL:       PackageRegistryDerefString(dependency.DependencyPURL),
		DependencyBOMRef:     PackageRegistryDerefString(dependency.DependencyBOMRef),
		DependencyManager:    PackageRegistryDerefString(dependency.DependencyManager),
		DependencyRange:      PackageRegistryDerefString(dependency.DependencyRange),
		DependencyType:       PackageRegistryDerefString(dependency.DependencyType),
		TargetFramework:      PackageRegistryDerefString(dependency.TargetFramework),
		Marker:               PackageRegistryDerefString(dependency.Marker),
		Optional:             PackageRegistryDerefBool(dependency.Optional),
		Excluded:             PackageRegistryDerefBool(dependency.Excluded),
		SourceFactID:         envelope.FactID,
		StableFactKey:        stableFactKey,
		SourceSystem:         packageRegistrySourceSystem(envelope),
		SourceRecordID:       envelope.SourceRef.SourceRecordID,
		SourceConfidence:     envelope.SourceConfidence,
		CollectorKind:        envelope.CollectorKind,
		CorrelationAnchors:   packageRegistrySortedStrings(dependency.CorrelationAnchors),
		CollectorInstanceID:  PackageRegistryDerefString(dependency.CollectorInstanceID),
		ObservedAt:           envelope.ObservedAt,
	}, true, nil
}

// packageRegistryParsedTimestamp parses an RFC 3339 timestamp string into a UTC
// time.Time, matching the pre-typing packageRegistryPublishedAtFromPayload
// behavior byte-for-byte: an absent or unparseable value yields the zero
// time.Time rather than an error, because the timestamps it parses
// (PublishedAt on a version row, OccurredAt on a registry-event row) are
// descriptive metadata, not identity fields.
func packageRegistryParsedTimestamp(raw *string) time.Time {
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

// packageRegistryTrimmedStringMap returns a copy of values with each key and
// value trimmed, dropping any entry whose key is blank after trimming, or nil
// when the result is empty. This matches the pre-typing
// packageRegistryStringMap behavior for a map[string]string payload field
// (package_version's checksums, package_artifact's hashes) for the common
// case.
//
// #5820 P2 review finding: two distinct original keys can normalize to the
// same trimmed key (for example "sha256" and " sha256 "). The original
// implementation iterated the input map directly, so which value survived a
// genuine collision (different values under the same normalized key) depended
// on Go's randomized map iteration order -- a different projected graph value
// on repeated runs of the identical fact, the worst class of defect this repo
// guards against. This implementation sorts the original keys before
// iterating, so which entry is visited first is always the same regardless of
// map iteration order: a collision where every colliding key agrees on the
// (trimmed) value merges deterministically with no error (not a real
// conflict, matches the pre-fix behavior for that case), and a collision
// where they disagree is a genuine data-integrity conflict that returns a
// deterministic error instead of silently picking a value. factKind and field
// name the caller's fact kind and payload field so the returned error carries
// a classified, dead-letterable *factschema.DecodeError, matching the shape
// decodePayloadFieldError produces for other malformed-but-present-field
// cases (see decode_aws.go's from_port validation).
func packageRegistryTrimmedStringMap(factKind, field string, values map[string]string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	originalKeys := make([]string, 0, len(values))
	for key := range values {
		originalKeys = append(originalKeys, key)
	}
	sort.Strings(originalKeys)

	out := make(map[string]string, len(values))
	for _, key := range originalKeys {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		trimmedValue := strings.TrimSpace(values[key])
		if existing, collided := out[trimmedKey]; collided && existing != trimmedValue {
			return nil, NewError(factKind, &factschema.DecodeError{
				FactKind:       factKind,
				Classification: factschema.ClassificationInputInvalid,
				Field:          field,
				Err: fmt.Errorf(
					"keys %q and a preceding entry both normalize to %q but carry different values (%q vs %q)",
					key, trimmedKey, trimmedValue, existing,
				),
			})
		}
		out[trimmedKey] = trimmedValue
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// packageRegistrySourceSystem returns the envelope's source system, falling
// back to its collector kind, matching the pre-typing behavior.
func packageRegistrySourceSystem(envelope facts.Envelope) string {
	if sourceSystem := strings.TrimSpace(envelope.SourceRef.SourceSystem); sourceSystem != "" {
		return sourceSystem
	}
	return strings.TrimSpace(envelope.CollectorKind)
}
