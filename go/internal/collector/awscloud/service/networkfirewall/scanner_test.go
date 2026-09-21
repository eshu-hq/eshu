// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package networkfirewall

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsFirewallMetadataAndRelationships(t *testing.T) {
	firewallARN := "arn:aws:network-firewall:us-east-1:123456789012:firewall/edge"
	policyARN := "arn:aws:network-firewall:us-east-1:123456789012:firewall-policy/edge-policy"

	client := fakeClient{
		firewalls: []Firewall{{
			ARN:                            firewallARN,
			ID:                             "fw-123",
			Name:                           "edge",
			Description:                    "perimeter firewall",
			VPCID:                          "vpc-aaa",
			FirewallPolicyARN:              policyARN,
			SubnetIDs:                      []string{"subnet-aaa", "subnet-bbb"},
			DeleteProtection:               true,
			SubnetChangeProtection:         true,
			FirewallPolicyChangeProtection: false,
			Status:                         "READY",
			ConfigurationSyncState:         "IN_SYNC",
			NumberOfAssociations:           2,
			Tags:                           map[string]string{"Environment": "prod"},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	firewall := resourceByType(t, envelopes, awscloud.ResourceTypeNetworkFirewallFirewall)
	attributes := attributesOf(t, firewall)
	if got, want := attributes["vpc_id"], "vpc-aaa"; got != want {
		t.Fatalf("vpc_id = %#v, want %q", got, want)
	}
	if got, want := attributes["status"], "READY"; got != want {
		t.Fatalf("status = %#v, want %q", got, want)
	}
	if got, want := attributes["delete_protection"], true; got != want {
		t.Fatalf("delete_protection = %#v, want %v", got, want)
	}
	if got, want := attributes["subnet_count"], 2; got != want {
		t.Fatalf("subnet_count = %#v, want %d", got, want)
	}

	vpcEdge := relationshipByTarget(t, envelopes, awscloud.RelationshipNetworkFirewallFirewallInVPC, "vpc-aaa")
	if got, want := targetType(vpcEdge), awscloud.ResourceTypeEC2VPC; got != want {
		t.Fatalf("firewall-to-VPC target_type = %q, want %q", got, want)
	}

	subnetEdge := relationshipByTarget(t, envelopes, awscloud.RelationshipNetworkFirewallFirewallUsesSubnet, "subnet-aaa")
	if got, want := targetType(subnetEdge), awscloud.ResourceTypeEC2Subnet; got != want {
		t.Fatalf("firewall-to-subnet target_type = %q, want %q", got, want)
	}
	relationshipByTarget(t, envelopes, awscloud.RelationshipNetworkFirewallFirewallUsesSubnet, "subnet-bbb")

	policyEdge := relationshipByTarget(t, envelopes, awscloud.RelationshipNetworkFirewallFirewallUsesPolicy, policyARN)
	if got, want := targetType(policyEdge), awscloud.ResourceTypeNetworkFirewallPolicy; got != want {
		t.Fatalf("firewall-to-policy target_type = %q, want %q", got, want)
	}
}

func TestScannerEmitsFirewallPolicyMetadataAndReferences(t *testing.T) {
	policyARN := "arn:aws:network-firewall:us-east-1:123456789012:firewall-policy/edge-policy"
	statefulRGARN := "arn:aws:network-firewall:us-east-1:123456789012:stateful-rulegroup/threats"
	statelessRGARN := "arn:aws:network-firewall:us-east-1:123456789012:stateless-rulegroup/allowlist"
	tlsARN := "arn:aws:network-firewall:us-east-1:123456789012:tls-configuration/inspect"

	client := fakeClient{
		policies: []FirewallPolicy{{
			ARN:                             policyARN,
			ID:                              "fp-123",
			Name:                            "edge-policy",
			Description:                     "edge inspection policy",
			Status:                          "ACTIVE",
			StatelessDefaultActions:         []string{"aws:forward_to_sfe"},
			StatelessFragmentDefaultActions: []string{"aws:drop"},
			StatefulDefaultActions:          []string{"aws:drop_strict"},
			RuleGroupARNs:                   []string{statefulRGARN, statelessRGARN},
			TLSInspectionConfigurationARN:   tlsARN,
			NumberOfAssociations:            1,
			Tags:                            map[string]string{"team": "sec"},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	policy := resourceByType(t, envelopes, awscloud.ResourceTypeNetworkFirewallPolicy)
	attributes := attributesOf(t, policy)
	if got, want := attributes["status"], "ACTIVE"; got != want {
		t.Fatalf("status = %#v, want %q", got, want)
	}
	stateless, ok := attributes["stateless_default_actions"].([]string)
	if !ok || len(stateless) != 1 || stateless[0] != "aws:forward_to_sfe" {
		t.Fatalf("stateless_default_actions = %#v, want [aws:forward_to_sfe]", attributes["stateless_default_actions"])
	}
	stateful, ok := attributes["stateful_default_actions"].([]string)
	if !ok || len(stateful) != 1 || stateful[0] != "aws:drop_strict" {
		t.Fatalf("stateful_default_actions = %#v, want [aws:drop_strict]", attributes["stateful_default_actions"])
	}
	assertNoForbiddenPolicyPayload(t, attributes)

	statefulEdge := relationshipByTarget(t, envelopes, awscloud.RelationshipNetworkFirewallPolicyUsesRuleGroup, statefulRGARN)
	if got, want := targetType(statefulEdge), awscloud.ResourceTypeNetworkFirewallRuleGroup; got != want {
		t.Fatalf("policy-to-rule-group target_type = %q, want %q", got, want)
	}
	relationshipByTarget(t, envelopes, awscloud.RelationshipNetworkFirewallPolicyUsesRuleGroup, statelessRGARN)

	tlsEdge := relationshipByTarget(t, envelopes, awscloud.RelationshipNetworkFirewallPolicyUsesTLSInspectionConfiguration, tlsARN)
	if got, want := targetType(tlsEdge), awscloud.ResourceTypeNetworkFirewallTLSInspectionConfiguration; got != want {
		t.Fatalf("policy-to-TLS target_type = %q, want %q", got, want)
	}
}

func TestScannerEmitsRuleGroupMetadataNeverRuleBodies(t *testing.T) {
	client := fakeClient{
		ruleGroups: []RuleGroup{{
			ARN:                  "arn:aws:network-firewall:us-east-1:123456789012:stateful-rulegroup/threats",
			ID:                   "rg-123",
			Name:                 "threats",
			Description:          "managed threat signatures",
			Type:                 "STATEFUL",
			Capacity:             1000,
			NumberOfAssociations: 3,
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	ruleGroup := resourceByType(t, envelopes, awscloud.ResourceTypeNetworkFirewallRuleGroup)
	attributes := attributesOf(t, ruleGroup)
	if got, want := attributes["type"], "STATEFUL"; got != want {
		t.Fatalf("type = %#v, want %q", got, want)
	}
	if got, want := attributes["capacity"], int32(1000); got != want {
		t.Fatalf("capacity = %#v, want %d", got, want)
	}
	assertNoForbiddenRuleGroupPayload(t, attributes)
}

func TestScannerEmitsTLSInspectionConfigurationMetadata(t *testing.T) {
	client := fakeClient{
		tlsConfigs: []TLSInspectionConfiguration{{
			ARN:                  "arn:aws:network-firewall:us-east-1:123456789012:tls-configuration/inspect",
			ID:                   "tls-123",
			Name:                 "inspect",
			Description:          "outbound TLS inspection",
			Status:               "ACTIVE",
			NumberOfAssociations: 1,
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	tlsConfig := resourceByType(t, envelopes, awscloud.ResourceTypeNetworkFirewallTLSInspectionConfiguration)
	attributes := attributesOf(t, tlsConfig)
	if got, want := attributes["status"], "ACTIVE"; got != want {
		t.Fatalf("status = %#v, want %q", got, want)
	}
	for _, forbidden := range []string{"certificate", "certificates", "certificate_authority", "scopes", "ssl_context"} {
		if _, exists := attributes[forbidden]; exists {
			t.Fatalf("TLS config persisted %q; only aggregate metadata is allowed", forbidden)
		}
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceVPC

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

func assertNoForbiddenPolicyPayload(t *testing.T, attributes map[string]any) {
	t.Helper()
	for _, forbidden := range []string{
		"rules", "rule_source", "rules_source", "policy", "stateful_rules",
		"stateless_rules", "suricata", "rule_string",
	} {
		if _, exists := attributes[forbidden]; exists {
			t.Fatalf("policy persisted %q; the full policy rule body must never be stored", forbidden)
		}
	}
}

func assertNoForbiddenRuleGroupPayload(t *testing.T, attributes map[string]any) {
	t.Helper()
	for _, forbidden := range []string{
		"rules", "rule_source", "rules_source", "rules_string", "suricata",
		"stateful_rules", "stateless_rules", "rule_variables", "rule_definition",
	} {
		if _, exists := attributes[forbidden]; exists {
			t.Fatalf("rule group persisted %q; Suricata signature bodies must never be stored", forbidden)
		}
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceNetworkFirewall,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:networkfirewall:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	firewalls  []Firewall
	policies   []FirewallPolicy
	ruleGroups []RuleGroup
	tlsConfigs []TLSInspectionConfiguration
}

func (c fakeClient) ListFirewalls(context.Context) ([]Firewall, error) { return c.firewalls, nil }

func (c fakeClient) ListFirewallPolicies(context.Context) ([]FirewallPolicy, error) {
	return c.policies, nil
}

func (c fakeClient) ListRuleGroups(context.Context) ([]RuleGroup, error) { return c.ruleGroups, nil }

func (c fakeClient) ListTLSInspectionConfigurations(context.Context) ([]TLSInspectionConfiguration, error) {
	return c.tlsConfigs, nil
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

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

func targetType(envelope facts.Envelope) string {
	value, _ := envelope.Payload["target_type"].(string)
	return value
}
