// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ram

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// shareJoinID resolves the join key used for a resource share across its
// resource fact and every relationship that sources from it. RAM normally
// reports a resource share ARN, but a defensively blank ARN falls back to the
// share name so a malformed record does not fail the whole scan and so the
// share's resource fact and edges still join on the same key. It returns the
// empty string only when both the ARN and the name are blank.
func shareJoinID(share ResourceShare) string {
	if arn := strings.TrimSpace(share.ARN); arn != "" {
		return arn
	}
	return strings.TrimSpace(share.Name)
}

// shareResourceRelationship records that a resource share shares one resource.
// The target is the shared resource's ARN, and the target type is the
// RAM-reported resource type (service-code:resource-code, for example
// ec2:subnet). When RAM reports a blank type, the edge falls back to the
// generic resource target type so it keeps a non-empty target type and does not
// violate the relationship invariant; the RAM-reported type is preserved in the
// attributes either way. The source join key is the share ARN, or the share
// name when the ARN is blank. It returns false for an empty share join key or
// resource ARN so a blank record does not emit a dangling edge.
func shareResourceRelationship(
	boundary awscloud.Boundary,
	share ResourceShare,
	resource SharedResource,
) (awscloud.RelationshipObservation, bool) {
	shareID := shareJoinID(share)
	resourceARN := strings.TrimSpace(resource.ARN)
	if shareID == "" || resourceARN == "" {
		return awscloud.RelationshipObservation{}, false
	}
	resourceType := strings.TrimSpace(resource.Type)
	targetType := resourceType
	if targetType == "" {
		targetType = awscloud.ResourceTypeGeneric
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipRAMShareIncludesResource,
		SourceResourceID: shareID,
		SourceARN:        strings.TrimSpace(share.ARN),
		TargetResourceID: resourceARN,
		TargetARN:        resourceARN,
		TargetType:       targetType,
		Attributes: map[string]any{
			"resource_type": resourceType,
			"status":        strings.TrimSpace(resource.Status),
			"region_scope":  strings.TrimSpace(resource.RegionScope),
		},
		SourceRecordID: shareID + "#resource#" + resourceARN,
	}, true
}

// sharePrincipalRelationship records that a resource share targets one
// principal. The principal id is an AWS account id, an Organizations OU ARN, or
// an Organizations organization or root ARN. The relationship type and target
// type are derived from the principal id form so each edge joins to the
// organizations scanner's resource id convention:
//
//   - a 12-digit account id targets aws_organizations_account by bare account
//     id (the organizations scanner emits the bare account id as its account
//     resource id);
//   - an OU ARN (path segment :ou/) targets
//     aws_organizations_organizational_unit by ARN;
//   - an organization or root ARN (path segment :organization/ or :root/)
//     targets aws_organizations_root by ARN.
//
// An unrecognized principal form targets the generic resource type with a
// distinct relationship type rather than masquerading as an account. The source
// join key is the share ARN, or the share name when the ARN is blank. It
// returns false for an empty share join key or principal id.
func sharePrincipalRelationship(
	boundary awscloud.Boundary,
	share ResourceShare,
	principal Principal,
) (awscloud.RelationshipObservation, bool) {
	shareID := shareJoinID(share)
	principalID := strings.TrimSpace(principal.ID)
	if shareID == "" || principalID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	relationshipType, targetType, targetARN := classifyPrincipal(principalID)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: relationshipType,
		SourceResourceID: shareID,
		SourceARN:        strings.TrimSpace(share.ARN),
		TargetResourceID: principalID,
		TargetARN:        targetARN,
		TargetType:       targetType,
		Attributes: map[string]any{
			"principal_id": principalID,
			"external":     principal.External,
		},
		SourceRecordID: shareID + "#principal#" + principalID,
	}, true
}

// sharePermissionRelationship records that a resource share uses one managed
// permission. The target is the permission ARN. The source join key is the
// share ARN, or the share name when the ARN is blank. It returns false for an
// empty share join key or permission ARN.
func sharePermissionRelationship(
	boundary awscloud.Boundary,
	share ResourceShare,
	permission Permission,
) (awscloud.RelationshipObservation, bool) {
	shareID := shareJoinID(share)
	permissionARN := strings.TrimSpace(permission.ARN)
	if shareID == "" || permissionARN == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipRAMShareUsesPermission,
		SourceResourceID: shareID,
		SourceARN:        strings.TrimSpace(share.ARN),
		TargetResourceID: permissionARN,
		TargetARN:        permissionARN,
		TargetType:       awscloud.ResourceTypeRAMPermission,
		Attributes: map[string]any{
			"permission_name":    strings.TrimSpace(permission.Name),
			"permission_version": strings.TrimSpace(permission.Version),
			"permission_type":    strings.TrimSpace(permission.PermissionType),
		},
		SourceRecordID: shareID + "#permission#" + permissionARN,
	}, true
}

// classifyPrincipal maps a RAM principal id to its relationship type, target
// type, and (for ARN principals) target ARN. A bare account id produces no
// target ARN because the organizations scanner keys accounts by bare id, not
// ARN. ARN classification keys on the resource-path segment rather than the
// partition prefix so non-default partitions (GovCloud, China) classify the
// same way.
func classifyPrincipal(principalID string) (relationshipType, targetType, targetARN string) {
	switch {
	case isAccountID(principalID):
		return awscloud.RelationshipRAMShareTargetsAccount,
			awscloud.ResourceTypeOrganizationsAccount,
			""
	case strings.Contains(principalID, ":ou/"):
		return awscloud.RelationshipRAMShareTargetsOrganizationalUnit,
			awscloud.ResourceTypeOrganizationsOrganizationalUnit,
			principalID
	case strings.Contains(principalID, ":organization/"), strings.Contains(principalID, ":root/"):
		return awscloud.RelationshipRAMShareTargetsOrganization,
			awscloud.ResourceTypeOrganizationsRoot,
			principalID
	default:
		// An unrecognized principal id (for example a service principal or a
		// future RAM principal form) records a distinct targeted-principal edge
		// with the generic target type rather than masquerading as an
		// Organizations account. This keeps evidence without inventing a wrong
		// account join key; the raw principal id remains the join key and no
		// derived target ARN is claimed.
		return awscloud.RelationshipRAMShareTargetsPrincipal,
			awscloud.ResourceTypeGeneric,
			""
	}
}

// isAccountID reports whether id is a bare 12-digit AWS account id.
func isAccountID(id string) bool {
	if len(id) != 12 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}
