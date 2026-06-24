// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	dxtypes "github.com/aws/aws-sdk-go-v2/service/directconnect/types"

	dxservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/directconnect"
)

func mapConnection(connection dxtypes.Connection) dxservice.Connection {
	return dxservice.Connection{
		ID:            aws.ToString(connection.ConnectionId),
		Name:          aws.ToString(connection.ConnectionName),
		OwnerAccount:  aws.ToString(connection.OwnerAccount),
		Location:      aws.ToString(connection.Location),
		Bandwidth:     aws.ToString(connection.Bandwidth),
		State:         string(connection.ConnectionState),
		Region:        aws.ToString(connection.Region),
		PartnerName:   aws.ToString(connection.PartnerName),
		ProviderName:  aws.ToString(connection.ProviderName),
		LAGID:         aws.ToString(connection.LagId),
		VLAN:          connection.Vlan,
		JumboFrames:   aws.ToBool(connection.JumboFrameCapable),
		MacSecCapable: aws.ToBool(connection.MacSecCapable),
		Tags:          mapTags(connection.Tags),
	}
}

// mapVirtualInterface maps an AWS virtual interface into the scanner-owned
// record. It deliberately ignores VirtualInterface.AuthKey and the per-peer
// BgpPeers[].AuthKey: the BGP authentication key is secret material outside the
// inventory contract and the scanner-owned type has no field for it.
func mapVirtualInterface(vif dxtypes.VirtualInterface) dxservice.VirtualInterface {
	return dxservice.VirtualInterface{
		ID:             aws.ToString(vif.VirtualInterfaceId),
		Name:           aws.ToString(vif.VirtualInterfaceName),
		Type:           aws.ToString(vif.VirtualInterfaceType),
		State:          string(vif.VirtualInterfaceState),
		OwnerAccount:   aws.ToString(vif.OwnerAccount),
		Location:       aws.ToString(vif.Location),
		ConnectionID:   aws.ToString(vif.ConnectionId),
		GatewayID:      aws.ToString(vif.DirectConnectGatewayId),
		VirtualGateway: aws.ToString(vif.VirtualGatewayId),
		VLAN:           vif.Vlan,
		ASN:            vif.Asn,
		AmazonSideASN:  aws.ToInt64(vif.AmazonSideAsn),
		AddressFamily:  string(vif.AddressFamily),
		Tags:           mapTags(vif.Tags),
	}
}

func mapGateway(gateway dxtypes.DirectConnectGateway) dxservice.Gateway {
	return dxservice.Gateway{
		ID:            aws.ToString(gateway.DirectConnectGatewayId),
		Name:          aws.ToString(gateway.DirectConnectGatewayName),
		State:         string(gateway.DirectConnectGatewayState),
		OwnerAccount:  aws.ToString(gateway.OwnerAccount),
		AmazonSideASN: aws.ToInt64(gateway.AmazonSideAsn),
	}
}

// mapLAG maps an AWS LAG into the scanner-owned record. As with mapConnection,
// LAG.MacSecKeys (CKN and secret ARNs) are intentionally not mapped; only the
// boolean MacSecCapable capability flag is surfaced.
func mapLAG(lag dxtypes.Lag) dxservice.LAG {
	return dxservice.LAG{
		ID:                  aws.ToString(lag.LagId),
		Name:                aws.ToString(lag.LagName),
		OwnerAccount:        aws.ToString(lag.OwnerAccount),
		Location:            aws.ToString(lag.Location),
		Bandwidth:           aws.ToString(lag.ConnectionsBandwidth),
		State:               string(lag.LagState),
		Region:              aws.ToString(lag.Region),
		ProviderName:        aws.ToString(lag.ProviderName),
		MinimumLinks:        lag.MinimumLinks,
		NumberOfConnections: lag.NumberOfConnections,
		MacSecCapable:       aws.ToBool(lag.MacSecCapable),
		Tags:                mapTags(lag.Tags),
	}
}

func mapGatewayAssociation(association dxtypes.DirectConnectGatewayAssociation) dxservice.GatewayAssociation {
	associatedID := ""
	associatedType := ""
	if association.AssociatedGateway != nil {
		associatedID = aws.ToString(association.AssociatedGateway.Id)
		associatedType = string(association.AssociatedGateway.Type)
	}
	return dxservice.GatewayAssociation{
		GatewayID:             aws.ToString(association.DirectConnectGatewayId),
		AssociationID:         aws.ToString(association.AssociationId),
		AssociationState:      string(association.AssociationState),
		AssociatedGatewayID:   associatedID,
		AssociatedGatewayType: associatedType,
		VirtualGatewayID:      aws.ToString(association.VirtualGatewayId),
	}
}

func mapTags(tags []dxtypes.Tag) map[string]string {
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
