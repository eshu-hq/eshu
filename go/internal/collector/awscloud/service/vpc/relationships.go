// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vpc

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func routeTableRelationships(boundary awscloud.Boundary, rt RouteTable) []awscloud.RelationshipObservation {
	rtID := strings.TrimSpace(rt.ID)
	if rtID == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	if vpcID := strings.TrimSpace(rt.VPCID); vpcID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipVPCRouteTableInVPC,
			SourceResourceID: rtID,
			TargetResourceID: vpcID,
			TargetType:       awscloud.ResourceTypeEC2VPC,
			SourceRecordID:   rtID + "#vpc#" + vpcID,
		})
	}
	for _, association := range rt.Associations {
		subnetID := strings.TrimSpace(association.SubnetID)
		if subnetID == "" {
			continue
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipVPCRouteTableAssociatedWithSubnet,
			SourceResourceID: rtID,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			Attributes: map[string]any{
				"association_id": strings.TrimSpace(association.AssociationID),
				"main":           association.Main,
				"state":          strings.TrimSpace(association.State),
			},
			SourceRecordID: rtID + "#association#" + subnetID,
		})
	}
	for _, route := range rt.Routes {
		observations = append(observations, routeTargetRelationships(boundary, rtID, route)...)
	}
	return observations
}

func routeTargetRelationships(
	boundary awscloud.Boundary,
	rtID string,
	route Route,
) []awscloud.RelationshipObservation {
	var observations []awscloud.RelationshipObservation
	destination := routeDestination(route)
	if igwID := strings.TrimSpace(route.GatewayID); strings.HasPrefix(igwID, "igw-") {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipVPCRouteTargetsInternetGateway,
			SourceResourceID: rtID,
			TargetResourceID: igwID,
			TargetType:       awscloud.ResourceTypeVPCInternetGateway,
			Attributes:       routeRelationshipAttributes(route, destination),
			SourceRecordID:   rtID + "#route#igw#" + destination + "#" + igwID,
		})
	}
	if natID := strings.TrimSpace(route.NATGatewayID); natID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipVPCRouteTargetsNATGateway,
			SourceResourceID: rtID,
			TargetResourceID: natID,
			TargetType:       awscloud.ResourceTypeVPCNATGateway,
			Attributes:       routeRelationshipAttributes(route, destination),
			SourceRecordID:   rtID + "#route#nat#" + destination + "#" + natID,
		})
	}
	if peeringID := strings.TrimSpace(route.VPCPeeringConnectionID); peeringID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipVPCRouteTargetsPeeringConnection,
			SourceResourceID: rtID,
			TargetResourceID: peeringID,
			TargetType:       awscloud.ResourceTypeVPCPeeringConnection,
			Attributes:       routeRelationshipAttributes(route, destination),
			SourceRecordID:   rtID + "#route#peering#" + destination + "#" + peeringID,
		})
	}
	if endpointID := strings.TrimSpace(route.VPCEndpointID); endpointID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipVPCRouteTargetsVPCEndpoint,
			SourceResourceID: rtID,
			TargetResourceID: endpointID,
			TargetType:       awscloud.ResourceTypeVPCEndpoint,
			Attributes:       routeRelationshipAttributes(route, destination),
			SourceRecordID:   rtID + "#route#endpoint#" + destination + "#" + endpointID,
		})
	}
	if tgwID := strings.TrimSpace(route.TransitGatewayID); tgwID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipVPCRouteTargetsTransitGateway,
			SourceResourceID: rtID,
			TargetResourceID: tgwID,
			TargetType:       "aws_ec2_transit_gateway",
			Attributes:       routeRelationshipAttributes(route, destination),
			SourceRecordID:   rtID + "#route#tgw#" + destination + "#" + tgwID,
		})
	}
	return observations
}

func routeDestination(route Route) string {
	if cidr := strings.TrimSpace(route.DestinationCIDRBlock); cidr != "" {
		return cidr
	}
	if cidr := strings.TrimSpace(route.DestinationIPv6CIDRBlock); cidr != "" {
		return cidr
	}
	if prefix := strings.TrimSpace(route.DestinationPrefixListID); prefix != "" {
		return prefix
	}
	return ""
}

func routeRelationshipAttributes(route Route, destination string) map[string]any {
	return map[string]any{
		"destination": destination,
		"origin":      strings.TrimSpace(route.Origin),
		"state":       strings.TrimSpace(route.State),
	}
}

func internetGatewayRelationships(
	boundary awscloud.Boundary,
	gateway InternetGateway,
) []awscloud.RelationshipObservation {
	id := strings.TrimSpace(gateway.ID)
	if id == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	for _, attachment := range gateway.Attachments {
		vpcID := strings.TrimSpace(attachment.VPCID)
		if vpcID == "" {
			continue
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipVPCInternetGatewayAttachedToVPC,
			SourceResourceID: id,
			TargetResourceID: vpcID,
			TargetType:       awscloud.ResourceTypeEC2VPC,
			Attributes: map[string]any{
				"state": strings.TrimSpace(attachment.State),
			},
			SourceRecordID: id + "#vpc#" + vpcID,
		})
	}
	return observations
}

func natGatewayRelationships(
	boundary awscloud.Boundary,
	gateway NATGateway,
) []awscloud.RelationshipObservation {
	id := strings.TrimSpace(gateway.ID)
	if id == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	if subnetID := strings.TrimSpace(gateway.SubnetID); subnetID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipVPCNATGatewayInSubnet,
			SourceResourceID: id,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			SourceRecordID:   id + "#subnet#" + subnetID,
		})
	}
	if vpcID := strings.TrimSpace(gateway.VPCID); vpcID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipVPCNATGatewayInVPC,
			SourceResourceID: id,
			TargetResourceID: vpcID,
			TargetType:       awscloud.ResourceTypeEC2VPC,
			SourceRecordID:   id + "#vpc#" + vpcID,
		})
	}
	return observations
}

func networkACLRelationships(
	boundary awscloud.Boundary,
	networkACL NetworkACL,
) []awscloud.RelationshipObservation {
	id := strings.TrimSpace(networkACL.ID)
	if id == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	if vpcID := strings.TrimSpace(networkACL.VPCID); vpcID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipVPCNetworkACLInVPC,
			SourceResourceID: id,
			TargetResourceID: vpcID,
			TargetType:       awscloud.ResourceTypeEC2VPC,
			SourceRecordID:   id + "#vpc#" + vpcID,
		})
	}
	for _, association := range networkACL.Associations {
		subnetID := strings.TrimSpace(association.SubnetID)
		if subnetID == "" {
			continue
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipVPCNetworkACLAssociatedWithSubnet,
			SourceResourceID: id,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			Attributes: map[string]any{
				"association_id": strings.TrimSpace(association.AssociationID),
			},
			SourceRecordID: id + "#association#" + subnetID,
		})
	}
	return observations
}

func vpcPeeringRelationships(
	boundary awscloud.Boundary,
	peering VPCPeeringConnection,
) []awscloud.RelationshipObservation {
	id := strings.TrimSpace(peering.ID)
	if id == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	for label, info := range map[string]VPCPeeringVPCInfo{
		"requester": peering.Requester,
		"accepter":  peering.Accepter,
	} {
		vpcID := strings.TrimSpace(info.VPCID)
		if vpcID == "" {
			continue
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipVPCPeeringConnectsVPC,
			SourceResourceID: id,
			TargetResourceID: vpcID,
			TargetType:       awscloud.ResourceTypeEC2VPC,
			Attributes: map[string]any{
				"owner_id": strings.TrimSpace(info.OwnerID),
				"region":   strings.TrimSpace(info.Region),
				"side":     label,
			},
			SourceRecordID: id + "#" + label + "#" + vpcID,
		})
	}
	return observations
}

func vpcEndpointRelationships(
	boundary awscloud.Boundary,
	endpoint VPCEndpoint,
) []awscloud.RelationshipObservation {
	id := strings.TrimSpace(endpoint.ID)
	if id == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	if vpcID := strings.TrimSpace(endpoint.VPCID); vpcID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipVPCEndpointInVPC,
			SourceResourceID: id,
			TargetResourceID: vpcID,
			TargetType:       awscloud.ResourceTypeEC2VPC,
			SourceRecordID:   id + "#vpc#" + vpcID,
		})
	}
	if service := strings.TrimSpace(endpoint.ServiceName); service != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipVPCEndpointUsesService,
			SourceResourceID: id,
			TargetResourceID: service,
			TargetType:       "aws_vpc_endpoint_service",
			Attributes: map[string]any{
				"endpoint_type": strings.TrimSpace(endpoint.EndpointType),
			},
			SourceRecordID: id + "#service#" + service,
		})
	}
	return observations
}

func elasticIPRelationships(
	boundary awscloud.Boundary,
	eip ElasticIP,
) []awscloud.RelationshipObservation {
	id := strings.TrimSpace(eip.AllocationID)
	if id == "" {
		id = strings.TrimSpace(eip.PublicIP)
	}
	if id == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	if instanceID := strings.TrimSpace(eip.InstanceID); instanceID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipVPCElasticIPAssociatedWithInstance,
			SourceResourceID: id,
			TargetResourceID: instanceID,
			TargetType:       "aws_ec2_instance",
			Attributes: map[string]any{
				"association_id": strings.TrimSpace(eip.AssociationID),
				"private_ip":     strings.TrimSpace(eip.PrivateIP),
				"public_ip":      strings.TrimSpace(eip.PublicIP),
			},
			SourceRecordID: id + "#instance#" + instanceID,
		})
	}
	if eniID := strings.TrimSpace(eip.NetworkInterfaceID); eniID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipVPCElasticIPAssociatedWithNetworkInterface,
			SourceResourceID: id,
			TargetResourceID: eniID,
			TargetType:       awscloud.ResourceTypeEC2NetworkInterface,
			Attributes: map[string]any{
				"association_id":             strings.TrimSpace(eip.AssociationID),
				"network_interface_owner_id": strings.TrimSpace(eip.NetworkInterfaceOwnerID),
				"private_ip":                 strings.TrimSpace(eip.PrivateIP),
				"public_ip":                  strings.TrimSpace(eip.PublicIP),
			},
			SourceRecordID: id + "#eni#" + eniID,
		})
	}
	return observations
}

func vpnGatewayRelationships(
	boundary awscloud.Boundary,
	gateway VPNGateway,
) []awscloud.RelationshipObservation {
	id := strings.TrimSpace(gateway.ID)
	if id == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	for _, attachment := range gateway.VPCAttachments {
		vpcID := strings.TrimSpace(attachment.VPCID)
		if vpcID == "" {
			continue
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipVPCVPNGatewayAttachedToVPC,
			SourceResourceID: id,
			TargetResourceID: vpcID,
			TargetType:       awscloud.ResourceTypeEC2VPC,
			Attributes: map[string]any{
				"state": strings.TrimSpace(attachment.State),
			},
			SourceRecordID: id + "#vpc#" + vpcID,
		})
	}
	return observations
}

func vpnConnectionRelationships(
	boundary awscloud.Boundary,
	connection VPNConnection,
) []awscloud.RelationshipObservation {
	id := strings.TrimSpace(connection.ID)
	if id == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	if cgwID := strings.TrimSpace(connection.CustomerGatewayID); cgwID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipVPCVPNConnectionUsesCustomerGateway,
			SourceResourceID: id,
			TargetResourceID: cgwID,
			TargetType:       awscloud.ResourceTypeVPCCustomerGateway,
			SourceRecordID:   id + "#customer-gateway#" + cgwID,
		})
	}
	if vgwID := strings.TrimSpace(connection.VPNGatewayID); vgwID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipVPCVPNConnectionUsesVPNGateway,
			SourceResourceID: id,
			TargetResourceID: vgwID,
			TargetType:       awscloud.ResourceTypeVPCVPNGateway,
			SourceRecordID:   id + "#vpn-gateway#" + vgwID,
		})
	}
	if tgwID := strings.TrimSpace(connection.TransitGatewayID); tgwID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipVPCVPNConnectionUsesTransitGateway,
			SourceResourceID: id,
			TargetResourceID: tgwID,
			TargetType:       "aws_ec2_transit_gateway",
			SourceRecordID:   id + "#transit-gateway#" + tgwID,
		})
	}
	return observations
}
