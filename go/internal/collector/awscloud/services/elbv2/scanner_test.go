package elbv2

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsRoutingTopologyWithoutTargetHealth(t *testing.T) {
	client := fakeClient{
		loadBalancers: []LoadBalancer{{
			ARN:                   "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/api/abc",
			Name:                  "api",
			DNSName:               "api-123.us-east-1.elb.amazonaws.com",
			CanonicalHostedZoneID: "Z35SXDOTRQ7X7K",
			Scheme:                "internet-facing",
			Type:                  "application",
			State:                 "active",
			VPCID:                 "vpc-123",
			IPAddressType:         "ipv4",
			CreatedAt:             time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
			AvailabilityZones: []AvailabilityZone{{
				Name:     "us-east-1a",
				SubnetID: "subnet-1",
			}},
			SecurityGroups: []string{"sg-1"},
			Tags:           map[string]string{"service": "api"},
		}},
		listeners: map[string][]Listener{
			"arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/api/abc": {
				{
					ARN:             "arn:aws:elasticloadbalancing:us-east-1:123456789012:listener/app/api/abc/def",
					LoadBalancerARN: "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/api/abc",
					Protocol:        "HTTPS",
					Port:            443,
					DefaultActions: []Action{{
						Type:           "forward",
						TargetGroupARN: "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/api/aaa",
					}},
				},
			},
		},
		rules: map[string][]Rule{
			"arn:aws:elasticloadbalancing:us-east-1:123456789012:listener/app/api/abc/def": {
				{
					ARN:         "arn:aws:elasticloadbalancing:us-east-1:123456789012:listener-rule/app/api/abc/def/rule",
					ListenerARN: "arn:aws:elasticloadbalancing:us-east-1:123456789012:listener/app/api/abc/def",
					Priority:    "10",
					Conditions: []Condition{{
						Field:            "host-header",
						HostHeaderValues: []string{"api.example.com"},
					}},
					Actions: []Action{{
						Type: "forward",
						ForwardTargetGroups: []WeightedTargetGroup{{
							ARN:    "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/api/aaa",
							Weight: 100,
						}},
					}},
				},
			},
		},
		targetGroups: []TargetGroup{{
			ARN:              "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/api/aaa",
			Name:             "api",
			Protocol:         "HTTP",
			Port:             8080,
			TargetType:       "ip",
			VPCID:            "vpc-123",
			LoadBalancerARNs: []string{"arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/api/abc"},
			HealthCheck: HealthCheck{
				Enabled:            true,
				Protocol:           "HTTP",
				Path:               "/healthz",
				Port:               "traffic-port",
				IntervalSeconds:    30,
				TimeoutSeconds:     5,
				HealthyThreshold:   5,
				UnhealthyThreshold: 2,
				Matcher:            "200-399",
			},
			Tags: map[string]string{"service": "api"},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	counts := factKindCounts(envelopes)
	if counts[facts.AWSResourceFactKind] != 4 {
		t.Fatalf("aws_resource count = %d, want 4", counts[facts.AWSResourceFactKind])
	}
	if counts[facts.AWSRelationshipFactKind] != 4 {
		t.Fatalf("aws_relationship count = %d, want 4", counts[facts.AWSRelationshipFactKind])
	}

	loadBalancer := assertResourceType(t, envelopes, awscloud.ResourceTypeELBv2LoadBalancer)
	assertAttribute(t, loadBalancer, "dns_name", "api-123.us-east-1.elb.amazonaws.com")
	rule := assertResourceType(t, envelopes, awscloud.ResourceTypeELBv2Rule)
	assertRuleConditions(t, rule)
	targetGroup := assertResourceType(t, envelopes, awscloud.ResourceTypeELBv2TargetGroup)
	assertHealthCheck(t, targetGroup)
	assertNoTargetHealth(t, targetGroup)
	assertRelationship(t, envelopes, awscloud.RelationshipELBv2LoadBalancerHasListener)
	assertRelationship(t, envelopes, awscloud.RelationshipELBv2ListenerHasRule)
	assertRelationship(t, envelopes, awscloud.RelationshipELBv2TargetGroupAttachedToLoadBalancer)
	route := assertRelationship(t, envelopes, awscloud.RelationshipELBv2ListenerRoutesToTargetGroup)
	assertRouteEvidence(t, route)
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceECS
	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceELBv2,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:elbv2:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	loadBalancers []LoadBalancer
	listeners     map[string][]Listener
	rules         map[string][]Rule
	targetGroups  []TargetGroup
}

func (c fakeClient) ListLoadBalancers(context.Context) ([]LoadBalancer, error) {
	return c.loadBalancers, nil
}

func (c fakeClient) ListListeners(_ context.Context, loadBalancer LoadBalancer) ([]Listener, error) {
	return c.listeners[loadBalancer.ARN], nil
}

func (c fakeClient) ListRules(_ context.Context, listener Listener) ([]Rule, error) {
	return c.rules[listener.ARN], nil
}

func (c fakeClient) ListTargetGroups(context.Context) ([]TargetGroup, error) {
	return c.targetGroups, nil
}

func factKindCounts(envelopes []facts.Envelope) map[string]int {
	counts := make(map[string]int)
	for _, envelope := range envelopes {
		counts[envelope.FactKind]++
	}
	return counts
}

func assertResourceType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
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

func assertRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
	return facts.Envelope{}
}

func assertAttribute(t *testing.T, envelope facts.Envelope, key string, want string) {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	if got, _ := attributes[key].(string); got != want {
		t.Fatalf("attribute %s = %q, want %q", key, got, want)
	}
}

func assertRuleConditions(t *testing.T, envelope facts.Envelope) {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	conditions, ok := attributes["conditions"].([]map[string]any)
	if !ok || len(conditions) != 1 {
		t.Fatalf("conditions = %#v, want one typed condition", attributes["conditions"])
	}
	values, ok := conditions[0]["host_header_values"].([]string)
	if !ok || strings.Join(values, ",") != "api.example.com" {
		t.Fatalf("host_header_values = %#v, want api.example.com", conditions[0]["host_header_values"])
	}
}

func assertNoTargetHealth(t *testing.T, envelope facts.Envelope) {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	for key := range attributes {
		if strings.Contains(strings.ToLower(key), "target_health") {
			t.Fatalf("target health leaked into attributes: %#v", attributes)
		}
	}
}

func assertHealthCheck(t *testing.T, envelope facts.Envelope) {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	healthCheck, ok := attributes["health_check"].(map[string]any)
	if !ok {
		t.Fatalf("health_check = %#v, want map", attributes["health_check"])
	}
	if got, _ := healthCheck["protocol"].(string); got != "HTTP" {
		t.Fatalf("health_check.protocol = %q, want HTTP", got)
	}
	if got, _ := healthCheck["path"].(string); got != "/healthz" {
		t.Fatalf("health_check.path = %q, want /healthz", got)
	}
	if got, _ := healthCheck["interval_seconds"].(int32); got != 30 {
		t.Fatalf("health_check.interval_seconds = %d, want 30", got)
	}
}

func assertRouteEvidence(t *testing.T, envelope facts.Envelope) {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	routes, ok := attributes["routes"].([]map[string]any)
	if !ok || len(routes) != 2 {
		t.Fatalf("routes = %#v, want default and rule route evidence", attributes["routes"])
	}
}
