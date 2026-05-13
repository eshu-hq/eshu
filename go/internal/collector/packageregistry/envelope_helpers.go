package packageregistry

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

var warningURLPattern = regexp.MustCompile(`https?://\S+`)

type envelopeInput struct {
	factKind            string
	stableFactKey       string
	schemaVersion       string
	scopeID             string
	generationID        string
	collectorInstanceID string
	fencingToken        int64
	sourceURI           string
	sourceRecordID      string
	payload             map[string]any
}

func newEnvelope(input envelopeInput) facts.Envelope {
	return facts.Envelope{
		FactID:           packageRegistryFactID(input.factKind, input.stableFactKey, input.scopeID, input.generationID),
		ScopeID:          input.scopeID,
		GenerationID:     input.generationID,
		FactKind:         input.factKind,
		StableFactKey:    input.stableFactKey,
		SchemaVersion:    input.schemaVersion,
		CollectorKind:    CollectorKind,
		FencingToken:     input.fencingToken,
		SourceConfidence: facts.SourceConfidenceReported,
		Payload:          input.payload,
		SourceRef: facts.Ref{
			SourceSystem:   CollectorKind,
			ScopeID:        input.scopeID,
			GenerationID:   input.generationID,
			FactKey:        input.stableFactKey,
			SourceURI:      sanitizeURL(input.sourceURI),
			SourceRecordID: input.sourceRecordID,
		},
	}
}

func packageRegistryFactID(factKind, stableFactKey, scopeID, generationID string) string {
	return facts.StableID("PackageRegistryFact", map[string]any{
		"fact_kind":       factKind,
		"generation_id":   generationID,
		"scope_id":        scopeID,
		"stable_fact_key": stableFactKey,
	})
}

func correlationAnchors(values ...string) []string {
	anchors := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		anchors = append(anchors, trimmed)
	}
	if len(anchors) == 0 {
		return nil
	}
	return anchors
}

func validateObservationBoundary(scopeID, generationID, collectorInstanceID, noun string) error {
	if scopeID == "" {
		return fmt.Errorf("%s scope_id must not be blank", noun)
	}
	if generationID == "" {
		return fmt.Errorf("%s generation_id must not be blank", noun)
	}
	if collectorInstanceID == "" {
		return fmt.Errorf("%s collector_instance_id must not be blank", noun)
	}
	return nil
}

func packageVersionID(identity PackageIdentity, version, noun string) (NormalizedPackageIdentity, string, string, error) {
	normalized, err := NormalizePackageIdentity(identity)
	if err != nil {
		return NormalizedPackageIdentity{}, "", "", err
	}
	trimmedVersion := strings.TrimSpace(version)
	if trimmedVersion == "" {
		return NormalizedPackageIdentity{}, "", "", fmt.Errorf("%s version must not be blank", noun)
	}
	return normalized, trimmedVersion, normalized.PackageID + "@" + trimmedVersion, nil
}

func optionalPackageVersionID(
	identity PackageIdentity,
	version string,
) (NormalizedPackageIdentity, string, string, error) {
	normalized, err := NormalizePackageIdentity(identity)
	if err != nil {
		return NormalizedPackageIdentity{}, "", "", err
	}
	trimmedVersion := strings.TrimSpace(version)
	versionID := ""
	if trimmedVersion != "" {
		versionID = normalized.PackageID + "@" + trimmedVersion
	}
	return normalized, trimmedVersion, versionID, nil
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
	return warningURLPattern.ReplaceAllStringFunc(input, sanitizeURL)
}

func isSensitiveQueryKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
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
	return slices.Contains(sensitiveKeys, normalized)
}
