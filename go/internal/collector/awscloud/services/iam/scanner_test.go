// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iam

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsIAMResourcesAndRelationships(t *testing.T) {
	client := fakeClient{
		roles: []Role{{
			ARN:              "arn:aws:iam::123456789012:role/eshu-runtime",
			Name:             "eshu-runtime",
			Path:             "/service/",
			AssumeRolePolicy: map[string]any{"Version": "2012-10-17"},
			TrustPrincipals: []TrustPrincipal{{
				Type:       "AWS",
				Identifier: "arn:aws:iam::111122223333:root",
			}},
			AttachedPolicyARNs: []string{"arn:aws:iam::123456789012:policy/eshu-read"},
		}},
		policies: []Policy{{
			ARN:              "arn:aws:iam::123456789012:policy/eshu-read",
			Name:             "eshu-read",
			Path:             "/service/",
			DefaultVersionID: "v1",
			AttachmentCount:  1,
		}},
		profiles: []InstanceProfile{{
			ARN:      "arn:aws:iam::123456789012:instance-profile/eshu-node",
			Name:     "eshu-node",
			Path:     "/service/",
			RoleARNs: []string{"arn:aws:iam::123456789012:role/eshu-runtime"},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	counts := factKindCounts(envelopes)
	if counts[facts.AWSResourceFactKind] != 3 {
		t.Fatalf("aws_resource count = %d, want 3", counts[facts.AWSResourceFactKind])
	}
	if counts[facts.AWSRelationshipFactKind] != 3 {
		t.Fatalf("aws_relationship count = %d, want 3", counts[facts.AWSRelationshipFactKind])
	}
	for _, envelope := range envelopes {
		if !isAWSCloudFact(envelope.FactKind) {
			continue
		}
		if envelope.CollectorKind != awscloud.CollectorKind {
			t.Fatalf("CollectorKind = %q, want %q", envelope.CollectorKind, awscloud.CollectorKind)
		}
		if envelope.SourceConfidence != facts.SourceConfidenceReported {
			t.Fatalf("SourceConfidence = %q, want %q", envelope.SourceConfidence, facts.SourceConfidenceReported)
		}
		if envelope.FencingToken != 42 {
			t.Fatalf("FencingToken = %d, want 42", envelope.FencingToken)
		}
	}
	assertRelationshipType(t, envelopes, awscloud.RelationshipIAMRoleTrustsPrincipal)
	assertRelationshipType(t, envelopes, awscloud.RelationshipIAMRoleAttachedPolicy)
	assertRelationshipType(t, envelopes, awscloud.RelationshipIAMRoleInInstanceProfile)
}

func TestScannerEmitsDerivedPermissionFacts(t *testing.T) {
	client := fakeClient{
		roles: []Role{{
			ARN:  "arn:aws:iam::123456789012:role/eshu-runtime",
			Name: "eshu-runtime",
			TrustPrincipals: []TrustPrincipal{{
				Type:       "AWS",
				Identifier: "arn:aws:iam::111122223333:root",
			}},
			PermissionStatements: []PolicyStatement{
				{
					Source:           PolicySourceTrust,
					Effect:           "Allow",
					Actions:          []string{"sts:AssumeRole"},
					AssumePrincipals: []string{"arn:aws:iam::111122223333:root"},
				},
				{
					Source:        PolicySourceInline,
					PolicyName:    "inline-escalate",
					StatementSID:  "AllowPassRole",
					Effect:        "Allow",
					Actions:       []string{"iam:PassRole"},
					Resources:     []string{"arn:aws:iam::123456789012:role/*"},
					ConditionKeys: []string{"aws:SourceIp"},
				},
				{
					Source:    PolicySourceAttachedManaged,
					PolicyARN: "arn:aws:iam::aws:policy/AdministratorAccess",
					Effect:    "Allow",
					Actions:   []string{"*"},
					Resources: []string{"*"},
				},
				{
					Source:    PolicySourcePermissionBoundary,
					PolicyARN: "arn:aws:iam::123456789012:policy/developer-boundary",
					Effect:    "Allow",
					Actions:   []string{"s3:GetObject"},
					Resources: []string{"arn:aws:s3:::prod-data-bucket"},
				},
			},
		}},
		users: []User{{
			ARN:  "arn:aws:iam::123456789012:user/breakglass",
			Name: "breakglass",
			PermissionStatements: []PolicyStatement{{
				Source:     PolicySourceInline,
				PolicyName: "inline-admin",
				Effect:     "Allow",
				Actions:    []string{"iam:AttachUserPolicy"},
				Resources:  []string{"*"},
			}},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	counts := factKindCounts(envelopes)
	if counts[facts.AWSIAMPermissionFactKind] != 5 {
		t.Fatalf("aws_iam_permission count = %d, want 5", counts[facts.AWSIAMPermissionFactKind])
	}

	// The user resource and its permission fact are emitted; the user principal
	// becomes its own aws_resource node.
	if counts[facts.AWSResourceFactKind] != 2 {
		t.Fatalf("aws_resource count = %d, want 2 (role + user)", counts[facts.AWSResourceFactKind])
	}

	assertPermissionPresent(t, envelopes, "arn:aws:iam::123456789012:role/eshu-runtime", awscloud.IAMPolicySourceTrust, "sts:assumerole")
	assertPermissionPresent(t, envelopes, "arn:aws:iam::123456789012:role/eshu-runtime", awscloud.IAMPolicySourceInline, "iam:passrole")
	assertPermissionPresent(t, envelopes, "arn:aws:iam::123456789012:role/eshu-runtime", awscloud.IAMPolicySourcePermissionBoundary, "s3:getobject")
	assertPermissionPresent(t, envelopes, "arn:aws:iam::123456789012:user/breakglass", awscloud.IAMPolicySourceInline, "iam:attachuserpolicy")

	assertNoRawPolicyJSON(t, envelopes)
}

func TestScannerEmitsSecretsIAMPostureSourceFacts(t *testing.T) {
	client := fakeClient{
		roles: []Role{{
			ARN:  "arn:aws:iam::123456789012:role/eshu-runtime",
			Name: "eshu-runtime",
			Path: "/service/",
			AssumeRolePolicy: map[string]any{
				"Statement": []any{map[string]any{
					"Condition": map[string]any{
						"IpAddress": map[string]any{"aws:SourceIp": "10.0.0.1/32"},
					},
				}},
			},
			PermissionBoundary: PermissionBoundary{
				PolicyARN: "arn:aws:iam::123456789012:policy/developer-boundary",
				Type:      "PermissionsBoundaryPolicy",
			},
			TrustPrincipals: []TrustPrincipal{{
				Type:       "AWS",
				Identifier: "arn:aws:iam::111122223333:root",
			}},
			AttachedPolicyARNs: []string{"arn:aws:iam::123456789012:policy/eshu-read"},
			InlinePolicyNames:  []string{"inline-escalate"},
			PermissionStatements: []PolicyStatement{
				{
					Source:           PolicySourceTrust,
					Effect:           "Allow",
					Actions:          []string{"sts:AssumeRole"},
					ConditionKeys:    []string{"aws:SourceIp"},
					AssumePrincipals: []string{"arn:aws:iam::111122223333:root"},
				},
				{
					Source:        PolicySourceInline,
					PolicyName:    "inline-escalate",
					StatementSID:  "AllowPassRole",
					Effect:        "Allow",
					Actions:       []string{"iam:PassRole"},
					Resources:     []string{"arn:aws:iam::123456789012:role/*"},
					ConditionKeys: []string{"aws:SourceIp"},
				},
				{
					Source:    PolicySourceAttachedManaged,
					PolicyARN: "arn:aws:iam::123456789012:policy/eshu-read",
					Effect:    "Allow",
					Actions:   []string{"s3:GetObject"},
					Resources: []string{"arn:aws:s3:::example/*"},
				},
				{
					Source:    PolicySourcePermissionBoundary,
					PolicyARN: "arn:aws:iam::123456789012:policy/developer-boundary",
					Effect:    "Allow",
					Actions:   []string{"s3:GetObject"},
					Resources: []string{"arn:aws:s3:::example/*"},
				},
			},
		}},
		users: []User{{
			ARN:  "arn:aws:iam::123456789012:user/breakglass",
			Name: "breakglass",
			PermissionBoundary: PermissionBoundary{
				PolicyARN: "arn:aws:iam::123456789012:policy/user-boundary",
				Type:      "PermissionsBoundaryPolicy",
			},
			AttachedPolicyARNs: []string{"arn:aws:iam::aws:policy/SecurityAudit"},
			InlinePolicyNames:  []string{"inline-admin"},
			PermissionStatements: []PolicyStatement{{
				Source:     PolicySourceInline,
				PolicyName: "inline-admin",
				Effect:     "Allow",
				Actions:    []string{"iam:AttachUserPolicy"},
				Resources:  []string{"*"},
			}},
		}},
		policies: []Policy{{
			ARN:              "arn:aws:iam::123456789012:policy/eshu-read",
			Name:             "eshu-read",
			DefaultVersionID: "v1",
			AttachmentCount:  1,
		}},
		profiles: []InstanceProfile{{
			ARN:      "arn:aws:iam::123456789012:instance-profile/eshu-node",
			Name:     "eshu-node",
			Path:     "/service/",
			RoleARNs: []string{"arn:aws:iam::123456789012:role/eshu-runtime"},
		}},
		oidcProviders: []OIDCProvider{{
			ARN:             "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com",
			URLFingerprint:  "sha256:github-actions",
			ClientIDCount:   2,
			ThumbprintCount: 1,
		}},
		warnings: []CoverageWarning{{
			WarningKind: "access_analyzer_not_enabled",
			SourceState: secretsiam.SourceStateUnsupported,
			ErrorClass:  "unsupported_source",
			Message:     "Access Analyzer source facts are not enabled for this fixture",
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	counts := factKindCounts(envelopes)
	wantCounts := map[string]int{
		facts.AWSIAMPrincipalFactKind:           3,
		facts.AWSIAMTrustPolicyFactKind:         1,
		facts.AWSIAMPermissionPolicyFactKind:    4,
		facts.AWSIAMPolicyAttachmentFactKind:    2,
		facts.AWSIAMPermissionBoundaryFactKind:  2,
		facts.AWSIAMInstanceProfileFactKind:     1,
		facts.SecretsIAMCoverageWarningFactKind: 1,
	}
	for factKind, want := range wantCounts {
		if counts[factKind] != want {
			t.Fatalf("%s count = %d, want %d", factKind, counts[factKind], want)
		}
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSIAMPermissionPolicyFactKind ||
			envelope.FactKind == facts.AWSIAMTrustPolicyFactKind {
			if envelope.CollectorKind != secretsiam.CollectorKind {
				t.Fatalf("%s CollectorKind = %q, want %q", envelope.FactKind, envelope.CollectorKind, secretsiam.CollectorKind)
			}
		}
		assertNoRawPolicyJSON(t, []facts.Envelope{envelope})
	}
}

func TestScannerStopsOnClientError(t *testing.T) {
	_, err := (Scanner{Client: fakeClient{roleErr: errBoom{}}}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatal("Scan returned nil error, want role list error")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "aws-global",
		ServiceKind:         awscloud.ServiceIAM,
		ScopeID:             "aws:123456789012:aws-global",
		GenerationID:        "aws:123456789012:aws-global:iam:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = "ec2"
	_, err := Scanner{Client: fakeClient{}}.Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

type fakeClient struct {
	roles         []Role
	users         []User
	policies      []Policy
	profiles      []InstanceProfile
	oidcProviders []OIDCProvider
	warnings      []CoverageWarning
	roleErr       error
}

func (c fakeClient) ListRoles(context.Context) ([]Role, error) {
	return c.roles, c.roleErr
}

func (c fakeClient) ListUsers(context.Context) ([]User, error) {
	return c.users, nil
}

func (c fakeClient) ListPolicies(context.Context) ([]Policy, error) {
	return c.policies, nil
}

func (c fakeClient) ListInstanceProfiles(context.Context) ([]InstanceProfile, error) {
	return c.profiles, nil
}

func (c fakeClient) ListOIDCProviders(context.Context) ([]OIDCProvider, error) {
	return c.oidcProviders, nil
}

func (c fakeClient) ListCoverageWarnings(context.Context) ([]CoverageWarning, error) {
	return c.warnings, nil
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }

func factKindCounts(envelopes []facts.Envelope) map[string]int {
	counts := make(map[string]int)
	for _, envelope := range envelopes {
		counts[envelope.FactKind]++
	}
	return counts
}

func isAWSCloudFact(factKind string) bool {
	switch factKind {
	case facts.AWSResourceFactKind,
		facts.AWSRelationshipFactKind,
		facts.AWSIAMPermissionFactKind:
		return true
	default:
		return false
	}
}

func assertPermissionPresent(t *testing.T, envelopes []facts.Envelope, principalARN, policySource, action string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSIAMPermissionFactKind {
			continue
		}
		if got, _ := envelope.Payload["principal_arn"].(string); got != principalARN {
			continue
		}
		if got, _ := envelope.Payload["policy_source"].(string); got != policySource {
			continue
		}
		actions, _ := envelope.Payload["actions"].([]string)
		for _, candidate := range actions {
			if candidate == action {
				return
			}
		}
	}
	t.Fatalf("missing permission fact principal=%q source=%q action=%q", principalARN, policySource, action)
}

// assertNoRawPolicyJSON proves the derived permission facts never carry a raw
// policy document body. The metadata-only contract forbids persisting the
// verbatim JSON or any condition values.
func assertNoRawPolicyJSON(t *testing.T, envelopes []facts.Envelope) {
	t.Helper()
	forbidden := []string{
		"Statement",
		"policy_document",
		"document",
		"raw_policy",
		"condition_values",
		"trust_policy",
		"10.0.0.1/32",
	}
	for _, envelope := range envelopes {
		assertNoForbiddenPayloadValue(t, envelope.FactKind, envelope.Payload, forbidden)
	}
}

func assertNoForbiddenPayloadValue(t *testing.T, factKind string, value any, forbidden []string) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			for _, forbiddenValue := range forbidden {
				if key == forbiddenValue {
					t.Fatalf("%s payload carries forbidden raw-policy key %q: %#v", factKind, key, typed)
				}
			}
			assertNoForbiddenPayloadValue(t, factKind, nested, forbidden)
		}
	case []any:
		for _, nested := range typed {
			assertNoForbiddenPayloadValue(t, factKind, nested, forbidden)
		}
	case []string:
		for _, nested := range typed {
			assertNoForbiddenPayloadValue(t, factKind, nested, forbidden)
		}
	case string:
		for _, forbiddenValue := range forbidden {
			if typed == forbiddenValue {
				t.Fatalf("%s payload carries forbidden raw-policy value %q", factKind, typed)
			}
		}
	}
}

func assertRelationshipType(t *testing.T, envelopes []facts.Envelope, relationshipType string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return
		}
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
}
