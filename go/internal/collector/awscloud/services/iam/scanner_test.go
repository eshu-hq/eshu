package iam

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
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
	if counts[facts.AWSIAMPermissionFactKind] != 4 {
		t.Fatalf("aws_iam_permission count = %d, want 4", counts[facts.AWSIAMPermissionFactKind])
	}

	// The user resource and its permission fact are emitted; the user principal
	// becomes its own aws_resource node.
	if counts[facts.AWSResourceFactKind] != 2 {
		t.Fatalf("aws_resource count = %d, want 2 (role + user)", counts[facts.AWSResourceFactKind])
	}

	assertPermissionPresent(t, envelopes, "arn:aws:iam::123456789012:role/eshu-runtime", awscloud.IAMPolicySourceTrust, "sts:assumerole")
	assertPermissionPresent(t, envelopes, "arn:aws:iam::123456789012:role/eshu-runtime", awscloud.IAMPolicySourceInline, "iam:passrole")
	assertPermissionPresent(t, envelopes, "arn:aws:iam::123456789012:user/breakglass", awscloud.IAMPolicySourceInline, "iam:attachuserpolicy")

	assertNoRawPolicyJSON(t, envelopes)
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
	roles    []Role
	users    []User
	policies []Policy
	profiles []InstanceProfile
	roleErr  error
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

type errBoom struct{}

func (errBoom) Error() string { return "boom" }

func factKindCounts(envelopes []facts.Envelope) map[string]int {
	counts := make(map[string]int)
	for _, envelope := range envelopes {
		counts[envelope.FactKind]++
	}
	return counts
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
	forbidden := []string{"Statement", "policy_document", "document", "raw_policy", "condition_values"}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSIAMPermissionFactKind {
			continue
		}
		for _, key := range forbidden {
			if _, ok := envelope.Payload[key]; ok {
				t.Fatalf("permission fact carries forbidden raw-policy key %q: %#v", key, envelope.Payload)
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
