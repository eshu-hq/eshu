// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package organizations

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// Scanner emits AWS Organizations metadata facts for one management or
// delegated-administrator account. It never calls mutation APIs or persists
// policy document bodies.
type Scanner struct {
	Client       Client
	RedactionKey redact.Key
}

// Scan observes Organizations roots, OUs, accounts, policy summaries, policy
// targets, and delegated administrators through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("organizations scanner client is required")
	}
	if s.RedactionKey.IsZero() {
		return nil, fmt.Errorf("organizations scanner redaction key is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceOrganizations:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceOrganizations
	default:
		return nil, fmt.Errorf("organizations scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot AWS Organizations metadata: %w", err)
	}
	envelopes := make([]facts.Envelope, 0, len(snapshot.Roots)+len(snapshot.OrganizationalUnits)+
		len(snapshot.Accounts)+len(snapshot.Policies)+len(snapshot.DelegatedAdministrators)+len(snapshot.Warnings))
	if err := appendWarnings(&envelopes, boundary, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, root := range snapshot.Roots {
		resource, err := awscloud.NewResourceEnvelope(rootObservation(boundary, snapshot.Organization, root))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}
	for _, ou := range snapshot.OrganizationalUnits {
		resource, err := awscloud.NewResourceEnvelope(ouObservation(boundary, ou))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		if rel, ok := ouParentRelationship(boundary, ou); ok {
			envelope, err := awscloud.NewRelationshipEnvelope(rel)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}
	for _, account := range snapshot.Accounts {
		resource, err := awscloud.NewResourceEnvelope(s.accountObservation(boundary, account))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		if rel, ok := accountParentRelationship(boundary, account); ok {
			envelope, err := awscloud.NewRelationshipEnvelope(rel)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}
	for _, policy := range snapshot.Policies {
		resource, err := awscloud.NewResourceEnvelope(policyObservation(boundary, policy))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		for _, target := range policy.Targets {
			relationship, ok := policyTargetRelationship(boundary, policy, target)
			if !ok {
				continue
			}
			envelope, err := awscloud.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}
	for _, admin := range snapshot.DelegatedAdministrators {
		resource, err := awscloud.NewResourceEnvelope(s.delegatedAdminObservation(boundary, admin))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		relationship, ok := delegatedAdminRelationship(boundary, admin)
		if !ok {
			continue
		}
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func appendWarnings(
	envelopes *[]facts.Envelope,
	boundary awscloud.Boundary,
	warnings []awscloud.WarningObservation,
) error {
	for _, warning := range warnings {
		warning.Boundary = boundary
		envelope, err := awscloud.NewWarningEnvelope(warning)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func rootObservation(
	boundary awscloud.Boundary,
	organization Organization,
	root Root,
) awscloud.ResourceObservation {
	rootID := strings.TrimSpace(root.ID)
	attrs := map[string]any{
		"organization_arn":        strings.TrimSpace(organization.ARN),
		"organization_id":         strings.TrimSpace(organization.ID),
		"management_account_id":   strings.TrimSpace(organization.ManagementAccount),
		"organization_features":   strings.TrimSpace(organization.FeatureSet),
		"enabled_policy_families": policyTypeMaps(root.PolicyTypes),
	}
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ARN:                strings.TrimSpace(root.ARN),
		ResourceID:         firstNonEmpty(rootID, root.ARN),
		ResourceType:       awscloud.ResourceTypeOrganizationsRoot,
		Name:               strings.TrimSpace(root.Name),
		Tags:               cloneStringMap(root.Tags),
		Attributes:         attrs,
		CorrelationAnchors: []string{rootID, root.ARN, organization.ID},
		SourceRecordID:     firstNonEmpty(rootID, root.ARN),
	}
}

func ouObservation(boundary awscloud.Boundary, ou OrganizationalUnit) awscloud.ResourceObservation {
	ouID := strings.TrimSpace(ou.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          strings.TrimSpace(ou.ARN),
		ResourceID:   firstNonEmpty(ouID, ou.ARN),
		ResourceType: awscloud.ResourceTypeOrganizationsOrganizationalUnit,
		Name:         strings.TrimSpace(ou.Name),
		Tags:         cloneStringMap(ou.Tags),
		Attributes: map[string]any{
			"parent_id": strings.TrimSpace(ou.ParentID),
		},
		CorrelationAnchors: []string{ouID, ou.ARN, ou.Name},
		SourceRecordID:     firstNonEmpty(ouID, ou.ARN),
	}
}

func (s Scanner) accountObservation(boundary awscloud.Boundary, account Account) awscloud.ResourceObservation {
	accountID := strings.TrimSpace(account.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          strings.TrimSpace(account.ARN),
		ResourceID:   firstNonEmpty(accountID, account.ARN),
		ResourceType: awscloud.ResourceTypeOrganizationsAccount,
		State:        firstNonEmpty(account.State, account.Status),
		Tags:         cloneStringMap(account.Tags),
		Attributes: map[string]any{
			"account_id":       accountID,
			"email":            awscloud.RedactString(account.Email, "aws_organizations_account.email", s.RedactionKey),
			"joined_method":    strings.TrimSpace(account.JoinedVia),
			"joined_timestamp": timeOrNil(account.JoinedAt),
			"name":             awscloud.RedactString(account.Name, "aws_organizations_account.name", s.RedactionKey),
			"parent_id":        strings.TrimSpace(account.ParentID),
		},
		CorrelationAnchors: []string{accountID, account.ARN},
		SourceRecordID:     firstNonEmpty(accountID, account.ARN),
	}
}

func policyObservation(boundary awscloud.Boundary, policy Policy) awscloud.ResourceObservation {
	policyID := strings.TrimSpace(policy.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          strings.TrimSpace(policy.ARN),
		ResourceID:   firstNonEmpty(policyID, policy.ARN),
		ResourceType: awscloud.ResourceTypeOrganizationsPolicy,
		Name:         strings.TrimSpace(policy.Name),
		Tags:         cloneStringMap(policy.Tags),
		Attributes: map[string]any{
			"aws_managed":  policy.AWSManaged,
			"description":  strings.TrimSpace(policy.Description),
			"policy_id":    policyID,
			"policy_type":  strings.TrimSpace(policy.Type),
			"target_count": len(policy.Targets),
		},
		CorrelationAnchors: []string{policyID, policy.ARN, policy.Name},
		SourceRecordID:     firstNonEmpty(policyID, policy.ARN),
	}
}

func (s Scanner) delegatedAdminObservation(
	boundary awscloud.Boundary,
	admin DelegatedAdministrator,
) awscloud.ResourceObservation {
	adminID := delegatedAdminID(admin)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   adminID,
		ResourceType: awscloud.ResourceTypeOrganizationsDelegatedAdministrator,
		Name:         strings.TrimSpace(admin.ServicePrincipal),
		Attributes: map[string]any{
			"account_arn":           strings.TrimSpace(admin.AccountARN),
			"account_email":         awscloud.RedactString(admin.AccountEmail, "aws_organizations_delegated_administrator.email", s.RedactionKey),
			"account_id":            strings.TrimSpace(admin.AccountID),
			"account_name":          awscloud.RedactString(admin.AccountName, "aws_organizations_delegated_administrator.name", s.RedactionKey),
			"delegation_enabled_at": timeOrNil(admin.DelegationEnabledAt),
			"joined_method":         strings.TrimSpace(admin.JoinedVia),
			"joined_timestamp":      timeOrNil(admin.JoinedAt),
			"service_principal":     strings.TrimSpace(admin.ServicePrincipal),
			"state":                 strings.TrimSpace(admin.State),
			"status":                strings.TrimSpace(admin.Status),
		},
		CorrelationAnchors: []string{admin.AccountID, admin.AccountARN, admin.ServicePrincipal},
		SourceRecordID:     adminID,
	}
}

func accountParentRelationship(
	boundary awscloud.Boundary,
	account Account,
) (awscloud.RelationshipObservation, bool) {
	accountID := strings.TrimSpace(account.ID)
	parentID := strings.TrimSpace(account.ParentID)
	if accountID == "" || parentID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	relationshipType := awscloud.RelationshipOrganizationsAccountInOU
	targetType := awscloud.ResourceTypeOrganizationsOrganizationalUnit
	if strings.HasPrefix(parentID, "r-") {
		relationshipType = awscloud.RelationshipOrganizationsAccountInRoot
		targetType = awscloud.ResourceTypeOrganizationsRoot
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: relationshipType,
		SourceResourceID: accountID,
		SourceARN:        strings.TrimSpace(account.ARN),
		TargetResourceID: parentID,
		TargetType:       targetType,
		Attributes: map[string]any{
			"parent_id": parentID,
		},
		SourceRecordID: accountID + "#parent#" + parentID,
	}, true
}

func ouParentRelationship(
	boundary awscloud.Boundary,
	ou OrganizationalUnit,
) (awscloud.RelationshipObservation, bool) {
	ouID := strings.TrimSpace(ou.ID)
	parentID := strings.TrimSpace(ou.ParentID)
	if ouID == "" || !strings.HasPrefix(parentID, "ou-") {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipOrganizationsOUInOU,
		SourceResourceID: ouID,
		SourceARN:        strings.TrimSpace(ou.ARN),
		TargetResourceID: parentID,
		TargetType:       awscloud.ResourceTypeOrganizationsOrganizationalUnit,
		Attributes: map[string]any{
			"parent_id": parentID,
		},
		SourceRecordID: ouID + "#parent#" + parentID,
	}, true
}

func policyTargetRelationship(
	boundary awscloud.Boundary,
	policy Policy,
	target PolicyTarget,
) (awscloud.RelationshipObservation, bool) {
	policyID := strings.TrimSpace(policy.ID)
	targetID := strings.TrimSpace(target.ID)
	if policyID == "" || targetID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipOrganizationsPolicyTargetsResource,
		SourceResourceID: policyID,
		SourceARN:        strings.TrimSpace(policy.ARN),
		TargetResourceID: targetID,
		TargetARN:        strings.TrimSpace(target.ARN),
		TargetType:       targetResourceType(target.Type),
		Attributes: map[string]any{
			"policy_type": strings.TrimSpace(policy.Type),
			"target_type": strings.TrimSpace(target.Type),
		},
		SourceRecordID: policyID + "#target#" + targetID,
	}, true
}

func delegatedAdminRelationship(
	boundary awscloud.Boundary,
	admin DelegatedAdministrator,
) (awscloud.RelationshipObservation, bool) {
	adminID := delegatedAdminID(admin)
	accountID := strings.TrimSpace(admin.AccountID)
	if adminID == "" || accountID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipOrganizationsDelegatedAdminForAccount,
		SourceResourceID: adminID,
		TargetResourceID: accountID,
		TargetARN:        strings.TrimSpace(admin.AccountARN),
		TargetType:       awscloud.ResourceTypeOrganizationsAccount,
		Attributes: map[string]any{
			"service_principal": strings.TrimSpace(admin.ServicePrincipal),
		},
		SourceRecordID: adminID + "#account#" + accountID,
	}, true
}

func delegatedAdminID(admin DelegatedAdministrator) string {
	accountID := strings.TrimSpace(admin.AccountID)
	servicePrincipal := strings.TrimSpace(admin.ServicePrincipal)
	if accountID == "" {
		return ""
	}
	if servicePrincipal == "" {
		return accountID + "#delegated-admin"
	}
	return accountID + "#delegated-admin#" + servicePrincipal
}

func targetResourceType(targetType string) string {
	switch strings.TrimSpace(targetType) {
	case "ACCOUNT":
		return awscloud.ResourceTypeOrganizationsAccount
	case "ORGANIZATIONAL_UNIT":
		return awscloud.ResourceTypeOrganizationsOrganizationalUnit
	case "ROOT":
		return awscloud.ResourceTypeOrganizationsRoot
	default:
		return "aws_resource"
	}
}

func policyTypeMaps(policyTypes []PolicyTypeSummary) []map[string]string {
	if len(policyTypes) == 0 {
		return nil
	}
	output := make([]map[string]string, 0, len(policyTypes))
	for _, policyType := range policyTypes {
		policyTypeName := strings.TrimSpace(policyType.Type)
		if policyTypeName == "" {
			continue
		}
		output = append(output, map[string]string{
			"status": strings.TrimSpace(policyType.Status),
			"type":   policyTypeName,
		})
	}
	return output
}

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
