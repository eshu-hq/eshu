package ociregistry

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

var sensitiveURLPattern = regexp.MustCompile(`https?://\S+`)

// NewRepositoryEnvelope builds the durable repository fact for one OCI
// repository observation.
func NewRepositoryEnvelope(observation RepositoryObservation) (facts.Envelope, error) {
	repository, err := NormalizeRepositoryIdentity(observation.Identity)
	if err != nil {
		return facts.Envelope{}, err
	}
	if err := validateBoundary(observation.GenerationID, observation.CollectorInstanceID, "oci repository observation"); err != nil {
		return facts.Envelope{}, err
	}
	visibility := observation.Visibility
	if visibility == "" {
		visibility = VisibilityUnknown
	}
	authMode := observation.AuthMode
	if authMode == "" {
		authMode = AuthModeUnknown
	}
	payload := map[string]any{
		"collector_instance_id": observation.CollectorInstanceID,
		"provider":              string(repository.Provider),
		"registry":              repository.Registry,
		"repository":            repository.Repository,
		"repository_id":         repository.RepositoryID,
		"visibility":            string(visibility),
		"auth_mode":             string(authMode),
		"correlation_anchors":   []string{repository.RepositoryID},
	}
	return newEnvelope(repository, facts.OCIRegistryRepositoryFactKind, facts.OCIRegistryRepositorySchemaVersion, repository.RepositoryID, observation.GenerationID, observation.CollectorInstanceID, observation.FencingToken, observation.ObservedAt, observation.SourceURI, repository.RepositoryID, payload), nil
}

// NewTagObservationEnvelope builds the durable mutable tag observation fact.
func NewTagObservationEnvelope(observation TagObservation) (facts.Envelope, error) {
	repository, err := NormalizeRepositoryIdentity(observation.Repository)
	if err != nil {
		return facts.Envelope{}, err
	}
	digest, err := normalizeDigest(observation.Digest)
	if err != nil {
		return facts.Envelope{}, err
	}
	tag := strings.TrimSpace(observation.Tag)
	if tag == "" {
		return facts.Envelope{}, fmt.Errorf("oci tag observation tag must not be blank")
	}
	if err := validateBoundary(observation.GenerationID, observation.CollectorInstanceID, "oci tag observation"); err != nil {
		return facts.Envelope{}, err
	}
	previousDigest := ""
	if strings.TrimSpace(observation.PreviousDigest) != "" {
		previousDigest, err = normalizeDigest(observation.PreviousDigest)
		if err != nil {
			return facts.Envelope{}, err
		}
	}
	payload := map[string]any{
		"collector_instance_id": observation.CollectorInstanceID,
		"provider":              string(repository.Provider),
		"registry":              repository.Registry,
		"repository":            repository.Repository,
		"repository_id":         repository.RepositoryID,
		"tag":                   tag,
		"resolved_digest":       digest,
		"media_type":            strings.TrimSpace(observation.MediaType),
		"previous_digest":       previousDigest,
		"mutated":               observation.Mutated,
		"identity_strength":     IdentityStrengthWeakTag,
		"correlation_anchors":   []string{repository.RepositoryID, digest},
	}
	stableKey := facts.StableID(facts.OCIImageTagObservationFactKind, map[string]any{
		"digest":        digest,
		"repository_id": repository.RepositoryID,
		"tag":           tag,
	})
	return newEnvelope(repository, facts.OCIImageTagObservationFactKind, facts.OCIImageTagObservationSchemaVersion, stableKey, observation.GenerationID, observation.CollectorInstanceID, observation.FencingToken, observation.ObservedAt, observation.SourceURI, repository.RepositoryID+"#"+tag, payload), nil
}

// NewManifestEnvelope builds the durable image manifest fact for one digest.
func NewManifestEnvelope(observation ManifestObservation) (facts.Envelope, error) {
	repository, descriptor, err := normalizedDescriptor(observation.Repository, observation.Descriptor)
	if err != nil {
		return facts.Envelope{}, err
	}
	if err := validateBoundary(observation.GenerationID, observation.CollectorInstanceID, "oci manifest observation"); err != nil {
		return facts.Envelope{}, err
	}
	payload := descriptorPayload(repository, descriptor, observation.Descriptor)
	payload["collector_instance_id"] = observation.CollectorInstanceID
	payload["source_tag"] = strings.TrimSpace(observation.SourceTag)
	payload["config"] = descriptorMap(observation.Config)
	payload["layers"] = descriptorMaps(observation.Layers)
	payload["correlation_anchors"] = []string{repository.RepositoryID, descriptor.Digest}
	return newEnvelope(repository, facts.OCIImageManifestFactKind, facts.OCIImageManifestSchemaVersion, descriptor.DescriptorID, observation.GenerationID, observation.CollectorInstanceID, observation.FencingToken, observation.ObservedAt, observation.SourceURI, descriptor.DescriptorID, payload), nil
}

// NewImageIndexEnvelope builds the durable image index fact for one digest.
func NewImageIndexEnvelope(observation IndexObservation) (facts.Envelope, error) {
	repository, descriptor, err := normalizedDescriptor(observation.Repository, observation.Descriptor)
	if err != nil {
		return facts.Envelope{}, err
	}
	if err := validateBoundary(observation.GenerationID, observation.CollectorInstanceID, "oci image index observation"); err != nil {
		return facts.Envelope{}, err
	}
	payload := descriptorPayload(repository, descriptor, observation.Descriptor)
	payload["collector_instance_id"] = observation.CollectorInstanceID
	payload["manifests"] = descriptorMaps(observation.Manifests)
	payload["correlation_anchors"] = []string{repository.RepositoryID, descriptor.Digest}
	return newEnvelope(repository, facts.OCIImageIndexFactKind, facts.OCIImageIndexSchemaVersion, descriptor.DescriptorID, observation.GenerationID, observation.CollectorInstanceID, observation.FencingToken, observation.ObservedAt, observation.SourceURI, descriptor.DescriptorID, payload), nil
}

// NewDescriptorEnvelope builds the durable reusable descriptor fact.
func NewDescriptorEnvelope(observation DescriptorObservation) (facts.Envelope, error) {
	repository, descriptor, err := normalizedDescriptor(observation.Repository, observation.Descriptor)
	if err != nil {
		return facts.Envelope{}, err
	}
	if err := validateBoundary(observation.GenerationID, observation.CollectorInstanceID, "oci descriptor observation"); err != nil {
		return facts.Envelope{}, err
	}
	payload := descriptorPayload(repository, descriptor, observation.Descriptor)
	payload["collector_instance_id"] = observation.CollectorInstanceID
	payload["correlation_anchors"] = []string{repository.RepositoryID, descriptor.Digest}
	return newEnvelope(repository, facts.OCIImageDescriptorFactKind, facts.OCIImageDescriptorSchemaVersion, descriptor.DescriptorID, observation.GenerationID, observation.CollectorInstanceID, observation.FencingToken, observation.ObservedAt, observation.SourceURI, descriptor.DescriptorID, payload), nil
}

// NewReferrerEnvelope builds the durable referrer fact for one subject and
// referrer descriptor pair.
func NewReferrerEnvelope(observation ReferrerObservation) (facts.Envelope, error) {
	repository, referrer, err := normalizedDescriptor(observation.Repository, observation.Referrer)
	if err != nil {
		return facts.Envelope{}, err
	}
	subjectDigest, err := normalizeDigest(observation.Subject.Digest)
	if err != nil {
		return facts.Envelope{}, err
	}
	if err := validateBoundary(observation.GenerationID, observation.CollectorInstanceID, "oci referrer observation"); err != nil {
		return facts.Envelope{}, err
	}
	stableKey := facts.StableID(facts.OCIImageReferrerFactKind, map[string]any{
		"referrer_digest": referrer.Digest,
		"repository_id":   repository.RepositoryID,
		"subject_digest":  subjectDigest,
	})
	payload := map[string]any{
		"collector_instance_id": observation.CollectorInstanceID,
		"provider":              string(repository.Provider),
		"registry":              repository.Registry,
		"repository":            repository.Repository,
		"repository_id":         repository.RepositoryID,
		"subject_digest":        subjectDigest,
		"subject_media_type":    strings.TrimSpace(observation.Subject.MediaType),
		"referrer_digest":       referrer.Digest,
		"referrer_media_type":   referrer.MediaType,
		"artifact_type":         strings.TrimSpace(observation.Referrer.ArtifactType),
		"size_bytes":            observation.Referrer.SizeBytes,
		"source_api_path":       strings.TrimSpace(observation.SourceAPIPath),
		"annotations":           redactedAnnotations(observation.Referrer.Annotations),
		"correlation_anchors":   []string{repository.RepositoryID, subjectDigest, referrer.Digest},
	}
	return newEnvelope(repository, facts.OCIImageReferrerFactKind, facts.OCIImageReferrerSchemaVersion, stableKey, observation.GenerationID, observation.CollectorInstanceID, observation.FencingToken, observation.ObservedAt, observation.SourceURI, subjectDigest+"->"+referrer.Digest, payload), nil
}

func newEnvelope(repository NormalizedRepositoryIdentity, factKind, schemaVersion, stableKey, generationID, collectorInstanceID string, fencingToken int64, observedAt time.Time, sourceURI, sourceRecordID string, payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactID:           ociRegistryFactID(factKind, stableKey, repository.ScopeID, generationID),
		ScopeID:          repository.ScopeID,
		GenerationID:     generationID,
		FactKind:         factKind,
		StableFactKey:    stableKey,
		SchemaVersion:    schemaVersion,
		CollectorKind:    CollectorKind,
		FencingToken:     fencingToken,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       normalizedObservedAt(observedAt),
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   CollectorKind,
			ScopeID:        repository.ScopeID,
			GenerationID:   generationID,
			FactKey:        stableKey,
			SourceURI:      sanitizeURL(sourceURI),
			SourceRecordID: sourceRecordID,
		},
	}
}

func ociRegistryFactID(factKind, stableFactKey, scopeID, generationID string) string {
	return facts.StableID("OCIRegistryFact", map[string]any{
		"fact_kind":       factKind,
		"generation_id":   generationID,
		"scope_id":        scopeID,
		"stable_fact_key": stableFactKey,
	})
}

func normalizedDescriptor(repository RepositoryIdentity, descriptor Descriptor) (NormalizedRepositoryIdentity, NormalizedDescriptorIdentity, error) {
	normalized, err := NormalizeDescriptorIdentity(DescriptorIdentity{
		Repository: repository,
		Digest:     descriptor.Digest,
		MediaType:  descriptor.MediaType,
	})
	return normalized.Repository, normalized, err
}

func descriptorPayload(repository NormalizedRepositoryIdentity, identity NormalizedDescriptorIdentity, descriptor Descriptor) map[string]any {
	return map[string]any{
		"provider":      string(repository.Provider),
		"registry":      repository.Registry,
		"repository":    repository.Repository,
		"repository_id": repository.RepositoryID,
		"descriptor_id": identity.DescriptorID,
		"digest":        identity.Digest,
		"media_type":    identity.MediaType,
		"size_bytes":    descriptor.SizeBytes,
		"artifact_type": strings.TrimSpace(descriptor.ArtifactType),
		"annotations":   redactedAnnotations(descriptor.Annotations),
	}
}

func descriptorMaps(descriptors []Descriptor) []map[string]any {
	if len(descriptors) == 0 {
		return nil
	}
	mapped := make([]map[string]any, 0, len(descriptors))
	for _, descriptor := range descriptors {
		mapped = append(mapped, descriptorMap(descriptor))
	}
	return mapped
}

func descriptorMap(descriptor Descriptor) map[string]any {
	digest, _ := normalizeDigest(descriptor.Digest)
	return map[string]any{
		"digest":        digest,
		"media_type":    strings.TrimSpace(descriptor.MediaType),
		"size_bytes":    descriptor.SizeBytes,
		"artifact_type": strings.TrimSpace(descriptor.ArtifactType),
		"annotations":   redactedAnnotations(descriptor.Annotations),
		"platform":      platformMap(descriptor.Platform),
	}
}

func platformMap(platform Platform) map[string]string {
	mapped := map[string]string{}
	if strings.TrimSpace(platform.OS) != "" {
		mapped["os"] = strings.TrimSpace(platform.OS)
	}
	if strings.TrimSpace(platform.Architecture) != "" {
		mapped["architecture"] = strings.TrimSpace(platform.Architecture)
	}
	if strings.TrimSpace(platform.Variant) != "" {
		mapped["variant"] = strings.TrimSpace(platform.Variant)
	}
	if len(mapped) == 0 {
		return nil
	}
	return mapped
}

func validateBoundary(generationID, collectorInstanceID, noun string) error {
	if generationID == "" {
		return fmt.Errorf("%s generation_id must not be blank", noun)
	}
	if collectorInstanceID == "" {
		return fmt.Errorf("%s collector_instance_id must not be blank", noun)
	}
	return nil
}

func normalizedObservedAt(observedAt time.Time) time.Time {
	if observedAt.IsZero() {
		return time.Now().UTC()
	}
	return observedAt.UTC()
}

func sanitizeURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return trimmed
	}
	parsed.User = nil
	query := parsed.Query()
	for key := range query {
		if isSensitiveQueryKey(key) {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func sanitizeText(input string) string {
	return sensitiveURLPattern.ReplaceAllStringFunc(input, sanitizeURL)
}

func isSensitiveQueryKey(key string) bool {
	sensitiveKeys := []string{
		"access_token",
		"api_key",
		"apikey",
		"auth",
		"authorization",
		"jwt",
		"key",
		"password",
		"passwd",
		"secret",
		"sig",
		"signature",
		"token",
		"x-amz-credential",
		"x-amz-security-token",
		"x-amz-signature",
	}
	return slices.Contains(sensitiveKeys, strings.ToLower(strings.TrimSpace(key)))
}

func redactedAnnotations(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		if knownAnnotation(key) {
			output[key] = sanitizeURL(value)
			continue
		}
		output[key] = RedactedValue
	}
	return output
}

func knownAnnotation(key string) bool {
	switch key {
	case "org.opencontainers.image.created",
		"org.opencontainers.image.description",
		"org.opencontainers.image.documentation",
		"org.opencontainers.image.revision",
		"org.opencontainers.image.source",
		"org.opencontainers.image.title",
		"org.opencontainers.image.url",
		"org.opencontainers.image.version":
		return true
	default:
		return false
	}
}
