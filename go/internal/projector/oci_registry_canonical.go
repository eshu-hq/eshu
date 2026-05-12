package projector

import (
	"fmt"
	"sort"
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

func extractOCIRegistryRows(mat *CanonicalMaterialization, envelopes []facts.Envelope) {
	if mat == nil || len(envelopes) == 0 {
		return
	}
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case facts.OCIRegistryRepositoryFactKind:
			if row, ok := ociRegistryRepositoryRow(envelope); ok {
				mat.OCIRegistryRepository = &row
			}
		case facts.OCIImageManifestFactKind:
			if row, ok := ociImageManifestRow(envelope); ok {
				mat.OCIImageManifests = append(mat.OCIImageManifests, row)
			}
		case facts.OCIImageIndexFactKind:
			if row, ok := ociImageIndexRow(envelope); ok {
				mat.OCIImageIndexes = append(mat.OCIImageIndexes, row)
			}
		case facts.OCIImageDescriptorFactKind:
			if row, ok := ociImageDescriptorRow(envelope); ok {
				mat.OCIImageDescriptors = append(mat.OCIImageDescriptors, row)
			}
		case facts.OCIImageTagObservationFactKind:
			if row, ok := ociImageTagObservationRow(envelope); ok {
				mat.OCIImageTagObservations = append(mat.OCIImageTagObservations, row)
			}
		case facts.OCIImageReferrerFactKind:
			if row, ok := ociImageReferrerRow(envelope); ok {
				mat.OCIImageReferrers = append(mat.OCIImageReferrers, row)
			}
		}
	}
}

func validateOCIRegistrySchemaVersion(envelope facts.Envelope) error {
	want, ok := facts.OCIRegistrySchemaVersion(envelope.FactKind)
	if !ok {
		return nil
	}
	got := strings.TrimSpace(envelope.SchemaVersion)
	if got == "" {
		return fmt.Errorf("oci registry fact %q schema_version must not be blank", envelope.FactID)
	}
	if got != want {
		return fmt.Errorf(
			"oci registry fact %q schema_version %q is unsupported for %s; want %q",
			envelope.FactID,
			got,
			envelope.FactKind,
			want,
		)
	}
	return nil
}

func ociRegistryRepositoryRow(envelope facts.Envelope) (OCIRegistryRepositoryRow, bool) {
	repositoryID, _ := payloadString(envelope.Payload, "repository_id")
	if repositoryID == "" {
		return OCIRegistryRepositoryRow{}, false
	}
	provider, _ := payloadString(envelope.Payload, "provider")
	registry, _ := payloadString(envelope.Payload, "registry")
	repository, _ := payloadString(envelope.Payload, "repository")
	visibility, _ := payloadString(envelope.Payload, "visibility")
	authMode, _ := payloadString(envelope.Payload, "auth_mode")
	return OCIRegistryRepositoryRow{
		UID:              repositoryID,
		Provider:         provider,
		Registry:         registry,
		Repository:       repository,
		Visibility:       visibility,
		AuthMode:         authMode,
		SourceFactID:     envelope.FactID,
		StableFactKey:    envelope.StableFactKey,
		SourceSystem:     ociRegistrySourceSystem(envelope),
		SourceRecordID:   envelope.SourceRef.SourceRecordID,
		SourceConfidence: envelope.SourceConfidence,
		CollectorKind:    envelope.CollectorKind,
		ObservedAt:       envelope.ObservedAt,
	}, true
}

func ociImageManifestRow(envelope facts.Envelope) (OCIImageManifestRow, bool) {
	descriptor := ociDescriptorFields(envelope)
	if descriptor.uid == "" || descriptor.digest == "" || descriptor.repositoryID == "" {
		return OCIImageManifestRow{}, false
	}
	sourceTag, _ := payloadString(envelope.Payload, "source_tag")
	collectorInstanceID, _ := payloadString(envelope.Payload, "collector_instance_id")
	return OCIImageManifestRow{
		UID:                  descriptor.uid,
		RepositoryID:         descriptor.repositoryID,
		Digest:               descriptor.digest,
		MediaType:            descriptor.mediaType,
		SizeBytes:            descriptor.sizeBytes,
		ArtifactType:         descriptor.artifactType,
		SourceTag:            sourceTag,
		ConfigDigest:         ociDescriptorMapDigest(envelope.Payload, "config"),
		LayerDigests:         ociDescriptorListDigests(envelope.Payload, "layers"),
		SourceFactID:         envelope.FactID,
		StableFactKey:        envelope.StableFactKey,
		SourceSystem:         ociRegistrySourceSystem(envelope),
		SourceRecordID:       envelope.SourceRef.SourceRecordID,
		SourceConfidence:     envelope.SourceConfidence,
		CollectorKind:        envelope.CollectorKind,
		CorrelationAnchors:   ociCorrelationAnchors(envelope.Payload),
		CollectorInstanceID:  collectorInstanceID,
		ResolvedDescriptorID: descriptor.uid,
		ObservedAt:           envelope.ObservedAt,
	}, true
}

func ociImageIndexRow(envelope facts.Envelope) (OCIImageIndexRow, bool) {
	descriptor := ociDescriptorFields(envelope)
	if descriptor.uid == "" || descriptor.digest == "" || descriptor.repositoryID == "" {
		return OCIImageIndexRow{}, false
	}
	return OCIImageIndexRow{
		UID:                descriptor.uid,
		RepositoryID:       descriptor.repositoryID,
		Digest:             descriptor.digest,
		MediaType:          descriptor.mediaType,
		SizeBytes:          descriptor.sizeBytes,
		ArtifactType:       descriptor.artifactType,
		ManifestDigests:    ociDescriptorListDigests(envelope.Payload, "manifests"),
		SourceFactID:       envelope.FactID,
		StableFactKey:      envelope.StableFactKey,
		SourceSystem:       ociRegistrySourceSystem(envelope),
		SourceRecordID:     envelope.SourceRef.SourceRecordID,
		SourceConfidence:   envelope.SourceConfidence,
		CollectorKind:      envelope.CollectorKind,
		CorrelationAnchors: ociCorrelationAnchors(envelope.Payload),
		ObservedAt:         envelope.ObservedAt,
	}, true
}

func ociImageDescriptorRow(envelope facts.Envelope) (OCIImageDescriptorRow, bool) {
	descriptor := ociDescriptorFields(envelope)
	if descriptor.uid == "" || descriptor.digest == "" || descriptor.repositoryID == "" {
		return OCIImageDescriptorRow{}, false
	}
	return OCIImageDescriptorRow{
		UID:              descriptor.uid,
		RepositoryID:     descriptor.repositoryID,
		Digest:           descriptor.digest,
		MediaType:        descriptor.mediaType,
		SizeBytes:        descriptor.sizeBytes,
		ArtifactType:     descriptor.artifactType,
		SourceFactID:     envelope.FactID,
		StableFactKey:    envelope.StableFactKey,
		SourceSystem:     ociRegistrySourceSystem(envelope),
		SourceRecordID:   envelope.SourceRef.SourceRecordID,
		SourceConfidence: envelope.SourceConfidence,
		CollectorKind:    envelope.CollectorKind,
		ObservedAt:       envelope.ObservedAt,
	}, true
}

func ociImageTagObservationRow(envelope facts.Envelope) (OCIImageTagObservationRow, bool) {
	repositoryID, _ := payloadString(envelope.Payload, "repository_id")
	tag, _ := payloadString(envelope.Payload, "tag")
	resolvedDigest, _ := payloadString(envelope.Payload, "resolved_digest")
	if repositoryID == "" || tag == "" || resolvedDigest == "" {
		return OCIImageTagObservationRow{}, false
	}
	mediaType, _ := payloadString(envelope.Payload, "media_type")
	previousDigest, _ := payloadString(envelope.Payload, "previous_digest")
	identityStrength, _ := payloadString(envelope.Payload, "identity_strength")
	if identityStrength == "" {
		identityStrength = "weak_tag"
	}
	mutated := false
	if ptr := payloadBoolPtr(envelope.Payload, "mutated"); ptr != nil {
		mutated = *ptr
	}
	return OCIImageTagObservationRow{
		UID:                   ociRegistryUID("tag_observation", repositoryID, tag, resolvedDigest),
		RepositoryID:          repositoryID,
		ImageRef:              ociImageRef(repositoryID, tag),
		Tag:                   tag,
		ResolvedDigest:        resolvedDigest,
		ResolvedDescriptorUID: ociDescriptorUID(repositoryID, resolvedDigest),
		MediaType:             mediaType,
		PreviousDigest:        previousDigest,
		Mutated:               mutated,
		IdentityStrength:      identityStrength,
		SourceFactID:          envelope.FactID,
		StableFactKey:         envelope.StableFactKey,
		SourceSystem:          ociRegistrySourceSystem(envelope),
		SourceRecordID:        envelope.SourceRef.SourceRecordID,
		SourceConfidence:      envelope.SourceConfidence,
		CollectorKind:         envelope.CollectorKind,
		ObservedAt:            envelope.ObservedAt,
	}, true
}

func ociImageReferrerRow(envelope facts.Envelope) (OCIImageReferrerRow, bool) {
	repositoryID, _ := payloadString(envelope.Payload, "repository_id")
	subjectDigest, _ := payloadString(envelope.Payload, "subject_digest")
	referrerDigest, _ := payloadString(envelope.Payload, "referrer_digest")
	if repositoryID == "" || subjectDigest == "" || referrerDigest == "" {
		return OCIImageReferrerRow{}, false
	}
	subjectMediaType, _ := payloadString(envelope.Payload, "subject_media_type")
	referrerMediaType, _ := payloadString(envelope.Payload, "referrer_media_type")
	artifactType, _ := payloadString(envelope.Payload, "artifact_type")
	sourceAPIPath, _ := payloadString(envelope.Payload, "source_api_path")
	sizeBytes, _ := payloadInt(envelope.Payload, "size_bytes")
	return OCIImageReferrerRow{
		UID:               ociRegistryUID("referrer", repositoryID, subjectDigest, referrerDigest),
		RepositoryID:      repositoryID,
		SubjectDigest:     subjectDigest,
		SubjectMediaType:  subjectMediaType,
		ReferrerDigest:    referrerDigest,
		ReferrerMediaType: referrerMediaType,
		ArtifactType:      artifactType,
		SizeBytes:         int64(sizeBytes),
		SourceAPIPath:     sourceAPIPath,
		SourceFactID:      envelope.FactID,
		StableFactKey:     envelope.StableFactKey,
		SourceSystem:      ociRegistrySourceSystem(envelope),
		SourceRecordID:    envelope.SourceRef.SourceRecordID,
		SourceConfidence:  envelope.SourceConfidence,
		CollectorKind:     envelope.CollectorKind,
		ObservedAt:        envelope.ObservedAt,
	}, true
}

type ociDescriptorPayload struct {
	uid          string
	repositoryID string
	digest       string
	mediaType    string
	sizeBytes    int64
	artifactType string
}

func ociDescriptorFields(envelope facts.Envelope) ociDescriptorPayload {
	repositoryID, _ := payloadString(envelope.Payload, "repository_id")
	descriptorID, _ := payloadString(envelope.Payload, "descriptor_id")
	digest, _ := payloadString(envelope.Payload, "digest")
	mediaType, _ := payloadString(envelope.Payload, "media_type")
	sizeBytes, _ := payloadInt(envelope.Payload, "size_bytes")
	artifactType, _ := payloadString(envelope.Payload, "artifact_type")
	if descriptorID == "" && repositoryID != "" && digest != "" {
		descriptorID = ociDescriptorUID(repositoryID, digest)
	}
	return ociDescriptorPayload{
		uid:          descriptorID,
		repositoryID: repositoryID,
		digest:       digest,
		mediaType:    mediaType,
		sizeBytes:    int64(sizeBytes),
		artifactType: artifactType,
	}
}

func ociDescriptorMapDigest(payload map[string]any, key string) string {
	raw, ok := payload[key].(map[string]any)
	if !ok {
		return ""
	}
	digest, _ := payloadString(raw, "digest")
	return digest
}

func ociDescriptorListDigests(payload map[string]any, key string) []string {
	raw, ok := payload[key].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	digests := make([]string, 0, len(raw))
	seen := map[string]struct{}{}
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		digest, _ := payloadString(entry, "digest")
		if digest == "" {
			continue
		}
		if _, ok := seen[digest]; ok {
			continue
		}
		seen[digest] = struct{}{}
		digests = append(digests, digest)
	}
	sort.Strings(digests)
	if len(digests) == 0 {
		return nil
	}
	return digests
}

func ociCorrelationAnchors(payload map[string]any) []string {
	raw, ok := payload["correlation_anchors"].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	anchors := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if !ok {
			continue
		}
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			anchors = append(anchors, trimmed)
		}
	}
	sort.Strings(anchors)
	if len(anchors) == 0 {
		return nil
	}
	return anchors
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
