// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package globalaccelerator

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Global Accelerator accelerator, listener, endpoint group,
// endpoint, and relationship facts for one claimed account. It never mutates a
// Global Accelerator resource and never reads beyond control-plane metadata.
type Scanner struct {
	Client Client
}

// Scan observes Global Accelerator topology through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("globalaccelerator scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceGlobalAccelerator:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceGlobalAccelerator
	default:
		return nil, fmt.Errorf("globalaccelerator scanner received service_kind %q", boundary.ServiceKind)
	}

	accelerators, err := s.Client.ListAccelerators(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Global Accelerator accelerators: %w", err)
	}
	var envelopes []facts.Envelope
	for _, accelerator := range accelerators {
		acceleratorEnvelopes, err := acceleratorEnvelopes(boundary, accelerator)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, acceleratorEnvelopes...)
	}
	return envelopes, nil
}

func acceleratorEnvelopes(
	boundary awscloud.Boundary,
	accelerator Accelerator,
) ([]facts.Envelope, error) {
	acceleratorARN := strings.TrimSpace(accelerator.ARN)
	if acceleratorARN == "" {
		return nil, fmt.Errorf("globalaccelerator accelerator missing arn for account %q", boundary.AccountID)
	}
	resource, err := awscloud.NewResourceEnvelope(acceleratorObservation(boundary, accelerator))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, listener := range accelerator.Listeners {
		listenerEnvelopes, err := listenerEnvelopes(boundary, acceleratorARN, listener)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, listenerEnvelopes...)
	}
	return envelopes, nil
}

func listenerEnvelopes(
	boundary awscloud.Boundary,
	acceleratorARN string,
	listener Listener,
) ([]facts.Envelope, error) {
	listenerARN := strings.TrimSpace(listener.ARN)
	if listenerARN == "" {
		return nil, fmt.Errorf("globalaccelerator listener missing arn for accelerator %q", acceleratorARN)
	}
	resource, err := awscloud.NewResourceEnvelope(listenerObservation(boundary, acceleratorARN, listener))
	if err != nil {
		return nil, err
	}
	relationship, err := awscloud.NewRelationshipEnvelope(acceleratorListenerRelationship(boundary, acceleratorARN, listenerARN))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource, relationship}
	for _, group := range listener.EndpointGroups {
		groupEnvelopes, err := endpointGroupEnvelopes(boundary, listenerARN, group)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, groupEnvelopes...)
	}
	return envelopes, nil
}

func endpointGroupEnvelopes(
	boundary awscloud.Boundary,
	listenerARN string,
	group EndpointGroup,
) ([]facts.Envelope, error) {
	groupARN := strings.TrimSpace(group.ARN)
	if groupARN == "" {
		return nil, fmt.Errorf("globalaccelerator endpoint group missing arn for listener %q", listenerARN)
	}
	resource, err := awscloud.NewResourceEnvelope(endpointGroupObservation(boundary, listenerARN, group))
	if err != nil {
		return nil, err
	}
	relationship, err := awscloud.NewRelationshipEnvelope(listenerEndpointGroupRelationship(boundary, listenerARN, groupARN, group.Region))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource, relationship}
	for index, endpoint := range group.Endpoints {
		endpointEnvelopes, err := endpointEnvelopes(boundary, groupARN, index, endpoint)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, endpointEnvelopes...)
	}
	return envelopes, nil
}

func endpointEnvelopes(
	boundary awscloud.Boundary,
	groupARN string,
	index int,
	endpoint Endpoint,
) ([]facts.Envelope, error) {
	endpointID := strings.TrimSpace(endpoint.EndpointID)
	if endpointID == "" {
		return nil, fmt.Errorf("globalaccelerator endpoint missing endpoint id for endpoint group %q", groupARN)
	}
	endpointResourceID := fmt.Sprintf("%s#endpoint#%s", groupARN, endpointID)
	resource, err := awscloud.NewResourceEnvelope(endpointObservation(boundary, groupARN, endpointResourceID, endpoint))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	membership, err := awscloud.NewRelationshipEnvelope(endpointGroupEndpointRelationship(boundary, groupARN, endpointResourceID, endpoint))
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, membership)
	target, err := awscloud.NewRelationshipEnvelope(endpointTargetRelationship(boundary, endpointResourceID, endpoint))
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, target)
	return envelopes, nil
}

func acceleratorObservation(boundary awscloud.Boundary, accelerator Accelerator) awscloud.ResourceObservation {
	acceleratorARN := strings.TrimSpace(accelerator.ARN)
	name := firstNonEmpty(accelerator.Name, acceleratorARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          acceleratorARN,
		ResourceID:   acceleratorARN,
		ResourceType: awscloud.ResourceTypeGlobalAcceleratorAccelerator,
		Name:         name,
		State:        strings.TrimSpace(accelerator.Status),
		Tags:         cloneStringMap(accelerator.Tags),
		Attributes: map[string]any{
			"name":                strings.TrimSpace(accelerator.Name),
			"status":              strings.TrimSpace(accelerator.Status),
			"enabled":             accelerator.Enabled,
			"ip_address_type":     strings.TrimSpace(accelerator.IPAddressType),
			"dns_name":            strings.TrimSpace(accelerator.DNSName),
			"dual_stack_dns_name": strings.TrimSpace(accelerator.DualStackDNSName),
			"created_time":        timeOrNil(accelerator.CreatedTime),
			"last_modified_time":  timeOrNil(accelerator.LastModifiedTime),
			"ip_sets":             ipSetAttributes(accelerator.IPSets),
		},
		CorrelationAnchors: append([]string{acceleratorARN, accelerator.DNSName, accelerator.DualStackDNSName}, ipSetAddresses(accelerator.IPSets)...),
		SourceRecordID:     acceleratorARN,
	}
}

func listenerObservation(
	boundary awscloud.Boundary,
	acceleratorARN string,
	listener Listener,
) awscloud.ResourceObservation {
	listenerARN := strings.TrimSpace(listener.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          listenerARN,
		ResourceID:   listenerARN,
		ResourceType: awscloud.ResourceTypeGlobalAcceleratorListener,
		Name:         listenerARN,
		Attributes: map[string]any{
			"accelerator_arn": acceleratorARN,
			"protocol":        strings.TrimSpace(listener.Protocol),
			"client_affinity": strings.TrimSpace(listener.ClientAffinity),
			"port_ranges":     portRangeAttributes(listener.PortRanges),
		},
		CorrelationAnchors: []string{listenerARN},
		SourceRecordID:     listenerARN,
	}
}

func endpointGroupObservation(
	boundary awscloud.Boundary,
	listenerARN string,
	group EndpointGroup,
) awscloud.ResourceObservation {
	groupARN := strings.TrimSpace(group.ARN)
	attributes := map[string]any{
		"listener_arn":          listenerARN,
		"endpoint_group_region": strings.TrimSpace(group.Region),
		"health_check_protocol": strings.TrimSpace(group.HealthCheckProtocol),
		"health_check_path":     strings.TrimSpace(group.HealthCheckPath),
		"port_overrides":        portOverrideAttributes(group.PortOverrides),
	}
	if group.TrafficDialPercentage != nil {
		attributes["traffic_dial_percentage"] = *group.TrafficDialPercentage
	}
	if group.HealthCheckPort != nil {
		attributes["health_check_port"] = *group.HealthCheckPort
	}
	if group.HealthCheckIntervalSeconds != nil {
		attributes["health_check_interval_seconds"] = *group.HealthCheckIntervalSeconds
	}
	if group.ThresholdCount != nil {
		attributes["threshold_count"] = *group.ThresholdCount
	}
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ARN:                groupARN,
		ResourceID:         groupARN,
		ResourceType:       awscloud.ResourceTypeGlobalAcceleratorEndpointGroup,
		Name:               firstNonEmpty(group.Region, groupARN),
		Attributes:         attributes,
		CorrelationAnchors: []string{groupARN},
		SourceRecordID:     groupARN,
	}
}

func endpointObservation(
	boundary awscloud.Boundary,
	groupARN string,
	endpointResourceID string,
	endpoint Endpoint,
) awscloud.ResourceObservation {
	endpointID := strings.TrimSpace(endpoint.EndpointID)
	attributes := map[string]any{
		"endpoint_group_arn": groupARN,
		"endpoint_id":        endpointID,
		"target_type":        endpointTargetType(endpointID),
		"health_state":       strings.TrimSpace(endpoint.HealthState),
	}
	if endpoint.Weight != nil {
		attributes["weight"] = *endpoint.Weight
	}
	if endpoint.ClientIPPreservationEnabled != nil {
		attributes["client_ip_preservation_enabled"] = *endpoint.ClientIPPreservationEnabled
	}
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ResourceID:         endpointResourceID,
		ResourceType:       awscloud.ResourceTypeGlobalAcceleratorEndpoint,
		Name:               endpointID,
		Attributes:         attributes,
		CorrelationAnchors: []string{endpointResourceID, endpointID},
		SourceRecordID:     endpointResourceID,
	}
}

func ipSetAttributes(sets []IPSet) []map[string]any {
	if len(sets) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(sets))
	for _, set := range sets {
		addresses := cloneStrings(set.IPAddresses)
		if strings.TrimSpace(set.IPAddressFamily) == "" && addresses == nil {
			continue
		}
		output = append(output, map[string]any{
			"ip_address_family": strings.TrimSpace(set.IPAddressFamily),
			"ip_addresses":      addresses,
		})
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func ipSetAddresses(sets []IPSet) []string {
	var addresses []string
	for _, set := range sets {
		addresses = append(addresses, cloneStrings(set.IPAddresses)...)
	}
	return addresses
}

func portRangeAttributes(ranges []PortRange) []map[string]any {
	if len(ranges) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(ranges))
	for _, portRange := range ranges {
		output = append(output, map[string]any{
			"from_port": portRange.FromPort,
			"to_port":   portRange.ToPort,
		})
	}
	return output
}

func portOverrideAttributes(overrides []PortOverride) []map[string]any {
	if len(overrides) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(overrides))
	for _, override := range overrides {
		output = append(output, map[string]any{
			"listener_port": override.ListenerPort,
			"endpoint_port": override.EndpointPort,
		})
	}
	return output
}
