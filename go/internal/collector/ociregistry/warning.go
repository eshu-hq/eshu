package ociregistry

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// NewWarningEnvelope builds the durable warning fact for one non-fatal OCI
// registry collection warning.
func NewWarningEnvelope(observation WarningObservation) (facts.Envelope, error) {
	warningCode := strings.TrimSpace(observation.WarningCode)
	if warningCode == "" {
		return facts.Envelope{}, fmt.Errorf("oci registry warning code must not be blank")
	}
	if err := validateBoundary(observation.GenerationID, observation.CollectorInstanceID, "oci registry warning"); err != nil {
		return facts.Envelope{}, err
	}

	var repository NormalizedRepositoryIdentity
	var err error
	if observation.Repository != nil {
		repository, err = NormalizeRepositoryIdentity(*observation.Repository)
		if err != nil {
			return facts.Envelope{}, err
		}
	} else {
		repository = NormalizedRepositoryIdentity{ScopeID: "oci-registry://warnings", RepositoryID: "oci-registry://warnings"}
	}
	digest := strings.TrimSpace(observation.Digest)
	if digest != "" {
		digest, err = normalizeDigest(digest)
		if err != nil {
			return facts.Envelope{}, err
		}
	}
	warningKey := strings.TrimSpace(observation.WarningKey)
	if warningKey == "" {
		warningKey = warningCode
	}
	severity := strings.TrimSpace(observation.Severity)
	if severity == "" {
		severity = "warning"
	}
	referrersState := ""
	if warningCode == WarningUnsupportedReferrersAPI {
		referrersState = ReferrersUnsupported
	}
	stableKey := facts.StableID(facts.OCIRegistryWarningFactKind, map[string]any{
		"digest":       digest,
		"repository":   repository.RepositoryID,
		"warning_code": warningCode,
		"warning_key":  warningKey,
	})
	payload := map[string]any{
		"collector_instance_id": observation.CollectorInstanceID,
		"provider":              string(repository.Provider),
		"registry":              repository.Registry,
		"repository":            repository.Repository,
		"repository_id":         repository.RepositoryID,
		"warning_key":           warningKey,
		"warning_code":          warningCode,
		"severity":              severity,
		"message":               sanitizeText(observation.Message),
		"digest":                digest,
		"referrers_state":       referrersState,
		"correlation_anchors":   []string{repository.RepositoryID, digest},
	}
	return newEnvelope(repository, facts.OCIRegistryWarningFactKind, facts.OCIRegistryWarningSchemaVersion, stableKey, observation.GenerationID, observation.CollectorInstanceID, observation.FencingToken, observation.ObservedAt, observation.SourceURI, warningKey, payload), nil
}
