// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package directconnect

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// connectionRelationships links a connection to its parent LAG when AWS reports
// one. The target is the aws_direct_connect_lag identity owned by this scanner.
func connectionRelationships(boundary awscloud.Boundary, connection Connection) []awscloud.RelationshipObservation {
	id := strings.TrimSpace(connection.ID)
	lagID := strings.TrimSpace(connection.LAGID)
	if id == "" || lagID == "" {
		return nil
	}
	return []awscloud.RelationshipObservation{{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipDirectConnectConnectionInLAG,
		SourceResourceID: id,
		TargetResourceID: lagID,
		TargetType:       awscloud.ResourceTypeDirectConnectLAG,
		SourceRecordID:   id + "#lag#" + lagID,
	}}
}

// virtualInterfaceRelationships links a virtual interface to the Direct Connect
// gateway it attaches to and to the physical connection it runs over, when AWS
// reports each. Both targets are scanner-owned identities keyed by AWS-reported
// ID so the graph join lands on the matching node.
func virtualInterfaceRelationships(boundary awscloud.Boundary, vif VirtualInterface) []awscloud.RelationshipObservation {
	id := strings.TrimSpace(vif.ID)
	if id == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	if gatewayID := strings.TrimSpace(vif.GatewayID); gatewayID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipDirectConnectVirtualInterfaceToGateway,
			SourceResourceID: id,
			TargetResourceID: gatewayID,
			TargetType:       awscloud.ResourceTypeDirectConnectGateway,
			SourceRecordID:   id + "#direct-connect-gateway#" + gatewayID,
		})
	}
	if connectionID := strings.TrimSpace(vif.ConnectionID); connectionID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipDirectConnectVirtualInterfaceToConnection,
			SourceResourceID: id,
			TargetResourceID: connectionID,
			TargetType:       awscloud.ResourceTypeDirectConnectConnection,
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
	boundary awscloud.Boundary,
	association GatewayAssociation,
) (awscloud.RelationshipObservation, bool) {
	gatewayID := strings.TrimSpace(association.GatewayID)
	if gatewayID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	targetID := firstNonEmpty(association.AssociatedGatewayID, association.VirtualGatewayID)
	if targetID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	attributes := map[string]any{
		"association_id":    strings.TrimSpace(association.AssociationID),
		"association_state": strings.TrimSpace(association.AssociationState),
	}
	switch normalizeGatewayType(association.AssociatedGatewayType) {
	case "transitgateway":
		return awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipDirectConnectGatewayToTransitGateway,
			SourceResourceID: gatewayID,
			TargetResourceID: targetID,
			TargetType:       awscloud.ResourceTypeTransitGateway,
			Attributes:       attributes,
			SourceRecordID:   gatewayID + "#transit-gateway#" + targetID,
		}, true
	case "virtualprivategateway":
		return awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipDirectConnectGatewayToVPNGateway,
			SourceResourceID: gatewayID,
			TargetResourceID: targetID,
			TargetType:       awscloud.ResourceTypeVPCVPNGateway,
			Attributes:       attributes,
			SourceRecordID:   gatewayID + "#vpn-gateway#" + targetID,
		}, true
	default:
		// A legacy association may report only VirtualGatewayID with no typed
		// AssociatedGateway. Treat that as a virtual private gateway edge.
		if strings.TrimSpace(association.AssociatedGatewayType) == "" &&
			strings.TrimSpace(association.VirtualGatewayID) != "" {
			return awscloud.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: awscloud.RelationshipDirectConnectGatewayToVPNGateway,
				SourceResourceID: gatewayID,
				TargetResourceID: strings.TrimSpace(association.VirtualGatewayID),
				TargetType:       awscloud.ResourceTypeVPCVPNGateway,
				Attributes:       attributes,
				SourceRecordID:   gatewayID + "#vpn-gateway#" + strings.TrimSpace(association.VirtualGatewayID),
			}, true
		}
		return awscloud.RelationshipObservation{}, false
	}
}

// normalizeGatewayType lower-cases the AWS-reported associated gateway type so
// the dispatch switch is stable across SDK enum casing.
func normalizeGatewayType(gatewayType string) string {
	return strings.ToLower(strings.TrimSpace(gatewayType))
}
