// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package transitgateway

import (
	"strings"
	"time"
)

func transitGatewayOptionsMap(options TransitGatewayOptions) map[string]any {
	return map[string]any{
		"amazon_side_asn":                    options.AmazonSideASN,
		"association_default_route_table_id": strings.TrimSpace(options.AssociationDefaultRouteTableID),
		"auto_accept_shared_attachments":     strings.TrimSpace(options.AutoAcceptSharedAttachments),
		"default_route_table_association":    strings.TrimSpace(options.DefaultRouteTableAssociation),
		"default_route_table_propagation":    strings.TrimSpace(options.DefaultRouteTablePropagation),
		"dns_support":                        strings.TrimSpace(options.DNSSupport),
		"multicast_support":                  strings.TrimSpace(options.MulticastSupport),
		"propagation_default_route_table_id": strings.TrimSpace(options.PropagationDefaultRouteTableID),
		"vpn_ecmp_support":                   strings.TrimSpace(options.VPNECMPSupport),
	}
}

func multicastDomainOptionsMap(options MulticastDomainOptions) map[string]any {
	return map[string]any{
		"auto_accept_shared_associations": strings.TrimSpace(options.AutoAcceptSharedAssociations),
		"igmpv2_support":                  strings.TrimSpace(options.IGMPv2Support),
		"static_sources_support":          strings.TrimSpace(options.StaticSourcesSupport),
	}
}

func peeringInfoMap(info PeeringTransitGatewayInfo) map[string]any {
	return map[string]any{
		"core_network_id":    strings.TrimSpace(info.CoreNetworkID),
		"owner_id":           strings.TrimSpace(info.OwnerID),
		"region":             strings.TrimSpace(info.Region),
		"transit_gateway_id": strings.TrimSpace(info.TransitGatewayID),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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
