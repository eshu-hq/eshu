// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shield

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// protectionRelationship builds the protection-to-protected-resource edge for
// one protection. It returns nil when the protection has no own identity, no
// protected resource ARN, or a protected ARN whose service has no canonical
// Eshu resource family, so a dangling or untyped edge is never emitted.
//
// The protected ARN comes from the Shield API already partition-correct. For an
// ARN-keyed target the edge carries both target_arn and an ARN-shaped
// target_resource_id; for a bare-id target (Elastic IP, hosted zone) the edge
// carries only the bare target_resource_id and leaves target_arn unset so the
// relguard join-mode check stays satisfied.
func protectionRelationship(
	boundary awscloud.Boundary,
	protection Protection,
) *awscloud.RelationshipObservation {
	sourceID := firstNonEmpty(protection.ARN, protection.ID, protection.Name)
	if sourceID == "" {
		return nil
	}
	target, ok := classifyProtectedARN(protection.ResourceARN)
	if !ok {
		return nil
	}
	protectedARN := strings.TrimSpace(protection.ResourceARN)
	observation := &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipShieldProtectionProtectsResource,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(protection.ARN),
		TargetResourceID: target.TargetResourceID,
		TargetType:       target.TargetType,
		Attributes: map[string]any{
			"protected_resource_arn": protectedARN,
		},
		SourceRecordID: sourceID + "->" +
			awscloud.RelationshipShieldProtectionProtectsResource + ":" +
			target.TargetResourceID,
	}
	if target.ARNKeyed {
		observation.TargetARN = protectedARN
	}
	return observation
}
