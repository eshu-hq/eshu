// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package globalaccelerator

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func acceleratorListenerRelationship(
	boundary aws.Boundary,
	acceleratorARN string,
	listenerARN string,
) aws.RelationshipObservation {
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipGlobalAcceleratorAcceleratorHasListener,
		SourceResourceID: acceleratorARN,
		SourceARN:        acceleratorARN,
		TargetResourceID: listenerARN,
		TargetARN:        listenerARN,
		TargetType:       aws.ResourceTypeGlobalAcceleratorListener,
		SourceRecordID:   acceleratorARN + "#listener#" + listenerARN,
	}
}

func listenerEndpointGroupRelationship(
	boundary aws.Boundary,
	listenerARN string,
	groupARN string,
	region string,
) aws.RelationshipObservation {
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipGlobalAcceleratorListenerHasEndpointGroup,
		SourceResourceID: listenerARN,
		SourceARN:        listenerARN,
		TargetResourceID: groupARN,
		TargetARN:        groupARN,
		TargetType:       aws.ResourceTypeGlobalAcceleratorEndpointGroup,
		Attributes: map[string]any{
			"endpoint_group_region": strings.TrimSpace(region),
		},
		SourceRecordID: listenerARN + "#endpoint-group#" + groupARN,
	}
}

func endpointGroupEndpointRelationship(
	boundary aws.Boundary,
	groupARN string,
	endpointResourceID string,
	endpoint Endpoint,
) aws.RelationshipObservation {
	attributes := map[string]any{
		"endpoint_id": strings.TrimSpace(endpoint.EndpointID),
	}
	if endpoint.Weight != nil {
		attributes["weight"] = *endpoint.Weight
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipGlobalAcceleratorEndpointGroupHasEndpoint,
		SourceResourceID: groupARN,
		SourceARN:        groupARN,
		TargetResourceID: endpointResourceID,
		TargetType:       aws.ResourceTypeGlobalAcceleratorEndpoint,
		Attributes:       attributes,
		SourceRecordID:   groupARN + "#endpoint#" + endpointResourceID,
	}
}

// endpointTargetRelationship records the resource a Global Accelerator endpoint
// routes traffic to. The endpoint id is an ALB/NLB ARN, an Elastic IP
// allocation id, or an EC2 instance id; target_type names the family and
// target_arn is set only when the id is ARN-shaped.
func endpointTargetRelationship(
	boundary aws.Boundary,
	endpointResourceID string,
	endpoint Endpoint,
) aws.RelationshipObservation {
	endpointID := strings.TrimSpace(endpoint.EndpointID)
	targetARN := ""
	if isARN(endpointID) {
		targetARN = endpointID
	}
	attributes := map[string]any{
		"endpoint_id": endpointID,
	}
	if endpoint.ClientIPPreservationEnabled != nil {
		attributes["client_ip_preservation_enabled"] = *endpoint.ClientIPPreservationEnabled
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipGlobalAcceleratorEndpointTargetsResource,
		SourceResourceID: endpointResourceID,
		TargetResourceID: endpointID,
		TargetARN:        targetARN,
		TargetType:       endpointTargetType(endpointID),
		Attributes:       attributes,
		SourceRecordID:   endpointResourceID + "#targets#" + endpointID,
	}
}
