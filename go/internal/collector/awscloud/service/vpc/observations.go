// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vpc

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func routeTableObservation(boundary awscloud.Boundary, rt RouteTable) awscloud.ResourceObservation {
	id := strings.TrimSpace(rt.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeVPCRouteTable,
		Name:         id,
		Tags:         cloneStringMap(rt.Tags),
		Attributes: map[string]any{
			"owner_id":     strings.TrimSpace(rt.OwnerID),
			"vpc_id":       strings.TrimSpace(rt.VPCID),
			"associations": routeTableAssociationMaps(rt.Associations),
			"routes":       routeMaps(rt.Routes),
		},
		CorrelationAnchors: []string{id},
		SourceRecordID:     id,
	}
}

func internetGatewayObservation(boundary awscloud.Boundary, gateway InternetGateway) awscloud.ResourceObservation {
	id := strings.TrimSpace(gateway.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeVPCInternetGateway,
		Name:         id,
		Tags:         cloneStringMap(gateway.Tags),
		Attributes: map[string]any{
			"owner_id":    strings.TrimSpace(gateway.OwnerID),
			"attachments": internetGatewayAttachmentMaps(gateway.Attachments),
		},
		CorrelationAnchors: []string{id},
		SourceRecordID:     id,
	}
}

func natGatewayObservation(boundary awscloud.Boundary, gateway NATGateway) awscloud.ResourceObservation {
	id := strings.TrimSpace(gateway.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeVPCNATGateway,
		Name:         id,
		State:        strings.TrimSpace(gateway.State),
		Tags:         cloneStringMap(gateway.Tags),
		Attributes: map[string]any{
			"connectivity_type":     strings.TrimSpace(gateway.ConnectivityType),
			"created_at":            timeOrNil(gateway.CreatedAt),
			"deleted_at":            timeOrNil(gateway.DeletedAt),
			"failure_code":          strings.TrimSpace(gateway.FailureCode),
			"failure_message":       strings.TrimSpace(gateway.FailureMessage),
			"nat_gateway_addresses": natGatewayAddressMaps(gateway.NATGatewayAddresses),
			"subnet_id":             strings.TrimSpace(gateway.SubnetID),
			"vpc_id":                strings.TrimSpace(gateway.VPCID),
		},
		CorrelationAnchors: []string{id},
		SourceRecordID:     id,
	}
}

func networkACLObservation(boundary awscloud.Boundary, networkACL NetworkACL) awscloud.ResourceObservation {
	id := strings.TrimSpace(networkACL.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeVPCNetworkACL,
		Name:         id,
		Tags:         cloneStringMap(networkACL.Tags),
		Attributes: map[string]any{
			"associations": networkACLAssociationMaps(networkACL.Associations),
			"entries":      networkACLEntryMaps(networkACL.Entries),
			"is_default":   networkACL.IsDefault,
			"owner_id":     strings.TrimSpace(networkACL.OwnerID),
			"vpc_id":       strings.TrimSpace(networkACL.VPCID),
		},
		CorrelationAnchors: []string{id},
		SourceRecordID:     id,
	}
}

func vpcPeeringObservation(boundary awscloud.Boundary, peering VPCPeeringConnection) awscloud.ResourceObservation {
	id := strings.TrimSpace(peering.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeVPCPeeringConnection,
		Name:         id,
		State:        strings.TrimSpace(peering.Status),
		Tags:         cloneStringMap(peering.Tags),
		Attributes: map[string]any{
			"accepter":       vpcPeeringInfoMap(peering.Accepter),
			"expiration_at":  timeOrNil(peering.ExpirationAt),
			"requester":      vpcPeeringInfoMap(peering.Requester),
			"status_message": strings.TrimSpace(peering.StatusMessage),
		},
		CorrelationAnchors: []string{id},
		SourceRecordID:     id,
	}
}

func vpcEndpointObservation(boundary awscloud.Boundary, endpoint VPCEndpoint) awscloud.ResourceObservation {
	id := strings.TrimSpace(endpoint.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeVPCEndpoint,
		Name:         id,
		State:        strings.TrimSpace(endpoint.State),
		Tags:         cloneStringMap(endpoint.Tags),
		Attributes: map[string]any{
			"created_at":            timeOrNil(endpoint.CreatedAt),
			"dns_entries":           vpcEndpointDNSEntryMaps(endpoint.DNSEntries),
			"endpoint_type":         strings.TrimSpace(endpoint.EndpointType),
			"network_interface_ids": cloneStrings(endpoint.NetworkInterfaceIDs),
			"owner_id":              strings.TrimSpace(endpoint.OwnerID),
			"private_dns_enabled":   endpoint.PrivateDNSEnabled,
			"requester_managed":     endpoint.RequesterManaged,
			"route_table_ids":       cloneStrings(endpoint.RouteTableIDs),
			"security_group_ids":    cloneStrings(endpoint.SecurityGroupIDs),
			"service_name":          strings.TrimSpace(endpoint.ServiceName),
			"subnet_ids":            cloneStrings(endpoint.SubnetIDs),
			"vpc_id":                strings.TrimSpace(endpoint.VPCID),
		},
		CorrelationAnchors: []string{id},
		SourceRecordID:     id,
	}
}

func elasticIPObservation(boundary awscloud.Boundary, eip ElasticIP) awscloud.ResourceObservation {
	id := strings.TrimSpace(eip.AllocationID)
	if id == "" {
		// Classic-platform Elastic IPs predate allocation IDs; fall back to the
		// public IPv4 address so the resource still has a stable identity.
		id = strings.TrimSpace(eip.PublicIP)
	}
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeVPCElasticIP,
		Name:         id,
		Tags:         cloneStringMap(eip.Tags),
		Attributes: map[string]any{
			"allocation_id":              strings.TrimSpace(eip.AllocationID),
			"association_id":             strings.TrimSpace(eip.AssociationID),
			"domain":                     strings.TrimSpace(eip.Domain),
			"instance_id":                strings.TrimSpace(eip.InstanceID),
			"network_border_group":       strings.TrimSpace(eip.NetworkBorderGroup),
			"network_interface_id":       strings.TrimSpace(eip.NetworkInterfaceID),
			"network_interface_owner_id": strings.TrimSpace(eip.NetworkInterfaceOwnerID),
			"private_ip":                 strings.TrimSpace(eip.PrivateIP),
			"public_ip":                  strings.TrimSpace(eip.PublicIP),
			"public_ipv4_pool":           strings.TrimSpace(eip.PublicIPv4Pool),
		},
		CorrelationAnchors: []string{id, strings.TrimSpace(eip.PublicIP)},
		SourceRecordID:     id,
	}
}

func dhcpOptionsObservation(boundary awscloud.Boundary, options DHCPOptions) awscloud.ResourceObservation {
	id := strings.TrimSpace(options.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeVPCDHCPOptions,
		Name:         id,
		Tags:         cloneStringMap(options.Tags),
		Attributes: map[string]any{
			"configuration": dhcpConfigurationMaps(options.Configuration),
			"owner_id":      strings.TrimSpace(options.OwnerID),
		},
		CorrelationAnchors: []string{id},
		SourceRecordID:     id,
	}
}

func customerGatewayObservation(boundary awscloud.Boundary, gateway CustomerGateway) awscloud.ResourceObservation {
	id := strings.TrimSpace(gateway.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeVPCCustomerGateway,
		Name:         id,
		State:        strings.TrimSpace(gateway.State),
		Tags:         cloneStringMap(gateway.Tags),
		Attributes: map[string]any{
			"bgp_asn":         strings.TrimSpace(gateway.BGPASN),
			"certificate_arn": strings.TrimSpace(gateway.CertificateARN),
			"device_name":     strings.TrimSpace(gateway.DeviceName),
			"ip_address":      strings.TrimSpace(gateway.IPAddress),
			"type":            strings.TrimSpace(gateway.Type),
		},
		CorrelationAnchors: []string{id},
		SourceRecordID:     id,
	}
}

func vpnGatewayObservation(boundary awscloud.Boundary, gateway VPNGateway) awscloud.ResourceObservation {
	id := strings.TrimSpace(gateway.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeVPCVPNGateway,
		Name:         id,
		State:        strings.TrimSpace(gateway.State),
		Tags:         cloneStringMap(gateway.Tags),
		Attributes: map[string]any{
			"amazon_side_asn":   gateway.AmazonSideASN,
			"availability_zone": strings.TrimSpace(gateway.AvailabilityZone),
			"type":              strings.TrimSpace(gateway.Type),
			"vpc_attachments":   vpnGatewayAttachmentMaps(gateway.VPCAttachments),
		},
		CorrelationAnchors: []string{id},
		SourceRecordID:     id,
	}
}

func vpnConnectionObservation(boundary awscloud.Boundary, connection VPNConnection) awscloud.ResourceObservation {
	id := strings.TrimSpace(connection.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeVPCVPNConnection,
		Name:         id,
		State:        strings.TrimSpace(connection.State),
		Tags:         cloneStringMap(connection.Tags),
		Attributes: map[string]any{
			"category":            strings.TrimSpace(connection.Category),
			"core_network_arn":    strings.TrimSpace(connection.CoreNetworkARN),
			"customer_gateway_id": strings.TrimSpace(connection.CustomerGatewayID),
			"static_routes_only":  connection.StaticRoutesOnly,
			"telemetry":           vpnTunnelTelemetryMaps(connection.TelemetrySummaries),
			"transit_gateway_id":  strings.TrimSpace(connection.TransitGatewayID),
			"type":                strings.TrimSpace(connection.Type),
			"vpn_gateway_id":      strings.TrimSpace(connection.VPNGatewayID),
		},
		CorrelationAnchors: []string{id},
		SourceRecordID:     id,
	}
}
