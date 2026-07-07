// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
	secretsiamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/secretsiam/v1"
)

func TestAWSBuilderPayloadsMatchTypedDirectMapEncoders(t *testing.T) {
	ctx := testContext()
	cases := []struct {
		name     string
		envelope facts.Envelope
		want     map[string]any
	}{
		{
			name: "principal",
			envelope: mustEnvelope(NewPrincipalEnvelope(PrincipalObservation{
				Context:          ctx,
				PrincipalARN:     "arn:aws:iam::123456789012:role/eshu-runtime",
				PrincipalType:    PrincipalTypeAWSRole,
				Name:             "eshu-runtime",
				Path:             "/service/",
				URLFingerprint:   "sha256:principal-url",
				ClientIDCount:    2,
				ThumbprintCount:  1,
				CorrelationHints: []string{" role-hint ", "role-hint", "app"},
			})),
			want: mustPayload(factschema.EncodeAWSIAMPrincipal(iamv1.Principal{
				AccountID:              ctx.AccountID,
				Region:                 ctx.Region,
				PrincipalARN:           "arn:aws:iam::123456789012:role/eshu-runtime",
				PrincipalID:            strPtr("arn:aws:iam::123456789012:role/eshu-runtime"),
				PrincipalType:          PrincipalTypeAWSRole,
				Provider:               strPtr(ProviderAWSIAM),
				CollectorInstanceID:    strPtr(ctx.CollectorInstanceID),
				RedactionPolicyVersion: strPtr(RedactionPolicyVersion),
				Name:                   strPtr("eshu-runtime"),
				Path:                   strPtr("/service/"),
				URLFingerprint:         strPtr("sha256:principal-url"),
				URLPresent:             boolPtr(true),
				ClientIDCount:          intPtr(2),
				ThumbprintCount:        intPtr(1),
				CorrelationHints:       []string{"app", "role-hint"},
			})),
		},
		{
			name: "trust_policy",
			envelope: mustEnvelope(NewTrustPolicyEnvelope(TrustPolicyObservation{
				Context:                        ctx,
				RoleARN:                        "arn:aws:iam::123456789012:role/eshu-runtime",
				StatementSID:                   "WebIdentity",
				Effect:                         "allow",
				Actions:                        []string{"sts:AssumeRoleWithWebIdentity", "sts:AssumeRoleWithWebIdentity"},
				ConditionKeys:                  []string{"oidc:sub", "aws:PrincipalOrgID"},
				ConditionOperators:             []string{"StringLike", "StringEquals"},
				AssumePrincipals:               []string{"arn:aws:iam::111122223333:root"},
				WebIdentitySubjectFingerprints: []string{"sha256:web-subject"},
				WebIdentitySubjectWildcard:     true,
			})),
			want: mustPayload(factschema.EncodeAWSIAMTrustPolicy(secretsiamv1.AWSIAMTrustPolicy{
				AccountID:                      ctx.AccountID,
				Region:                         ctx.Region,
				Provider:                       ProviderAWSIAM,
				CollectorInstanceID:            ctx.CollectorInstanceID,
				RedactionPolicyVersion:         RedactionPolicyVersion,
				RoleARN:                        "arn:aws:iam::123456789012:role/eshu-runtime",
				PolicySource:                   PolicySourceTrust,
				Effect:                         "Allow",
				StatementSID:                   strPtr("WebIdentity"),
				Actions:                        []string{"sts:assumerolewithwebidentity"},
				ConditionKeys:                  []string{"aws:PrincipalOrgID", "oidc:sub"},
				ConditionOperators:             []string{"StringEquals", "StringLike"},
				ConditionOperatorCount:         intPtr(2),
				AssumePrincipals:               []string{"arn:aws:iam::111122223333:root"},
				HasConditions:                  boolPtr(true),
				WebIdentitySubjectFingerprints: []string{"sha256:web-subject"},
				WebIdentitySubjectWildcard:     boolPtr(true),
			})),
		},
		{
			name: "permission_policy",
			envelope: mustEnvelope(NewPermissionPolicyEnvelope(PermissionPolicyObservation{
				Context:            ctx,
				PrincipalARN:       "arn:aws:iam::123456789012:role/eshu-runtime",
				PrincipalType:      PrincipalTypeAWSRole,
				PolicySource:       PolicySourceInline,
				PolicyARN:          "arn:aws:iam::123456789012:policy/eshu-inline",
				PolicyName:         "eshu-inline",
				StatementSID:       "AllowSecretRead",
				Effect:             "allow",
				Actions:            []string{"secretsmanager:GetSecretValue"},
				NotActions:         []string{"iam:*"},
				Resources:          []string{"arn:aws:secretsmanager:us-east-1:123456789012:secret:*"},
				NotResources:       []string{"arn:aws:s3:::raw"},
				ConditionKeys:      []string{"aws:SourceVpce"},
				ConditionOperators: []string{"StringEquals"},
			})),
			want: mustPayload(factschema.EncodeAWSIAMPermissionPolicy(secretsiamv1.AWSIAMPermissionPolicy{
				AccountID:              ctx.AccountID,
				Region:                 ctx.Region,
				Provider:               ProviderAWSIAM,
				CollectorInstanceID:    ctx.CollectorInstanceID,
				RedactionPolicyVersion: RedactionPolicyVersion,
				PrincipalARN:           "arn:aws:iam::123456789012:role/eshu-runtime",
				PolicySource:           PolicySourceInline,
				Effect:                 "Allow",
				PrincipalType:          strPtr(PrincipalTypeAWSRole),
				PolicyARN:              strPtr("arn:aws:iam::123456789012:policy/eshu-inline"),
				PolicyName:             strPtr("eshu-inline"),
				StatementSID:           strPtr("AllowSecretRead"),
				Actions:                []string{"secretsmanager:getsecretvalue"},
				NotActions:             []string{"iam:*"},
				Resources:              []string{"arn:aws:secretsmanager:us-east-1:123456789012:secret:*"},
				NotResources:           []string{"arn:aws:s3:::raw"},
				ConditionKeys:          []string{"aws:SourceVpce"},
				ConditionOperators:     []string{"StringEquals"},
				ConditionOperatorCount: intPtr(1),
				HasConditions:          boolPtr(true),
				IsWildcardAction:       boolPtr(false),
				IsWildcardResource:     boolPtr(false),
			})),
		},
		{
			name: "policy_attachment",
			envelope: mustEnvelope(NewPolicyAttachmentEnvelope(PolicyAttachmentObservation{
				Context:       ctx,
				PrincipalARN:  "arn:aws:iam::123456789012:role/eshu-runtime",
				PrincipalType: PrincipalTypeAWSRole,
				PolicyARN:     "arn:aws:iam::123456789012:policy/eshu-read",
				PolicyName:    "eshu-read",
				PolicySource:  PolicySourceAttachedManaged,
			})),
			want: mustPayload(factschema.EncodeAWSIAMPolicyAttachment(secretsiamv1.AWSIAMPolicyAttachment{
				AccountID:              ctx.AccountID,
				Region:                 ctx.Region,
				Provider:               ProviderAWSIAM,
				CollectorInstanceID:    ctx.CollectorInstanceID,
				RedactionPolicyVersion: RedactionPolicyVersion,
				PrincipalARN:           "arn:aws:iam::123456789012:role/eshu-runtime",
				PolicyARN:              "arn:aws:iam::123456789012:policy/eshu-read",
				PrincipalType:          strPtr(PrincipalTypeAWSRole),
				PolicyName:             strPtr("eshu-read"),
				PolicySource:           strPtr(PolicySourceAttachedManaged),
			})),
		},
		{
			name: "permission_boundary",
			envelope: mustEnvelope(NewPermissionBoundaryEnvelope(PermissionBoundaryObservation{
				Context:           ctx,
				PrincipalARN:      "arn:aws:iam::123456789012:role/eshu-runtime",
				PrincipalType:     PrincipalTypeAWSRole,
				BoundaryPolicyARN: "arn:aws:iam::123456789012:policy/developer-boundary",
				BoundaryType:      "PermissionsBoundaryPolicy",
			})),
			want: mustPayload(factschema.EncodeAWSIAMPermissionBoundary(secretsiamv1.AWSIAMPermissionBoundary{
				AccountID:              ctx.AccountID,
				Region:                 ctx.Region,
				Provider:               ProviderAWSIAM,
				CollectorInstanceID:    ctx.CollectorInstanceID,
				RedactionPolicyVersion: RedactionPolicyVersion,
				PrincipalARN:           "arn:aws:iam::123456789012:role/eshu-runtime",
				BoundaryPolicyARN:      "arn:aws:iam::123456789012:policy/developer-boundary",
				PrincipalType:          strPtr(PrincipalTypeAWSRole),
				BoundaryType:           strPtr("PermissionsBoundaryPolicy"),
			})),
		},
		{
			name: "instance_profile",
			envelope: mustEnvelope(NewInstanceProfileEnvelope(InstanceProfileObservation{
				Context:    ctx,
				ProfileARN: "arn:aws:iam::123456789012:instance-profile/eshu-runtime",
				Name:       "eshu-runtime",
				Path:       "/service/",
				RoleARNs:   []string{"arn:aws:iam::123456789012:role/eshu-runtime"},
			})),
			want: mustPayload(factschema.EncodeAWSIAMInstanceProfile(secretsiamv1.AWSIAMInstanceProfile{
				AccountID:              ctx.AccountID,
				Region:                 ctx.Region,
				Provider:               ProviderAWSIAM,
				CollectorInstanceID:    ctx.CollectorInstanceID,
				RedactionPolicyVersion: RedactionPolicyVersion,
				ProfileARN:             "arn:aws:iam::123456789012:instance-profile/eshu-runtime",
				Name:                   strPtr("eshu-runtime"),
				Path:                   strPtr("/service/"),
				RoleARNs:               []string{"arn:aws:iam::123456789012:role/eshu-runtime"},
				RoleCount:              intPtr(1),
			})),
		},
		{
			name: "access_analyzer_finding",
			envelope: mustEnvelope(NewAccessAnalyzerFindingEnvelope(AccessAnalyzerFindingObservation{
				Context:       ctx,
				FindingID:     "finding-123",
				AnalyzerARN:   "arn:aws:access-analyzer:us-east-1:123456789012:analyzer/account",
				ResourceARN:   "arn:aws:iam::123456789012:role/eshu-runtime",
				ResourceType:  PrincipalTypeAWSRole,
				Status:        "ACTIVE",
				FindingType:   "ExternalAccess",
				ConditionKeys: []string{"aws:PrincipalOrgID"},
			})),
			want: mustPayload(factschema.EncodeAWSIAMAccessAnalyzerFinding(secretsiamv1.AWSIAMAccessAnalyzerFinding{
				AccountID:              ctx.AccountID,
				Region:                 ctx.Region,
				Provider:               ProviderAWSIAM,
				CollectorInstanceID:    ctx.CollectorInstanceID,
				RedactionPolicyVersion: RedactionPolicyVersion,
				FindingID:              strPtr("finding-123"),
				AnalyzerARN:            strPtr("arn:aws:access-analyzer:us-east-1:123456789012:analyzer/account"),
				ResourceARN:            strPtr("arn:aws:iam::123456789012:role/eshu-runtime"),
				ResourceType:           strPtr(PrincipalTypeAWSRole),
				Status:                 strPtr("ACTIVE"),
				FindingType:            strPtr("ExternalAccess"),
				ConditionKeys:          []string{"aws:PrincipalOrgID"},
			})),
		},
		{
			name: "coverage_warning",
			envelope: mustEnvelope(NewCoverageWarningEnvelope(CoverageWarningObservation{
				Context:     ctx,
				WarningKind: "access_analyzer_not_enabled",
				SourceState: SourceStateUnsupported,
				ErrorClass:  "unsupported_source",
				Message:     "Access Analyzer source facts are not enabled for this fixture",
				Attributes:  map[string]any{"coverage_scope": "account"},
			})),
			want: mustPayload(factschema.EncodeSecretsIAMCoverageWarning(secretsiamv1.CoverageWarning{
				Provider:               ProviderAWSIAM,
				CollectorInstanceID:    ctx.CollectorInstanceID,
				RedactionPolicyVersion: RedactionPolicyVersion,
				WarningKind:            "access_analyzer_not_enabled",
				SourceState:            SourceStateUnsupported,
				AccountID:              strPtr(ctx.AccountID),
				Region:                 strPtr(ctx.Region),
				ErrorClass:             strPtr("unsupported_source"),
				Message:                strPtr("Access Analyzer source facts are not enabled for this fixture"),
				Attributes:             map[string]any{"coverage_scope": "account"},
			})),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !reflect.DeepEqual(tc.envelope.Payload, tc.want) {
				t.Fatalf("payload drifted from typed direct-map encoder\nbuilder: %#v\nencoder: %#v", tc.envelope.Payload, tc.want)
			}
			assertNoForbiddenPayloadKeys(t, tc.envelope.Payload)
		})
	}
}

func mustEnvelope(env facts.Envelope, err error) facts.Envelope {
	if err != nil {
		panic(err)
	}
	return env
}

func mustPayload(payload map[string]any, err error) map[string]any {
	if err != nil {
		panic(err)
	}
	return payload
}

func strPtr(value string) *string {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func intPtr(value int) *int {
	return &value
}
