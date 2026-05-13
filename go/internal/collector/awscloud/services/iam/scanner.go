package iam

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Client is the IAM read surface consumed by Scanner. Runtime adapters should
// translate AWS SDK responses into these scanner-owned types.
type Client interface {
	ListRoles(context.Context) ([]Role, error)
	ListPolicies(context.Context) ([]Policy, error)
	ListInstanceProfiles(context.Context) ([]InstanceProfile, error)
}

// Scanner emits AWS IAM facts for one claimed account/global-region scan.
type Scanner struct {
	Client Client
}

// Scan observes IAM roles, policies, instance profiles, and trust
// relationships through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("iam scanner client is required")
	}
	boundary.ServiceKind = awscloud.ServiceIAM
	roles, err := s.Client.ListRoles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list IAM roles: %w", err)
	}
	policies, err := s.Client.ListPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list IAM policies: %w", err)
	}
	profiles, err := s.Client.ListInstanceProfiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list IAM instance profiles: %w", err)
	}

	envelopes := make([]facts.Envelope, 0, len(roles)+len(policies)+len(profiles))
	for _, role := range roles {
		resource, err := awscloud.NewResourceEnvelope(roleObservation(boundary, role))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		for _, principal := range role.TrustPrincipals {
			relationship, err := awscloud.NewRelationshipEnvelope(roleTrustRelationship(boundary, role, principal))
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, relationship)
		}
		for _, policyARN := range role.AttachedPolicyARNs {
			relationship, err := awscloud.NewRelationshipEnvelope(rolePolicyRelationship(boundary, role, policyARN))
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, relationship)
		}
	}
	for _, policy := range policies {
		resource, err := awscloud.NewResourceEnvelope(policyObservation(boundary, policy))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}
	for _, profile := range profiles {
		resource, err := awscloud.NewResourceEnvelope(instanceProfileObservation(boundary, profile))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		for _, roleARN := range profile.RoleARNs {
			relationship, err := awscloud.NewRelationshipEnvelope(profileRoleRelationship(boundary, profile, roleARN))
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, relationship)
		}
	}
	return envelopes, nil
}

func roleObservation(boundary awscloud.Boundary, role Role) awscloud.ResourceObservation {
	roleARN := strings.TrimSpace(role.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          roleARN,
		ResourceID:   roleARN,
		ResourceType: awscloud.ResourceTypeIAMRole,
		Name:         role.Name,
		Attributes: map[string]any{
			"attached_policy_arns": role.AttachedPolicyARNs,
			"inline_policy_names":  role.InlinePolicyNames,
			"path":                 strings.TrimSpace(role.Path),
			"trust_policy":         role.AssumeRolePolicy,
			"trust_principals":     trustPrincipalMaps(role.TrustPrincipals),
		},
		CorrelationAnchors: []string{roleARN, strings.TrimSpace(role.Name)},
		SourceRecordID:     roleARN,
	}
}

func policyObservation(boundary awscloud.Boundary, policy Policy) awscloud.ResourceObservation {
	policyARN := strings.TrimSpace(policy.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          policyARN,
		ResourceID:   policyARN,
		ResourceType: awscloud.ResourceTypeIAMPolicy,
		Name:         policy.Name,
		Attributes: map[string]any{
			"attachment_count":   policy.AttachmentCount,
			"default_version_id": policy.DefaultVersionID,
			"path":               strings.TrimSpace(policy.Path),
		},
		CorrelationAnchors: []string{policyARN, strings.TrimSpace(policy.Name)},
		SourceRecordID:     policyARN,
	}
}

func instanceProfileObservation(boundary awscloud.Boundary, profile InstanceProfile) awscloud.ResourceObservation {
	profileARN := strings.TrimSpace(profile.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          profileARN,
		ResourceID:   profileARN,
		ResourceType: awscloud.ResourceTypeIAMInstanceProfile,
		Name:         profile.Name,
		Attributes: map[string]any{
			"path":      strings.TrimSpace(profile.Path),
			"role_arns": profile.RoleARNs,
		},
		CorrelationAnchors: []string{profileARN, strings.TrimSpace(profile.Name)},
		SourceRecordID:     profileARN,
	}
}

func roleTrustRelationship(boundary awscloud.Boundary, role Role, principal TrustPrincipal) awscloud.RelationshipObservation {
	principalID := strings.TrimSpace(principal.Type) + ":" + strings.TrimSpace(principal.Identifier)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipIAMRoleTrustsPrincipal,
		SourceResourceID: strings.TrimSpace(role.ARN),
		SourceARN:        strings.TrimSpace(role.ARN),
		TargetResourceID: principalID,
		TargetType:       awscloud.ResourceTypeIAMPrincipal,
		Attributes: map[string]any{
			"principal_type": strings.TrimSpace(principal.Type),
		},
		SourceRecordID: strings.TrimSpace(role.ARN) + "#trust#" + principalID,
	}
}

func rolePolicyRelationship(boundary awscloud.Boundary, role Role, policyARN string) awscloud.RelationshipObservation {
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipIAMRoleAttachedPolicy,
		SourceResourceID: strings.TrimSpace(role.ARN),
		SourceARN:        strings.TrimSpace(role.ARN),
		TargetResourceID: strings.TrimSpace(policyARN),
		TargetARN:        strings.TrimSpace(policyARN),
		TargetType:       awscloud.ResourceTypeIAMPolicy,
		SourceRecordID:   strings.TrimSpace(role.ARN) + "#policy#" + strings.TrimSpace(policyARN),
	}
}

func profileRoleRelationship(boundary awscloud.Boundary, profile InstanceProfile, roleARN string) awscloud.RelationshipObservation {
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipIAMRoleInInstanceProfile,
		SourceResourceID: strings.TrimSpace(roleARN),
		SourceARN:        strings.TrimSpace(roleARN),
		TargetResourceID: strings.TrimSpace(profile.ARN),
		TargetARN:        strings.TrimSpace(profile.ARN),
		TargetType:       awscloud.ResourceTypeIAMInstanceProfile,
		SourceRecordID:   strings.TrimSpace(roleARN) + "#instance-profile#" + strings.TrimSpace(profile.ARN),
	}
}

func trustPrincipalMaps(principals []TrustPrincipal) []map[string]string {
	if len(principals) == 0 {
		return nil
	}
	output := make([]map[string]string, 0, len(principals))
	for _, principal := range principals {
		output = append(output, map[string]string{
			"type":       strings.TrimSpace(principal.Type),
			"identifier": strings.TrimSpace(principal.Identifier),
		})
	}
	return output
}
