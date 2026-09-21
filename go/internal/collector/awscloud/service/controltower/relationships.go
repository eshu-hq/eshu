// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package controltower

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// controlGovernsTargetRelationship records that an enabled control governs an
// Organizations target. Control Tower reports the target as an Organizations OU
// ARN; the edge is keyed by the bare ou-… id the organizations scanner
// publishes so it joins that node. It returns nil when the control identity or
// the target cannot be resolved to a known Organizations family, so the edge is
// skipped rather than dangled.
func controlGovernsTargetRelationship(
	boundary awscloud.Boundary,
	control EnabledControl,
) *awscloud.RelationshipObservation {
	sourceID := strings.TrimSpace(control.ARN)
	if sourceID == "" {
		return nil
	}
	target, ok := resolveOrganizationsTarget(control.TargetIdentifier)
	if !ok {
		return nil
	}
	attributes := map[string]any{
		"target_arn": target.ARN,
	}
	if controlID := strings.TrimSpace(control.ControlIdentifier); controlID != "" {
		attributes["control_identifier"] = controlID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipControlTowerControlGovernsTarget,
		SourceResourceID: sourceID,
		SourceARN:        sourceID,
		// The organizations scanner keys OU/account/root nodes by their bare id,
		// not their ARN, so target_arn is intentionally left blank: setting it
		// would mark the edge ARN-keyed and break the bare-id join. The original
		// target ARN is preserved in attributes for provenance.
		TargetResourceID: target.ResourceID,
		TargetType:       target.ResourceType,
		Attributes:       attributes,
		SourceRecordID: sourceID + "->" +
			awscloud.RelationshipControlTowerControlGovernsTarget + ":" + target.ResourceID,
	}
}

// baselineGovernsTargetRelationship records that an enabled baseline governs an
// Organizations target (organizational unit, account, or root). The edge is
// keyed by the bare Organizations id the organizations scanner publishes. It
// returns nil when the baseline identity or the target cannot be resolved, so
// the edge is skipped rather than dangled.
func baselineGovernsTargetRelationship(
	boundary awscloud.Boundary,
	baseline EnabledBaseline,
) *awscloud.RelationshipObservation {
	sourceID := strings.TrimSpace(baseline.ARN)
	if sourceID == "" {
		return nil
	}
	target, ok := resolveOrganizationsTarget(baseline.TargetIdentifier)
	if !ok {
		return nil
	}
	attributes := map[string]any{
		"target_arn": target.ARN,
	}
	if baselineID := strings.TrimSpace(baseline.BaselineIdentifier); baselineID != "" {
		attributes["baseline_identifier"] = baselineID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipControlTowerBaselineGovernsTarget,
		SourceResourceID: sourceID,
		SourceARN:        sourceID,
		// The organizations scanner keys OU/account/root nodes by their bare id,
		// not their ARN, so target_arn is intentionally left blank to preserve the
		// bare-id join. The original target ARN is preserved in attributes.
		TargetResourceID: target.ResourceID,
		TargetType:       target.ResourceType,
		Attributes:       attributes,
		SourceRecordID: sourceID + "->" +
			awscloud.RelationshipControlTowerBaselineGovernsTarget + ":" + target.ResourceID,
	}
}

// baselineForLandingZoneRelationship records that an enabled baseline belongs to
// the boundary's landing zone. Control Tower governs at most one landing zone
// per management account, so the baseline is keyed to that single landing-zone
// ARN, which is the resource_id the landing-zone node publishes. It returns nil
// when no landing zone is present or either identity is missing, so the edge is
// skipped rather than dangled.
func baselineForLandingZoneRelationship(
	boundary awscloud.Boundary,
	baseline EnabledBaseline,
	landingZoneARN string,
) *awscloud.RelationshipObservation {
	sourceID := strings.TrimSpace(baseline.ARN)
	landingZoneARN = strings.TrimSpace(landingZoneARN)
	if sourceID == "" || landingZoneARN == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipControlTowerBaselineForLandingZone,
		SourceResourceID: sourceID,
		SourceARN:        sourceID,
		TargetResourceID: landingZoneARN,
		TargetARN:        landingZoneARN,
		TargetType:       awscloud.ResourceTypeControlTowerLandingZone,
		SourceRecordID: sourceID + "->" +
			awscloud.RelationshipControlTowerBaselineForLandingZone + ":" + landingZoneARN,
	}
}
