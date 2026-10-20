// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awselbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"
)

// TestAdapterAPIClientForbidsMutationAndLifecycle is the metadata-only security
// gate: the Classic ELB SDK adapter must never be able to create, delete,
// register, deregister, attach, detach, modify, configure, apply, or otherwise
// mutate an ELB resource. It reflects over the adapter-local apiClient interface
// and fails the build if any forbidden operation becomes reachable.
func TestAdapterAPIClientForbidsMutationAndLifecycle(t *testing.T) {
	forbiddenExact := []string{
		"CreateLoadBalancer", "DeleteLoadBalancer",
		"CreateLoadBalancerListeners", "DeleteLoadBalancerListeners",
		"CreateLoadBalancerPolicy", "DeleteLoadBalancerPolicy",
		"CreateAppCookieStickinessPolicy", "CreateLBCookieStickinessPolicy",
		"RegisterInstancesWithLoadBalancer", "DeregisterInstancesFromLoadBalancer",
		"AttachLoadBalancerToSubnets", "DetachLoadBalancerFromSubnets",
		"ApplySecurityGroupsToLoadBalancer", "ConfigureHealthCheck",
		"EnableAvailabilityZonesForLoadBalancer", "DisableAvailabilityZonesForLoadBalancer",
		"ModifyLoadBalancerAttributes", "SetLoadBalancerListenerSSLCertificate",
		"SetLoadBalancerPoliciesForBackendServer", "SetLoadBalancerPoliciesOfListener",
		"AddTags", "RemoveTags",
		"DescribeInstanceHealth",
	}
	// Any method whose name begins with one of these verbs is a write, lifecycle,
	// or live-state read and must not exist on the metadata-only adapter.
	forbiddenPrefixes := []string{
		"Create", "Delete", "Modify", "Set", "Apply", "Configure",
		"Register", "Deregister", "Attach", "Detach",
		"Enable", "Disable", "Add", "Remove",
	}
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		for _, banned := range forbiddenExact {
			if name == banned {
				t.Fatalf("apiClient exposes forbidden method %q; the Classic ELB adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation/lifecycle method %q (prefix %q); the Classic ELB adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreReadOnly asserts every method on the apiClient interface
// is a Describe read so the read surface stays explicit and auditable.
func TestAdapterMethodsAreReadOnly(t *testing.T) {
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	if iface.NumMethod() == 0 {
		t.Fatalf("apiClient interface has no methods; expected the Classic ELB read surface")
	}
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		if !strings.HasPrefix(name, "Describe") {
			t.Fatalf("apiClient method %q is not a Describe read; the Classic ELB adapter is metadata-only", name)
		}
	}
}

func TestMapLoadBalancerPreservesIdentityListenersAndTags(t *testing.T) {
	createdAt := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	loadBalancer := mapLoadBalancer(awselbtypes.LoadBalancerDescription{
		AvailabilityZones:         []string{"us-east-1a", "us-east-1b"},
		CanonicalHostedZoneName:   aws.String("web-123.us-east-1.elb.amazonaws.com"),
		CanonicalHostedZoneNameID: aws.String("Z35SXDOTRQ7X7K"),
		CreatedTime:               aws.Time(createdAt),
		DNSName:                   aws.String("web-123.us-east-1.elb.amazonaws.com"),
		HealthCheck: &awselbtypes.HealthCheck{
			HealthyThreshold:   aws.Int32(3),
			Interval:           aws.Int32(30),
			Target:             aws.String("HTTP:8080/healthz"),
			Timeout:            aws.Int32(5),
			UnhealthyThreshold: aws.Int32(2),
		},
		Instances: []awselbtypes.Instance{
			{InstanceId: aws.String("i-0abc")},
			{InstanceId: aws.String("i-0def")},
		},
		ListenerDescriptions: []awselbtypes.ListenerDescription{
			{
				Listener: &awselbtypes.Listener{
					Protocol:         aws.String("HTTPS"),
					LoadBalancerPort: 443,
					InstanceProtocol: aws.String("HTTP"),
					InstancePort:     aws.Int32(8080),
					SSLCertificateId: aws.String("arn:aws:acm:us-east-1:123456789012:certificate/abc"),
				},
			},
		},
		LoadBalancerName:    aws.String("web"),
		Scheme:              aws.String("internet-facing"),
		SecurityGroups:      []string{"sg-1"},
		SourceSecurityGroup: &awselbtypes.SourceSecurityGroup{GroupName: aws.String("amazon-elb-sg")},
		Subnets:             []string{"subnet-1", "subnet-2"},
		VPCId:               aws.String("vpc-123"),
	}, map[string]string{"service": "web"})

	if loadBalancer.Name != "web" {
		t.Fatalf("Name = %q, want web", loadBalancer.Name)
	}
	if loadBalancer.Tags["service"] != "web" {
		t.Fatalf("tag service = %q, want web", loadBalancer.Tags["service"])
	}
	if got := strings.Join(loadBalancer.InstanceIDs, ","); got != "i-0abc,i-0def" {
		t.Fatalf("InstanceIDs = %q, want i-0abc,i-0def", got)
	}
	if len(loadBalancer.Listeners) != 1 {
		t.Fatalf("Listeners = %#v, want one", loadBalancer.Listeners)
	}
	if loadBalancer.Listeners[0].SSLCertificateID != "arn:aws:acm:us-east-1:123456789012:certificate/abc" {
		t.Fatalf("SSLCertificateID = %q", loadBalancer.Listeners[0].SSLCertificateID)
	}
	if loadBalancer.HealthCheck.Target != "HTTP:8080/healthz" {
		t.Fatalf("HealthCheck.Target = %q", loadBalancer.HealthCheck.Target)
	}
	if loadBalancer.SourceSecurityGroupName != "amazon-elb-sg" {
		t.Fatalf("SourceSecurityGroupName = %q", loadBalancer.SourceSecurityGroupName)
	}
}

func TestMapLoadBalancerHandlesNilOptionalFields(t *testing.T) {
	loadBalancer := mapLoadBalancer(awselbtypes.LoadBalancerDescription{
		LoadBalancerName: aws.String("bare"),
	}, nil)
	if loadBalancer.Name != "bare" {
		t.Fatalf("Name = %q, want bare", loadBalancer.Name)
	}
	if loadBalancer.HealthCheck.Target != "" {
		t.Fatalf("HealthCheck.Target = %q, want empty for nil health check", loadBalancer.HealthCheck.Target)
	}
	if loadBalancer.Listeners != nil {
		t.Fatalf("Listeners = %#v, want nil", loadBalancer.Listeners)
	}
	if loadBalancer.SourceSecurityGroupName != "" {
		t.Fatalf("SourceSecurityGroupName = %q, want empty for nil source group", loadBalancer.SourceSecurityGroupName)
	}
}

func TestChunkStringsSplitsDescribeTagsLimit(t *testing.T) {
	values := make([]string, describeTagsLimit+1)
	for index := range values {
		values[index] = "name"
	}
	chunks := chunkStrings(values, describeTagsLimit)
	if len(chunks) != 2 {
		t.Fatalf("chunk count = %d, want 2", len(chunks))
	}
	if len(chunks[0]) != describeTagsLimit || len(chunks[1]) != 1 {
		t.Fatalf("chunks = %#v, want %d then 1", chunks, describeTagsLimit)
	}
}
