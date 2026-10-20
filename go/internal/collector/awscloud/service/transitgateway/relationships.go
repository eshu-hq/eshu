// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package transitgateway

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func routeTableRelationships(boundary awscloud.Boundary, rt RouteTable) []awscloud.RelationshipObservation {
	id := strings.TrimSpace(rt.ID)
	tgwID := strings.TrimSpace(rt.TransitGatewayID)
	if id == "" || tgwID == "" {
		return nil
	}
	return []awscloud.RelationshipObservation{{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipTransitGatewayRouteTableInTransitGateway,
		SourceResourceID: id,
		TargetResourceID: tgwID,
		TargetType:       awscloud.ResourceTypeTransitGateway,
		SourceRecordID:   id + "#transit-gateway#" + tgwID,
	}}
}

func attachmentRelationships(boundary awscloud.Boundary, attachment Attachment) []awscloud.RelationshipObservation {
	id := strings.TrimSpace(attachment.ID)
	if id == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation

	if tgwID := strings.TrimSpace(attachment.TransitGatewayID); tgwID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipTransitGatewayAttachmentToTransitGateway,
			SourceResourceID: id,
			TargetResourceID: tgwID,
			TargetType:       awscloud.ResourceTypeTransitGateway,
			Attributes: map[string]any{
				"transit_gateway_owner_id": strings.TrimSpace(attachment.TransitGatewayOwnerID),
			},
			SourceRecordID: id + "#transit-gateway#" + tgwID,
		})
	}

	if observation, ok := attachmentResourceRelationship(boundary, id, attachment); ok {
		observations = append(observations, observation)
	}

	if rtID := strings.TrimSpace(attachment.AssociationRouteTableID); rtID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipTransitGatewayRouteTableToAttachment,
			SourceResourceID: rtID,
			TargetResourceID: id,
			TargetType:       awscloud.ResourceTypeTransitGatewayAttachment,
			Attributes: map[string]any{
				"association_state": strings.TrimSpace(attachment.AssociationState),
			},
			SourceRecordID: rtID + "#attachment#" + id,
		})
	}

	return observations
}

// attachmentResourceRelationship maps the AWS-reported attachment resource type
// to the edge that links the attachment to the resource it attaches. VPC, VPN,
// Direct Connect gateway, and peering attachments cross-reference resources
// owned by the VPC scanner, the EC2 scanner, or this scanner; Connect and other
// attachment types do not yet have a typed target and emit no edge.
func attachmentResourceRelationship(
	boundary awscloud.Boundary,
	id string,
	attachment Attachment,
) (awscloud.RelationshipObservation, bool) {
	resourceID := strings.TrimSpace(attachment.ResourceID)
	if resourceID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	resourceOwnerID := strings.TrimSpace(attachment.ResourceOwnerID)
	switch normalizeAttachmentResourceType(attachment.ResourceType) {
	case "vpc":
		return awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipTransitGatewayAttachmentToVPC,
			SourceResourceID: id,
			TargetResourceID: resourceID,
			TargetType:       awscloud.ResourceTypeEC2VPC,
			Attributes:       attachmentResourceAttributes(resourceOwnerID),
			SourceRecordID:   id + "#vpc#" + resourceID,
		}, true
	case "vpn":
		return awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipTransitGatewayAttachmentToVPNConnection,
			SourceResourceID: id,
			TargetResourceID: resourceID,
			TargetType:       awscloud.ResourceTypeVPCVPNConnection,
			Attributes:       attachmentResourceAttributes(resourceOwnerID),
			SourceRecordID:   id + "#vpn#" + resourceID,
		}, true
	case "direct-connect-gateway":
		return awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipTransitGatewayAttachmentToDirectConnectGateway,
			SourceResourceID: id,
			TargetResourceID: resourceID,
			TargetType:       "aws_direct_connect_gateway",
			Attributes:       attachmentResourceAttributes(resourceOwnerID),
			SourceRecordID:   id + "#direct-connect-gateway#" + resourceID,
		}, true
	case "peering":
		return awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipTransitGatewayAttachmentToPeer,
			SourceResourceID: id,
			TargetResourceID: resourceID,
			TargetType:       awscloud.ResourceTypeTransitGatewayPeeringAttachment,
			Attributes:       attachmentResourceAttributes(resourceOwnerID),
			SourceRecordID:   id + "#peer#" + resourceID,
		}, true
	default:
		return awscloud.RelationshipObservation{}, false
	}
}

func peeringAttachmentRelationships(
	boundary awscloud.Boundary,
	peering PeeringAttachment,
) []awscloud.RelationshipObservation {
	id := strings.TrimSpace(peering.ID)
	if id == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	if observation, ok := peeringSideRelationship(
		boundary,
		id,
		awscloud.RelationshipTransitGatewayPeeringRequestsTransitGateway,
		"requester",
		peering.Requester,
	); ok {
		observations = append(observations, observation)
	}
	if observation, ok := peeringSideRelationship(
		boundary,
		id,
		awscloud.RelationshipTransitGatewayPeeringAcceptsTransitGateway,
		"accepter",
		peering.Accepter,
	); ok {
		observations = append(observations, observation)
	}
	return observations
}

// peeringSideRelationship emits one side of a peering attachment. The peer
// transit gateway can live in a different account and Region; owner_id, region,
// and cross_account are surfaced as AWS reports them so a downstream
// org-context join can resolve the remote account. The scanner never resolves
// the remote account itself.
func peeringSideRelationship(
	boundary awscloud.Boundary,
	id string,
	relationshipType string,
	side string,
	info PeeringTransitGatewayInfo,
) (awscloud.RelationshipObservation, bool) {
	peerTGW := strings.TrimSpace(info.TransitGatewayID)
	if peerTGW == "" {
		return awscloud.RelationshipObservation{}, false
	}
	ownerID := strings.TrimSpace(info.OwnerID)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: relationshipType,
		SourceResourceID: id,
		TargetResourceID: peerTGW,
		TargetType:       awscloud.ResourceTypeTransitGateway,
		Attributes: map[string]any{
			"core_network_id": strings.TrimSpace(info.CoreNetworkID),
			"cross_account":   ownerID != "" && ownerID != strings.TrimSpace(boundary.AccountID),
			"owner_id":        ownerID,
			"region":          strings.TrimSpace(info.Region),
			"side":            side,
		},
		SourceRecordID: id + "#" + side + "#" + peerTGW,
	}, true
}

func multicastDomainRelationships(
	boundary awscloud.Boundary,
	domain MulticastDomain,
) []awscloud.RelationshipObservation {
	id := strings.TrimSpace(domain.ID)
	tgwID := strings.TrimSpace(domain.TransitGatewayID)
	if id == "" || tgwID == "" {
		return nil
	}
	return []awscloud.RelationshipObservation{{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipTransitGatewayMulticastDomainInTransitGateway,
		SourceResourceID: id,
		TargetResourceID: tgwID,
		TargetType:       awscloud.ResourceTypeTransitGateway,
		SourceRecordID:   id + "#transit-gateway#" + tgwID,
	}}
}

func policyTableRelationships(
	boundary awscloud.Boundary,
	policyTable PolicyTable,
) []awscloud.RelationshipObservation {
	id := strings.TrimSpace(policyTable.ID)
	tgwID := strings.TrimSpace(policyTable.TransitGatewayID)
	if id == "" || tgwID == "" {
		return nil
	}
	return []awscloud.RelationshipObservation{{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipTransitGatewayPolicyTableInTransitGateway,
		SourceResourceID: id,
		TargetResourceID: tgwID,
		TargetType:       awscloud.ResourceTypeTransitGateway,
		SourceRecordID:   id + "#transit-gateway#" + tgwID,
	}}
}

func attachmentResourceAttributes(resourceOwnerID string) map[string]any {
	if resourceOwnerID == "" {
		return nil
	}
	return map[string]any{
		"resource_owner_id": resourceOwnerID,
	}
}

// normalizeAttachmentResourceType lower-cases the AWS-reported attachment
// resource type so the dispatch switch is stable across SDK enum casing.
func normalizeAttachmentResourceType(resourceType string) string {
	return strings.ToLower(strings.TrimSpace(resourceType))
}
