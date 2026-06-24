// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	vpcservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/vpc"
)

func mapRouteTable(rt awsec2types.RouteTable) vpcservice.RouteTable {
	associations := make([]vpcservice.RouteTableAssociation, 0, len(rt.Associations))
	for _, association := range rt.Associations {
		associations = append(associations, mapRouteTableAssociation(association))
	}
	routes := make([]vpcservice.Route, 0, len(rt.Routes))
	for _, route := range rt.Routes {
		routes = append(routes, mapRoute(route))
	}
	return vpcservice.RouteTable{
		ID:           aws.ToString(rt.RouteTableId),
		VPCID:        aws.ToString(rt.VpcId),
		OwnerID:      aws.ToString(rt.OwnerId),
		Associations: associations,
		Routes:       routes,
		Tags:         mapTags(rt.Tags),
	}
}

func mapRouteTableAssociation(association awsec2types.RouteTableAssociation) vpcservice.RouteTableAssociation {
	state := ""
	if association.AssociationState != nil {
		state = string(association.AssociationState.State)
	}
	return vpcservice.RouteTableAssociation{
		AssociationID: aws.ToString(association.RouteTableAssociationId),
		SubnetID:      aws.ToString(association.SubnetId),
		GatewayID:     aws.ToString(association.GatewayId),
		Main:          aws.ToBool(association.Main),
		State:         state,
	}
}

func mapRoute(route awsec2types.Route) vpcservice.Route {
	// AWS has no dedicated VpcEndpointId field on a route. A gateway VPC
	// endpoint target (for example an S3 gateway endpoint) is reported in the
	// GatewayId field with a vpce- prefix, alongside igw-/vgw-/local gateway
	// targets. Steer the vpce- variant into VPCEndpointID so the scanner can
	// emit the vpc_route_targets_vpc_endpoint relationship, and keep it out of
	// GatewayID so the internet-gateway relationship builder's igw- guard only
	// ever sees true gateway targets.
	gatewayID := aws.ToString(route.GatewayId)
	vpcEndpointID := ""
	if strings.HasPrefix(gatewayID, "vpce-") {
		vpcEndpointID = gatewayID
		gatewayID = ""
	}
	return vpcservice.Route{
		DestinationCIDRBlock:     aws.ToString(route.DestinationCidrBlock),
		DestinationIPv6CIDRBlock: aws.ToString(route.DestinationIpv6CidrBlock),
		DestinationPrefixListID:  aws.ToString(route.DestinationPrefixListId),
		GatewayID:                gatewayID,
		NATGatewayID:             aws.ToString(route.NatGatewayId),
		VPCPeeringConnectionID:   aws.ToString(route.VpcPeeringConnectionId),
		VPCEndpointID:            vpcEndpointID,
		TransitGatewayID:         aws.ToString(route.TransitGatewayId),
		NetworkInterfaceID:       aws.ToString(route.NetworkInterfaceId),
		InstanceID:               aws.ToString(route.InstanceId),
		CarrierGatewayID:         aws.ToString(route.CarrierGatewayId),
		EgressOnlyIGWID:          aws.ToString(route.EgressOnlyInternetGatewayId),
		State:                    string(route.State),
		Origin:                   string(route.Origin),
	}
}

func mapInternetGateway(gateway awsec2types.InternetGateway) vpcservice.InternetGateway {
	attachments := make([]vpcservice.InternetGatewayAttachment, 0, len(gateway.Attachments))
	for _, attachment := range gateway.Attachments {
		attachments = append(attachments, vpcservice.InternetGatewayAttachment{
			VPCID: aws.ToString(attachment.VpcId),
			State: string(attachment.State),
		})
	}
	return vpcservice.InternetGateway{
		ID:          aws.ToString(gateway.InternetGatewayId),
		OwnerID:     aws.ToString(gateway.OwnerId),
		Attachments: attachments,
		Tags:        mapTags(gateway.Tags),
	}
}

func mapNATGateway(gateway awsec2types.NatGateway) vpcservice.NATGateway {
	addresses := make([]vpcservice.NATGatewayAddress, 0, len(gateway.NatGatewayAddresses))
	for _, address := range gateway.NatGatewayAddresses {
		addresses = append(addresses, vpcservice.NATGatewayAddress{
			AllocationID:       aws.ToString(address.AllocationId),
			NetworkInterfaceID: aws.ToString(address.NetworkInterfaceId),
			PrivateIP:          aws.ToString(address.PrivateIp),
			PublicIP:           aws.ToString(address.PublicIp),
			IsPrimary:          aws.ToBool(address.IsPrimary),
		})
	}
	return vpcservice.NATGateway{
		ID:                  aws.ToString(gateway.NatGatewayId),
		VPCID:               aws.ToString(gateway.VpcId),
		SubnetID:            aws.ToString(gateway.SubnetId),
		State:               string(gateway.State),
		ConnectivityType:    string(gateway.ConnectivityType),
		FailureCode:         aws.ToString(gateway.FailureCode),
		FailureMessage:      aws.ToString(gateway.FailureMessage),
		CreatedAt:           aws.ToTime(gateway.CreateTime),
		DeletedAt:           aws.ToTime(gateway.DeleteTime),
		NATGatewayAddresses: addresses,
		Tags:                mapTags(gateway.Tags),
	}
}

func mapNetworkACL(networkACL awsec2types.NetworkAcl) vpcservice.NetworkACL {
	associations := make([]vpcservice.NetworkACLAssociation, 0, len(networkACL.Associations))
	for _, association := range networkACL.Associations {
		associations = append(associations, vpcservice.NetworkACLAssociation{
			AssociationID: aws.ToString(association.NetworkAclAssociationId),
			SubnetID:      aws.ToString(association.SubnetId),
		})
	}
	entries := make([]vpcservice.NetworkACLEntry, 0, len(networkACL.Entries))
	for _, entry := range networkACL.Entries {
		entries = append(entries, mapNetworkACLEntry(entry))
	}
	return vpcservice.NetworkACL{
		ID:           aws.ToString(networkACL.NetworkAclId),
		VPCID:        aws.ToString(networkACL.VpcId),
		OwnerID:      aws.ToString(networkACL.OwnerId),
		IsDefault:    aws.ToBool(networkACL.IsDefault),
		Associations: associations,
		Entries:      entries,
		Tags:         mapTags(networkACL.Tags),
	}
}

func mapNetworkACLEntry(entry awsec2types.NetworkAclEntry) vpcservice.NetworkACLEntry {
	mapped := vpcservice.NetworkACLEntry{
		RuleNumber:    aws.ToInt32(entry.RuleNumber),
		Protocol:      aws.ToString(entry.Protocol),
		RuleAction:    string(entry.RuleAction),
		Egress:        aws.ToBool(entry.Egress),
		CIDRBlock:     aws.ToString(entry.CidrBlock),
		IPv6CIDRBlock: aws.ToString(entry.Ipv6CidrBlock),
	}
	if entry.PortRange != nil {
		mapped.PortRangeFrom = entry.PortRange.From
		mapped.PortRangeTo = entry.PortRange.To
	}
	if entry.IcmpTypeCode != nil {
		mapped.ICMPCode = entry.IcmpTypeCode.Code
		mapped.ICMPType = entry.IcmpTypeCode.Type
	}
	return mapped
}

func mapVPCPeeringConnection(peering awsec2types.VpcPeeringConnection) vpcservice.VPCPeeringConnection {
	status := ""
	statusMessage := ""
	if peering.Status != nil {
		status = string(peering.Status.Code)
		statusMessage = aws.ToString(peering.Status.Message)
	}
	return vpcservice.VPCPeeringConnection{
		ID:            aws.ToString(peering.VpcPeeringConnectionId),
		Status:        status,
		StatusMessage: statusMessage,
		Requester:     mapPeeringInfo(peering.RequesterVpcInfo),
		Accepter:      mapPeeringInfo(peering.AccepterVpcInfo),
		ExpirationAt:  aws.ToTime(peering.ExpirationTime),
		Tags:          mapTags(peering.Tags),
	}
}

func mapPeeringInfo(info *awsec2types.VpcPeeringConnectionVpcInfo) vpcservice.VPCPeeringVPCInfo {
	if info == nil {
		return vpcservice.VPCPeeringVPCInfo{}
	}
	return vpcservice.VPCPeeringVPCInfo{
		VPCID:     aws.ToString(info.VpcId),
		OwnerID:   aws.ToString(info.OwnerId),
		Region:    aws.ToString(info.Region),
		CIDRBlock: aws.ToString(info.CidrBlock),
	}
}

func mapVPCEndpoint(endpoint awsec2types.VpcEndpoint) vpcservice.VPCEndpoint {
	dnsEntries := make([]vpcservice.VPCEndpointDNSEntry, 0, len(endpoint.DnsEntries))
	for _, entry := range endpoint.DnsEntries {
		dnsEntries = append(dnsEntries, vpcservice.VPCEndpointDNSEntry{
			DNSName:      aws.ToString(entry.DnsName),
			HostedZoneID: aws.ToString(entry.HostedZoneId),
		})
	}
	networkInterfaceIDs := append([]string(nil), endpoint.NetworkInterfaceIds...)
	routeTableIDs := append([]string(nil), endpoint.RouteTableIds...)
	subnetIDs := append([]string(nil), endpoint.SubnetIds...)
	securityGroupIDs := make([]string, 0, len(endpoint.Groups))
	for _, group := range endpoint.Groups {
		if id := strings.TrimSpace(aws.ToString(group.GroupId)); id != "" {
			securityGroupIDs = append(securityGroupIDs, id)
		}
	}
	return vpcservice.VPCEndpoint{
		ID:                  aws.ToString(endpoint.VpcEndpointId),
		VPCID:               aws.ToString(endpoint.VpcId),
		ServiceName:         aws.ToString(endpoint.ServiceName),
		EndpointType:        string(endpoint.VpcEndpointType),
		State:               string(endpoint.State),
		PrivateDNSEnabled:   aws.ToBool(endpoint.PrivateDnsEnabled),
		RequesterManaged:    aws.ToBool(endpoint.RequesterManaged),
		OwnerID:             aws.ToString(endpoint.OwnerId),
		RouteTableIDs:       routeTableIDs,
		SubnetIDs:           subnetIDs,
		SecurityGroupIDs:    securityGroupIDs,
		NetworkInterfaceIDs: networkInterfaceIDs,
		DNSEntries:          dnsEntries,
		CreatedAt:           aws.ToTime(endpoint.CreationTimestamp),
		Tags:                mapTags(endpoint.Tags),
	}
}

func mapElasticIP(address awsec2types.Address) vpcservice.ElasticIP {
	return vpcservice.ElasticIP{
		AllocationID:            aws.ToString(address.AllocationId),
		AssociationID:           aws.ToString(address.AssociationId),
		Domain:                  string(address.Domain),
		PublicIP:                aws.ToString(address.PublicIp),
		PublicIPv4Pool:          aws.ToString(address.PublicIpv4Pool),
		NetworkBorderGroup:      aws.ToString(address.NetworkBorderGroup),
		InstanceID:              aws.ToString(address.InstanceId),
		NetworkInterfaceID:      aws.ToString(address.NetworkInterfaceId),
		NetworkInterfaceOwnerID: aws.ToString(address.NetworkInterfaceOwnerId),
		PrivateIP:               aws.ToString(address.PrivateIpAddress),
		Tags:                    mapTags(address.Tags),
	}
}

func mapDHCPOptions(options awsec2types.DhcpOptions) vpcservice.DHCPOptions {
	entries := make([]vpcservice.DHCPConfigurationEntry, 0, len(options.DhcpConfigurations))
	for _, entry := range options.DhcpConfigurations {
		values := make([]string, 0, len(entry.Values))
		for _, value := range entry.Values {
			values = append(values, aws.ToString(value.Value))
		}
		entries = append(entries, vpcservice.DHCPConfigurationEntry{
			Key:    aws.ToString(entry.Key),
			Values: values,
		})
	}
	return vpcservice.DHCPOptions{
		ID:            aws.ToString(options.DhcpOptionsId),
		OwnerID:       aws.ToString(options.OwnerId),
		Configuration: entries,
		Tags:          mapTags(options.Tags),
	}
}

func mapCustomerGateway(gateway awsec2types.CustomerGateway) vpcservice.CustomerGateway {
	return vpcservice.CustomerGateway{
		ID:             aws.ToString(gateway.CustomerGatewayId),
		State:          aws.ToString(gateway.State),
		Type:           aws.ToString(gateway.Type),
		IPAddress:      aws.ToString(gateway.IpAddress),
		BGPASN:         aws.ToString(gateway.BgpAsn),
		DeviceName:     aws.ToString(gateway.DeviceName),
		CertificateARN: aws.ToString(gateway.CertificateArn),
		Tags:           mapTags(gateway.Tags),
	}
}

func mapVPNGateway(gateway awsec2types.VpnGateway) vpcservice.VPNGateway {
	attachments := make([]vpcservice.VPNGatewayAttachment, 0, len(gateway.VpcAttachments))
	for _, attachment := range gateway.VpcAttachments {
		attachments = append(attachments, vpcservice.VPNGatewayAttachment{
			VPCID: aws.ToString(attachment.VpcId),
			State: string(attachment.State),
		})
	}
	return vpcservice.VPNGateway{
		ID:               aws.ToString(gateway.VpnGatewayId),
		State:            string(gateway.State),
		Type:             string(gateway.Type),
		AvailabilityZone: aws.ToString(gateway.AvailabilityZone),
		AmazonSideASN:    aws.ToInt64(gateway.AmazonSideAsn),
		VPCAttachments:   attachments,
		Tags:             mapTags(gateway.Tags),
	}
}

func mapVPNConnection(connection awsec2types.VpnConnection) vpcservice.VPNConnection {
	telemetry := make([]vpcservice.VPNTunnelTelemetry, 0, len(connection.VgwTelemetry))
	for _, summary := range connection.VgwTelemetry {
		telemetry = append(telemetry, vpcservice.VPNTunnelTelemetry{
			OutsideIPAddress:   aws.ToString(summary.OutsideIpAddress),
			Status:             string(summary.Status),
			StatusMessage:      aws.ToString(summary.StatusMessage),
			AcceptedRouteCount: aws.ToInt32(summary.AcceptedRouteCount),
			LastStatusChange:   aws.ToTime(summary.LastStatusChange),
			CertificateARN:     aws.ToString(summary.CertificateArn),
		})
	}
	staticRoutesOnly := false
	if connection.Options != nil {
		staticRoutesOnly = aws.ToBool(connection.Options.StaticRoutesOnly)
	}
	return vpcservice.VPNConnection{
		ID:                 aws.ToString(connection.VpnConnectionId),
		State:              string(connection.State),
		Type:               string(connection.Type),
		Category:           aws.ToString(connection.Category),
		CustomerGatewayID:  aws.ToString(connection.CustomerGatewayId),
		VPNGatewayID:       aws.ToString(connection.VpnGatewayId),
		TransitGatewayID:   aws.ToString(connection.TransitGatewayId),
		CoreNetworkARN:     aws.ToString(connection.CoreNetworkArn),
		StaticRoutesOnly:   staticRoutesOnly,
		Tags:               mapTags(connection.Tags),
		TelemetrySummaries: telemetry,
	}
}

func mapTags(tags []awsec2types.Tag) map[string]string {
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
