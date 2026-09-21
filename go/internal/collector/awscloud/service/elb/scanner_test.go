// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package elb

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func sampleLoadBalancer() LoadBalancer {
	return LoadBalancer{
		Name:                      "web",
		DNSName:                   "web-123.us-east-1.elb.amazonaws.com",
		CanonicalHostedZoneName:   "web-123.us-east-1.elb.amazonaws.com",
		CanonicalHostedZoneNameID: "Z35SXDOTRQ7X7K",
		Scheme:                    "internet-facing",
		VPCID:                     "vpc-123",
		CreatedAt:                 time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
		AvailabilityZones:         []string{"us-east-1a", "us-east-1b"},
		Subnets:                   []string{"subnet-1", "subnet-2"},
		SecurityGroups:            []string{"sg-1"},
		SourceSecurityGroupName:   "amazon-elb-sg",
		InstanceIDs:               []string{"i-0abc", "i-0def"},
		Listeners: []Listener{
			{
				Protocol:         "HTTP",
				LoadBalancerPort: 80,
				InstanceProtocol: "HTTP",
				InstancePort:     8080,
			},
			{
				Protocol:         "HTTPS",
				LoadBalancerPort: 443,
				InstanceProtocol: "HTTP",
				InstancePort:     8080,
				SSLCertificateID: "arn:aws:acm:us-east-1:123456789012:certificate/abc",
			},
			{
				Protocol:         "SSL",
				LoadBalancerPort: 8443,
				InstanceProtocol: "SSL",
				InstancePort:     8443,
				SSLCertificateID: "arn:aws:iam::123456789012:server-certificate/legacy",
			},
		},
		HealthCheck: HealthCheck{
			Target:             "HTTP:8080/healthz",
			IntervalSeconds:    30,
			TimeoutSeconds:     5,
			HealthyThreshold:   3,
			UnhealthyThreshold: 2,
		},
		Tags: map[string]string{"service": "web"},
	}
}

func TestScannerEmitsLoadBalancerAndTopologyEdges(t *testing.T) {
	client := fakeClient{loadBalancers: []LoadBalancer{sampleLoadBalancer()}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	counts := factKindCounts(envelopes)
	if counts[facts.AWSResourceFactKind] != 1 {
		t.Fatalf("aws_resource count = %d, want 1", counts[facts.AWSResourceFactKind])
	}
	// 2 instances + 2 subnets + 1 security group + 1 vpc + 1 acm cert + 1 iam cert.
	if counts[facts.AWSRelationshipFactKind] != 8 {
		t.Fatalf("aws_relationship count = %d, want 8", counts[facts.AWSRelationshipFactKind])
	}

	loadBalancer := assertResourceType(t, envelopes, awscloud.ResourceTypeELBLoadBalancer)
	wantARN := "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/web"
	if got, _ := loadBalancer.Payload["resource_id"].(string); got != wantARN {
		t.Fatalf("resource_id = %q, want %q", got, wantARN)
	}
	if got, _ := loadBalancer.Payload["arn"].(string); got != wantARN {
		t.Fatalf("arn = %q, want %q", got, wantARN)
	}
	assertAttribute(t, loadBalancer, "dns_name", "web-123.us-east-1.elb.amazonaws.com")
	assertAttribute(t, loadBalancer, "scheme", "internet-facing")
}

func TestScannerRelationshipTargetsResolveToTargetScanners(t *testing.T) {
	client := fakeClient{loadBalancers: []LoadBalancer{sampleLoadBalancer()}}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	wantARN := "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/web"

	cases := []struct {
		relationshipType string
		targetType       string
		targetResourceID string
	}{
		{awscloud.RelationshipELBLoadBalancerRegistersInstance, "aws_ec2_instance", "i-0abc"},
		{awscloud.RelationshipELBLoadBalancerInSubnet, awscloud.ResourceTypeEC2Subnet, "subnet-1"},
		{awscloud.RelationshipELBLoadBalancerUsesSecurityGroup, awscloud.ResourceTypeEC2SecurityGroup, "sg-1"},
		{awscloud.RelationshipELBLoadBalancerInVPC, awscloud.ResourceTypeEC2VPC, "vpc-123"},
		{
			awscloud.RelationshipELBLoadBalancerUsesACMCertificate,
			awscloud.ResourceTypeACMCertificate,
			"arn:aws:acm:us-east-1:123456789012:certificate/abc",
		},
		{
			awscloud.RelationshipELBLoadBalancerUsesIAMServerCertificate,
			"aws_iam_server_certificate",
			"arn:aws:iam::123456789012:server-certificate/legacy",
		},
	}
	for _, tc := range cases {
		t.Run(tc.relationshipType, func(t *testing.T) {
			rel := assertRelationship(t, envelopes, tc.relationshipType, tc.targetResourceID)
			if got, _ := rel.Payload["target_type"].(string); got != tc.targetType {
				t.Fatalf("target_type = %q, want %q", got, tc.targetType)
			}
			if got, _ := rel.Payload["source_resource_id"].(string); got != wantARN {
				t.Fatalf("source_resource_id = %q, want %q", got, wantARN)
			}
		})
	}
}

func TestScannerEnforcesGraphJoinContract(t *testing.T) {
	observations := loadBalancerRelationships(
		testBoundary(),
		sampleLoadBalancer(),
		loadBalancerARN(testBoundary(), "web"),
	)
	if len(observations) == 0 {
		t.Fatal("expected relationship observations to validate")
	}
	relguard.AssertObservations(t, observations...)
}

func TestScannerExcludesLiveInstanceHealth(t *testing.T) {
	client := fakeClient{loadBalancers: []LoadBalancer{sampleLoadBalancer()}}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	loadBalancer := assertResourceType(t, envelopes, awscloud.ResourceTypeELBLoadBalancer)
	attributes, ok := loadBalancer.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", loadBalancer.Payload["attributes"])
	}
	for key := range attributes {
		lowered := strings.ToLower(key)
		if strings.Contains(lowered, "instance_health") || strings.Contains(lowered, "instance_state") {
			t.Fatalf("live instance health leaked into attributes: %q", key)
		}
	}
	if _, exists := attributes["instance_count"]; !exists {
		t.Fatal("expected aggregate instance_count attribute")
	}
}

func TestScannerOmitsCertificateBodies(t *testing.T) {
	client := fakeClient{loadBalancers: []LoadBalancer{sampleLoadBalancer()}}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	loadBalancer := assertResourceType(t, envelopes, awscloud.ResourceTypeELBLoadBalancer)
	attributes, _ := loadBalancer.Payload["attributes"].(map[string]any)
	listeners, ok := attributes["listeners"].([]map[string]any)
	if !ok || len(listeners) != 3 {
		t.Fatalf("listeners = %#v, want three listener maps", attributes["listeners"])
	}
	for _, listener := range listeners {
		for key := range listener {
			lowered := strings.ToLower(key)
			if strings.Contains(lowered, "body") || strings.Contains(lowered, "private") || strings.Contains(lowered, "pem") {
				t.Fatalf("certificate material leaked into listener attribute %q", key)
			}
		}
	}
}

func TestSynthesizedLoadBalancerARNIsPartitionAware(t *testing.T) {
	cases := []struct {
		name    string
		region  string
		account string
		lbName  string
		want    string
	}{
		{
			name:    "commercial",
			region:  "us-east-1",
			account: "123456789012",
			lbName:  "web",
			want:    "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/web",
		},
		{
			name:    "govcloud",
			region:  "us-gov-west-1",
			account: "123456789012",
			lbName:  "web",
			want:    "arn:aws-us-gov:elasticloadbalancing:us-gov-west-1:123456789012:loadbalancer/web",
		},
		{
			name:    "china",
			region:  "cn-north-1",
			account: "123456789012",
			lbName:  "web",
			want:    "arn:aws-cn:elasticloadbalancing:cn-north-1:123456789012:loadbalancer/web",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			boundary := awscloud.Boundary{AccountID: tc.account, Region: tc.region, ServiceKind: awscloud.ServiceELB}
			if got := loadBalancerARN(boundary, tc.lbName); got != tc.want {
				t.Fatalf("loadBalancerARN = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSynthesizedARNFlowsIntoGovCloudResourceAndEdges(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	client := fakeClient{loadBalancers: []LoadBalancer{sampleLoadBalancer()}}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	wantARN := "arn:aws-us-gov:elasticloadbalancing:us-gov-west-1:123456789012:loadbalancer/web"
	loadBalancer := assertResourceType(t, envelopes, awscloud.ResourceTypeELBLoadBalancer)
	if got, _ := loadBalancer.Payload["resource_id"].(string); got != wantARN {
		t.Fatalf("govcloud resource_id = %q, want %q", got, wantARN)
	}
	rel := assertRelationship(t, envelopes, awscloud.RelationshipELBLoadBalancerInVPC, "vpc-123")
	if got, _ := rel.Payload["source_resource_id"].(string); got != wantARN {
		t.Fatalf("govcloud edge source_resource_id = %q, want %q", got, wantARN)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceELBv2
	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceELB,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:elb:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	loadBalancers []LoadBalancer
}

func (c fakeClient) ListLoadBalancers(context.Context) ([]LoadBalancer, error) {
	return c.loadBalancers, nil
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

func assertRelationship(
	t *testing.T,
	envelopes []facts.Envelope,
	relationshipType string,
	targetResourceID string,
) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got != relationshipType {
			continue
		}
		if got, _ := envelope.Payload["target_resource_id"].(string); got == targetResourceID {
			return envelope
		}
	}
	t.Fatalf("missing relationship %q -> %q in %#v", relationshipType, targetResourceID, envelopes)
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
