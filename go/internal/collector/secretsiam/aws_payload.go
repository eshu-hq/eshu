// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/sdk/go/factschema"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
	secretsiamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/secretsiam/v1"
)

func principalPayload(
	ctx EnvelopeContext,
	observation PrincipalObservation,
	principalARN string,
	principalType string,
) (map[string]any, error) {
	common := commonPayload(ctx)
	payload, err := factschema.EncodeAWSIAMPrincipal(iamv1.Principal{
		AccountID:              common.AccountID,
		Region:                 common.Region,
		PrincipalARN:           principalARN,
		PrincipalID:            payloadStringPtr(principalARN),
		PrincipalType:          principalType,
		Provider:               payloadStringPtr(common.Provider),
		CollectorInstanceID:    payloadStringPtr(common.CollectorInstanceID),
		RedactionPolicyVersion: payloadStringPtr(common.RedactionPolicyVersion),
		Name:                   payloadStringPtr(strings.TrimSpace(observation.Name)),
		Path:                   payloadStringPtr(strings.TrimSpace(observation.Path)),
		URLFingerprint:         payloadStringPtr(strings.TrimSpace(observation.URLFingerprint)),
		URLPresent:             payloadBoolPtr(strings.TrimSpace(observation.URLFingerprint) != ""),
		ClientIDCount:          payloadIntPtr(observation.ClientIDCount),
		ThumbprintCount:        payloadIntPtr(observation.ThumbprintCount),
		CorrelationHints:       normalizePatternList(observation.CorrelationHints),
	})
	if err != nil {
		return nil, fmt.Errorf("encode aws iam principal payload: %w", err)
	}
	return payload, nil
}

func trustPolicyPayload(
	ctx EnvelopeContext,
	roleARN string,
	statementSID string,
	effect string,
	actions []string,
	conditionKeys []string,
	conditionOperators []string,
	assumePrincipals []string,
	webIdentitySubjects []string,
	webIdentitySubjectWildcard bool,
) (map[string]any, error) {
	common := commonPayload(ctx)
	payload, err := factschema.EncodeAWSIAMTrustPolicy(secretsiamv1.AWSIAMTrustPolicy{
		AccountID:                      common.AccountID,
		Region:                         common.Region,
		Provider:                       common.Provider,
		CollectorInstanceID:            common.CollectorInstanceID,
		RedactionPolicyVersion:         common.RedactionPolicyVersion,
		RoleARN:                        roleARN,
		PolicySource:                   PolicySourceTrust,
		Effect:                         effect,
		StatementSID:                   payloadStringPtr(statementSID),
		Actions:                        actions,
		ConditionKeys:                  conditionKeys,
		ConditionOperators:             conditionOperators,
		ConditionOperatorCount:         payloadIntPtr(len(conditionOperators)),
		AssumePrincipals:               assumePrincipals,
		HasConditions:                  payloadBoolPtr(len(conditionKeys) > 0 || len(conditionOperators) > 0),
		WebIdentitySubjectFingerprints: webIdentitySubjects,
		WebIdentitySubjectWildcard:     payloadBoolPtr(webIdentitySubjectWildcard),
	})
	if err != nil {
		return nil, fmt.Errorf("encode aws iam trust policy payload: %w", err)
	}
	return payload, nil
}

func permissionPolicyPayload(
	ctx EnvelopeContext,
	observation PermissionPolicyObservation,
	principalARN string,
	policySource string,
	policyARN string,
	policyName string,
	statementSID string,
	effect string,
	actions []string,
	notActions []string,
	resources []string,
	notResources []string,
	conditionKeys []string,
	conditionOperators []string,
) (map[string]any, error) {
	common := commonPayload(ctx)
	payload, err := factschema.EncodeAWSIAMPermissionPolicy(secretsiamv1.AWSIAMPermissionPolicy{
		AccountID:              common.AccountID,
		Region:                 common.Region,
		Provider:               common.Provider,
		CollectorInstanceID:    common.CollectorInstanceID,
		RedactionPolicyVersion: common.RedactionPolicyVersion,
		PrincipalARN:           principalARN,
		PolicySource:           policySource,
		Effect:                 effect,
		PrincipalType:          payloadStringPtr(strings.TrimSpace(observation.PrincipalType)),
		PolicyARN:              payloadStringPtr(policyARN),
		PolicyName:             payloadStringPtr(policyName),
		StatementSID:           payloadStringPtr(statementSID),
		Actions:                actions,
		NotActions:             notActions,
		Resources:              resources,
		NotResources:           notResources,
		ConditionKeys:          conditionKeys,
		ConditionOperators:     conditionOperators,
		ConditionOperatorCount: payloadIntPtr(len(conditionOperators)),
		HasConditions:          payloadBoolPtr(len(conditionKeys) > 0 || len(conditionOperators) > 0),
		IsWildcardAction:       payloadBoolPtr(containsValue(actions, wildcardAction)),
		IsWildcardResource:     payloadBoolPtr(containsValue(resources, wildcardAction)),
	})
	if err != nil {
		return nil, fmt.Errorf("encode aws iam permission policy payload: %w", err)
	}
	return payload, nil
}

func policyAttachmentPayload(
	ctx EnvelopeContext,
	observation PolicyAttachmentObservation,
	principalARN string,
	policyARN string,
	policySource string,
) (map[string]any, error) {
	common := commonPayload(ctx)
	payload, err := factschema.EncodeAWSIAMPolicyAttachment(secretsiamv1.AWSIAMPolicyAttachment{
		AccountID:              common.AccountID,
		Region:                 common.Region,
		Provider:               common.Provider,
		CollectorInstanceID:    common.CollectorInstanceID,
		RedactionPolicyVersion: common.RedactionPolicyVersion,
		PrincipalARN:           principalARN,
		PolicyARN:              policyARN,
		PrincipalType:          payloadStringPtr(strings.TrimSpace(observation.PrincipalType)),
		PolicyName:             payloadStringPtr(strings.TrimSpace(observation.PolicyName)),
		PolicySource:           payloadStringPtr(policySource),
	})
	if err != nil {
		return nil, fmt.Errorf("encode aws iam policy attachment payload: %w", err)
	}
	return payload, nil
}

func permissionBoundaryPayload(
	ctx EnvelopeContext,
	observation PermissionBoundaryObservation,
	principalARN string,
	boundaryPolicyARN string,
) (map[string]any, error) {
	common := commonPayload(ctx)
	payload, err := factschema.EncodeAWSIAMPermissionBoundary(secretsiamv1.AWSIAMPermissionBoundary{
		AccountID:              common.AccountID,
		Region:                 common.Region,
		Provider:               common.Provider,
		CollectorInstanceID:    common.CollectorInstanceID,
		RedactionPolicyVersion: common.RedactionPolicyVersion,
		PrincipalARN:           principalARN,
		BoundaryPolicyARN:      boundaryPolicyARN,
		PrincipalType:          payloadStringPtr(strings.TrimSpace(observation.PrincipalType)),
		BoundaryType:           payloadStringPtr(strings.TrimSpace(observation.BoundaryType)),
	})
	if err != nil {
		return nil, fmt.Errorf("encode aws iam permission boundary payload: %w", err)
	}
	return payload, nil
}

func instanceProfilePayload(
	ctx EnvelopeContext,
	observation InstanceProfileObservation,
	profileARN string,
	roleARNs []string,
) (map[string]any, error) {
	common := commonPayload(ctx)
	payload, err := factschema.EncodeAWSIAMInstanceProfile(secretsiamv1.AWSIAMInstanceProfile{
		AccountID:              common.AccountID,
		Region:                 common.Region,
		Provider:               common.Provider,
		CollectorInstanceID:    common.CollectorInstanceID,
		RedactionPolicyVersion: common.RedactionPolicyVersion,
		ProfileARN:             profileARN,
		Name:                   payloadStringPtr(strings.TrimSpace(observation.Name)),
		Path:                   payloadStringPtr(strings.TrimSpace(observation.Path)),
		RoleARNs:               roleARNs,
		RoleCount:              payloadIntPtr(len(roleARNs)),
	})
	if err != nil {
		return nil, fmt.Errorf("encode aws iam instance profile payload: %w", err)
	}
	return payload, nil
}

func accessAnalyzerFindingPayload(
	ctx EnvelopeContext,
	observation AccessAnalyzerFindingObservation,
	findingID string,
	resourceARN string,
	conditionKeys []string,
) (map[string]any, error) {
	common := commonPayload(ctx)
	payload, err := factschema.EncodeAWSIAMAccessAnalyzerFinding(secretsiamv1.AWSIAMAccessAnalyzerFinding{
		AccountID:              common.AccountID,
		Region:                 common.Region,
		Provider:               common.Provider,
		CollectorInstanceID:    common.CollectorInstanceID,
		RedactionPolicyVersion: common.RedactionPolicyVersion,
		FindingID:              payloadStringPtr(findingID),
		AnalyzerARN:            payloadStringPtr(strings.TrimSpace(observation.AnalyzerARN)),
		ResourceARN:            payloadStringPtr(resourceARN),
		ResourceType:           payloadStringPtr(strings.TrimSpace(observation.ResourceType)),
		Status:                 payloadStringPtr(strings.TrimSpace(observation.Status)),
		FindingType:            payloadStringPtr(strings.TrimSpace(observation.FindingType)),
		ConditionKeys:          conditionKeys,
	})
	if err != nil {
		return nil, fmt.Errorf("encode aws iam access analyzer finding payload: %w", err)
	}
	return payload, nil
}

func coverageWarningPayload(
	ctx EnvelopeContext,
	observation CoverageWarningObservation,
	warningKind string,
	sourceState string,
) (map[string]any, error) {
	common := commonPayload(ctx)
	payload, err := factschema.EncodeSecretsIAMCoverageWarning(secretsiamv1.CoverageWarning{
		Provider:               common.Provider,
		CollectorInstanceID:    common.CollectorInstanceID,
		RedactionPolicyVersion: common.RedactionPolicyVersion,
		WarningKind:            warningKind,
		SourceState:            sourceState,
		AccountID:              payloadStringPtr(common.AccountID),
		Region:                 payloadStringPtr(common.Region),
		ErrorClass:             payloadStringPtr(strings.TrimSpace(observation.ErrorClass)),
		Message:                payloadStringPtr(strings.TrimSpace(observation.Message)),
		Attributes:             cloneAnyMap(observation.Attributes),
	})
	if err != nil {
		return nil, fmt.Errorf("encode secrets iam coverage warning payload: %w", err)
	}
	return payload, nil
}

func payloadStringPtr(value string) *string {
	return &value
}

func payloadBoolPtr(value bool) *bool {
	return &value
}

func payloadIntPtr(value int) *int {
	return &value
}
