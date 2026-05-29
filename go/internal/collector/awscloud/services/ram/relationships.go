package ram

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// shareResourceRelationship records that a resource share shares one resource.
// The target is the shared resource's ARN, and the target type is the
// RAM-reported resource type (service-code:resource-code, for example
// ec2:subnet). It returns false for an empty share ARN or resource ARN so a
// blank record does not emit a dangling edge.
func shareResourceRelationship(
	boundary awscloud.Boundary,
	share ResourceShare,
	resource SharedResource,
) (awscloud.RelationshipObservation, bool) {
	shareARN := strings.TrimSpace(share.ARN)
	resourceARN := strings.TrimSpace(resource.ARN)
	if shareARN == "" || resourceARN == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipRAMShareIncludesResource,
		SourceResourceID: shareARN,
		SourceARN:        shareARN,
		TargetResourceID: resourceARN,
		TargetARN:        resourceARN,
		TargetType:       strings.TrimSpace(resource.Type),
		Attributes: map[string]any{
			"resource_type": strings.TrimSpace(resource.Type),
			"status":        strings.TrimSpace(resource.Status),
			"region_scope":  strings.TrimSpace(resource.RegionScope),
		},
		SourceRecordID: shareARN + "#resource#" + resourceARN,
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
// It returns false for an empty share ARN or principal id.
func sharePrincipalRelationship(
	boundary awscloud.Boundary,
	share ResourceShare,
	principal Principal,
) (awscloud.RelationshipObservation, bool) {
	shareARN := strings.TrimSpace(share.ARN)
	principalID := strings.TrimSpace(principal.ID)
	if shareARN == "" || principalID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	relationshipType, targetType, targetARN := classifyPrincipal(principalID)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: relationshipType,
		SourceResourceID: shareARN,
		SourceARN:        shareARN,
		TargetResourceID: principalID,
		TargetARN:        targetARN,
		TargetType:       targetType,
		Attributes: map[string]any{
			"principal_id": principalID,
			"external":     principal.External,
		},
		SourceRecordID: shareARN + "#principal#" + principalID,
	}, true
}

// sharePermissionRelationship records that a resource share uses one managed
// permission. The target is the permission ARN. It returns false for an empty
// share ARN or permission ARN.
func sharePermissionRelationship(
	boundary awscloud.Boundary,
	share ResourceShare,
	permission Permission,
) (awscloud.RelationshipObservation, bool) {
	shareARN := strings.TrimSpace(share.ARN)
	permissionARN := strings.TrimSpace(permission.ARN)
	if shareARN == "" || permissionARN == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipRAMShareUsesPermission,
		SourceResourceID: shareARN,
		SourceARN:        shareARN,
		TargetResourceID: permissionARN,
		TargetARN:        permissionARN,
		TargetType:       awscloud.ResourceTypeRAMPermission,
		Attributes: map[string]any{
			"permission_name":    strings.TrimSpace(permission.Name),
			"permission_version": strings.TrimSpace(permission.Version),
			"permission_type":    strings.TrimSpace(permission.PermissionType),
		},
		SourceRecordID: shareARN + "#permission#" + permissionARN,
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
		// future RAM principal form) still records a targeted-account edge
		// rather than dropping evidence, but carries no derived target ARN.
		return awscloud.RelationshipRAMShareTargetsAccount,
			awscloud.ResourceTypeOrganizationsAccount,
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
