// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package transitgateway

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func transitGatewayObservation(boundary awscloud.Boundary, gateway TransitGateway) awscloud.ResourceObservation {
	id := strings.TrimSpace(gateway.ID)
	arn := strings.TrimSpace(gateway.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeTransitGateway,
		Name:         id,
		State:        strings.TrimSpace(gateway.State),
		Tags:         cloneStringMap(gateway.Tags),
		Attributes: map[string]any{
			"created_at":  timeOrNil(gateway.CreatedAt),
			"description": strings.TrimSpace(gateway.Description),
			"options":     transitGatewayOptionsMap(gateway.Options),
			"owner_id":    strings.TrimSpace(gateway.OwnerID),
		},
		CorrelationAnchors: []string{id, arn},
		SourceRecordID:     firstNonEmpty(arn, id),
	}
}

func routeTableObservation(boundary awscloud.Boundary, rt RouteTable) awscloud.ResourceObservation {
	id := strings.TrimSpace(rt.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeTransitGatewayRouteTable,
		Name:         id,
		State:        strings.TrimSpace(rt.State),
		Tags:         cloneStringMap(rt.Tags),
		Attributes: map[string]any{
			"created_at":                      timeOrNil(rt.CreatedAt),
			"default_association_route_table": rt.DefaultAssociationRouteTable,
			"default_propagation_route_table": rt.DefaultPropagationRouteTable,
			"transit_gateway_id":              strings.TrimSpace(rt.TransitGatewayID),
		},
		CorrelationAnchors: []string{id},
		SourceRecordID:     id,
	}
}

func attachmentObservation(boundary awscloud.Boundary, attachment Attachment) awscloud.ResourceObservation {
	id := strings.TrimSpace(attachment.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeTransitGatewayAttachment,
		Name:         id,
		State:        strings.TrimSpace(attachment.State),
		Tags:         cloneStringMap(attachment.Tags),
		Attributes: map[string]any{
			"association_route_table_id": strings.TrimSpace(attachment.AssociationRouteTableID),
			"association_state":          strings.TrimSpace(attachment.AssociationState),
			"created_at":                 timeOrNil(attachment.CreatedAt),
			"resource_id":                strings.TrimSpace(attachment.ResourceID),
			"resource_owner_id":          strings.TrimSpace(attachment.ResourceOwnerID),
			"resource_type":              strings.TrimSpace(attachment.ResourceType),
			"transit_gateway_id":         strings.TrimSpace(attachment.TransitGatewayID),
			"transit_gateway_owner_id":   strings.TrimSpace(attachment.TransitGatewayOwnerID),
		},
		CorrelationAnchors: []string{id},
		SourceRecordID:     id,
	}
}

func peeringAttachmentObservation(boundary awscloud.Boundary, peering PeeringAttachment) awscloud.ResourceObservation {
	id := strings.TrimSpace(peering.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeTransitGatewayPeeringAttachment,
		Name:         id,
		State:        strings.TrimSpace(peering.State),
		Tags:         cloneStringMap(peering.Tags),
		Attributes: map[string]any{
			"accepter":       peeringInfoMap(peering.Accepter),
			"created_at":     timeOrNil(peering.CreatedAt),
			"requester":      peeringInfoMap(peering.Requester),
			"status_code":    strings.TrimSpace(peering.StatusCode),
			"status_message": strings.TrimSpace(peering.StatusMessage),
		},
		CorrelationAnchors: []string{id},
		SourceRecordID:     id,
	}
}

func multicastDomainObservation(boundary awscloud.Boundary, domain MulticastDomain) awscloud.ResourceObservation {
	id := strings.TrimSpace(domain.ID)
	arn := strings.TrimSpace(domain.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeTransitGatewayMulticastDomain,
		Name:         id,
		State:        strings.TrimSpace(domain.State),
		Tags:         cloneStringMap(domain.Tags),
		Attributes: map[string]any{
			"created_at":         timeOrNil(domain.CreatedAt),
			"options":            multicastDomainOptionsMap(domain.Options),
			"owner_id":           strings.TrimSpace(domain.OwnerID),
			"transit_gateway_id": strings.TrimSpace(domain.TransitGatewayID),
		},
		CorrelationAnchors: []string{id, arn},
		SourceRecordID:     firstNonEmpty(arn, id),
	}
}

func policyTableObservation(boundary awscloud.Boundary, policyTable PolicyTable) awscloud.ResourceObservation {
	id := strings.TrimSpace(policyTable.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeTransitGatewayPolicyTable,
		Name:         id,
		State:        strings.TrimSpace(policyTable.State),
		Tags:         cloneStringMap(policyTable.Tags),
		Attributes: map[string]any{
			"created_at":         timeOrNil(policyTable.CreatedAt),
			"transit_gateway_id": strings.TrimSpace(policyTable.TransitGatewayID),
		},
		CorrelationAnchors: []string{id},
		SourceRecordID:     id,
	}
}
