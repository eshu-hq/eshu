// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package directconnect

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// connectionRelationships links a connection to its parent LAG when AWS reports
// one. The target is the aws_direct_connect_lag identity owned by this scanner.
func connectionRelationships(boundary aws.Boundary, connection Connection) []aws.RelationshipObservation {
	id := strings.TrimSpace(connection.ID)
	lagID := strings.TrimSpace(connection.LAGID)
	if id == "" || lagID == "" {
		return nil
	}
	return []aws.RelationshipObservation{{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipDirectConnectConnectionInLAG,
		SourceResourceID: id,
		TargetResourceID: lagID,
		TargetType:       aws.ResourceTypeDirectConnectLAG,
		SourceRecordID:   id + "#lag#" + lagID,
	}}
}

// virtualInterfaceRelationships links a virtual interface to the Direct Connect
// gateway it attaches to and to the physical connection it runs over, when AWS
// reports each. Both targets are scanner-owned identities keyed by AWS-reported
// ID so the graph join lands on the matching node.
func virtualInterfaceRelationships(boundary aws.Boundary, vif VirtualInterface) []aws.RelationshipObservation {
	id := strings.TrimSpace(vif.ID)
	if id == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	if gatewayID := strings.TrimSpace(vif.GatewayID); gatewayID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipDirectConnectVirtualInterfaceToGateway,
			SourceResourceID: id,
			TargetResourceID: gatewayID,
			TargetType:       aws.ResourceTypeDirectConnectGateway,
			SourceRecordID:   id + "#direct-connect-gateway#" + gatewayID,
		})
	}
	if connectionID := strings.TrimSpace(vif.ConnectionID); connectionID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipDirectConnectVirtualInterfaceToConnection,
			SourceResourceID: id,
			TargetResourceID: connectionID,
			TargetType:       aws.ResourceTypeDirectConnectConnection,
			SourceRecordID:   id + "#connection#" + connectionID,
		})
	}
	return observations
}

// gatewayAssociationRelationship maps a Direct Connect gateway association to
// the edge that links the gateway to its associated transit gateway or virtual
// private gateway. The associated gateway can be in a different account; the
// owner accounts AWS reports are surfaced on the association attributes by the
// scanner. Associations whose type is neither transit nor virtual private
// gateway emit no edge rather than fabricate a typed target.
func gatewayAssociationRelationship(
	boundary aws.Boundary,
	association GatewayAssociation,
) (aws.RelationshipObservation, bool) {
	gatewayID := strings.TrimSpace(association.GatewayID)
	if gatewayID == "" {
		return aws.RelationshipObservation{}, false
	}
	targetID := firstNonEmpty(association.AssociatedGatewayID, association.VirtualGatewayID)
	if targetID == "" {
		return aws.RelationshipObservation{}, false
	}
	attributes := map[string]any{
		"association_id":    strings.TrimSpace(association.AssociationID),
		"association_state": strings.TrimSpace(association.AssociationState),
	}
	switch normalizeGatewayType(association.AssociatedGatewayType) {
	case "transitgateway":
		return aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipDirectConnectGatewayToTransitGateway,
			SourceResourceID: gatewayID,
			TargetResourceID: targetID,
			TargetType:       aws.ResourceTypeTransitGateway,
			Attributes:       attributes,
			SourceRecordID:   gatewayID + "#transit-gateway#" + targetID,
		}, true
	case "virtualprivategateway":
		return aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipDirectConnectGatewayToVPNGateway,
			SourceResourceID: gatewayID,
			TargetResourceID: targetID,
			TargetType:       aws.ResourceTypeVPCVPNGateway,
			Attributes:       attributes,
			SourceRecordID:   gatewayID + "#vpn-gateway#" + targetID,
		}, true
	default:
		// A legacy association may report only VirtualGatewayID with no typed
		// AssociatedGateway. Treat that as a virtual private gateway edge.
		if strings.TrimSpace(association.AssociatedGatewayType) == "" &&
			strings.TrimSpace(association.VirtualGatewayID) != "" {
			return aws.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: aws.RelationshipDirectConnectGatewayToVPNGateway,
				SourceResourceID: gatewayID,
				TargetResourceID: strings.TrimSpace(association.VirtualGatewayID),
				TargetType:       aws.ResourceTypeVPCVPNGateway,
				Attributes:       attributes,
				SourceRecordID:   gatewayID + "#vpn-gateway#" + strings.TrimSpace(association.VirtualGatewayID),
			}, true
		}
		return aws.RelationshipObservation{}, false
	}
}

// normalizeGatewayType lower-cases the AWS-reported associated gateway type so
// the dispatch switch is stable across SDK enum casing.
func normalizeGatewayType(gatewayType string) string {
	return strings.ToLower(strings.TrimSpace(gatewayType))
}
