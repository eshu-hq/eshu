// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package globalaccelerator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testAcceleratorARN  = "arn:aws:globalaccelerator::123456789012:accelerator/abcd1234"
	testListenerARN     = "arn:aws:globalaccelerator::123456789012:accelerator/abcd1234/listener/0123abcd"
	testEndpointGroupAR = "arn:aws:globalaccelerator::123456789012:accelerator/abcd1234/listener/0123abcd/endpoint-group/ef567890"
	testLoadBalancerARN = "arn:aws:elasticloadbalancing:us-west-2:123456789012:loadbalancer/app/api/abc123"
)

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-west-2",
		ServiceKind:         awscloud.ServiceGlobalAccelerator,
		ScopeID:             "scope-1",
		GenerationID:        "gen-1",
		CollectorInstanceID: "collector-1",
		FencingToken:        1,
		ObservedAt:          time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC),
	}
}

func fullTopologyAccelerator() Accelerator {
	weight := int32(128)
	preservation := true
	traffic := float32(80)
	healthPort := int32(8080)
	interval := int32(30)
	threshold := int32(3)
	return Accelerator{
		ARN:              testAcceleratorARN,
		Name:             "edge-front-door",
		Status:           "DEPLOYED",
		Enabled:          true,
		IPAddressType:    "IPV4",
		DNSName:          "a1234567890abcdef.awsglobalaccelerator.com",
		DualStackDNSName: "a1234567890abcdef.dualstack.awsglobalaccelerator.com",
		CreatedTime:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		LastModifiedTime: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		IPSets: []IPSet{{
			IPAddressFamily: "IPv4",
			IPAddresses:     []string{"75.2.0.1", "99.83.0.1"},
		}},
		Tags: map[string]string{"Environment": "prod"},
		Listeners: []Listener{{
			ARN:            testListenerARN,
			Protocol:       "TCP",
			ClientAffinity: "SOURCE_IP",
			PortRanges:     []PortRange{{FromPort: 443, ToPort: 443}},
			EndpointGroups: []EndpointGroup{{
				ARN:                        testEndpointGroupAR,
				Region:                     "us-west-2",
				TrafficDialPercentage:      &traffic,
				HealthCheckProtocol:        "TCP",
				HealthCheckPath:            "/healthz",
				HealthCheckPort:            &healthPort,
				HealthCheckIntervalSeconds: &interval,
				ThresholdCount:             &threshold,
				PortOverrides:              []PortOverride{{ListenerPort: 443, EndpointPort: 8443}},
				Endpoints: []Endpoint{
					{
						EndpointID:                  testLoadBalancerARN,
						Weight:                      &weight,
						ClientIPPreservationEnabled: &preservation,
						HealthState:                 "HEALTHY",
					},
					{
						EndpointID: "eipalloc-0a1b2c3d4e5f",
						Weight:     &weight,
					},
					{
						EndpointID: "i-0abc123def456",
					},
					{
						EndpointID: "custom-target-id",
					},
				},
			}},
		}},
	}
}

func TestScannerEmitsFullTopology(t *testing.T) {
	scanner := Scanner{Client: fakeClient{accelerators: []Accelerator{fullTopologyAccelerator()}}}

	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	accelerator := assertResource(t, envelopes, awscloud.ResourceTypeGlobalAcceleratorAccelerator)
	if got := accelerator.Payload["arn"]; got != testAcceleratorARN {
		t.Fatalf("accelerator arn = %v, want %v", got, testAcceleratorARN)
	}
	attrs := accelerator.Payload["attributes"].(map[string]any)
	if got := attrs["dns_name"]; got != "a1234567890abcdef.awsglobalaccelerator.com" {
		t.Fatalf("accelerator dns_name = %v", got)
	}
	if got := attrs["ip_address_type"]; got != "IPV4" {
		t.Fatalf("accelerator ip_address_type = %v", got)
	}
	if _, ok := attrs["ip_sets"].([]map[string]any); !ok {
		t.Fatalf("accelerator ip_sets missing, got %#v", attrs["ip_sets"])
	}

	listener := assertResource(t, envelopes, awscloud.ResourceTypeGlobalAcceleratorListener)
	listenerAttrs := listener.Payload["attributes"].(map[string]any)
	if got := listenerAttrs["client_affinity"]; got != "SOURCE_IP" {
		t.Fatalf("listener client_affinity = %v", got)
	}
	if got := listenerAttrs["accelerator_arn"]; got != testAcceleratorARN {
		t.Fatalf("listener accelerator_arn = %v", got)
	}

	group := assertResource(t, envelopes, awscloud.ResourceTypeGlobalAcceleratorEndpointGroup)
	groupAttrs := group.Payload["attributes"].(map[string]any)
	if got := groupAttrs["endpoint_group_region"]; got != "us-west-2" {
		t.Fatalf("endpoint group region = %v", got)
	}
	if got := groupAttrs["traffic_dial_percentage"]; got != float32(80) {
		t.Fatalf("endpoint group traffic_dial_percentage = %v", got)
	}

	endpoints := allResources(envelopes, awscloud.ResourceTypeGlobalAcceleratorEndpoint)
	if got, want := len(endpoints), 4; got != want {
		t.Fatalf("endpoint resource count = %d, want %d", got, want)
	}
}

func TestScannerEndpointTargetTypesAreTyped(t *testing.T) {
	scanner := Scanner{Client: fakeClient{accelerators: []Accelerator{fullTopologyAccelerator()}}}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	targets := allRelationships(envelopes, awscloud.RelationshipGlobalAcceleratorEndpointTargetsResource)
	if got, want := len(targets), 4; got != want {
		t.Fatalf("endpoint target relationship count = %d, want %d", got, want)
	}
	byEndpointID := map[string]map[string]any{}
	for _, target := range targets {
		attrs := target.Payload["attributes"].(map[string]any)
		byEndpointID[attrs["endpoint_id"].(string)] = target.Payload
	}

	cases := []struct {
		endpointID string
		targetType string
		wantARN    string
	}{
		{testLoadBalancerARN, awscloud.ResourceTypeELBv2LoadBalancer, testLoadBalancerARN},
		{"eipalloc-0a1b2c3d4e5f", awscloud.ResourceTypeVPCElasticIP, ""},
		{"i-0abc123def456", "aws_ec2_instance", ""},
		{"custom-target-id", "aws_resource", ""},
	}
	for _, tc := range cases {
		payload, ok := byEndpointID[tc.endpointID]
		if !ok {
			t.Fatalf("missing endpoint target relationship for %q", tc.endpointID)
		}
		if got := payload["target_type"]; got != tc.targetType {
			t.Fatalf("endpoint %q target_type = %v, want %v", tc.endpointID, got, tc.targetType)
		}
		if got := payload["target_arn"]; got != tc.wantARN {
			t.Fatalf("endpoint %q target_arn = %v, want %v", tc.endpointID, got, tc.wantARN)
		}
		if got := payload["target_resource_id"]; got != tc.endpointID {
			t.Fatalf("endpoint %q target_resource_id = %v, want %v", tc.endpointID, got, tc.endpointID)
		}
	}
}

func TestScannerEmitsMembershipRelationships(t *testing.T) {
	scanner := Scanner{Client: fakeClient{accelerators: []Accelerator{fullTopologyAccelerator()}}}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	hasListener := assertRelationship(t, envelopes, awscloud.RelationshipGlobalAcceleratorAcceleratorHasListener)
	if got := hasListener.Payload["source_resource_id"]; got != testAcceleratorARN {
		t.Fatalf("accelerator->listener source = %v", got)
	}
	if got := hasListener.Payload["target_resource_id"]; got != testListenerARN {
		t.Fatalf("accelerator->listener target = %v", got)
	}
	if got := hasListener.Payload["target_type"]; got != awscloud.ResourceTypeGlobalAcceleratorListener {
		t.Fatalf("accelerator->listener target_type = %v", got)
	}

	hasGroup := assertRelationship(t, envelopes, awscloud.RelationshipGlobalAcceleratorListenerHasEndpointGroup)
	if got := hasGroup.Payload["source_resource_id"]; got != testListenerARN {
		t.Fatalf("listener->group source = %v", got)
	}
	if got := hasGroup.Payload["target_resource_id"]; got != testEndpointGroupAR {
		t.Fatalf("listener->group target = %v", got)
	}

	hasEndpoint := assertRelationship(t, envelopes, awscloud.RelationshipGlobalAcceleratorEndpointGroupHasEndpoint)
	if got := hasEndpoint.Payload["source_resource_id"]; got != testEndpointGroupAR {
		t.Fatalf("group->endpoint source = %v", got)
	}
	if got := hasEndpoint.Payload["target_type"]; got != awscloud.ResourceTypeGlobalAcceleratorEndpoint {
		t.Fatalf("group->endpoint target_type = %v", got)
	}
}

func TestScannerAllRelationshipsHaveTargetType(t *testing.T) {
	scanner := Scanner{Client: fakeClient{accelerators: []Accelerator{fullTopologyAccelerator()}}}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got := envelope.Payload["target_type"]; got == "" || got == nil {
			t.Fatalf("relationship %v has empty target_type", envelope.Payload["relationship_type"])
		}
	}
}

func TestScannerRejectsUnexpectedServiceKind(t *testing.T) {
	scanner := Scanner{Client: fakeClient{}}
	boundary := testBoundary()
	boundary.ServiceKind = "elbv2"
	if _, err := scanner.Scan(context.Background(), boundary); err == nil {
		t.Fatalf("Scan() error = nil, want service_kind rejection")
	}
}

func TestScannerDefaultsServiceKind(t *testing.T) {
	scanner := Scanner{Client: fakeClient{accelerators: []Accelerator{fullTopologyAccelerator()}}}
	boundary := testBoundary()
	boundary.ServiceKind = ""
	envelopes, err := scanner.Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	resource := assertResource(t, envelopes, awscloud.ResourceTypeGlobalAcceleratorAccelerator)
	if got := resource.Payload["service_kind"]; got != awscloud.ServiceGlobalAccelerator {
		t.Fatalf("service_kind = %v, want %v", got, awscloud.ServiceGlobalAccelerator)
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
	scanner := Scanner{Client: fakeClient{err: wantErr}}
	if _, err := scanner.Scan(context.Background(), testBoundary()); !errors.Is(err, wantErr) {
		t.Fatalf("Scan() error = %v, want wrap of %v", err, wantErr)
	}
}

type fakeClient struct {
	accelerators []Accelerator
	err          error
}

func (f fakeClient) ListAccelerators(context.Context) ([]Accelerator, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.accelerators, nil
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

func assertRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	relationships := allRelationships(envelopes, relationshipType)
	if len(relationships) == 0 {
		t.Fatalf("no relationship envelope with relationship_type %q", relationshipType)
	}
	return relationships[0]
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
