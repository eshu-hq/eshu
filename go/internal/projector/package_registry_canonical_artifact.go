// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// PackageRegistryArtifactRow carries one published package-version artifact
// (a binary/source download) for canonical graph projection, including the
// per-artifact hash digests the owning PackageRegistryVersionRow's
// ChecksumAlgorithms property drops (it keeps only algorithm names, not
// digests). This row is the #5458 provenance-backbone gap this kind's
// promotion to a real consumer closes.
type PackageRegistryArtifactRow struct {
	UID                 string
	PackageID           string
	VersionID           string
	ArtifactKey         string
	Version             string
	Ecosystem           string
	Registry            string
	ArtifactType        string
	ArtifactURL         string
	ArtifactPath        string
	SizeBytes           int64
	Hashes              map[string]string
	Classifier          string
	PlatformTags        []string
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

// packageRegistryArtifactRow decodes one package_registry.package_artifact
// envelope through the typed factschema seam and builds its canonical row. A
// missing required package_id, version_id, or artifact_key dead-letters via
// the returned error; a present-but-empty value for any of them is a valid
// decode the row builder's own identity gate still drops. A blank
// StableFactKey drops the row exactly as pre-typing (the artifact's own uid),
// independent of the payload decode.
//
// Hashes is a sorted "algorithm:digest" string list on the graph node (Cypher
// properties cannot hold a nested map); see decodePackageRegistryPackageArtifact
// in factschema_decode_packageregistry.go for the colon-rejection validation
// that keeps that encoding unambiguous.
func packageRegistryArtifactRow(envelope facts.Envelope) (PackageRegistryArtifactRow, bool, error) {
	if envelope.IsTombstone {
		return PackageRegistryArtifactRow{}, false, nil
	}
	artifact, err := decodePackageRegistryPackageArtifact(envelope)
	if err != nil {
		return PackageRegistryArtifactRow{}, false, err
	}
	packageID := strings.TrimSpace(artifact.PackageID)
	versionID := strings.TrimSpace(artifact.VersionID)
	artifactKey := strings.TrimSpace(artifact.ArtifactKey)
	if packageID == "" || versionID == "" || artifactKey == "" {
		// Present-but-empty (or whitespace-only) identity is a valid decode,
		// distinct from an absent required key. See packageRegistryPackageRow.
		return PackageRegistryArtifactRow{}, false, nil
	}
	stableFactKey := strings.TrimSpace(envelope.StableFactKey)
	if stableFactKey == "" {
		return PackageRegistryArtifactRow{}, false, nil
	}
	return PackageRegistryArtifactRow{
		UID:                 stableFactKey,
		PackageID:           packageID,
		VersionID:           versionID,
		ArtifactKey:         artifactKey,
		Version:             packageRegistryDerefString(artifact.Version),
		Ecosystem:           packageRegistryDerefString(artifact.Ecosystem),
		Registry:            packageRegistryDerefString(artifact.Registry),
		ArtifactType:        packageRegistryDerefString(artifact.ArtifactType),
		ArtifactURL:         packageRegistryDerefString(artifact.ArtifactURL),
		ArtifactPath:        packageRegistryDerefString(artifact.ArtifactPath),
		SizeBytes:           packageRegistryDerefInt64(artifact.SizeBytes),
		Hashes:              packageRegistryTrimmedStringMap(artifact.Hashes),
		Classifier:          packageRegistryDerefString(artifact.Classifier),
		PlatformTags:        packageRegistrySortedStrings(artifact.PlatformTags),
		SourceFactID:        envelope.FactID,
		StableFactKey:       stableFactKey,
		SourceSystem:        packageRegistrySourceSystem(envelope),
		SourceRecordID:      envelope.SourceRef.SourceRecordID,
		SourceConfidence:    envelope.SourceConfidence,
		CollectorKind:       envelope.CollectorKind,
		CorrelationAnchors:  packageRegistrySortedStrings(artifact.CorrelationAnchors),
		CollectorInstanceID: packageRegistryDerefString(artifact.CollectorInstanceID),
		ObservedAt:          envelope.ObservedAt,
	}, true, nil
}
