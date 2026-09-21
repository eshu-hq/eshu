// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package transitgateway

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func routeTableRelationships(boundary aws.Boundary, rt RouteTable) []aws.RelationshipObservation {
	id := strings.TrimSpace(rt.ID)
	tgwID := strings.TrimSpace(rt.TransitGatewayID)
	if id == "" || tgwID == "" {
		return nil
	}
	return []aws.RelationshipObservation{{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipTransitGatewayRouteTableInTransitGateway,
		SourceResourceID: id,
		TargetResourceID: tgwID,
		TargetType:       aws.ResourceTypeTransitGateway,
		SourceRecordID:   id + "#transit-gateway#" + tgwID,
	}}
}

func attachmentRelationships(boundary aws.Boundary, attachment Attachment) []aws.RelationshipObservation {
	id := strings.TrimSpace(attachment.ID)
	if id == "" {
		return nil
	}
	var observations []aws.RelationshipObservation

	if tgwID := strings.TrimSpace(attachment.TransitGatewayID); tgwID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipTransitGatewayAttachmentToTransitGateway,
			SourceResourceID: id,
			TargetResourceID: tgwID,
			TargetType:       aws.ResourceTypeTransitGateway,
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
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipTransitGatewayRouteTableToAttachment,
			SourceResourceID: rtID,
			TargetResourceID: id,
			TargetType:       aws.ResourceTypeTransitGatewayAttachment,
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
	boundary aws.Boundary,
	id string,
	attachment Attachment,
) (aws.RelationshipObservation, bool) {
	resourceID := strings.TrimSpace(attachment.ResourceID)
	if resourceID == "" {
		return aws.RelationshipObservation{}, false
	}
	resourceOwnerID := strings.TrimSpace(attachment.ResourceOwnerID)
	switch normalizeAttachmentResourceType(attachment.ResourceType) {
	case "vpc":
		return aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipTransitGatewayAttachmentToVPC,
			SourceResourceID: id,
			TargetResourceID: resourceID,
			TargetType:       aws.ResourceTypeEC2VPC,
			Attributes:       attachmentResourceAttributes(resourceOwnerID),
			SourceRecordID:   id + "#vpc#" + resourceID,
		}, true
	case "vpn":
		return aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipTransitGatewayAttachmentToVPNConnection,
			SourceResourceID: id,
			TargetResourceID: resourceID,
			TargetType:       aws.ResourceTypeVPCVPNConnection,
			Attributes:       attachmentResourceAttributes(resourceOwnerID),
			SourceRecordID:   id + "#vpn#" + resourceID,
		}, true
	case "direct-connect-gateway":
		return aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipTransitGatewayAttachmentToDirectConnectGateway,
			SourceResourceID: id,
			TargetResourceID: resourceID,
			TargetType:       "aws_direct_connect_gateway",
			Attributes:       attachmentResourceAttributes(resourceOwnerID),
			SourceRecordID:   id + "#direct-connect-gateway#" + resourceID,
		}, true
	case "peering":
		return aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipTransitGatewayAttachmentToPeer,
			SourceResourceID: id,
			TargetResourceID: resourceID,
			TargetType:       aws.ResourceTypeTransitGatewayPeeringAttachment,
			Attributes:       attachmentResourceAttributes(resourceOwnerID),
			SourceRecordID:   id + "#peer#" + resourceID,
		}, true
	default:
		return aws.RelationshipObservation{}, false
	}
}

func peeringAttachmentRelationships(
	boundary aws.Boundary,
	peering PeeringAttachment,
) []aws.RelationshipObservation {
	id := strings.TrimSpace(peering.ID)
	if id == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	if observation, ok := peeringSideRelationship(
		boundary,
		id,
		aws.RelationshipTransitGatewayPeeringRequestsTransitGateway,
		"requester",
		peering.Requester,
	); ok {
		observations = append(observations, observation)
	}
	if observation, ok := peeringSideRelationship(
		boundary,
		id,
		aws.RelationshipTransitGatewayPeeringAcceptsTransitGateway,
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
	boundary aws.Boundary,
	id string,
	relationshipType string,
	side string,
	info PeeringTransitGatewayInfo,
) (aws.RelationshipObservation, bool) {
	peerTGW := strings.TrimSpace(info.TransitGatewayID)
	if peerTGW == "" {
		return aws.RelationshipObservation{}, false
	}
	ownerID := strings.TrimSpace(info.OwnerID)
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: relationshipType,
		SourceResourceID: id,
		TargetResourceID: peerTGW,
		TargetType:       aws.ResourceTypeTransitGateway,
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
	boundary aws.Boundary,
	domain MulticastDomain,
) []aws.RelationshipObservation {
	id := strings.TrimSpace(domain.ID)
	tgwID := strings.TrimSpace(domain.TransitGatewayID)
	if id == "" || tgwID == "" {
		return nil
	}
	return []aws.RelationshipObservation{{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipTransitGatewayMulticastDomainInTransitGateway,
		SourceResourceID: id,
		TargetResourceID: tgwID,
		TargetType:       aws.ResourceTypeTransitGateway,
		SourceRecordID:   id + "#transit-gateway#" + tgwID,
	}}
}

func policyTableRelationships(
	boundary aws.Boundary,
	policyTable PolicyTable,
) []aws.RelationshipObservation {
	id := strings.TrimSpace(policyTable.ID)
	tgwID := strings.TrimSpace(policyTable.TransitGatewayID)
	if id == "" || tgwID == "" {
		return nil
	}
	return []aws.RelationshipObservation{{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipTransitGatewayPolicyTableInTransitGateway,
		SourceResourceID: id,
		TargetResourceID: tgwID,
		TargetType:       aws.ResourceTypeTransitGateway,
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
