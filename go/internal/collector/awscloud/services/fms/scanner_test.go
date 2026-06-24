// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fms

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsPolicyMetadataAndSecurityServiceTypeLabel(t *testing.T) {
	policyARN := "arn:aws:fms:us-east-1:123456789012:policy/p-abc123"
	client := fakeClient{
		policies: []Policy{{
			ARN:                            policyARN,
			ID:                             "p-abc123",
			Name:                           "org-wafv2-baseline",
			SecurityServiceType:            "WAFV2",
			ResourceType:                   "AWS::ElasticLoadBalancingV2::LoadBalancer",
			RemediationEnabled:             true,
			DeleteUnusedFMManagedResources: true,
			PolicyStatus:                   "ACTIVE",
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	policy := resourceByType(t, envelopes, awscloud.ResourceTypeFMSPolicy)
	if got, _ := policy.Payload["arn"].(string); got != policyARN {
		t.Fatalf("arn = %q, want %q", got, policyARN)
	}
	if got, _ := policy.Payload["resource_id"].(string); got != policyARN {
		t.Fatalf("resource_id = %q, want %q (the policy ARN)", got, policyARN)
	}
	attributes := attributesOf(t, policy)
	if got, want := attributes["security_service_type"], "WAFV2"; got != want {
		t.Fatalf("security_service_type = %#v, want %q", got, want)
	}
	if got, want := attributes["managed_resource_type"], "AWS::ElasticLoadBalancingV2::LoadBalancer"; got != want {
		t.Fatalf("managed_resource_type = %#v, want %q", got, want)
	}
	if got, want := attributes["remediation_enabled"], true; got != want {
		t.Fatalf("remediation_enabled = %#v, want %v", got, want)
	}
	if got, want := attributes["policy_id"], "p-abc123"; got != want {
		t.Fatalf("policy_id = %#v, want %q", got, want)
	}
	assertNoForbiddenPolicyPayload(t, attributes)
}

func TestScannerEmitsPolicyMemberAccountRelationships(t *testing.T) {
	policyARN := "arn:aws:fms:us-east-1:123456789012:policy/p-abc123"
	client := fakeClient{
		policies: []Policy{{
			ARN:                 policyARN,
			ID:                  "p-abc123",
			Name:                "org-wafv2-baseline",
			SecurityServiceType: "WAFV2",
			ResourceType:        "AWS::ElasticLoadBalancingV2::LoadBalancer",
		}},
		memberAccounts: map[string][]string{
			// Deliberately unordered and duplicated to prove the scanner sorts
			// and dedupes rather than keying identity on API order.
			"p-abc123": {"222222222222", "111111111111", "222222222222"},
		},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	first := relationshipByTarget(t, envelopes, awscloud.RelationshipFMSPolicyAppliesToAccount, "111111111111")
	if got, want := first.Payload["target_type"], awscloud.ResourceTypeOrganizationsAccount; got != want {
		t.Fatalf("target_type = %#v, want %q", got, want)
	}
	if got, want := first.Payload["source_resource_id"], policyARN; got != want {
		t.Fatalf("source_resource_id = %#v, want %q", got, want)
	}
	// The member-account edge is bare-id keyed: no synthesized target ARN, so it
	// joins the aws_organizations_account node the organizations scanner
	// publishes by bare 12-digit account id.
	if got, _ := first.Payload["target_arn"].(string); got != "" {
		t.Fatalf("target_arn = %q, want empty (bare-id keyed member account)", got)
	}
	relationshipByTarget(t, envelopes, awscloud.RelationshipFMSPolicyAppliesToAccount, "222222222222")

	if got := countRelationships(envelopes, awscloud.RelationshipFMSPolicyAppliesToAccount); got != 2 {
		t.Fatalf("member account relationship count = %d, want 2 (deduped)", got)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinContract(t *testing.T) {
	policy := Policy{
		ARN:                 "arn:aws:fms:us-east-1:123456789012:policy/p-abc123",
		ID:                  "p-abc123",
		Name:                "org-wafv2-baseline",
		SecurityServiceType: "WAFV2",
		ResourceType:        "AWS::ElasticLoadBalancingV2::LoadBalancer",
	}
	observation, ok := policyMemberAccountRelationship(testBoundary(), policy, "111111111111")
	if !ok {
		t.Fatalf("policyMemberAccountRelationship ok = false, want true")
	}
	relguard.AssertObservations(t, observation)
}

func TestScannerStableIdentityIgnoresMemberAccountOrder(t *testing.T) {
	policy := Policy{ARN: "arn:aws:fms:us-east-1:123456789012:policy/p-abc123", ID: "p-abc123"}
	forward, _ := policyMemberAccountRelationship(testBoundary(), policy, "111111111111")
	// The source record id must key on policy id and account id, never on a
	// list index, so re-ordering the API response yields the same identity.
	if forward.SourceRecordID != "p-abc123#account#111111111111" {
		t.Fatalf("SourceRecordID = %q, want stable policy#account key", forward.SourceRecordID)
	}
}

func TestScannerEmitsPartitionAwareARNsFromAPI(t *testing.T) {
	govARN := "arn:aws-us-gov:fms:us-gov-west-1:123456789012:policy/p-gov1"
	client := fakeClient{
		policies: []Policy{{
			ARN:                 govARN,
			ID:                  "p-gov1",
			Name:                "gov-baseline",
			SecurityServiceType: "NETWORK_FIREWALL",
			ResourceType:        "AWS::EC2::VPC",
		}},
		memberAccounts: map[string][]string{"p-gov1": {"333333333333"}},
	}

	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	policy := resourceByType(t, envelopes, awscloud.ResourceTypeFMSPolicy)
	// The scanner uses the AWS-reported ARN verbatim; it never hardcodes
	// arn:aws: so the GovCloud partition survives.
	if got, _ := policy.Payload["arn"].(string); got != govARN {
		t.Fatalf("arn = %q, want %q (partition preserved from API)", got, govARN)
	}
	relationship := relationshipByTarget(t, envelopes, awscloud.RelationshipFMSPolicyAppliesToAccount, "333333333333")
	if got, _ := relationship.Payload["source_arn"].(string); got != govARN {
		t.Fatalf("source_arn = %q, want %q", got, govARN)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceOrganizations

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want missing client error")
	}
}

func TestScannerDefaultsServiceKindWhenBlank(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = ""

	client := fakeClient{policies: []Policy{{ARN: "arn:aws:fms:us-east-1:123456789012:policy/p-1", ID: "p-1", Name: "n"}}}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	policy := resourceByType(t, envelopes, awscloud.ResourceTypeFMSPolicy)
	if got, _ := policy.Payload["service_kind"].(string); got != awscloud.ServiceFMS {
		t.Fatalf("service_kind = %q, want %q", got, awscloud.ServiceFMS)
	}
}

// assertNoForbiddenPolicyPayload proves the scanner never persists the policy
// rule payload (the SecurityServicePolicyData managed service data document) or
// account inclusion/exclusion maps.
func assertNoForbiddenPolicyPayload(t *testing.T, attributes map[string]any) {
	t.Helper()
	for _, forbidden := range []string{
		"security_service_policy_data",
		"managed_service_data",
		"policy_data",
		"rules",
		"include_map",
		"exclude_map",
		"resource_tags",
	} {
		if _, exists := attributes[forbidden]; exists {
			t.Fatalf("policy persisted %q; FMS rule payloads must never be stored", forbidden)
		}
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceFMS,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:fms:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	policies       []Policy
	memberAccounts map[string][]string
}

func (c fakeClient) ListPolicies(context.Context) ([]Policy, error) { return c.policies, nil }

func (c fakeClient) ListPolicyMemberAccounts(_ context.Context, policyID string) ([]string, error) {
	return c.memberAccounts[policyID], nil
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q in %#v", resourceType, envelopes)
	return facts.Envelope{}
}

// relationshipByTarget returns the relationship envelope matching the given
// relationship type and target resource id, failing the test if none exists.
func relationshipByTarget(t *testing.T, envelopes []facts.Envelope, relationshipType, targetID string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got != relationshipType {
			continue
		}
		if got, _ := envelope.Payload["target_resource_id"].(string); got == targetID {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q target %q in %#v", relationshipType, targetID, envelopes)
	return facts.Envelope{}
}

func countRelationships(envelopes []facts.Envelope, relationshipType string) int {
	count := 0
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			count++
		}
	}
	return count
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}
