// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package directconnect

import "context"

// Client is the Direct Connect metadata read surface consumed by Scanner.
// Runtime adapters translate AWS SDK Direct Connect responses into these
// scanner-owned records. The interface intentionally exposes only list/read
// operations; no mutation method (Create/Delete/Update/Associate/Disassociate/
// Confirm/Allocate/Tag/Untag) appears here, and the AWS SDK adapter must not
// gain access to any such method either.
type Client interface {
	ListConnections(context.Context) ([]Connection, error)
	ListVirtualInterfaces(context.Context) ([]VirtualInterface, error)
	ListGateways(context.Context) ([]Gateway, error)
	ListLAGs(context.Context) ([]LAG, error)
	// ListGatewayAssociations returns the gateway-to-(transit gateway | virtual
	// private gateway) associations AWS reports for the visible Direct Connect
	// gateways. The adapter resolves the per-gateway associations and returns
	// the flattened list so the scanner stays free of pagination concerns.
	ListGatewayAssociations(context.Context) ([]GatewayAssociation, error)
}

// Connection is the scanner-owned representation of one Direct Connect physical
// connection (port). It carries identity, location, bandwidth, state, and
// partner/provider metadata. The scanner never persists MACsec connectivity
// association key names (CKN) or secret ARNs; only the boolean MACsec
// capability flag is surfaced.
type Connection struct {
	ID            string
	Name          string
	OwnerAccount  string
	Location      string
	Bandwidth     string
	State         string
	Region        string
	PartnerName   string
	ProviderName  string
	LAGID         string
	VLAN          int32
	JumboFrames   bool
	MacSecCapable bool
	Tags          map[string]string
}

// VirtualInterface is the scanner-owned representation of one Direct Connect
// virtual interface. It carries identity, VLAN, BGP ASN, type (private, public,
// or transit), and state. The scanner NEVER reads or persists the BGP
// authentication key (the AWS authKey field), neither the interface-level key
// nor any per-BGP-peer key; there is no field for it on this type.
type VirtualInterface struct {
	ID             string
	Name           string
	Type           string
	State          string
	OwnerAccount   string
	Location       string
	ConnectionID   string
	GatewayID      string
	VirtualGateway string
	VLAN           int32
	ASN            int32
	AmazonSideASN  int64
	AddressFamily  string
	Tags           map[string]string
}

// Gateway is the scanner-owned representation of one Direct Connect gateway. It
// carries identity, name, state, owner account, and Amazon-side ASN. The
// resource_type this maps to matches the target type the transitgateway scanner
// emits, so the dangling transit_gateway_attachment_to_direct_connect_gateway
// edge resolves once this scanner runs.
type Gateway struct {
	ID            string
	Name          string
	State         string
	OwnerAccount  string
	AmazonSideASN int64
}

// LAG is the scanner-owned representation of one Direct Connect link
// aggregation group. It carries identity, location, bandwidth, state, and
// member counts. As with Connection, MACsec key material is never persisted.
type LAG struct {
	ID                  string
	Name                string
	OwnerAccount        string
	Location            string
	Bandwidth           string
	State               string
	Region              string
	ProviderName        string
	MinimumLinks        int32
	NumberOfConnections int32
	MacSecCapable       bool
	Tags                map[string]string
}

// GatewayAssociation is the scanner-owned representation of one Direct Connect
// gateway association reported by DescribeDirectConnectGatewayAssociations. The
// associated gateway is either a transit gateway or a virtual private gateway;
// AssociatedGatewayType records the AWS-reported variant and AssociatedGatewayID
// names the associated resource by its AWS-reported identifier (tgw-... or
// vgw-...). VirtualGatewayID is also surfaced when AWS reports the legacy
// virtual-gateway field directly.
type GatewayAssociation struct {
	GatewayID             string
	AssociationID         string
	AssociationState      string
	AssociatedGatewayID   string
	AssociatedGatewayType string
	VirtualGatewayID      string
}
