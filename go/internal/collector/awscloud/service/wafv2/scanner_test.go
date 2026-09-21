// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package wafv2

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsWebACLMetadataAndAllRelationshipKinds(t *testing.T) {
	webACLARN := "arn:aws:wafv2:us-east-1:123456789012:regional/webacl/edge/abc"
	ruleGroupARN := "arn:aws:wafv2:us-east-1:123456789012:regional/rulegroup/custom/rg1"
	ipSetARN := "arn:aws:wafv2:us-east-1:123456789012:regional/ipset/blocklist/ip1"
	regexSetARN := "arn:aws:wafv2:us-east-1:123456789012:regional/regexpatternset/badpaths/rx1"
	albARN := "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/web/1234"

	client := fakeClient{
		webACLs: []WebACL{{
			ARN:              webACLARN,
			ID:               "abc",
			Name:             "edge",
			Description:      "edge protection",
			Scope:            "REGIONAL",
			RuleCount:        3,
			Capacity:         500,
			DefaultAction:    "Allow",
			Tags:             map[string]string{"Environment": "prod"},
			RuleGroupRefARNs: []string{ruleGroupARN},
			IPSetRefARNs:     []string{ipSetARN},
			RegexSetRefARNs:  []string{regexSetARN},
			ManagedRuleSetRefs: []ManagedRuleSetRef{{
				VendorName: "AWS",
				Name:       "AWSManagedRulesCommonRuleSet",
				Version:    "Version_1.0",
			}},
			ProtectedResources: []ProtectedResource{{
				ARN:          albARN,
				ResourceType: "APPLICATION_LOAD_BALANCER",
			}},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	webACL := resourceByType(t, envelopes, awscloud.ResourceTypeWAFv2WebACL)
	attributes := attributesOf(t, webACL)
	if got, want := attributes["scope"], "REGIONAL"; got != want {
		t.Fatalf("scope = %#v, want %q", got, want)
	}
	if got, want := attributes["rule_count"], 3; got != want {
		t.Fatalf("rule_count = %#v, want %d", got, want)
	}
	if got, want := attributes["default_action"], "Allow"; got != want {
		t.Fatalf("default_action = %#v, want %q", got, want)
	}
	managed, ok := attributes["managed_rule_set_refs"].([]map[string]any)
	if !ok || len(managed) != 1 {
		t.Fatalf("managed_rule_set_refs = %#v, want one ref", attributes["managed_rule_set_refs"])
	}
	if got, want := managed[0]["vendor_name"], "AWS"; got != want {
		t.Fatalf("managed vendor_name = %#v, want %q", got, want)
	}
	if got, want := managed[0]["name"], "AWSManagedRulesCommonRuleSet"; got != want {
		t.Fatalf("managed name = %#v, want %q", got, want)
	}
	assertNoForbiddenWebACLPayload(t, attributes)

	assertRelationship(t, envelopes, awscloud.RelationshipWAFv2WebACLProtectsResource, albARN)
	assertRelationship(t, envelopes, awscloud.RelationshipWAFv2WebACLUsesRuleGroup, ruleGroupARN)
	assertRelationship(t, envelopes, awscloud.RelationshipWAFv2WebACLUsesIPSet, ipSetARN)
	assertRelationship(t, envelopes, awscloud.RelationshipWAFv2WebACLUsesRegexPatternSet, regexSetARN)
}

func TestScannerSetsProtectedResourceTargetType(t *testing.T) {
	const account = "123456789012"
	cases := []struct {
		name         string
		resourceType string
		targetARN    string
		wantType     string
	}{
		{
			name:         "alb",
			resourceType: "APPLICATION_LOAD_BALANCER",
			targetARN:    "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/web/1234",
			wantType:     awscloud.ResourceTypeELBv2LoadBalancer,
		},
		{
			name:         "api_gateway",
			resourceType: "API_GATEWAY",
			targetARN:    "arn:aws:apigateway:us-east-1::/restapis/abc123/stages/prod",
			wantType:     awscloud.ResourceTypeAPIGatewayStage,
		},
		{
			name:         "appsync",
			resourceType: "APPSYNC",
			targetARN:    "arn:aws:appsync:us-east-1:123456789012:apis/abcd1234",
			wantType:     "aws_resource",
		},
		{
			name:         "cognito_user_pool",
			resourceType: "COGNITO_USER_POOL",
			targetARN:    "arn:aws:cognito-idp:us-east-1:123456789012:userpool/us-east-1_abc",
			wantType:     "aws_resource",
		},
		{
			name:         "app_runner",
			resourceType: "APP_RUNNER_SERVICE",
			targetARN:    "arn:aws:apprunner:us-east-1:123456789012:service/web/abc",
			wantType:     "aws_resource",
		},
		{
			name:         "amplify",
			resourceType: "AMPLIFY",
			targetARN:    "arn:aws:amplify:us-east-1:123456789012:apps/d123/branches/main",
			wantType:     "aws_resource",
		},
		{
			name:         "verified_access",
			resourceType: "VERIFIED_ACCESS_INSTANCE",
			targetARN:    "arn:aws:ec2:us-east-1:123456789012:verified-access-instance/vai-abc",
			wantType:     "aws_resource",
		},
		{
			name:         "cloudfront",
			resourceType: "",
			targetARN:    "arn:aws:cloudfront::123456789012:distribution/E123",
			wantType:     awscloud.ResourceTypeCloudFrontDistribution,
		},
		{
			name:         "unknown_arn",
			resourceType: "SOMETHING_NEW",
			targetARN:    "arn:aws:mystery:us-east-1:123456789012:thing/abc",
			wantType:     "aws_resource",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := fakeClient{
				webACLs: []WebACL{{
					ARN:   "arn:aws:wafv2:us-east-1:" + account + ":regional/webacl/edge/abc",
					ID:    "abc",
					Name:  "edge",
					Scope: "REGIONAL",
					ProtectedResources: []ProtectedResource{{
						ARN:          tc.targetARN,
						ResourceType: tc.resourceType,
					}},
				}},
			}

			envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
			if err != nil {
				t.Fatalf("Scan() error = %v, want nil", err)
			}

			relationship := relationshipByTarget(t, envelopes, awscloud.RelationshipWAFv2WebACLProtectsResource, tc.targetARN)
			if got, _ := relationship.Payload["target_type"].(string); got != tc.wantType {
				t.Fatalf("target_type = %q, want %q", got, tc.wantType)
			}
		})
	}
}

func TestScannerEmitsIPSetCountOnlyNeverAddresses(t *testing.T) {
	client := fakeClient{
		ipSets: []IPSet{{
			ARN:          "arn:aws:wafv2:us-east-1:123456789012:regional/ipset/blocklist/ip1",
			ID:           "ip1",
			Name:         "blocklist",
			Scope:        "REGIONAL",
			IPVersion:    "IPV4",
			AddressCount: 42,
			Tags:         map[string]string{"team": "sec"},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	ipSet := resourceByType(t, envelopes, awscloud.ResourceTypeWAFv2IPSet)
	attributes := attributesOf(t, ipSet)
	if got, want := attributes["address_count"], 42; got != want {
		t.Fatalf("address_count = %#v, want %d", got, want)
	}
	if got, want := attributes["ip_version"], "IPV4"; got != want {
		t.Fatalf("ip_version = %#v, want %q", got, want)
	}
	for _, forbidden := range []string{"addresses", "address_list", "cidrs", "ip_addresses"} {
		if _, exists := attributes[forbidden]; exists {
			t.Fatalf("IP set persisted %q; scanner must store the count only", forbidden)
		}
	}
}

func TestScannerEmitsRegexSetCountOnlyNeverBodies(t *testing.T) {
	client := fakeClient{
		regexSets: []RegexPatternSet{{
			ARN:          "arn:aws:wafv2:us-east-1:123456789012:regional/regexpatternset/badpaths/rx1",
			ID:           "rx1",
			Name:         "badpaths",
			Scope:        "REGIONAL",
			PatternCount: 7,
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	regexSet := resourceByType(t, envelopes, awscloud.ResourceTypeWAFv2RegexPatternSet)
	attributes := attributesOf(t, regexSet)
	if got, want := attributes["pattern_count"], 7; got != want {
		t.Fatalf("pattern_count = %#v, want %d", got, want)
	}
	for _, forbidden := range []string{"patterns", "regular_expression_list", "regex", "bodies"} {
		if _, exists := attributes[forbidden]; exists {
			t.Fatalf("regex set persisted %q; scanner must store the count only", forbidden)
		}
	}
}

func TestScannerEmitsCustomerRuleGroupMetadata(t *testing.T) {
	client := fakeClient{
		ruleGroups: []RuleGroup{{
			ARN:       "arn:aws:wafv2:us-east-1:123456789012:regional/rulegroup/custom/rg1",
			ID:        "rg1",
			Name:      "custom",
			Scope:     "REGIONAL",
			RuleCount: 4,
			Capacity:  200,
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	ruleGroup := resourceByType(t, envelopes, awscloud.ResourceTypeWAFv2RuleGroup)
	attributes := attributesOf(t, ruleGroup)
	if got, want := attributes["rule_count"], 4; got != want {
		t.Fatalf("rule_count = %#v, want %d", got, want)
	}
	if got, want := attributes["capacity"], int64(200); got != want {
		t.Fatalf("capacity = %#v, want %d", got, want)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceCloudFront

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

func assertNoForbiddenWebACLPayload(t *testing.T, attributes map[string]any) {
	t.Helper()
	for _, forbidden := range []string{
		"rules",
		"statement",
		"statements",
		"and_statement",
		"or_statement",
		"not_statement",
		"byte_match_statement",
		"search_string",
	} {
		if _, exists := attributes[forbidden]; exists {
			t.Fatalf("web ACL persisted %q; rule Statement bodies must never be stored", forbidden)
		}
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceWAFv2,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:wafv2:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	webACLs    []WebACL
	ruleGroups []RuleGroup
	ipSets     []IPSet
	regexSets  []RegexPatternSet
}

func (c fakeClient) ListWebACLs(context.Context) ([]WebACL, error) { return c.webACLs, nil }

func (c fakeClient) ListRuleGroups(context.Context) ([]RuleGroup, error) { return c.ruleGroups, nil }

func (c fakeClient) ListIPSets(context.Context) ([]IPSet, error) { return c.ipSets, nil }

func (c fakeClient) ListRegexPatternSets(context.Context) ([]RegexPatternSet, error) {
	return c.regexSets, nil
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

func assertRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType, targetID string) {
	t.Helper()
	relationshipByTarget(t, envelopes, relationshipType, targetID)
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

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}
