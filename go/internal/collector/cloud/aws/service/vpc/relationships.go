// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vpc

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func routeTableRelationships(boundary aws.Boundary, rt RouteTable) []aws.RelationshipObservation {
	rtID := strings.TrimSpace(rt.ID)
	if rtID == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	if vpcID := strings.TrimSpace(rt.VPCID); vpcID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVPCRouteTableInVPC,
			SourceResourceID: rtID,
			TargetResourceID: vpcID,
			TargetType:       aws.ResourceTypeEC2VPC,
			SourceRecordID:   rtID + "#vpc#" + vpcID,
		})
	}
	for _, association := range rt.Associations {
		subnetID := strings.TrimSpace(association.SubnetID)
		if subnetID == "" {
			continue
		}
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVPCRouteTableAssociatedWithSubnet,
			SourceResourceID: rtID,
			TargetResourceID: subnetID,
			TargetType:       aws.ResourceTypeEC2Subnet,
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
	boundary aws.Boundary,
	rtID string,
	route Route,
) []aws.RelationshipObservation {
	var observations []aws.RelationshipObservation
	destination := routeDestination(route)
	if igwID := strings.TrimSpace(route.GatewayID); strings.HasPrefix(igwID, "igw-") {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVPCRouteTargetsInternetGateway,
			SourceResourceID: rtID,
			TargetResourceID: igwID,
			TargetType:       aws.ResourceTypeVPCInternetGateway,
			Attributes:       routeRelationshipAttributes(route, destination),
			SourceRecordID:   rtID + "#route#igw#" + destination + "#" + igwID,
		})
	}
	if natID := strings.TrimSpace(route.NATGatewayID); natID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVPCRouteTargetsNATGateway,
			SourceResourceID: rtID,
			TargetResourceID: natID,
			TargetType:       aws.ResourceTypeVPCNATGateway,
			Attributes:       routeRelationshipAttributes(route, destination),
			SourceRecordID:   rtID + "#route#nat#" + destination + "#" + natID,
		})
	}
	if peeringID := strings.TrimSpace(route.VPCPeeringConnectionID); peeringID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVPCRouteTargetsPeeringConnection,
			SourceResourceID: rtID,
			TargetResourceID: peeringID,
			TargetType:       aws.ResourceTypeVPCPeeringConnection,
			Attributes:       routeRelationshipAttributes(route, destination),
			SourceRecordID:   rtID + "#route#peering#" + destination + "#" + peeringID,
		})
	}
	if endpointID := strings.TrimSpace(route.VPCEndpointID); endpointID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVPCRouteTargetsVPCEndpoint,
			SourceResourceID: rtID,
			TargetResourceID: endpointID,
			TargetType:       aws.ResourceTypeVPCEndpoint,
			Attributes:       routeRelationshipAttributes(route, destination),
			SourceRecordID:   rtID + "#route#endpoint#" + destination + "#" + endpointID,
		})
	}
	if tgwID := strings.TrimSpace(route.TransitGatewayID); tgwID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVPCRouteTargetsTransitGateway,
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
	boundary aws.Boundary,
	gateway InternetGateway,
) []aws.RelationshipObservation {
	id := strings.TrimSpace(gateway.ID)
	if id == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	for _, attachment := range gateway.Attachments {
		vpcID := strings.TrimSpace(attachment.VPCID)
		if vpcID == "" {
			continue
		}
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVPCInternetGatewayAttachedToVPC,
			SourceResourceID: id,
			TargetResourceID: vpcID,
			TargetType:       aws.ResourceTypeEC2VPC,
			Attributes: map[string]any{
				"state": strings.TrimSpace(attachment.State),
			},
			SourceRecordID: id + "#vpc#" + vpcID,
		})
	}
	return observations
}

func natGatewayRelationships(
	boundary aws.Boundary,
	gateway NATGateway,
) []aws.RelationshipObservation {
	id := strings.TrimSpace(gateway.ID)
	if id == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	if subnetID := strings.TrimSpace(gateway.SubnetID); subnetID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVPCNATGatewayInSubnet,
			SourceResourceID: id,
			TargetResourceID: subnetID,
			TargetType:       aws.ResourceTypeEC2Subnet,
			SourceRecordID:   id + "#subnet#" + subnetID,
		})
	}
	if vpcID := strings.TrimSpace(gateway.VPCID); vpcID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVPCNATGatewayInVPC,
			SourceResourceID: id,
			TargetResourceID: vpcID,
			TargetType:       aws.ResourceTypeEC2VPC,
			SourceRecordID:   id + "#vpc#" + vpcID,
		})
	}
	return observations
}

func networkACLRelationships(
	boundary aws.Boundary,
	networkACL NetworkACL,
) []aws.RelationshipObservation {
	id := strings.TrimSpace(networkACL.ID)
	if id == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	if vpcID := strings.TrimSpace(networkACL.VPCID); vpcID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVPCNetworkACLInVPC,
			SourceResourceID: id,
			TargetResourceID: vpcID,
			TargetType:       aws.ResourceTypeEC2VPC,
			SourceRecordID:   id + "#vpc#" + vpcID,
		})
	}
	for _, association := range networkACL.Associations {
		subnetID := strings.TrimSpace(association.SubnetID)
		if subnetID == "" {
			continue
		}
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVPCNetworkACLAssociatedWithSubnet,
			SourceResourceID: id,
			TargetResourceID: subnetID,
			TargetType:       aws.ResourceTypeEC2Subnet,
			Attributes: map[string]any{
				"association_id": strings.TrimSpace(association.AssociationID),
			},
			SourceRecordID: id + "#association#" + subnetID,
		})
	}
	return observations
}

func vpcPeeringRelationships(
	boundary aws.Boundary,
	peering VPCPeeringConnection,
) []aws.RelationshipObservation {
	id := strings.TrimSpace(peering.ID)
	if id == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	for label, info := range map[string]VPCPeeringVPCInfo{
		"requester": peering.Requester,
		"accepter":  peering.Accepter,
	} {
		vpcID := strings.TrimSpace(info.VPCID)
		if vpcID == "" {
			continue
		}
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVPCPeeringConnectsVPC,
			SourceResourceID: id,
			TargetResourceID: vpcID,
			TargetType:       aws.ResourceTypeEC2VPC,
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
	boundary aws.Boundary,
	endpoint VPCEndpoint,
) []aws.RelationshipObservation {
	id := strings.TrimSpace(endpoint.ID)
	if id == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	if vpcID := strings.TrimSpace(endpoint.VPCID); vpcID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVPCEndpointInVPC,
			SourceResourceID: id,
			TargetResourceID: vpcID,
			TargetType:       aws.ResourceTypeEC2VPC,
			SourceRecordID:   id + "#vpc#" + vpcID,
		})
	}
	if service := strings.TrimSpace(endpoint.ServiceName); service != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVPCEndpointUsesService,
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
	boundary aws.Boundary,
	eip ElasticIP,
) []aws.RelationshipObservation {
	id := strings.TrimSpace(eip.AllocationID)
	if id == "" {
		id = strings.TrimSpace(eip.PublicIP)
	}
	if id == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	if instanceID := strings.TrimSpace(eip.InstanceID); instanceID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVPCElasticIPAssociatedWithInstance,
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
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVPCElasticIPAssociatedWithNetworkInterface,
			SourceResourceID: id,
			TargetResourceID: eniID,
			TargetType:       aws.ResourceTypeEC2NetworkInterface,
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
	boundary aws.Boundary,
	gateway VPNGateway,
) []aws.RelationshipObservation {
	id := strings.TrimSpace(gateway.ID)
	if id == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	for _, attachment := range gateway.VPCAttachments {
		vpcID := strings.TrimSpace(attachment.VPCID)
		if vpcID == "" {
			continue
		}
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVPCVPNGatewayAttachedToVPC,
			SourceResourceID: id,
			TargetResourceID: vpcID,
			TargetType:       aws.ResourceTypeEC2VPC,
			Attributes: map[string]any{
				"state": strings.TrimSpace(attachment.State),
			},
			SourceRecordID: id + "#vpc#" + vpcID,
		})
	}
	return observations
}

func vpnConnectionRelationships(
	boundary aws.Boundary,
	connection VPNConnection,
) []aws.RelationshipObservation {
	id := strings.TrimSpace(connection.ID)
	if id == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	if cgwID := strings.TrimSpace(connection.CustomerGatewayID); cgwID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVPCVPNConnectionUsesCustomerGateway,
			SourceResourceID: id,
			TargetResourceID: cgwID,
			TargetType:       aws.ResourceTypeVPCCustomerGateway,
			SourceRecordID:   id + "#customer-gateway#" + cgwID,
		})
	}
	if vgwID := strings.TrimSpace(connection.VPNGatewayID); vgwID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVPCVPNConnectionUsesVPNGateway,
			SourceResourceID: id,
			TargetResourceID: vgwID,
			TargetType:       aws.ResourceTypeVPCVPNGateway,
			SourceRecordID:   id + "#vpn-gateway#" + vgwID,
		})
	}
	if tgwID := strings.TrimSpace(connection.TransitGatewayID); tgwID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVPCVPNConnectionUsesTransitGateway,
			SourceResourceID: id,
			TargetResourceID: tgwID,
			TargetType:       "aws_ec2_transit_gateway",
			SourceRecordID:   id + "#transit-gateway#" + tgwID,
		})
	}
	return observations
}
