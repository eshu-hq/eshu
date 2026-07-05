// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// OCIRegistryRepositoryRow carries one observed OCI registry repository for
// canonical graph projection.
type OCIRegistryRepositoryRow struct {
	UID              string
	Provider         string
	Registry         string
	Repository       string
	Visibility       string
	AuthMode         string
	SourceFactID     string
	StableFactKey    string
	SourceSystem     string
	SourceRecordID   string
	SourceConfidence string
	CollectorKind    string
	ObservedAt       time.Time
}

// OCIImageManifestRow carries one digest-addressed OCI image manifest.
type OCIImageManifestRow struct {
	UID                  string
	RepositoryID         string
	Digest               string
	MediaType            string
	SizeBytes            int64
	ArtifactType         string
	SourceTag            string
	ConfigDigest         string
	LayerDigests         []string
	SourceFactID         string
	StableFactKey        string
	SourceSystem         string
	SourceRecordID       string
	SourceConfidence     string
	CollectorKind        string
	CorrelationAnchors   []string
	CollectorInstanceID  string
	ResolvedDescriptorID string
	ObservedAt           time.Time
}

// OCIImageIndexRow carries one digest-addressed OCI image index.
type OCIImageIndexRow struct {
	UID                string
	RepositoryID       string
	Digest             string
	MediaType          string
	SizeBytes          int64
	ArtifactType       string
	ManifestDigests    []string
	SourceFactID       string
	StableFactKey      string
	SourceSystem       string
	SourceRecordID     string
	SourceConfidence   string
	CollectorKind      string
	CorrelationAnchors []string
	ObservedAt         time.Time
}

// OCIImageDescriptorRow carries one reusable digest-addressed descriptor.
type OCIImageDescriptorRow struct {
	UID              string
	RepositoryID     string
	Digest           string
	MediaType        string
	SizeBytes        int64
	ArtifactType     string
	SourceFactID     string
	StableFactKey    string
	SourceSystem     string
	SourceRecordID   string
	SourceConfidence string
	CollectorKind    string
	ObservedAt       time.Time
}

// OCIImageTagObservationRow carries mutable tag-to-digest evidence. Its UID is
// an observation identity, not image identity; ResolvedDescriptorUID is the
// digest-backed target when a digest was reported.
type OCIImageTagObservationRow struct {
	UID                   string
	RepositoryID          string
	ImageRef              string
	Tag                   string
	ResolvedDigest        string
	ResolvedDescriptorUID string
	MediaType             string
	PreviousDigest        string
	Mutated               bool
	IdentityStrength      string
	SourceFactID          string
	StableFactKey         string
	SourceSystem          string
	SourceRecordID        string
	SourceConfidence      string
	CollectorKind         string
	ObservedAt            time.Time
}

// OCIImageReferrerRow carries one subject/referrer descriptor observation.
type OCIImageReferrerRow struct {
	UID               string
	RepositoryID      string
	SubjectDigest     string
	SubjectMediaType  string
	ReferrerDigest    string
	ReferrerMediaType string
	ArtifactType      string
	SizeBytes         int64
	SourceAPIPath     string
	SourceFactID      string
	StableFactKey     string
	SourceSystem      string
	SourceRecordID    string
	SourceConfidence  string
	CollectorKind     string
	ObservedAt        time.Time
}

// ociRegistryCanonicalStage is the bounded telemetry stage label the projector's
// OCI canonical extractor reports on eshu_dp_projector_input_invalid_facts_total.
const ociRegistryCanonicalStage = "oci_registry_canonical"

// extractOCIRegistryRows projects committed OCI registry fact envelopes into
// digest-keyed canonical image rows on mat, decoding each fact through the typed
// factschema seam. A fact missing a required identity field is QUARANTINED
// per-fact (returned in the []quarantinedFact slice) rather than producing a
// graph identity from an empty-string segment: that one fact is skipped while
// every valid fact — OCI and non-OCI — still projects. The caller
// (buildCanonicalMaterialization) records the quarantined facts as visible
// input_invalid dead-letters via recordProjectorQuarantinedFacts. A
// present-but-empty identity field is a valid decode that the row builders' own
// identity gate still drops, byte-identical to the pre-typing behavior.
//
// oci_registry.warning is intentionally not consumed here (design §3.4,
// typed-but-deferred), so no case handles it.
func extractOCIRegistryRows(mat *CanonicalMaterialization, envelopes []facts.Envelope) []quarantinedFact {
	if mat == nil || len(envelopes) == 0 {
		return nil
	}
	var quarantined []quarantinedFact
	for _, envelope := range envelopes {
		var err error
		switch envelope.FactKind {
		case facts.OCIRegistryRepositoryFactKind:
			if row, ok, rowErr := ociRegistryRepositoryRow(envelope); ok {
				mat.OCIRegistryRepository = &row
			} else {
				err = rowErr
			}
		case facts.OCIImageManifestFactKind:
			if row, ok, rowErr := ociImageManifestRow(envelope); ok {
				mat.OCIImageManifests = append(mat.OCIImageManifests, row)
			} else {
				err = rowErr
			}
		case facts.OCIImageIndexFactKind:
			if row, ok, rowErr := ociImageIndexRow(envelope); ok {
				mat.OCIImageIndexes = append(mat.OCIImageIndexes, row)
			} else {
				err = rowErr
			}
		case facts.OCIImageDescriptorFactKind:
			if row, ok, rowErr := ociImageDescriptorRow(envelope); ok {
				mat.OCIImageDescriptors = append(mat.OCIImageDescriptors, row)
			} else {
				err = rowErr
			}
		case facts.OCIImageTagObservationFactKind:
			if row, ok, rowErr := ociImageTagObservationRow(envelope); ok {
				mat.OCIImageTagObservations = append(mat.OCIImageTagObservations, row)
			} else {
				err = rowErr
			}
		case facts.OCIImageReferrerFactKind:
			if row, ok, rowErr := ociImageReferrerRow(envelope); ok {
				mat.OCIImageReferrers = append(mat.OCIImageReferrers, row)
			} else {
				err = rowErr
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

func ociRegistryRepositoryRow(envelope facts.Envelope) (OCIRegistryRepositoryRow, bool, error) {
	repository, err := decodeOCIRegistryRepository(envelope)
	if err != nil {
		return OCIRegistryRepositoryRow{}, false, err
	}
	repositoryID := strings.TrimSpace(repository.RepositoryID)
	if repositoryID == "" {
		// Present-but-empty (or whitespace-only) repository_id is a valid decode,
		// distinct from an absent required key (which the decode seam already
		// dead-lettered). Trim before the gate so a whitespace-only identity is
		// dropped as non-materializable exactly as the pre-typing payloadString
		// path did, never keying a row on an empty-after-trim graph identity.
		return OCIRegistryRepositoryRow{}, false, nil
	}
	return OCIRegistryRepositoryRow{
		UID:              repositoryID,
		Provider:         ociDerefString(repository.Provider),
		Registry:         ociDerefString(repository.Registry),
		Repository:       ociDerefString(repository.Repository),
		Visibility:       ociDerefString(repository.Visibility),
		AuthMode:         ociDerefString(repository.AuthMode),
		SourceFactID:     envelope.FactID,
		StableFactKey:    envelope.StableFactKey,
		SourceSystem:     ociRegistrySourceSystem(envelope),
		SourceRecordID:   envelope.SourceRef.SourceRecordID,
		SourceConfidence: envelope.SourceConfidence,
		CollectorKind:    envelope.CollectorKind,
		ObservedAt:       envelope.ObservedAt,
	}, true, nil
}

func ociImageManifestRow(envelope facts.Envelope) (OCIImageManifestRow, bool, error) {
	manifest, err := decodeOCIImageManifest(envelope)
	if err != nil {
		return OCIImageManifestRow{}, false, err
	}
	repositoryID := strings.TrimSpace(manifest.RepositoryID)
	digest := strings.TrimSpace(manifest.Digest)
	uid := ociResolvedDescriptorUID(repositoryID, digest, ociDerefString(manifest.DescriptorID))
	if uid == "" || digest == "" || repositoryID == "" {
		// Present-but-empty (or whitespace-only) identity is a valid decode,
		// distinct from an absent required key (already dead-lettered by the
		// decode seam). Trim the identity keys before the gate so a
		// whitespace-only digest/repository_id drops the row as non-materializable
		// exactly as the pre-typing payloadString path did, never keying a
		// descriptor row on an empty-after-trim graph identity.
		return OCIImageManifestRow{}, false, nil
	}
	return OCIImageManifestRow{
		UID:                  uid,
		RepositoryID:         repositoryID,
		Digest:               digest,
		MediaType:            ociDerefString(manifest.MediaType),
		SizeBytes:            ociDerefInt64(manifest.SizeBytes),
		ArtifactType:         ociDerefString(manifest.ArtifactType),
		SourceTag:            ociDerefString(manifest.SourceTag),
		ConfigDigest:         ociDescriptorDigest(manifest.Config),
		LayerDigests:         ociDescriptorSliceDigests(manifest.Layers),
		SourceFactID:         envelope.FactID,
		StableFactKey:        envelope.StableFactKey,
		SourceSystem:         ociRegistrySourceSystem(envelope),
		SourceRecordID:       envelope.SourceRef.SourceRecordID,
		SourceConfidence:     envelope.SourceConfidence,
		CollectorKind:        envelope.CollectorKind,
		CorrelationAnchors:   ociUniqueSortedAnchors(manifest.CorrelationAnchors),
		CollectorInstanceID:  ociDerefString(manifest.CollectorInstanceID),
		ResolvedDescriptorID: uid,
		ObservedAt:           envelope.ObservedAt,
	}, true, nil
}

func ociImageIndexRow(envelope facts.Envelope) (OCIImageIndexRow, bool, error) {
	index, err := decodeOCIImageIndex(envelope)
	if err != nil {
		return OCIImageIndexRow{}, false, err
	}
	repositoryID := strings.TrimSpace(index.RepositoryID)
	digest := strings.TrimSpace(index.Digest)
	uid := ociResolvedDescriptorUID(repositoryID, digest, ociDerefString(index.DescriptorID))
	if uid == "" || digest == "" || repositoryID == "" {
		// Whitespace-only identity drops the row as non-materializable, matching
		// the pre-typing payloadString trim (see ociImageManifestRow).
		return OCIImageIndexRow{}, false, nil
	}
	return OCIImageIndexRow{
		UID:                uid,
		RepositoryID:       repositoryID,
		Digest:             digest,
		MediaType:          ociDerefString(index.MediaType),
		SizeBytes:          ociDerefInt64(index.SizeBytes),
		ArtifactType:       ociDerefString(index.ArtifactType),
		ManifestDigests:    ociDescriptorSliceDigests(index.Manifests),
		SourceFactID:       envelope.FactID,
		StableFactKey:      envelope.StableFactKey,
		SourceSystem:       ociRegistrySourceSystem(envelope),
		SourceRecordID:     envelope.SourceRef.SourceRecordID,
		SourceConfidence:   envelope.SourceConfidence,
		CollectorKind:      envelope.CollectorKind,
		CorrelationAnchors: ociUniqueSortedAnchors(index.CorrelationAnchors),
		ObservedAt:         envelope.ObservedAt,
	}, true, nil
}

func ociImageDescriptorRow(envelope facts.Envelope) (OCIImageDescriptorRow, bool, error) {
	descriptor, err := decodeOCIImageDescriptor(envelope)
	if err != nil {
		return OCIImageDescriptorRow{}, false, err
	}
	repositoryID := strings.TrimSpace(descriptor.RepositoryID)
	digest := strings.TrimSpace(descriptor.Digest)
	uid := ociResolvedDescriptorUID(repositoryID, digest, ociDerefString(descriptor.DescriptorID))
	if uid == "" || digest == "" || repositoryID == "" {
		// Whitespace-only identity drops the row as non-materializable, matching
		// the pre-typing payloadString trim (see ociImageManifestRow).
		return OCIImageDescriptorRow{}, false, nil
	}
	return OCIImageDescriptorRow{
		UID:              uid,
		RepositoryID:     repositoryID,
		Digest:           digest,
		MediaType:        ociDerefString(descriptor.MediaType),
		SizeBytes:        ociDerefInt64(descriptor.SizeBytes),
		ArtifactType:     ociDerefString(descriptor.ArtifactType),
		SourceFactID:     envelope.FactID,
		StableFactKey:    envelope.StableFactKey,
		SourceSystem:     ociRegistrySourceSystem(envelope),
		SourceRecordID:   envelope.SourceRef.SourceRecordID,
		SourceConfidence: envelope.SourceConfidence,
		CollectorKind:    envelope.CollectorKind,
		ObservedAt:       envelope.ObservedAt,
	}, true, nil
}

func ociImageTagObservationRow(envelope facts.Envelope) (OCIImageTagObservationRow, bool, error) {
	observation, err := decodeOCIImageTagObservation(envelope)
	if err != nil {
		return OCIImageTagObservationRow{}, false, err
	}
	repositoryID := strings.TrimSpace(observation.RepositoryID)
	tag := strings.TrimSpace(observation.Tag)
	resolvedDigest := strings.TrimSpace(observation.ResolvedDigest)
	if repositoryID == "" || tag == "" || resolvedDigest == "" {
		// Whitespace-only identity drops the row as non-materializable, matching
		// the pre-typing payloadString trim (see ociImageManifestRow).
		return OCIImageTagObservationRow{}, false, nil
	}
	identityStrength := ociDerefString(observation.IdentityStrength)
	if identityStrength == "" {
		identityStrength = "weak_tag"
	}
	return OCIImageTagObservationRow{
		UID:                   ociRegistryUID("tag_observation", repositoryID, tag, resolvedDigest),
		RepositoryID:          repositoryID,
		ImageRef:              ociImageRef(repositoryID, tag),
		Tag:                   tag,
		ResolvedDigest:        resolvedDigest,
		ResolvedDescriptorUID: ociDescriptorUID(repositoryID, resolvedDigest),
		MediaType:             ociDerefString(observation.MediaType),
		PreviousDigest:        ociDerefString(observation.PreviousDigest),
		Mutated:               ociDerefBool(observation.Mutated),
		IdentityStrength:      identityStrength,
		SourceFactID:          envelope.FactID,
		StableFactKey:         envelope.StableFactKey,
		SourceSystem:          ociRegistrySourceSystem(envelope),
		SourceRecordID:        envelope.SourceRef.SourceRecordID,
		SourceConfidence:      envelope.SourceConfidence,
		CollectorKind:         envelope.CollectorKind,
		ObservedAt:            envelope.ObservedAt,
	}, true, nil
}

func ociImageReferrerRow(envelope facts.Envelope) (OCIImageReferrerRow, bool, error) {
	referrer, err := decodeOCIImageReferrer(envelope)
	if err != nil {
		return OCIImageReferrerRow{}, false, err
	}
	repositoryID := strings.TrimSpace(referrer.RepositoryID)
	subjectDigest := strings.TrimSpace(referrer.SubjectDigest)
	referrerDigest := strings.TrimSpace(referrer.ReferrerDigest)
	if repositoryID == "" || subjectDigest == "" || referrerDigest == "" {
		// Whitespace-only identity drops the row as non-materializable, matching
		// the pre-typing payloadString trim (see ociImageManifestRow).
		return OCIImageReferrerRow{}, false, nil
	}
	return OCIImageReferrerRow{
		UID:               ociRegistryUID("referrer", repositoryID, subjectDigest, referrerDigest),
		RepositoryID:      repositoryID,
		SubjectDigest:     subjectDigest,
		SubjectMediaType:  ociDerefString(referrer.SubjectMediaType),
		ReferrerDigest:    referrerDigest,
		ReferrerMediaType: ociDerefString(referrer.ReferrerMediaType),
		ArtifactType:      ociDerefString(referrer.ArtifactType),
		SizeBytes:         ociDerefInt64(referrer.SizeBytes),
		SourceAPIPath:     ociDerefString(referrer.SourceAPIPath),
		SourceFactID:      envelope.FactID,
		StableFactKey:     envelope.StableFactKey,
		SourceSystem:      ociRegistrySourceSystem(envelope),
		SourceRecordID:    envelope.SourceRef.SourceRecordID,
		SourceConfidence:  envelope.SourceConfidence,
		CollectorKind:     envelope.CollectorKind,
		ObservedAt:        envelope.ObservedAt,
	}, true, nil
}

// ociResolvedDescriptorUID returns the collector-supplied descriptor UID when
// present, else synthesizes it from (repositoryID, digest), matching the
// pre-typing ociDescriptorFields fallback. An empty result means the identity is
// incomplete (no digest), so the row is dropped.
func ociResolvedDescriptorUID(repositoryID, digest, descriptorID string) string {
	if descriptorID != "" {
		return descriptorID
	}
	if repositoryID != "" && digest != "" {
		return ociDescriptorUID(repositoryID, digest)
	}
	return ""
}

func ociRegistrySourceSystem(envelope facts.Envelope) string {
	if sourceSystem := strings.TrimSpace(envelope.SourceRef.SourceSystem); sourceSystem != "" {
		return sourceSystem
	}
	return strings.TrimSpace(envelope.CollectorKind)
}

func ociDescriptorUID(repositoryID, digest string) string {
	repositoryID = strings.TrimSpace(repositoryID)
	digest = strings.TrimSpace(digest)
	if strings.HasPrefix(repositoryID, "oci-registry://") && digest != "" {
		return "oci-descriptor://" + strings.TrimPrefix(repositoryID, "oci-registry://") + "@" + digest
	}
	return ociRegistryUID("descriptor", repositoryID, digest)
}

func ociImageRef(repositoryID, tag string) string {
	repositoryID = strings.TrimSpace(repositoryID)
	tag = strings.TrimSpace(tag)
	if strings.HasPrefix(repositoryID, "oci-registry://") && tag != "" {
		return strings.TrimPrefix(repositoryID, "oci-registry://") + ":" + tag
	}
	return ""
}

func ociRegistryUID(kind string, parts ...string) string {
	identity := map[string]any{"kind": kind}
	for index, part := range parts {
		identity[fmt.Sprintf("part_%02d", index)] = strings.TrimSpace(part)
	}
	return facts.StableID("OCIRegistryCanonicalNode", identity)
}
