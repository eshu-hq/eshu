// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// GCP relationship support states classify how completely the provider described
// an observed relationship. They are provenance only: the collector reports what
// Cloud Asset Inventory returned and resolves no endpoints and writes no graph
// edge (reducer-owned).
const (
	// RelationshipSupportSupported means the provider returned a complete
	// relationship between two resources. It is the default for an observed
	// relationship.
	RelationshipSupportSupported = "supported"
	// RelationshipSupportPartial means the relationship was observed but the
	// target is opaque or outside the readable boundary (e.g. cross-project), so a
	// reducer must treat the target as unresolved rather than a clean match.
	RelationshipSupportPartial = "partial"
	// RelationshipSupportUnsupported means the relationship type or tier is not
	// fully supported (e.g. a Security Command Center premium/enterprise-gated
	// edge), carried as provenance only.
	RelationshipSupportUnsupported = "unsupported"
)

// RelationshipObservation is one provider-observed relationship between two GCP
// resources, identified by Cloud Asset Inventory full resource names. The
// collector preserves both full resource names, the asset types, the bounded
// relationship type, and the support state as source evidence; it resolves
// nothing and writes no graph edge.
type RelationshipObservation struct {
	// Boundary carries the scope and generation contract fields.
	Boundary Boundary
	// SourceFullResourceName is the CAI full resource name of the owning resource.
	SourceFullResourceName string
	// SourceAssetType is the CAI asset type of the source resource.
	SourceAssetType string
	// RelationshipType is the bounded provider relationship type.
	RelationshipType string
	// TargetFullResourceName is the CAI full resource name of the related
	// resource. It may be in a different project; it is preserved verbatim.
	TargetFullResourceName string
	// TargetAssetType is the CAI asset type of the target resource.
	TargetAssetType string
	// SupportState classifies completeness; blank defaults to supported.
	SupportState string
	// UpdateTime is the relationship read/update time.
	UpdateTime time.Time
	// SourceRecordID overrides the default record id.
	SourceRecordID string
	// SourceURI is the bounded source URI.
	SourceURI string
}

// NewCloudRelationshipEnvelope builds the durable gcp_cloud_relationship fact for
// one observed relationship. It preserves both endpoint full resource names, the
// asset types, the relationship type, and the support state as provenance-only
// evidence: it does not resolve endpoints or write any graph edge (the reducer
// materializes edges only when both endpoints resolve in the allowed scope). The
// stable fact key is derived from the two full resource names, the relationship
// type, and the content family, so observation-time churn does not split
// idempotent re-emission of the same generation.
//
// It fails closed on a missing endpoint, a missing relationship type, or an
// unknown support state.
func NewCloudRelationshipEnvelope(obs RelationshipObservation) (facts.Envelope, error) {
	if err := validateBoundary(obs.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	sourceName := strings.TrimSpace(obs.SourceFullResourceName)
	if sourceName == "" {
		return facts.Envelope{}, fmt.Errorf("gcp relationship observation requires source full_resource_name")
	}
	targetName := strings.TrimSpace(obs.TargetFullResourceName)
	if targetName == "" {
		return facts.Envelope{}, fmt.Errorf("gcp relationship observation requires target full_resource_name")
	}
	relationshipType := strings.TrimSpace(obs.RelationshipType)
	if relationshipType == "" {
		return facts.Envelope{}, fmt.Errorf("gcp relationship observation requires relationship_type")
	}
	supportState, err := normalizeRelationshipSupportState(obs.SupportState)
	if err != nil {
		return facts.Envelope{}, err
	}

	stableKey := facts.StableID(facts.GCPCloudRelationshipFactKind, map[string]any{
		"source_full_resource_name": sourceName,
		"target_full_resource_name": targetName,
		"relationship_type":         relationshipType,
		"content_family":            obs.Boundary.ContentFamily,
	})

	payload := map[string]any{
		"collector_instance_id":     obs.Boundary.CollectorInstanceID,
		"parent_scope_kind":         string(obs.Boundary.ParentScopeKind),
		"parent_scope_id":           obs.Boundary.ParentScopeID,
		"asset_type_family":         obs.Boundary.AssetTypeFamily,
		"content_family":            obs.Boundary.ContentFamily,
		"location_bucket":           obs.Boundary.LocationBucket,
		"source_full_resource_name": sourceName,
		"source_asset_type":         strings.TrimSpace(obs.SourceAssetType),
		"source_project_id":         strings.TrimSpace(ProjectIDFromFullName(sourceName)),
		"relationship_type":         relationshipType,
		"target_full_resource_name": targetName,
		"target_asset_type":         strings.TrimSpace(obs.TargetAssetType),
		"target_project_id":         strings.TrimSpace(ProjectIDFromFullName(targetName)),
		"support_state":             supportState,
		"read_time":                 timeOrNil(obs.Boundary.ReadTime),
		"update_time":               timeOrNil(obs.UpdateTime.UTC()),
		"redaction_policy_version":  RedactionPolicyVersion,
	}

	return newEnvelope(
		obs.Boundary,
		facts.GCPCloudRelationshipFactKind,
		facts.GCPCloudRelationshipSchemaVersion,
		stableKey,
		sourceRecordID(obs.SourceRecordID, sourceName+"|"+relationshipType+"|"+targetName),
		obs.SourceURI,
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
		return "", fmt.Errorf("gcp relationship observation has unknown support_state %q", state)
	}
}
