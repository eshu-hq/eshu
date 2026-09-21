// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vpc

import (
	"strings"
	"time"
)

func routeTableAssociationMaps(associations []RouteTableAssociation) []map[string]any {
	if len(associations) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(associations))
	for _, association := range associations {
		out = append(out, map[string]any{
			"association_id": strings.TrimSpace(association.AssociationID),
			"gateway_id":     strings.TrimSpace(association.GatewayID),
			"main":           association.Main,
			"state":          strings.TrimSpace(association.State),
			"subnet_id":      strings.TrimSpace(association.SubnetID),
		})
	}
	return out
}

func routeMaps(routes []Route) []map[string]any {
	if len(routes) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(routes))
	for _, route := range routes {
		out = append(out, map[string]any{
			"carrier_gateway_id":          strings.TrimSpace(route.CarrierGatewayID),
			"destination_cidr_block":      strings.TrimSpace(route.DestinationCIDRBlock),
			"destination_ipv6_cidr_block": strings.TrimSpace(route.DestinationIPv6CIDRBlock),
			"destination_prefix_list_id":  strings.TrimSpace(route.DestinationPrefixListID),
			"egress_only_igw_id":          strings.TrimSpace(route.EgressOnlyIGWID),
			"gateway_id":                  strings.TrimSpace(route.GatewayID),
			"instance_id":                 strings.TrimSpace(route.InstanceID),
			"nat_gateway_id":              strings.TrimSpace(route.NATGatewayID),
			"network_interface_id":        strings.TrimSpace(route.NetworkInterfaceID),
			"origin":                      strings.TrimSpace(route.Origin),
			"state":                       strings.TrimSpace(route.State),
			"transit_gateway_id":          strings.TrimSpace(route.TransitGatewayID),
			"vpc_endpoint_id":             strings.TrimSpace(route.VPCEndpointID),
			"vpc_peering_connection_id":   strings.TrimSpace(route.VPCPeeringConnectionID),
		})
	}
	return out
}

func internetGatewayAttachmentMaps(attachments []InternetGatewayAttachment) []map[string]any {
	if len(attachments) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(attachments))
	for _, attachment := range attachments {
		out = append(out, map[string]any{
			"state":  strings.TrimSpace(attachment.State),
			"vpc_id": strings.TrimSpace(attachment.VPCID),
		})
	}
	return out
}

func natGatewayAddressMaps(addresses []NATGatewayAddress) []map[string]any {
	if len(addresses) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(addresses))
	for _, address := range addresses {
		out = append(out, map[string]any{
			"allocation_id":        strings.TrimSpace(address.AllocationID),
			"is_primary":           address.IsPrimary,
			"network_interface_id": strings.TrimSpace(address.NetworkInterfaceID),
			"private_ip":           strings.TrimSpace(address.PrivateIP),
			"public_ip":            strings.TrimSpace(address.PublicIP),
		})
	}
	return out
}

func networkACLAssociationMaps(associations []NetworkACLAssociation) []map[string]any {
	if len(associations) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(associations))
	for _, association := range associations {
		out = append(out, map[string]any{
			"association_id": strings.TrimSpace(association.AssociationID),
			"subnet_id":      strings.TrimSpace(association.SubnetID),
		})
	}
	return out
}

func networkACLEntryMaps(entries []NetworkACLEntry) []map[string]any {
	if len(entries) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		out = append(out, map[string]any{
			"cidr_block":      strings.TrimSpace(entry.CIDRBlock),
			"egress":          entry.Egress,
			"icmp_code":       int32Value(entry.ICMPCode),
			"icmp_type":       int32Value(entry.ICMPType),
			"ipv6_cidr_block": strings.TrimSpace(entry.IPv6CIDRBlock),
			"port_range_from": int32Value(entry.PortRangeFrom),
			"port_range_to":   int32Value(entry.PortRangeTo),
			"protocol":        strings.TrimSpace(entry.Protocol),
			"rule_action":     strings.TrimSpace(entry.RuleAction),
			"rule_number":     entry.RuleNumber,
		})
	}
	return out
}

func vpcPeeringInfoMap(info VPCPeeringVPCInfo) map[string]any {
	return map[string]any{
		"cidr_block": strings.TrimSpace(info.CIDRBlock),
		"owner_id":   strings.TrimSpace(info.OwnerID),
		"region":     strings.TrimSpace(info.Region),
		"vpc_id":     strings.TrimSpace(info.VPCID),
	}
}

func vpcEndpointDNSEntryMaps(entries []VPCEndpointDNSEntry) []map[string]any {
	if len(entries) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		out = append(out, map[string]any{
			"dns_name":       strings.TrimSpace(entry.DNSName),
			"hosted_zone_id": strings.TrimSpace(entry.HostedZoneID),
		})
	}
	return out
}

func dhcpConfigurationMaps(entries []DHCPConfigurationEntry) []map[string]any {
	if len(entries) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		out = append(out, map[string]any{
			"key":    strings.TrimSpace(entry.Key),
			"values": cloneStrings(entry.Values),
		})
	}
	return out
}

func vpnGatewayAttachmentMaps(attachments []VPNGatewayAttachment) []map[string]any {
	if len(attachments) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(attachments))
	for _, attachment := range attachments {
		out = append(out, map[string]any{
			"state":  strings.TrimSpace(attachment.State),
			"vpc_id": strings.TrimSpace(attachment.VPCID),
		})
	}
	return out
}

func vpnTunnelTelemetryMaps(summaries []VPNTunnelTelemetry) []map[string]any {
	if len(summaries) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(summaries))
	for _, summary := range summaries {
		out = append(out, map[string]any{
			"accepted_route_count": summary.AcceptedRouteCount,
			"certificate_arn":      strings.TrimSpace(summary.CertificateARN),
			"last_status_change":   timeOrNil(summary.LastStatusChange),
			"outside_ip_address":   strings.TrimSpace(summary.OutsideIPAddress),
			"status":               strings.TrimSpace(summary.Status),
			"status_message":       strings.TrimSpace(summary.StatusMessage),
		})
	}
	return out
}

func int32Value(value *int32) any {
	if value == nil {
		return nil
	}
	return *value
}

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
