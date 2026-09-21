// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsga "github.com/aws/aws-sdk-go-v2/service/globalaccelerator"
	awsgatypes "github.com/aws/aws-sdk-go-v2/service/globalaccelerator/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

const (
	acceleratorARN  = "arn:aws:globalaccelerator::123456789012:accelerator/abcd1234"
	listenerARN     = "arn:aws:globalaccelerator::123456789012:accelerator/abcd1234/listener/0123abcd"
	endpointGroupAR = "arn:aws:globalaccelerator::123456789012:accelerator/abcd1234/listener/0123abcd/endpoint-group/ef567890"
	loadBalancerARN = "arn:aws:elasticloadbalancing:us-west-2:123456789012:loadbalancer/app/api/abc123"
)

func TestClientListsGlobalAcceleratorTopologyMetadataOnly(t *testing.T) {
	api := &fakeAPI{
		acceleratorPages: []*awsga.ListAcceleratorsOutput{
			{
				Accelerators: []awsgatypes.Accelerator{{
					AcceleratorArn:   aws.String(acceleratorARN),
					Name:             aws.String("edge-front-door"),
					Status:           awsgatypes.AcceleratorStatusDeployed,
					Enabled:          aws.Bool(true),
					IpAddressType:    awsgatypes.IpAddressTypeIpv4,
					DnsName:          aws.String("a1234567890abcdef.awsglobalaccelerator.com"),
					DualStackDnsName: aws.String("a1234567890abcdef.dualstack.awsglobalaccelerator.com"),
					IpSets: []awsgatypes.IpSet{{
						IpAddressFamily: awsgatypes.IpAddressFamilyIPv4,
						IpAddresses:     []string{"75.2.0.1", "99.83.0.1"},
					}},
				}},
				NextToken: aws.String("acc-page-2"),
			},
			{
				Accelerators: []awsgatypes.Accelerator{{
					AcceleratorArn: aws.String("arn:aws:globalaccelerator::123456789012:accelerator/second"),
					Name:           aws.String("second"),
					Status:         awsgatypes.AcceleratorStatusInProgress,
				}},
			},
		},
		listenerPages: map[string][]*awsga.ListListenersOutput{
			acceleratorARN: {{
				Listeners: []awsgatypes.Listener{{
					ListenerArn:    aws.String(listenerARN),
					Protocol:       awsgatypes.ProtocolTcp,
					ClientAffinity: awsgatypes.ClientAffinitySourceIp,
					PortRanges:     []awsgatypes.PortRange{{FromPort: aws.Int32(443), ToPort: aws.Int32(443)}},
				}},
			}},
		},
		endpointGroupPages: map[string][]*awsga.ListEndpointGroupsOutput{
			listenerARN: {{
				EndpointGroups: []awsgatypes.EndpointGroup{{
					EndpointGroupArn:      aws.String(endpointGroupAR),
					EndpointGroupRegion:   aws.String("us-west-2"),
					TrafficDialPercentage: aws.Float32(80),
					HealthCheckProtocol:   awsgatypes.HealthCheckProtocolTcp,
					HealthCheckPort:       aws.Int32(8080),
					EndpointDescriptions: []awsgatypes.EndpointDescription{{
						EndpointId:                  aws.String(loadBalancerARN),
						Weight:                      aws.Int32(128),
						ClientIPPreservationEnabled: aws.Bool(true),
						HealthState:                 awsgatypes.HealthStateHealthy,
					}},
				}},
			}},
		},
		tags: map[string]*awsga.ListTagsForResourceOutput{
			acceleratorARN: {Tags: []awsgatypes.Tag{{
				Key:   aws.String("Environment"),
				Value: aws.String("prod"),
			}}},
		},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	accelerators, err := adapter.ListAccelerators(context.Background())
	if err != nil {
		t.Fatalf("ListAccelerators() error = %v, want nil", err)
	}

	if got, want := len(accelerators), 2; got != want {
		t.Fatalf("len(accelerators) = %d, want %d", got, want)
	}
	if got, want := api.acceleratorTokens, []string{"", "acc-page-2"}; !stringSlicesEqual(got, want) {
		t.Fatalf("ListAccelerators NextToken = %#v, want %#v", got, want)
	}

	first := accelerators[0]
	if first.ARN != acceleratorARN || first.DNSName != "a1234567890abcdef.awsglobalaccelerator.com" {
		t.Fatalf("accelerator identity = %#v, want mapped ARN/DNS", first)
	}
	if first.IPAddressType != "IPV4" {
		t.Fatalf("accelerator ip_address_type = %q, want IPV4", first.IPAddressType)
	}
	if len(first.IPSets) != 1 || len(first.IPSets[0].IPAddresses) != 2 {
		t.Fatalf("accelerator ip sets = %#v, want one set with two addresses", first.IPSets)
	}
	if first.Tags["Environment"] != "prod" {
		t.Fatalf("accelerator tags = %#v, want Environment tag", first.Tags)
	}
	if len(first.Listeners) != 1 {
		t.Fatalf("accelerator listeners = %d, want 1", len(first.Listeners))
	}
	listener := first.Listeners[0]
	if listener.ARN != listenerARN || listener.ClientAffinity != "SOURCE_IP" || listener.Protocol != "TCP" {
		t.Fatalf("listener = %#v, want mapped TCP/SOURCE_IP listener", listener)
	}
	if len(listener.EndpointGroups) != 1 {
		t.Fatalf("listener endpoint groups = %d, want 1", len(listener.EndpointGroups))
	}
	group := listener.EndpointGroups[0]
	if group.ARN != endpointGroupAR || group.Region != "us-west-2" {
		t.Fatalf("endpoint group = %#v, want mapped group", group)
	}
	if group.TrafficDialPercentage == nil || *group.TrafficDialPercentage != 80 {
		t.Fatalf("endpoint group traffic dial = %#v, want 80", group.TrafficDialPercentage)
	}
	if len(group.Endpoints) != 1 || group.Endpoints[0].EndpointID != loadBalancerARN {
		t.Fatalf("endpoint group endpoints = %#v, want one load balancer endpoint", group.Endpoints)
	}

	// The second accelerator has no listeners; the adapter must still query for
	// them and produce an accelerator with an empty listener slice.
	if len(accelerators[1].Listeners) != 0 {
		t.Fatalf("second accelerator listeners = %d, want 0", len(accelerators[1].Listeners))
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-west-2",
		ServiceKind: awscloud.ServiceGlobalAccelerator,
	}
}

type fakeAPI struct {
	acceleratorPages   []*awsga.ListAcceleratorsOutput
	acceleratorCalls   int
	acceleratorTokens  []string
	listenerPages      map[string][]*awsga.ListListenersOutput
	listenerCalls      map[string]int
	endpointGroupPages map[string][]*awsga.ListEndpointGroupsOutput
	endpointGroupCalls map[string]int
	tags               map[string]*awsga.ListTagsForResourceOutput
}

func (f *fakeAPI) ListAccelerators(
	_ context.Context,
	input *awsga.ListAcceleratorsInput,
	_ ...func(*awsga.Options),
) (*awsga.ListAcceleratorsOutput, error) {
	f.acceleratorTokens = append(f.acceleratorTokens, aws.ToString(input.NextToken))
	if f.acceleratorCalls >= len(f.acceleratorPages) {
		return &awsga.ListAcceleratorsOutput{}, nil
	}
	page := f.acceleratorPages[f.acceleratorCalls]
	f.acceleratorCalls++
	return page, nil
}

func (f *fakeAPI) ListListeners(
	_ context.Context,
	input *awsga.ListListenersInput,
	_ ...func(*awsga.Options),
) (*awsga.ListListenersOutput, error) {
	if f.listenerCalls == nil {
		f.listenerCalls = map[string]int{}
	}
	arn := aws.ToString(input.AcceleratorArn)
	pages := f.listenerPages[arn]
	idx := f.listenerCalls[arn]
	if idx >= len(pages) {
		return &awsga.ListListenersOutput{}, nil
	}
	f.listenerCalls[arn]++
	return pages[idx], nil
}

func (f *fakeAPI) ListEndpointGroups(
	_ context.Context,
	input *awsga.ListEndpointGroupsInput,
	_ ...func(*awsga.Options),
) (*awsga.ListEndpointGroupsOutput, error) {
	if f.endpointGroupCalls == nil {
		f.endpointGroupCalls = map[string]int{}
	}
	arn := aws.ToString(input.ListenerArn)
	pages := f.endpointGroupPages[arn]
	idx := f.endpointGroupCalls[arn]
	if idx >= len(pages) {
		return &awsga.ListEndpointGroupsOutput{}, nil
	}
	f.endpointGroupCalls[arn]++
	return pages[idx], nil
}

func (f *fakeAPI) ListTagsForResource(
	_ context.Context,
	input *awsga.ListTagsForResourceInput,
	_ ...func(*awsga.Options),
) (*awsga.ListTagsForResourceOutput, error) {
	if output := f.tags[aws.ToString(input.ResourceArn)]; output != nil {
		return output, nil
	}
	return &awsga.ListTagsForResourceOutput{}, nil
}

var _ apiClient = (*fakeAPI)(nil)

func stringSlicesEqual(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
