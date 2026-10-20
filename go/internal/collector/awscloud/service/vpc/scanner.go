// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vpc

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS VPC network-fabric metadata facts for one claimed account
// and region. It never reads or persists tunnel pre-shared keys, IAM policy
// JSON, or any data-plane payload; AWS-reported metadata is the product.
//
// The scanner intentionally does not emit VPC, subnet, security group, security
// group rule, or network interface resources; those identities belong to the
// EC2 scanner. Relationship edges in this scanner cross back to EC2-owned
// resources by AWS-reported identifier.
type Scanner struct {
	Client Client
}

// Scan observes VPC topology metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("vpc scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceVPC:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceVPC
	default:
		return nil, fmt.Errorf("vpc scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	routeTables, err := s.Client.ListRouteTables(ctx)
	if err != nil {
		return nil, fmt.Errorf("list VPC route tables: %w", err)
	}
	for _, routeTable := range routeTables {
		emitted, err := routeTableEnvelopes(boundary, routeTable)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, emitted...)
	}

	internetGateways, err := s.Client.ListInternetGateways(ctx)
	if err != nil {
		return nil, fmt.Errorf("list VPC internet gateways: %w", err)
	}
	for _, gateway := range internetGateways {
		emitted, err := internetGatewayEnvelopes(boundary, gateway)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, emitted...)
	}

	natGateways, err := s.Client.ListNATGateways(ctx)
	if err != nil {
		return nil, fmt.Errorf("list VPC NAT gateways: %w", err)
	}
	for _, gateway := range natGateways {
		emitted, err := natGatewayEnvelopes(boundary, gateway)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, emitted...)
	}

	networkACLs, err := s.Client.ListNetworkACLs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list VPC network ACLs: %w", err)
	}
	for _, networkACL := range networkACLs {
		emitted, err := networkACLEnvelopes(boundary, networkACL)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, emitted...)
	}

	peerings, err := s.Client.ListVPCPeeringConnections(ctx)
	if err != nil {
		return nil, fmt.Errorf("list VPC peering connections: %w", err)
	}
	for _, peering := range peerings {
		emitted, err := vpcPeeringEnvelopes(boundary, peering)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, emitted...)
	}

	endpoints, err := s.Client.ListVPCEndpoints(ctx)
	if err != nil {
		return nil, fmt.Errorf("list VPC endpoints: %w", err)
	}
	for _, endpoint := range endpoints {
		emitted, err := vpcEndpointEnvelopes(boundary, endpoint)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, emitted...)
	}

	elasticIPs, err := s.Client.ListElasticIPs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list VPC elastic IPs: %w", err)
	}
	for _, elasticIP := range elasticIPs {
		emitted, err := elasticIPEnvelopes(boundary, elasticIP)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, emitted...)
	}

	dhcpOptions, err := s.Client.ListDHCPOptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list VPC DHCP options: %w", err)
	}
	for _, options := range dhcpOptions {
		envelope, err := awscloud.NewResourceEnvelope(dhcpOptionsObservation(boundary, options))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	customerGateways, err := s.Client.ListCustomerGateways(ctx)
	if err != nil {
		return nil, fmt.Errorf("list VPC customer gateways: %w", err)
	}
	for _, gateway := range customerGateways {
		envelope, err := awscloud.NewResourceEnvelope(customerGatewayObservation(boundary, gateway))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	vpnGateways, err := s.Client.ListVPNGateways(ctx)
	if err != nil {
		return nil, fmt.Errorf("list VPC VPN gateways: %w", err)
	}
	for _, gateway := range vpnGateways {
		emitted, err := vpnGatewayEnvelopes(boundary, gateway)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, emitted...)
	}

	vpnConnections, err := s.Client.ListVPNConnections(ctx)
	if err != nil {
		return nil, fmt.Errorf("list VPC VPN connections: %w", err)
	}
	for _, connection := range vpnConnections {
		emitted, err := vpnConnectionEnvelopes(boundary, connection)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, emitted...)
	}

	return envelopes, nil
}

func routeTableEnvelopes(boundary awscloud.Boundary, rt RouteTable) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(routeTableObservation(boundary, rt))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range routeTableRelationships(boundary, rt) {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func internetGatewayEnvelopes(boundary awscloud.Boundary, gateway InternetGateway) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(internetGatewayObservation(boundary, gateway))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range internetGatewayRelationships(boundary, gateway) {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func natGatewayEnvelopes(boundary awscloud.Boundary, gateway NATGateway) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(natGatewayObservation(boundary, gateway))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range natGatewayRelationships(boundary, gateway) {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func networkACLEnvelopes(boundary awscloud.Boundary, networkACL NetworkACL) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(networkACLObservation(boundary, networkACL))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range networkACLRelationships(boundary, networkACL) {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func vpcPeeringEnvelopes(boundary awscloud.Boundary, peering VPCPeeringConnection) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(vpcPeeringObservation(boundary, peering))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range vpcPeeringRelationships(boundary, peering) {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func vpcEndpointEnvelopes(boundary awscloud.Boundary, endpoint VPCEndpoint) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(vpcEndpointObservation(boundary, endpoint))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range vpcEndpointRelationships(boundary, endpoint) {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func elasticIPEnvelopes(boundary awscloud.Boundary, elasticIP ElasticIP) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(elasticIPObservation(boundary, elasticIP))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range elasticIPRelationships(boundary, elasticIP) {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func vpnGatewayEnvelopes(boundary awscloud.Boundary, gateway VPNGateway) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(vpnGatewayObservation(boundary, gateway))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range vpnGatewayRelationships(boundary, gateway) {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func vpnConnectionEnvelopes(boundary awscloud.Boundary, connection VPNConnection) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(vpnConnectionObservation(boundary, connection))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range vpnConnectionRelationships(boundary, connection) {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}
