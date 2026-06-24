// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iam

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func secretsIAMContext(boundary awscloud.Boundary) secretsiam.EnvelopeContext {
	return secretsiam.EnvelopeContext{
		AccountID:           boundary.AccountID,
		Region:              boundary.Region,
		ScopeID:             boundary.ScopeID,
		GenerationID:        boundary.GenerationID,
		CollectorInstanceID: boundary.CollectorInstanceID,
		FencingToken:        boundary.FencingToken,
		ObservedAt:          boundary.ObservedAt,
	}
}

func rolePrincipalObservation(ctx secretsiam.EnvelopeContext, role Role) secretsiam.PrincipalObservation {
	roleARN := strings.TrimSpace(role.ARN)
	return secretsiam.PrincipalObservation{
		Context:          ctx,
		PrincipalARN:     roleARN,
		PrincipalType:    secretsiam.PrincipalTypeAWSRole,
		Name:             strings.TrimSpace(role.Name),
		Path:             strings.TrimSpace(role.Path),
		SourceRecordID:   roleARN,
		CorrelationHints: []string{roleARN, strings.TrimSpace(role.Name)},
	}
}

func userPrincipalObservation(ctx secretsiam.EnvelopeContext, user User) secretsiam.PrincipalObservation {
	userARN := strings.TrimSpace(user.ARN)
	return secretsiam.PrincipalObservation{
		Context:          ctx,
		PrincipalARN:     userARN,
		PrincipalType:    secretsiam.PrincipalTypeAWSUser,
		Name:             strings.TrimSpace(user.Name),
		Path:             strings.TrimSpace(user.Path),
		SourceRecordID:   userARN,
		CorrelationHints: []string{userARN, strings.TrimSpace(user.Name)},
	}
}

func oidcProviderPrincipalObservation(ctx secretsiam.EnvelopeContext, provider OIDCProvider) secretsiam.PrincipalObservation {
	providerARN := strings.TrimSpace(provider.ARN)
	return secretsiam.PrincipalObservation{
		Context:          ctx,
		PrincipalARN:     providerARN,
		PrincipalType:    secretsiam.PrincipalTypeAWSOIDCProvider,
		URLFingerprint:   strings.TrimSpace(provider.URLFingerprint),
		ClientIDCount:    provider.ClientIDCount,
		ThumbprintCount:  provider.ThumbprintCount,
		SourceRecordID:   providerARN,
		CorrelationHints: []string{providerARN},
	}
}

func permissionBoundaryObservation(
	ctx secretsiam.EnvelopeContext,
	principalARN string,
	principalType string,
	boundary PermissionBoundary,
) secretsiam.PermissionBoundaryObservation {
	return secretsiam.PermissionBoundaryObservation{
		Context:           ctx,
		PrincipalARN:      strings.TrimSpace(principalARN),
		PrincipalType:     principalType,
		BoundaryPolicyARN: strings.TrimSpace(boundary.PolicyARN),
		BoundaryType:      strings.TrimSpace(boundary.Type),
	}
}

func policyAttachmentObservation(
	ctx secretsiam.EnvelopeContext,
	principalARN string,
	principalType string,
	policyARN string,
) secretsiam.PolicyAttachmentObservation {
	return secretsiam.PolicyAttachmentObservation{
		Context:       ctx,
		PrincipalARN:  strings.TrimSpace(principalARN),
		PrincipalType: principalType,
		PolicyARN:     strings.TrimSpace(policyARN),
		PolicySource:  secretsiam.PolicySourceAttachedManaged,
	}
}

func instanceProfileSecretsObservation(ctx secretsiam.EnvelopeContext, profile InstanceProfile) secretsiam.InstanceProfileObservation {
	profileARN := strings.TrimSpace(profile.ARN)
	return secretsiam.InstanceProfileObservation{
		Context:        ctx,
		ProfileARN:     profileARN,
		Name:           strings.TrimSpace(profile.Name),
		Path:           strings.TrimSpace(profile.Path),
		RoleARNs:       profile.RoleARNs,
		SourceRecordID: profileARN,
	}
}

func coverageWarningObservation(ctx secretsiam.EnvelopeContext, warning CoverageWarning) secretsiam.CoverageWarningObservation {
	return secretsiam.CoverageWarningObservation{
		Context:     ctx,
		WarningKind: strings.TrimSpace(warning.WarningKind),
		SourceState: strings.TrimSpace(warning.SourceState),
		ErrorClass:  strings.TrimSpace(warning.ErrorClass),
		Message:     strings.TrimSpace(warning.Message),
		Attributes:  warning.Attributes,
	}
}

func secretsIAMPolicyEnvelopes(
	ctx secretsiam.EnvelopeContext,
	principalARN string,
	principalType string,
	statements []PolicyStatement,
) ([]facts.Envelope, error) {
	if len(statements) == 0 {
		return nil, nil
	}
	envelopes := make([]facts.Envelope, 0, len(statements))
	for _, statement := range statements {
		if strings.TrimSpace(statement.Source) == PolicySourceTrust {
			envelope, err := secretsiam.NewTrustPolicyEnvelope(secretsiam.TrustPolicyObservation{
				Context:                        ctx,
				RoleARN:                        strings.TrimSpace(principalARN),
				StatementSID:                   strings.TrimSpace(statement.StatementSID),
				Effect:                         statement.Effect,
				Actions:                        statement.Actions,
				ConditionKeys:                  statement.ConditionKeys,
				ConditionOperators:             statement.ConditionOperators,
				AssumePrincipals:               statement.AssumePrincipals,
				WebIdentitySubjectFingerprints: statement.WebIdentitySubjectFingerprints,
				WebIdentitySubjectWildcard:     statement.WebIdentitySubjectWildcard,
			})
			if err != nil {
				return nil, fmt.Errorf("build IAM trust policy fact for principal %q: %w", principalARN, err)
			}
			envelopes = append(envelopes, envelope)
			continue
		}
		envelope, err := secretsiam.NewPermissionPolicyEnvelope(secretsiam.PermissionPolicyObservation{
			Context:            ctx,
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
		})
		if err != nil {
			return nil, fmt.Errorf("build IAM permission policy fact for principal %q: %w", principalARN, err)
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}
