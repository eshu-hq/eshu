// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package directconnect

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func connectionObservation(boundary awscloud.Boundary, connection Connection) awscloud.ResourceObservation {
	id := strings.TrimSpace(connection.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeDirectConnectConnection,
		Name:         firstNonEmpty(connection.Name, id),
		State:        strings.TrimSpace(connection.State),
		Tags:         cloneStringMap(connection.Tags),
		Attributes: map[string]any{
			"bandwidth":      strings.TrimSpace(connection.Bandwidth),
			"jumbo_frames":   connection.JumboFrames,
			"lag_id":         strings.TrimSpace(connection.LAGID),
			"location":       strings.TrimSpace(connection.Location),
			"macsec_capable": connection.MacSecCapable,
			"owner_account":  strings.TrimSpace(connection.OwnerAccount),
			"partner_name":   strings.TrimSpace(connection.PartnerName),
			"provider_name":  strings.TrimSpace(connection.ProviderName),
			"region":         strings.TrimSpace(connection.Region),
			"vlan":           connection.VLAN,
		},
		CorrelationAnchors: []string{id},
		SourceRecordID:     id,
	}
}

func virtualInterfaceObservation(boundary awscloud.Boundary, vif VirtualInterface) awscloud.ResourceObservation {
	id := strings.TrimSpace(vif.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeDirectConnectVirtualInterface,
		Name:         firstNonEmpty(vif.Name, id),
		State:        strings.TrimSpace(vif.State),
		Tags:         cloneStringMap(vif.Tags),
		Attributes: map[string]any{
			"address_family":            strings.TrimSpace(vif.AddressFamily),
			"amazon_side_asn":           vif.AmazonSideASN,
			"bgp_asn":                   vif.ASN,
			"connection_id":             strings.TrimSpace(vif.ConnectionID),
			"direct_connect_gateway_id": strings.TrimSpace(vif.GatewayID),
			"location":                  strings.TrimSpace(vif.Location),
			"owner_account":             strings.TrimSpace(vif.OwnerAccount),
			"virtual_gateway_id":        strings.TrimSpace(vif.VirtualGateway),
			"virtual_interface_type":    strings.TrimSpace(vif.Type),
			"vlan":                      vif.VLAN,
		},
		CorrelationAnchors: []string{id},
		SourceRecordID:     id,
	}
}

func gatewayObservation(boundary awscloud.Boundary, gateway Gateway) awscloud.ResourceObservation {
	id := strings.TrimSpace(gateway.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeDirectConnectGateway,
		Name:         firstNonEmpty(gateway.Name, id),
		State:        strings.TrimSpace(gateway.State),
		Attributes: map[string]any{
			"amazon_side_asn": gateway.AmazonSideASN,
			"owner_account":   strings.TrimSpace(gateway.OwnerAccount),
		},
		CorrelationAnchors: []string{id},
		SourceRecordID:     id,
	}
}

func lagObservation(boundary awscloud.Boundary, lag LAG) awscloud.ResourceObservation {
	id := strings.TrimSpace(lag.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeDirectConnectLAG,
		Name:         firstNonEmpty(lag.Name, id),
		State:        strings.TrimSpace(lag.State),
		Tags:         cloneStringMap(lag.Tags),
		Attributes: map[string]any{
			"bandwidth":             strings.TrimSpace(lag.Bandwidth),
			"location":              strings.TrimSpace(lag.Location),
			"macsec_capable":        lag.MacSecCapable,
			"minimum_links":         lag.MinimumLinks,
			"number_of_connections": lag.NumberOfConnections,
			"owner_account":         strings.TrimSpace(lag.OwnerAccount),
			"provider_name":         strings.TrimSpace(lag.ProviderName),
			"region":                strings.TrimSpace(lag.Region),
		},
		CorrelationAnchors: []string{id},
		SourceRecordID:     id,
	}
}
