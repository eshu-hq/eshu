// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	azurev1 "github.com/eshu-hq/eshu/sdk/go/factschema/azure/v1"
)

// Relationship support states classify how completely the provider described an
// observed relationship. They are provenance only: the collector reports what
// Azure returned and never resolves endpoints or decides graph truth, which is
// reducer-owned.
const (
	// RelationshipSupportSupported means the provider returned a complete
	// relationship between two ARM resources. It is the default for an observed
	// relationship.
	RelationshipSupportSupported = "supported"
	// RelationshipSupportPartial means the relationship was observed but the
	// target is opaque or outside the readable boundary (e.g. cross-tenant), so a
	// reducer must treat the target as unresolved rather than a clean match.
	RelationshipSupportPartial = "partial"
	// RelationshipSupportUnsupported means the relationship type or tier is not
	// fully supported by the provider (e.g. a Security Center premium-gated edge),
	// carried as provenance only.
	RelationshipSupportUnsupported = "unsupported"
)

// RelationshipObservation is one provider-observed relationship between two ARM
// resources. The collector preserves both raw ARM identities and the bounded
// relationship type and support state as source evidence; it resolves nothing
// and writes no graph edge.
type RelationshipObservation struct {
	// Boundary carries the scope and generation contract fields.
	Boundary Boundary
	// SourceARMResourceID is the raw ARM identity of the owning resource.
	SourceARMResourceID string
	// RelationshipType is the bounded provider relationship type.
	RelationshipType string
	// TargetARMResourceID is the raw ARM identity of the related resource. It may
	// be in a different subscription; the builder preserves it verbatim.
	TargetARMResourceID string
	// SupportState classifies completeness; blank defaults to supported.
	SupportState string
	// ProviderTime is the Resource Graph read/update time, or nil when absent.
	ProviderTime *time.Time
	// SourceRecordID overrides the default record id.
	SourceRecordID string
	// SourceURI is the bounded Resource Graph source URI.
	SourceURI string
}

// NewRelationshipEnvelope builds the durable azure_cloud_relationship fact for
// one observed relationship. It preserves both endpoint ARM identities, the
// relationship type, and the support state as provenance-only evidence: it does
// not resolve endpoints or write any graph edge (the reducer materializes edges
// only when both endpoints resolve in the allowed scope). The stable fact key is
// derived from the two normalized identities, the relationship type, source
// lane, and tenant only, so observation-time churn does not split idempotent
// re-emission of the same generation.
func NewRelationshipEnvelope(observation RelationshipObservation) (facts.Envelope, error) {
	if err := validateBoundary(observation.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	sourceID := strings.TrimSpace(observation.SourceARMResourceID)
	if sourceID == "" {
		return facts.Envelope{}, fmt.Errorf("azure relationship observation requires source_arm_resource_id")
	}
	targetID := strings.TrimSpace(observation.TargetARMResourceID)
	if targetID == "" {
		return facts.Envelope{}, fmt.Errorf("azure relationship observation requires target_arm_resource_id")
	}
	relationshipType := strings.TrimSpace(observation.RelationshipType)
	if relationshipType == "" {
		return facts.Envelope{}, fmt.Errorf("azure relationship observation requires relationship_type")
	}
	supportState, err := normalizeRelationshipSupportState(observation.SupportState)
	if err != nil {
		return facts.Envelope{}, err
	}

	source, err := ParseARMIdentity(sourceID)
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("normalize source arm identity: %w", err)
	}
	// The target may be a non-ARM or cross-boundary reference; normalize
	// best-effort and preserve the raw identity regardless.
	target, _ := ParseARMIdentity(targetID)

	stableKey := facts.StableID(facts.AzureCloudRelationshipFactKind, map[string]any{
		"source_normalized_id": source.Normalized,
		"target_normalized_id": target.Normalized,
		"target_raw_id":        targetID,
		"relationship_type":    relationshipType,
		"source_lane":          observation.Boundary.SourceLane,
		"tenant_id":            observation.Boundary.TenantID,
	})

	attributes := map[string]any{
		"collector_kind":                CollectorKind,
		"collector_instance_id":         observation.Boundary.CollectorInstanceID,
		"tenant_id":                     observation.Boundary.TenantID,
		"scope_kind":                    observation.Boundary.ScopeKind,
		"provider_scope_id":             observation.Boundary.ProviderScopeID,
		"source_lane":                   observation.Boundary.SourceLane,
		"source_arm_resource_id":        sourceID,
		"source_normalized_resource_id": source.Normalized,
		"source_subscription_id":        source.SubscriptionID,
		"source_resource_group":         source.ResourceGroup,
		"source_provider_namespace":     source.ProviderNamespace,
		"source_resource_type":          source.ResourceType,
		"relationship_type":             relationshipType,
		"target_arm_resource_id":        targetID,
		"target_normalized_resource_id": target.Normalized,
		"target_subscription_id":        target.SubscriptionID,
		"target_resource_type":          target.ResourceType,
		"support_state":                 supportState,
		"provider_time":                 timeOrNil(observation.ProviderTime),
		"redaction_policy_version":      RedactionPolicyVersion,
	}
	sourceNormalized := source.Normalized
	targetNormalized := target.Normalized
	targetType := target.ResourceType
	payload, err := factschema.EncodeAzureCloudRelationship(azurev1.CloudRelationship{
		RelationshipType:           relationshipType,
		SourceARMResourceID:        sourceID,
		TargetARMResourceID:        targetID,
		SourceNormalizedResourceID: &sourceNormalized,
		TargetNormalizedResourceID: &targetNormalized,
		TargetResourceType:         &targetType,
		SupportState:               &supportState,
		Attributes:                 attributes,
	})
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("encode azure_cloud_relationship payload: %w", err)
	}

	return newEnvelope(
		observation.Boundary,
		facts.AzureCloudRelationshipFactKind,
		facts.AzureCloudRelationshipSchemaVersion,
		stableKey,
		sourceRecordID(observation.SourceRecordID, source.Normalized+"|"+relationshipType+"|"+target.Normalized),
		observation.SourceURI,
		payload,
	), nil
}

// normalizeRelationshipSupportState defaults a blank state to supported and
// rejects any value outside the bounded set so a fabricated state never reaches
// durable facts.
func normalizeRelationshipSupportState(state string) (string, error) {
	switch strings.TrimSpace(state) {
	case "":
		return RelationshipSupportSupported, nil
	case RelationshipSupportSupported:
		return RelationshipSupportSupported, nil
	case RelationshipSupportPartial:
		return RelationshipSupportPartial, nil
	case RelationshipSupportUnsupported:
		return RelationshipSupportUnsupported, nil
	default:
		return "", fmt.Errorf("azure relationship observation has unknown support_state %q", state)
	}
}
