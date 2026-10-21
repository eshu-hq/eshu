// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
)

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

	stableKey := facts.StableID(cloud.GCPCloudRelationshipFactKind, map[string]any{
		"source_full_resource_name": sourceName,
		"target_full_resource_name": targetName,
		"relationship_type":         relationshipType,
		"content_family":            obs.Boundary.ContentFamily,
	})

	attributes := map[string]any{
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
	sourceAssetType := strings.TrimSpace(obs.SourceAssetType)
	targetAssetType := strings.TrimSpace(obs.TargetAssetType)
	payload, err := factschema.EncodeGCPCloudRelationship(gcpv1.Relationship{
		SourceFullResourceName: sourceName,
		TargetFullResourceName: targetName,
		RelationshipType:       relationshipType,
		SourceAssetType:        &sourceAssetType,
		TargetAssetType:        &targetAssetType,
		SupportState:           &supportState,
		Attributes:             attributes,
	})
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("encode gcp_cloud_relationship payload: %w", err)
	}

	return newEnvelope(
		obs.Boundary,
		cloud.GCPCloudRelationshipFactKind,
		cloud.GCPCloudRelationshipSchemaVersion,
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
