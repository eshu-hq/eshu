// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// AzureExtensionSchemaVersion versions the provider-specific extension object
// embedded in azure_cloud_resource facts. It is independent of the fact schema
// version so the extension shape can evolve without a fact-kind migration.
const AzureExtensionSchemaVersion = "1.0.0"

// NewResourceEnvelope builds the durable azure_cloud_resource fact for one
// normalized Resource Graph resource. It preserves the raw ARM resource ID,
// adds normalized identity fields, embeds a versioned and redacted
// provider-specific extension object, and records the redaction policy version.
//
// The stable fact key is derived from the normalized ARM identity, resource
// type, and source lane only, so churn in volatile extension or tag values does
// not split idempotent re-emission of the same generation.
func NewResourceEnvelope(observation ResourceObservation) (facts.Envelope, error) {
	if err := validateBoundary(observation.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	armResourceID := strings.TrimSpace(observation.ARMResourceID)
	if armResourceID == "" {
		return facts.Envelope{}, fmt.Errorf("azure resource observation requires arm_resource_id")
	}
	identity := observation.Identity
	if strings.TrimSpace(identity.Normalized) == "" {
		parsed, err := ParseARMIdentity(armResourceID)
		if err != nil {
			return facts.Envelope{}, fmt.Errorf("normalize arm identity: %w", err)
		}
		identity = parsed
	}

	stableKey := facts.StableID(facts.AzureCloudResourceFactKind, map[string]any{
		"normalized_id": identity.Normalized,
		"resource_type": identity.ResourceType,
		"source_lane":   observation.Boundary.SourceLane,
		"tenant_id":     observation.Boundary.TenantID,
	})

	redaction := RedactExtension(observation.RawExtension)
	extension := map[string]any{
		"schema_version": AzureExtensionSchemaVersion,
		"data":           redaction.Extension,
		"redacted":       redaction.Redacted,
		"redacted_keys":  redaction.RedactedKeys,
		"managed_by":     strings.TrimSpace(observation.ManagedBy),
		"api_version":    strings.TrimSpace(observation.APIVersion),
		"has_identity":   observation.HasIdentity,
	}

	payload := map[string]any{
		"collector_kind":           CollectorKind,
		"collector_instance_id":    observation.Boundary.CollectorInstanceID,
		"tenant_id":                observation.Boundary.TenantID,
		"scope_kind":               observation.Boundary.ScopeKind,
		"provider_scope_id":        observation.Boundary.ProviderScopeID,
		"source_lane":              observation.Boundary.SourceLane,
		"arm_resource_id":          armResourceID,
		"subscription_id":          identity.SubscriptionID,
		"resource_group":           identity.ResourceGroup,
		"provider_namespace":       identity.ProviderNamespace,
		"resource_type":            identity.ResourceType,
		"resource_name":            identity.ResourceName,
		"normalized_resource_id":   identity.Normalized,
		"location":                 strings.TrimSpace(observation.Boundary.LocationBucket),
		"kind":                     strings.TrimSpace(observation.Kind),
		"sku_class":                strings.TrimSpace(observation.SKUClass),
		"tags":                     cloneStringMap(observation.Tags),
		"provider_time":            timeOrNil(observation.ProviderTime),
		"redaction_policy_version": RedactionPolicyVersion,
		"extension":                extension,
	}

	return newEnvelope(
		observation.Boundary,
		facts.AzureCloudResourceFactKind,
		facts.AzureCloudResourceSchemaVersion,
		stableKey,
		sourceRecordID(observation.SourceRecordID, identity.Normalized),
		observation.SourceURI,
		payload,
	), nil
}

// NewTagObservationEnvelope builds the durable azure_tag_observation fact for
// one resource's tags. Tag values are fingerprinted with the provided redaction
// key so tag value text never reaches durable facts; tag keys are preserved as
// correlation taxonomy. The stable fact key is derived from the normalized ARM
// identity, resource type, and source lane only, so tag value churn does not
// split idempotent re-emission of the same generation.
//
// It returns an error when the observation carries no usable tags (an empty tag
// observation is missing evidence, not a clean match) or when the redaction key
// is zero (fingerprinting must never run keyless). Callers must skip emission
// for untagged resources rather than treating the error as fatal.
func NewTagObservationEnvelope(observation ResourceObservation, key redact.Key) (facts.Envelope, error) {
	if err := validateBoundary(observation.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	if key.IsZero() {
		return facts.Envelope{}, fmt.Errorf("azure tag observation requires a redaction key")
	}
	armResourceID := strings.TrimSpace(observation.ARMResourceID)
	if armResourceID == "" {
		return facts.Envelope{}, fmt.Errorf("azure tag observation requires arm_resource_id")
	}
	identity := observation.Identity
	if strings.TrimSpace(identity.Normalized) == "" {
		parsed, err := ParseARMIdentity(armResourceID)
		if err != nil {
			return facts.Envelope{}, fmt.Errorf("normalize arm identity: %w", err)
		}
		identity = parsed
	}

	fingerprints, tagKeys, truncated := FingerprintTagValues(observation.Tags, key)
	if len(fingerprints) == 0 {
		return facts.Envelope{}, fmt.Errorf("azure tag observation requires at least one tag")
	}

	stableKey := facts.StableID(facts.AzureTagObservationFactKind, map[string]any{
		"normalized_id": identity.Normalized,
		"resource_type": identity.ResourceType,
		"source_lane":   observation.Boundary.SourceLane,
		"tenant_id":     observation.Boundary.TenantID,
	})

	payload := map[string]any{
		"collector_kind":           CollectorKind,
		"collector_instance_id":    observation.Boundary.CollectorInstanceID,
		"tenant_id":                observation.Boundary.TenantID,
		"scope_kind":               observation.Boundary.ScopeKind,
		"provider_scope_id":        observation.Boundary.ProviderScopeID,
		"source_lane":              observation.Boundary.SourceLane,
		"arm_resource_id":          armResourceID,
		"subscription_id":          identity.SubscriptionID,
		"resource_group":           identity.ResourceGroup,
		"provider_namespace":       identity.ProviderNamespace,
		"resource_type":            identity.ResourceType,
		"resource_name":            identity.ResourceName,
		"normalized_resource_id":   identity.Normalized,
		"tag_value_fingerprints":   fingerprints,
		"tag_keys":                 tagKeys,
		"tag_count":                len(fingerprints),
		"tag_truncated":            truncated,
		"provider_time":            timeOrNil(observation.ProviderTime),
		"redaction_policy_version": RedactionPolicyVersion,
	}

	return newEnvelope(
		observation.Boundary,
		facts.AzureTagObservationFactKind,
		facts.AzureTagObservationSchemaVersion,
		stableKey,
		sourceRecordID(observation.SourceRecordID, identity.Normalized),
		observation.SourceURI,
		payload,
	), nil
}

// NewWarningEnvelope builds the durable azure_collection_warning fact for one
// explicit partial, permission-hidden, truncation, throttle, fallback,
// stale, unsupported, or redaction outcome. Warning facts are how the collector
// reports incomplete coverage as evidence instead of silent success.
func NewWarningEnvelope(observation WarningObservation) (facts.Envelope, error) {
	if err := validateBoundary(observation.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	warningKind := strings.TrimSpace(observation.WarningKind)
	if warningKind == "" {
		return facts.Envelope{}, fmt.Errorf("azure warning observation requires warning_kind")
	}
	outcome := strings.TrimSpace(observation.Outcome)
	if outcome == "" {
		outcome = OutcomePartial
	}

	stableKey := facts.StableID(facts.AzureCollectionWarningFactKind, map[string]any{
		"generation_id":   observation.Boundary.GenerationID,
		"resource_family": observation.Boundary.ResourceTypeFamily,
		"scope_id":        observation.Boundary.ScopeID,
		"source_lane":     observation.Boundary.SourceLane,
		"warning_kind":    warningKind,
	})

	payload := map[string]any{
		"collector_kind":        CollectorKind,
		"collector_instance_id": observation.Boundary.CollectorInstanceID,
		"tenant_id":             observation.Boundary.TenantID,
		"scope_kind":            observation.Boundary.ScopeKind,
		"provider_scope_id":     observation.Boundary.ProviderScopeID,
		"resource_family":       observation.Boundary.ResourceTypeFamily,
		"source_lane":           observation.Boundary.SourceLane,
		"warning_kind":          warningKind,
		"outcome":               outcome,
		"retryable":             observation.Retryable,
		"hidden_resource_count": observation.HiddenResourceCount,
		"message":               sanitizeWarningMessage(observation.Message),
	}

	return newEnvelope(
		observation.Boundary,
		facts.AzureCollectionWarningFactKind,
		facts.AzureCollectionWarningSchemaVersion,
		stableKey,
		sourceRecordID(observation.SourceRecordID, warningKind),
		observation.SourceURI,
		payload,
	), nil
}

func newEnvelope(
	boundary Boundary,
	factKind string,
	schemaVersion string,
	stableKey string,
	sourceRecordID string,
	sourceURI string,
	payload map[string]any,
) facts.Envelope {
	observedAt := boundary.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	return facts.Envelope{
		FactID:           azureFactID(factKind, stableKey, boundary.ScopeID, boundary.GenerationID),
		ScopeID:          boundary.ScopeID,
		GenerationID:     boundary.GenerationID,
		FactKind:         factKind,
		StableFactKey:    stableKey,
		SchemaVersion:    schemaVersion,
		CollectorKind:    CollectorKind,
		FencingToken:     boundary.FencingToken,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       observedAt,
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   CollectorKind,
			ScopeID:        boundary.ScopeID,
			GenerationID:   boundary.GenerationID,
			FactKey:        stableKey,
			SourceURI:      strings.TrimSpace(sourceURI),
			SourceRecordID: sourceRecordID,
		},
	}
}

func azureFactID(factKind, stableFactKey, scopeID, generationID string) string {
	return facts.StableID("AzureFact", map[string]any{
		"fact_kind":       factKind,
		"generation_id":   generationID,
		"scope_id":        scopeID,
		"stable_fact_key": stableFactKey,
	})
}

func validateBoundary(boundary Boundary) error {
	switch {
	case strings.TrimSpace(boundary.CollectorInstanceID) == "":
		return fmt.Errorf("azure observation requires collector_instance_id")
	case strings.TrimSpace(boundary.TenantID) == "":
		return fmt.Errorf("azure observation requires tenant_id")
	case !validScopeKind(boundary.ScopeKind):
		return fmt.Errorf("azure observation requires a valid scope_kind, got %q", boundary.ScopeKind)
	case !validSourceLane(boundary.SourceLane):
		return fmt.Errorf("azure observation requires a valid source_lane, got %q", boundary.SourceLane)
	case strings.TrimSpace(boundary.ScopeID) == "":
		return fmt.Errorf("azure observation requires scope_id")
	case strings.TrimSpace(boundary.GenerationID) == "":
		return fmt.Errorf("azure observation requires generation_id")
	case boundary.FencingToken <= 0:
		return fmt.Errorf("azure observation fencing_token must be positive")
	default:
		return nil
	}
}

func validScopeKind(kind string) bool {
	switch kind {
	case ScopeKindSubscription, ScopeKindManagementGroup, ScopeKindTenant:
		return true
	default:
		return false
	}
}

func validSourceLane(lane string) bool {
	switch lane {
	case SourceLaneResourceGraph, SourceLaneResourceChanges, SourceLaneARMFallback:
		return true
	default:
		return false
	}
}

func sourceRecordID(candidate, fallback string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate != "" {
		return candidate
	}
	return strings.TrimSpace(fallback)
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func timeOrNil(input *time.Time) any {
	if input == nil || input.IsZero() {
		return nil
	}
	return input.UTC()
}

func sanitizeWarningMessage(message string) string {
	const maxWarningMessageLength = 240
	sanitized := strings.Join(strings.Fields(strings.TrimSpace(message)), " ")
	if len(sanitized) <= maxWarningMessageLength {
		return sanitized
	}
	return sanitized[:maxWarningMessageLength-3] + "..."
}
