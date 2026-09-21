// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shield

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testProtectionELBARN    = "arn:aws:shield::123456789012:protection/elb-1111"
	testProtectionCFARN     = "arn:aws:shield::123456789012:protection/cf-2222"
	testProtectionEIPARN    = "arn:aws:shield::123456789012:protection/eip-3333"
	testProtectionZoneARN   = "arn:aws:shield::123456789012:protection/zone-4444"
	testProtectionGAARN     = "arn:aws:shield::123456789012:protection/ga-5555"
	testProtectionOtherARN  = "arn:aws:shield::123456789012:protection/other-6666"
	testLoadBalancerARN     = "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/web/abc123"
	testDistributionARN     = "arn:aws:cloudfront::123456789012:distribution/E2EXAMPLE"
	testElasticIPARN        = "arn:aws:ec2:us-east-1:123456789012:eip-allocation/eipalloc-0a1b2c3d4e5f"
	testElasticIPAllocation = "eipalloc-0a1b2c3d4e5f"
	testHostedZoneARN       = "arn:aws:route53:::hostedzone/Z1234567890ABC"
	testHostedZoneID        = "Z1234567890ABC"
	testAcceleratorARN      = "arn:aws:globalaccelerator::123456789012:accelerator/abcd1234"
	testQueueARN            = "arn:aws:sqs:us-east-1:123456789012:not-protectable"
)

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceShield,
		ScopeID:             "scope-1",
		GenerationID:        "gen-1",
		CollectorInstanceID: "collector-1",
		FencingToken:        1,
		ObservedAt:          time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC),
	}
}

func allProtections() []Protection {
	return []Protection{
		{ARN: testProtectionELBARN, ID: "elb-1111", Name: "elb-protection", ResourceARN: testLoadBalancerARN},
		{ARN: testProtectionCFARN, ID: "cf-2222", Name: "cf-protection", ResourceARN: testDistributionARN},
		{ARN: testProtectionEIPARN, ID: "eip-3333", Name: "eip-protection", ResourceARN: testElasticIPARN},
		{ARN: testProtectionZoneARN, ID: "zone-4444", Name: "zone-protection", ResourceARN: testHostedZoneARN},
		{ARN: testProtectionGAARN, ID: "ga-5555", Name: "ga-protection", ResourceARN: testAcceleratorARN},
		{ARN: testProtectionOtherARN, ID: "other-6666", Name: "other-protection", ResourceARN: testQueueARN},
	}
}

func TestScannerEmitsProtectionResources(t *testing.T) {
	scanner := Scanner{Client: fakeClient{protections: allProtections()}}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	protections := allResources(envelopes, awscloud.ResourceTypeShieldProtection)
	if got, want := len(protections), 6; got != want {
		t.Fatalf("protection resource count = %d, want %d", got, want)
	}

	byID := map[string]map[string]any{}
	for _, protection := range protections {
		byID[protection.Payload["resource_id"].(string)] = protection.Payload
	}
	payload, ok := byID[testProtectionELBARN]
	if !ok {
		t.Fatalf("missing protection resource for %q", testProtectionELBARN)
	}
	if got := payload["arn"]; got != testProtectionELBARN {
		t.Fatalf("protection arn = %v, want %v", got, testProtectionELBARN)
	}
	attrs := payload["attributes"].(map[string]any)
	if got := attrs["protected_resource_arn"]; got != testLoadBalancerARN {
		t.Fatalf("protection protected_resource_arn = %v, want %v", got, testLoadBalancerARN)
	}
	if got := attrs["id"]; got != "elb-1111" {
		t.Fatalf("protection id = %v, want elb-1111", got)
	}
}

func TestScannerClassifiesProtectedResourceTargets(t *testing.T) {
	scanner := Scanner{Client: fakeClient{protections: allProtections()}}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	edges := allRelationships(envelopes, awscloud.RelationshipShieldProtectionProtectsResource)
	// Five recognized families emit an edge; the SQS protection is skipped.
	if got, want := len(edges), 5; got != want {
		t.Fatalf("protection edge count = %d, want %d", got, want)
	}

	bySource := map[string]map[string]any{}
	for _, edge := range edges {
		bySource[edge.Payload["source_resource_id"].(string)] = edge.Payload
	}

	cases := []struct {
		name           string
		sourceID       string
		wantTargetType string
		wantTargetID   string
		wantTargetARN  string
	}{
		{"elbv2", testProtectionELBARN, awscloud.ResourceTypeELBv2LoadBalancer, testLoadBalancerARN, testLoadBalancerARN},
		{"cloudfront", testProtectionCFARN, awscloud.ResourceTypeCloudFrontDistribution, testDistributionARN, testDistributionARN},
		{"eip", testProtectionEIPARN, awscloud.ResourceTypeVPCElasticIP, testElasticIPAllocation, ""},
		{"route53", testProtectionZoneARN, awscloud.ResourceTypeRoute53HostedZone, "/hostedzone/" + testHostedZoneID, ""},
		{"globalaccelerator", testProtectionGAARN, awscloud.ResourceTypeGlobalAcceleratorAccelerator, testAcceleratorARN, testAcceleratorARN},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, ok := bySource[tc.sourceID]
			if !ok {
				t.Fatalf("missing protection edge for %q", tc.sourceID)
			}
			if got := payload["target_type"]; got != tc.wantTargetType {
				t.Fatalf("target_type = %v, want %v", got, tc.wantTargetType)
			}
			if got := payload["target_resource_id"]; got != tc.wantTargetID {
				t.Fatalf("target_resource_id = %v, want %v", got, tc.wantTargetID)
			}
			if got := payload["target_arn"]; got != tc.wantTargetARN {
				t.Fatalf("target_arn = %v, want %q", got, tc.wantTargetARN)
			}
		})
	}
}

func TestScannerSkipsUnrecognizedProtectedResource(t *testing.T) {
	scanner := Scanner{Client: fakeClient{protections: []Protection{
		{ARN: testProtectionOtherARN, ID: "other-6666", Name: "other-protection", ResourceARN: testQueueARN},
	}}}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := len(allResources(envelopes, awscloud.ResourceTypeShieldProtection)); got != 1 {
		t.Fatalf("protection resource count = %d, want 1", got)
	}
	if edges := allRelationships(envelopes, awscloud.RelationshipShieldProtectionProtectsResource); len(edges) != 0 {
		t.Fatalf("unrecognized protected ARN emitted %d edges, want 0", len(edges))
	}
}

func TestScannerSkipsProtectionWithoutProtectedARN(t *testing.T) {
	scanner := Scanner{Client: fakeClient{protections: []Protection{
		{ARN: testProtectionELBARN, ID: "elb-1111", Name: "no-target", ResourceARN: ""},
	}}}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if edges := allRelationships(envelopes, awscloud.RelationshipShieldProtectionProtectsResource); len(edges) != 0 {
		t.Fatalf("protection with empty protected ARN emitted %d edges, want 0", len(edges))
	}
}

func TestScannerEmitsSubscriptionResource(t *testing.T) {
	scanner := Scanner{Client: fakeClient{
		protections:  nil,
		subscription: &Subscription{ARN: "arn:aws:shield::123456789012:subscription", State: "ACTIVE", AutoRenew: "ENABLED"},
	}}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	subscription := assertResource(t, envelopes, awscloud.ResourceTypeShieldSubscription)
	if got := subscription.Payload["state"]; got != "ACTIVE" {
		t.Fatalf("subscription state = %v, want ACTIVE", got)
	}
	attrs := subscription.Payload["attributes"].(map[string]any)
	if got := attrs["auto_renew"]; got != "ENABLED" {
		t.Fatalf("subscription auto_renew = %v, want ENABLED", got)
	}
	if got := attrs["state"]; got != "ACTIVE" {
		t.Fatalf("subscription attribute state = %v, want ACTIVE", got)
	}
}

func TestScannerEmitsSubscriptionResourceWithoutARN(t *testing.T) {
	scanner := Scanner{Client: fakeClient{
		subscription: &Subscription{State: "INACTIVE", AutoRenew: "DISABLED"},
	}}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	subscription := assertResource(t, envelopes, awscloud.ResourceTypeShieldSubscription)
	if got := subscription.Payload["resource_id"]; got != "shield-subscription/123456789012" {
		t.Fatalf("subscription resource_id = %v, want shield-subscription/123456789012", got)
	}
}

func TestScannerOmitsSubscriptionWhenAbsent(t *testing.T) {
	scanner := Scanner{Client: fakeClient{protections: allProtections(), subscription: nil}}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := len(allResources(envelopes, awscloud.ResourceTypeShieldSubscription)); got != 0 {
		t.Fatalf("subscription resource count = %d, want 0", got)
	}
}

func TestScannerAllRelationshipsSatisfyGraphJoinContract(t *testing.T) {
	observations := make([]awscloud.RelationshipObservation, 0)
	for _, protection := range allProtections() {
		if rel := protectionRelationship(testBoundary(), protection); rel != nil {
			observations = append(observations, *rel)
		}
	}
	if len(observations) != 5 {
		t.Fatalf("relationship observation count = %d, want 5", len(observations))
	}
	relguard.AssertObservations(t, observations...)
}

func TestScannerDefaultsServiceKind(t *testing.T) {
	scanner := Scanner{Client: fakeClient{protections: allProtections()}}
	boundary := testBoundary()
	boundary.ServiceKind = ""
	envelopes, err := scanner.Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	resource := assertResource(t, envelopes, awscloud.ResourceTypeShieldProtection)
	if got := resource.Payload["service_kind"]; got != awscloud.ServiceShield {
		t.Fatalf("service_kind = %v, want %v", got, awscloud.ServiceShield)
	}
}

func TestScannerRejectsUnexpectedServiceKind(t *testing.T) {
	scanner := Scanner{Client: fakeClient{}}
	boundary := testBoundary()
	boundary.ServiceKind = "wafv2"
	if _, err := scanner.Scan(context.Background(), boundary); err == nil {
		t.Fatalf("Scan() error = nil, want service_kind rejection")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	scanner := Scanner{}
	if _, err := scanner.Scan(context.Background(), testBoundary()); err == nil {
		t.Fatalf("Scan() error = nil, want missing client error")
	}
}

func TestScannerPropagatesListError(t *testing.T) {
	wantErr := errors.New("boom")
	scanner := Scanner{Client: fakeClient{listErr: wantErr}}
	if _, err := scanner.Scan(context.Background(), testBoundary()); !errors.Is(err, wantErr) {
		t.Fatalf("Scan() error = %v, want wrap of %v", err, wantErr)
	}
}

func TestScannerPropagatesSubscriptionError(t *testing.T) {
	wantErr := errors.New("subscription boom")
	scanner := Scanner{Client: fakeClient{subscriptionErr: wantErr}}
	if _, err := scanner.Scan(context.Background(), testBoundary()); !errors.Is(err, wantErr) {
		t.Fatalf("Scan() error = %v, want wrap of %v", err, wantErr)
	}
}

type fakeClient struct {
	protections     []Protection
	subscription    *Subscription
	listErr         error
	subscriptionErr error
}

func (f fakeClient) ListProtections(context.Context) ([]Protection, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.protections, nil
}

func (f fakeClient) DescribeSubscription(context.Context) (*Subscription, error) {
	if f.subscriptionErr != nil {
		return nil, f.subscriptionErr
	}
	return f.subscription, nil
}

func assertResource(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	resources := allResources(envelopes, resourceType)
	if len(resources) == 0 {
		t.Fatalf("no resource envelope with resource_type %q", resourceType)
	}
	return resources[0]
}

func allResources(envelopes []facts.Envelope, resourceType string) []facts.Envelope {
	var out []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if envelope.Payload["resource_type"] == resourceType {
			out = append(out, envelope)
		}
	}
	return out
}

func allRelationships(envelopes []facts.Envelope, relationshipType string) []facts.Envelope {
	var out []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if envelope.Payload["relationship_type"] == relationshipType {
			out = append(out, envelope)
		}
	}
	return out
}
