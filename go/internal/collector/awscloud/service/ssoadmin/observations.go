// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ssoadmin

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func instanceObservation(boundary awscloud.Boundary, instance Instance) awscloud.ResourceObservation {
	instanceARN := strings.TrimSpace(instance.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          instanceARN,
		ResourceID:   firstNonEmpty(instanceARN, instance.IdentityStoreID),
		ResourceType: awscloud.ResourceTypeSSOAdminInstance,
		Name:         strings.TrimSpace(instance.Name),
		State:        strings.TrimSpace(instance.Status),
		Tags:         cloneStringMap(instance.Tags),
		Attributes: map[string]any{
			"identity_store_id":        strings.TrimSpace(instance.IdentityStoreID),
			"owner_account_id":         strings.TrimSpace(instance.OwnerAccountID),
			"created_at":               timeOrNil(instance.CreatedAt),
			"permission_set_count":     len(instance.PermissionSets),
			"account_assignment_count": len(instance.AccountAssignments),
			"trusted_issuer_count":     len(instance.TrustedTokenIssuers),
		},
		CorrelationAnchors: []string{instanceARN, instance.IdentityStoreID},
		SourceRecordID:     firstNonEmpty(instanceARN, instance.IdentityStoreID),
	}
}

func permissionSetObservation(boundary awscloud.Boundary, permSet PermissionSet) awscloud.ResourceObservation {
	permSetARN := strings.TrimSpace(permSet.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          permSetARN,
		ResourceID:   permSetARN,
		ResourceType: awscloud.ResourceTypeSSOAdminPermissionSet,
		Name:         strings.TrimSpace(permSet.Name),
		Tags:         cloneStringMap(permSet.Tags),
		Attributes: map[string]any{
			"instance_arn":                  strings.TrimSpace(permSet.InstanceARN),
			"description":                   strings.TrimSpace(permSet.Description),
			"session_duration":              strings.TrimSpace(permSet.SessionDuration),
			"relay_state":                   strings.TrimSpace(permSet.RelayState),
			"created_at":                    timeOrNil(permSet.CreatedAt),
			"managed_policy_count":          len(permSet.ManagedPolicies),
			"customer_managed_policy_count": len(permSet.CustomerManagedPolicies),
		},
		CorrelationAnchors: []string{permSetARN},
		SourceRecordID:     permSetARN,
	}
}

func assignmentObservation(boundary awscloud.Boundary, assignment AccountAssignment) awscloud.ResourceObservation {
	assignmentID := assignmentID(assignment)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   assignmentID,
		ResourceType: awscloud.ResourceTypeSSOAdminAccountAssignment,
		Attributes: map[string]any{
			"instance_arn":       strings.TrimSpace(assignment.InstanceARN),
			"permission_set_arn": strings.TrimSpace(assignment.PermissionSetARN),
			"target_account_id":  strings.TrimSpace(assignment.AccountID),
			"principal_id":       strings.TrimSpace(assignment.PrincipalID),
			"principal_type":     strings.TrimSpace(assignment.PrincipalType),
		},
		CorrelationAnchors: []string{assignmentID, assignment.PrincipalID, assignment.PermissionSetARN},
		SourceRecordID:     assignmentID,
	}
}

func trustedTokenIssuerObservation(boundary awscloud.Boundary, issuer TrustedTokenIssuer) awscloud.ResourceObservation {
	issuerARN := strings.TrimSpace(issuer.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          issuerARN,
		ResourceID:   issuerARN,
		ResourceType: awscloud.ResourceTypeSSOAdminTrustedTokenIssuer,
		Name:         strings.TrimSpace(issuer.Name),
		Attributes: map[string]any{
			"instance_arn":              strings.TrimSpace(issuer.InstanceARN),
			"trusted_token_issuer_type": strings.TrimSpace(issuer.Type),
		},
		CorrelationAnchors: []string{issuerARN},
		SourceRecordID:     issuerARN,
	}
}

func applicationObservation(boundary awscloud.Boundary, application Application) awscloud.ResourceObservation {
	appARN := strings.TrimSpace(application.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          appARN,
		ResourceID:   appARN,
		ResourceType: awscloud.ResourceTypeSSOAdminApplication,
		Name:         strings.TrimSpace(application.Name),
		State:        strings.TrimSpace(application.Status),
		Attributes: map[string]any{
			"instance_arn":             strings.TrimSpace(application.InstanceARN),
			"description":              strings.TrimSpace(application.Description),
			"application_account_id":   strings.TrimSpace(application.ApplicationAccountID),
			"application_provider_arn": strings.TrimSpace(application.ApplicationProviderARN),
			"identity_store_arn":       strings.TrimSpace(application.IdentityStoreARN),
			"portal_visibility":        strings.TrimSpace(application.PortalVisibility),
			"created_at":               timeOrNil(application.CreatedAt),
		},
		CorrelationAnchors: []string{appARN},
		SourceRecordID:     appARN,
	}
}

func (s Scanner) principalObservation(boundary awscloud.Boundary, principal Principal) awscloud.ResourceObservation {
	principalID := strings.TrimSpace(principal.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   principalID,
		ResourceType: awscloud.ResourceTypeSSOAdminPrincipal,
		Attributes: map[string]any{
			"principal_id":   principalID,
			"principal_type": strings.TrimSpace(principal.Type),
			"display_name":   awscloud.RedactString(principal.DisplayName, "aws_identitycenter_principal.display_name", s.RedactionKey),
		},
		CorrelationAnchors: []string{principalID},
		SourceRecordID:     principalID,
	}
}

func permissionSetInInstanceRelationship(
	boundary awscloud.Boundary,
	instance Instance,
	permSet PermissionSet,
) (awscloud.RelationshipObservation, bool) {
	permSetARN := strings.TrimSpace(permSet.ARN)
	instanceARN := firstNonEmpty(permSet.InstanceARN, instance.ARN)
	if permSetARN == "" || instanceARN == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipSSOAdminPermissionSetInInstance,
		SourceResourceID: permSetARN,
		SourceARN:        permSetARN,
		TargetResourceID: instanceARN,
		TargetARN:        instanceARN,
		TargetType:       awscloud.ResourceTypeSSOAdminInstance,
		SourceRecordID:   permSetARN + "#instance#" + instanceARN,
	}, true
}

func applicationInInstanceRelationship(
	boundary awscloud.Boundary,
	application Application,
) (awscloud.RelationshipObservation, bool) {
	appARN := strings.TrimSpace(application.ARN)
	instanceARN := strings.TrimSpace(application.InstanceARN)
	if appARN == "" || instanceARN == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipSSOAdminApplicationInInstance,
		SourceResourceID: appARN,
		SourceARN:        appARN,
		TargetResourceID: instanceARN,
		TargetARN:        instanceARN,
		TargetType:       awscloud.ResourceTypeSSOAdminInstance,
		SourceRecordID:   appARN + "#instance#" + instanceARN,
	}, true
}

func managedPolicyRelationship(
	boundary awscloud.Boundary,
	permSet PermissionSet,
	managed ManagedPolicyReference,
) (awscloud.RelationshipObservation, bool) {
	permSetARN := strings.TrimSpace(permSet.ARN)
	policyARN := strings.TrimSpace(managed.ARN)
	if permSetARN == "" || policyARN == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipSSOAdminPermissionSetUsesManagedPolicy,
		SourceResourceID: permSetARN,
		SourceARN:        permSetARN,
		TargetResourceID: policyARN,
		TargetARN:        policyARN,
		TargetType:       awscloud.ResourceTypeIAMPolicy,
		Attributes: map[string]any{
			"policy_name": strings.TrimSpace(managed.Name),
		},
		SourceRecordID: permSetARN + "#managed#" + policyARN,
	}, true
}

func customerManagedPolicyRelationship(
	boundary awscloud.Boundary,
	permSet PermissionSet,
	customer CustomerManagedPolicyReference,
) (awscloud.RelationshipObservation, bool) {
	permSetARN := strings.TrimSpace(permSet.ARN)
	policyName := strings.TrimSpace(customer.Name)
	if permSetARN == "" || policyName == "" {
		return awscloud.RelationshipObservation{}, false
	}
	policyPath := firstNonEmpty(customer.Path, "/")
	targetID := permSetARN + "#cmp#" + policyPath + policyName
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipSSOAdminPermissionSetUsesCustomerManagedPolicy,
		SourceResourceID: permSetARN,
		SourceARN:        permSetARN,
		TargetResourceID: targetID,
		TargetType:       awscloud.ResourceTypeIAMPolicy,
		Attributes: map[string]any{
			"policy_name": policyName,
			"policy_path": policyPath,
		},
		SourceRecordID: targetID,
	}, true
}

func assignmentUsesPermissionSetRelationship(
	boundary awscloud.Boundary,
	assignment AccountAssignment,
) (awscloud.RelationshipObservation, bool) {
	id := assignmentID(assignment)
	permSetARN := strings.TrimSpace(assignment.PermissionSetARN)
	if id == "" || permSetARN == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipSSOAdminAssignmentUsesPermissionSet,
		SourceResourceID: id,
		TargetResourceID: permSetARN,
		TargetARN:        permSetARN,
		TargetType:       awscloud.ResourceTypeSSOAdminPermissionSet,
		SourceRecordID:   id + "#permset#" + permSetARN,
	}, true
}

func assignmentTargetsAccountRelationship(
	boundary awscloud.Boundary,
	assignment AccountAssignment,
) (awscloud.RelationshipObservation, bool) {
	id := assignmentID(assignment)
	accountID := strings.TrimSpace(assignment.AccountID)
	if id == "" || accountID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipSSOAdminAssignmentTargetsAccount,
		SourceResourceID: id,
		TargetResourceID: accountID,
		TargetType:       awscloud.ResourceTypeOrganizationsAccount,
		SourceRecordID:   id + "#account#" + accountID,
	}, true
}

func assignmentGrantsPrincipalRelationship(
	boundary awscloud.Boundary,
	assignment AccountAssignment,
) (awscloud.RelationshipObservation, bool) {
	id := assignmentID(assignment)
	principalID := strings.TrimSpace(assignment.PrincipalID)
	if id == "" || principalID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipSSOAdminAssignmentGrantsPrincipal,
		SourceResourceID: id,
		TargetResourceID: principalID,
		TargetType:       awscloud.ResourceTypeSSOAdminPrincipal,
		Attributes: map[string]any{
			"principal_type": strings.TrimSpace(assignment.PrincipalType),
		},
		SourceRecordID: id + "#principal#" + principalID,
	}, true
}

// assignmentID derives a stable identity for one account assignment from the
// permission set ARN, target account, and principal. Identity Center does not
// expose an assignment ID, so the tuple is the durable identity.
func assignmentID(assignment AccountAssignment) string {
	permSetARN := strings.TrimSpace(assignment.PermissionSetARN)
	accountID := strings.TrimSpace(assignment.AccountID)
	principalID := strings.TrimSpace(assignment.PrincipalID)
	if permSetARN == "" || accountID == "" || principalID == "" {
		return ""
	}
	return permSetARN + "#" + accountID + "#" + principalID
}

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}
