// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iam

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Client is the IAM read surface consumed by Scanner. Runtime adapters should
// translate AWS SDK responses into these scanner-owned types.
type Client interface {
	ListRoles(context.Context) ([]Role, error)
	ListUsers(context.Context) ([]User, error)
	ListPolicies(context.Context) ([]Policy, error)
	ListInstanceProfiles(context.Context) ([]InstanceProfile, error)
	ListOIDCProviders(context.Context) ([]OIDCProvider, error)
	ListCoverageWarnings(context.Context) ([]CoverageWarning, error)
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
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceIAM:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceIAM
	default:
		return nil, fmt.Errorf("iam scanner received service_kind %q", boundary.ServiceKind)
	}
	roles, err := s.Client.ListRoles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list IAM roles: %w", err)
	}
	users, err := s.Client.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list IAM users: %w", err)
	}
	policies, err := s.Client.ListPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list IAM policies: %w", err)
	}
	profiles, err := s.Client.ListInstanceProfiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list IAM instance profiles: %w", err)
	}
	oidcProviders, err := s.Client.ListOIDCProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list IAM OIDC providers: %w", err)
	}
	warnings, err := s.Client.ListCoverageWarnings(ctx)
	if err != nil {
		return nil, fmt.Errorf("list IAM coverage warnings: %w", err)
	}

	envelopes := make([]facts.Envelope, 0, len(roles)+len(users)+len(policies)+len(profiles)+len(oidcProviders)+len(warnings))
	secretsCtx := secretsIAMContext(boundary)
	for _, role := range roles {
		resource, err := awscloud.NewResourceEnvelope(roleObservation(boundary, role))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		principal, err := secretsiam.NewPrincipalEnvelope(rolePrincipalObservation(secretsCtx, role))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, principal)
		if strings.TrimSpace(role.PermissionBoundary.PolicyARN) != "" {
			boundaryFact, err := secretsiam.NewPermissionBoundaryEnvelope(permissionBoundaryObservation(
				secretsCtx,
				role.ARN,
				secretsiam.PrincipalTypeAWSRole,
				role.PermissionBoundary,
			))
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, boundaryFact)
		}
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
			attachment, err := secretsiam.NewPolicyAttachmentEnvelope(policyAttachmentObservation(
				secretsCtx,
				role.ARN,
				secretsiam.PrincipalTypeAWSRole,
				policyARN,
			))
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, attachment)
		}
		permissions, err := permissionEnvelopes(boundary, role.ARN, awscloud.ResourceTypeIAMRole, role.PermissionStatements)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, permissions...)
		secretsPolicies, err := secretsIAMPolicyEnvelopes(secretsCtx, role.ARN, secretsiam.PrincipalTypeAWSRole, role.PermissionStatements)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, secretsPolicies...)
	}
	for _, user := range users {
		resource, err := awscloud.NewResourceEnvelope(userObservation(boundary, user))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		principal, err := secretsiam.NewPrincipalEnvelope(userPrincipalObservation(secretsCtx, user))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, principal)
		if strings.TrimSpace(user.PermissionBoundary.PolicyARN) != "" {
			boundaryFact, err := secretsiam.NewPermissionBoundaryEnvelope(permissionBoundaryObservation(
				secretsCtx,
				user.ARN,
				secretsiam.PrincipalTypeAWSUser,
				user.PermissionBoundary,
			))
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, boundaryFact)
		}
		for _, policyARN := range user.AttachedPolicyARNs {
			attachment, err := secretsiam.NewPolicyAttachmentEnvelope(policyAttachmentObservation(
				secretsCtx,
				user.ARN,
				secretsiam.PrincipalTypeAWSUser,
				policyARN,
			))
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, attachment)
		}
		permissions, err := permissionEnvelopes(boundary, user.ARN, awscloud.ResourceTypeIAMUser, user.PermissionStatements)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, permissions...)
		secretsPolicies, err := secretsIAMPolicyEnvelopes(secretsCtx, user.ARN, secretsiam.PrincipalTypeAWSUser, user.PermissionStatements)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, secretsPolicies...)
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
		profileFact, err := secretsiam.NewInstanceProfileEnvelope(instanceProfileSecretsObservation(secretsCtx, profile))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, profileFact)
	}
	for _, provider := range oidcProviders {
		principal, err := secretsiam.NewPrincipalEnvelope(oidcProviderPrincipalObservation(secretsCtx, provider))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, principal)
	}
	for _, warning := range warnings {
		envelope, err := secretsiam.NewCoverageWarningEnvelope(coverageWarningObservation(secretsCtx, warning))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
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
			"attached_policy_arns":     role.AttachedPolicyARNs,
			"inline_policy_names":      role.InlinePolicyNames,
			"path":                     strings.TrimSpace(role.Path),
			"permission_boundary_arn":  strings.TrimSpace(role.PermissionBoundary.PolicyARN),
			"permission_boundary_type": strings.TrimSpace(role.PermissionBoundary.Type),
			"trust_policy_present":     len(role.AssumeRolePolicy) > 0,
			"trust_principal_count":    len(role.TrustPrincipals),
			"trust_principals":         trustPrincipalMaps(role.TrustPrincipals),
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

func userObservation(boundary awscloud.Boundary, user User) awscloud.ResourceObservation {
	userARN := strings.TrimSpace(user.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          userARN,
		ResourceID:   userARN,
		ResourceType: awscloud.ResourceTypeIAMUser,
		Name:         user.Name,
		Attributes: map[string]any{
			"attached_policy_arns":     user.AttachedPolicyARNs,
			"inline_policy_names":      user.InlinePolicyNames,
			"path":                     strings.TrimSpace(user.Path),
			"permission_boundary_arn":  strings.TrimSpace(user.PermissionBoundary.PolicyARN),
			"permission_boundary_type": strings.TrimSpace(user.PermissionBoundary.Type),
		},
		CorrelationAnchors: []string{userARN, strings.TrimSpace(user.Name)},
		SourceRecordID:     userARN,
	}
}

// permissionEnvelopes maps the normalized policy statements for one principal
// into derived aws_iam_permission facts. It bounds nothing itself: the SDK
// adapter is responsible for paging and bounding the per-principal policy
// fan-out before handing the normalized statements to the scanner.
func permissionEnvelopes(boundary awscloud.Boundary, principalARN, principalType string, statements []PolicyStatement) ([]facts.Envelope, error) {
	if len(statements) == 0 {
		return nil, nil
	}
	envelopes := make([]facts.Envelope, 0, len(statements))
	for _, statement := range statements {
		envelope, err := awscloud.NewIAMPermissionEnvelope(awscloud.IAMPermissionObservation{
			Boundary:           boundary,
			PrincipalARN:       strings.TrimSpace(principalARN),
			PrincipalType:      principalType,
			PolicySource:       strings.TrimSpace(statement.Source),
			PolicyARN:          strings.TrimSpace(statement.PolicyARN),
			PolicyName:         strings.TrimSpace(statement.PolicyName),
			StatementSID:       strings.TrimSpace(statement.StatementSID),
			Effect:             statement.Effect,
			Actions:            statement.Actions,
			NotActions:         statement.NotActions,
			Resources:          statement.Resources,
			NotResources:       statement.NotResources,
			ConditionKeys:      statement.ConditionKeys,
			ConditionOperators: statement.ConditionOperators,
			AssumePrincipals:   statement.AssumePrincipals,
		})
		if err != nil {
			return nil, fmt.Errorf("build IAM permission fact for principal %q: %w", principalARN, err)
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
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
