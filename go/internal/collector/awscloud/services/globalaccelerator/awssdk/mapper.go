// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsgatypes "github.com/aws/aws-sdk-go-v2/service/globalaccelerator/types"

	gaservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/globalaccelerator"
)

// awsgaAccelerator aliases the SDK accelerator type so the client signatures
// stay readable without leaking the long import path.
type awsgaAccelerator = awsgatypes.Accelerator

func mapAccelerator(
	accelerator awsgatypes.Accelerator,
	listeners []gaservice.Listener,
	tags map[string]string,
) gaservice.Accelerator {
	return gaservice.Accelerator{
		ARN:              strings.TrimSpace(aws.ToString(accelerator.AcceleratorArn)),
		Name:             strings.TrimSpace(aws.ToString(accelerator.Name)),
		Status:           string(accelerator.Status),
		Enabled:          aws.ToBool(accelerator.Enabled),
		IPAddressType:    string(accelerator.IpAddressType),
		DNSName:          strings.TrimSpace(aws.ToString(accelerator.DnsName)),
		DualStackDNSName: strings.TrimSpace(aws.ToString(accelerator.DualStackDnsName)),
		CreatedTime:      aws.ToTime(accelerator.CreatedTime),
		LastModifiedTime: aws.ToTime(accelerator.LastModifiedTime),
		IPSets:           mapIPSets(accelerator.IpSets),
		Listeners:        listeners,
		Tags:             tags,
	}
}

func mapIPSets(sets []awsgatypes.IpSet) []gaservice.IPSet {
	if len(sets) == 0 {
		return nil
	}
	output := make([]gaservice.IPSet, 0, len(sets))
	for _, set := range sets {
		output = append(output, gaservice.IPSet{
			IPAddressFamily: string(set.IpAddressFamily),
			IPAddresses:     cloneStrings(set.IpAddresses),
		})
	}
	return output
}

func mapListener(
	listener awsgatypes.Listener,
	groups []gaservice.EndpointGroup,
) gaservice.Listener {
	return gaservice.Listener{
		ARN:            strings.TrimSpace(aws.ToString(listener.ListenerArn)),
		Protocol:       string(listener.Protocol),
		ClientAffinity: string(listener.ClientAffinity),
		PortRanges:     mapPortRanges(listener.PortRanges),
		EndpointGroups: groups,
	}
}

func mapPortRanges(ranges []awsgatypes.PortRange) []gaservice.PortRange {
	if len(ranges) == 0 {
		return nil
	}
	output := make([]gaservice.PortRange, 0, len(ranges))
	for _, portRange := range ranges {
		output = append(output, gaservice.PortRange{
			FromPort: aws.ToInt32(portRange.FromPort),
			ToPort:   aws.ToInt32(portRange.ToPort),
		})
	}
	return output
}

func mapEndpointGroup(group awsgatypes.EndpointGroup) gaservice.EndpointGroup {
	return gaservice.EndpointGroup{
		ARN:                        strings.TrimSpace(aws.ToString(group.EndpointGroupArn)),
		Region:                     strings.TrimSpace(aws.ToString(group.EndpointGroupRegion)),
		TrafficDialPercentage:      cloneFloat32(group.TrafficDialPercentage),
		HealthCheckProtocol:        string(group.HealthCheckProtocol),
		HealthCheckPath:            strings.TrimSpace(aws.ToString(group.HealthCheckPath)),
		HealthCheckPort:            cloneInt32(group.HealthCheckPort),
		HealthCheckIntervalSeconds: cloneInt32(group.HealthCheckIntervalSeconds),
		ThresholdCount:             cloneInt32(group.ThresholdCount),
		PortOverrides:              mapPortOverrides(group.PortOverrides),
		Endpoints:                  mapEndpoints(group.EndpointDescriptions),
	}
}

func mapPortOverrides(overrides []awsgatypes.PortOverride) []gaservice.PortOverride {
	if len(overrides) == 0 {
		return nil
	}
	output := make([]gaservice.PortOverride, 0, len(overrides))
	for _, override := range overrides {
		output = append(output, gaservice.PortOverride{
			ListenerPort: aws.ToInt32(override.ListenerPort),
			EndpointPort: aws.ToInt32(override.EndpointPort),
		})
	}
	return output
}

func mapEndpoints(endpoints []awsgatypes.EndpointDescription) []gaservice.Endpoint {
	if len(endpoints) == 0 {
		return nil
	}
	output := make([]gaservice.Endpoint, 0, len(endpoints))
	for _, endpoint := range endpoints {
		output = append(output, gaservice.Endpoint{
			EndpointID:                  strings.TrimSpace(aws.ToString(endpoint.EndpointId)),
			Weight:                      cloneInt32(endpoint.Weight),
			ClientIPPreservationEnabled: cloneBool(endpoint.ClientIPPreservationEnabled),
			HealthState:                 string(endpoint.HealthState),
		})
	}
	return output
}

func mapTags(tags []awsgatypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = aws.ToString(tag.Value)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneInt32(input *int32) *int32 {
	if input == nil {
		return nil
	}
	value := *input
	return &value
}

func cloneFloat32(input *float32) *float32 {
	if input == nil {
		return nil
	}
	value := *input
	return &value
}

func cloneBool(input *bool) *bool {
	if input == nil {
		return nil
	}
	value := *input
	return &value
}
