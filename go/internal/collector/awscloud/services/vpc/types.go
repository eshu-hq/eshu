// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vpc

import (
	"context"
	"time"
)

// Client is the VPC topology read surface consumed by Scanner. Runtime adapters
// translate AWS SDK responses into these scanner-owned network-fabric records.
// The interface intentionally exposes only list/read operations; no mutation
// method (Create/Delete/Modify/Associate/Disassociate/Authorize/Allocate/
// Release) appears here, and the AWS SDK adapter must not gain access to any
// such method through its embedded paginator interfaces either.
type Client interface {
	ListRouteTables(context.Context) ([]RouteTable, error)
	ListInternetGateways(context.Context) ([]InternetGateway, error)
	ListNATGateways(context.Context) ([]NATGateway, error)
	ListNetworkACLs(context.Context) ([]NetworkACL, error)
	ListVPCPeeringConnections(context.Context) ([]VPCPeeringConnection, error)
	ListVPCEndpoints(context.Context) ([]VPCEndpoint, error)
	ListElasticIPs(context.Context) ([]ElasticIP, error)
	ListDHCPOptions(context.Context) ([]DHCPOptions, error)
	ListCustomerGateways(context.Context) ([]CustomerGateway, error)
	ListVPNGateways(context.Context) ([]VPNGateway, error)
	ListVPNConnections(context.Context) ([]VPNConnection, error)
}

// RouteTable is the scanner-owned representation of one VPC route table.
type RouteTable struct {
	ID           string
	VPCID        string
	OwnerID      string
	Associations []RouteTableAssociation
	Routes       []Route
	Tags         map[string]string
}

// RouteTableAssociation captures one route-table association reported by AWS.
type RouteTableAssociation struct {
	AssociationID string
	SubnetID      string
	GatewayID     string
	Main          bool
	State         string
}

// Route captures one route in a route table. The scanner emits the AWS-reported
// target identifiers and never simulates next-hop semantics.
type Route struct {
	DestinationCIDRBlock     string
	DestinationIPv6CIDRBlock string
	DestinationPrefixListID  string
	GatewayID                string
	NATGatewayID             string
	VPCPeeringConnectionID   string
	VPCEndpointID            string
	TransitGatewayID         string
	NetworkInterfaceID       string
	InstanceID               string
	CarrierGatewayID         string
	EgressOnlyIGWID          string
	State                    string
	Origin                   string
}

// InternetGateway is the scanner-owned representation of one internet gateway.
type InternetGateway struct {
	ID          string
	OwnerID     string
	Attachments []InternetGatewayAttachment
	Tags        map[string]string
}

// InternetGatewayAttachment captures one VPC attachment on an internet gateway.
type InternetGatewayAttachment struct {
	VPCID string
	State string
}

// NATGateway is the scanner-owned representation of one NAT gateway.
type NATGateway struct {
	ID                  string
	VPCID               string
	SubnetID            string
	State               string
	ConnectivityType    string
	FailureCode         string
	FailureMessage      string
	CreatedAt           time.Time
	DeletedAt           time.Time
	NATGatewayAddresses []NATGatewayAddress
	Tags                map[string]string
}

// NATGatewayAddress captures one address attached to a NAT gateway.
type NATGatewayAddress struct {
	AllocationID       string
	NetworkInterfaceID string
	PrivateIP          string
	PublicIP           string
	IsPrimary          bool
}

// NetworkACL is the scanner-owned representation of one network ACL.
type NetworkACL struct {
	ID           string
	VPCID        string
	OwnerID      string
	IsDefault    bool
	Associations []NetworkACLAssociation
	Entries      []NetworkACLEntry
	Tags         map[string]string
}

// NetworkACLAssociation captures one network-ACL-to-subnet association.
type NetworkACLAssociation struct {
	AssociationID string
	SubnetID      string
}

// NetworkACLEntry captures one rule entry on a network ACL. The scanner emits
// AWS-reported metadata only; it does not synthesize effective policy.
type NetworkACLEntry struct {
	RuleNumber    int32
	Protocol      string
	RuleAction    string
	Egress        bool
	CIDRBlock     string
	IPv6CIDRBlock string
	PortRangeFrom *int32
	PortRangeTo   *int32
	ICMPCode      *int32
	ICMPType      *int32
}

// VPCPeeringConnection is the scanner-owned representation of one VPC peering
// connection.
type VPCPeeringConnection struct {
	ID            string
	Status        string
	StatusMessage string
	Requester     VPCPeeringVPCInfo
	Accepter      VPCPeeringVPCInfo
	ExpirationAt  time.Time
	Tags          map[string]string
}

// VPCPeeringVPCInfo captures one side of a peering connection.
type VPCPeeringVPCInfo struct {
	VPCID     string
	OwnerID   string
	Region    string
	CIDRBlock string
}

// VPCEndpoint is the scanner-owned representation of one VPC endpoint, gateway
// or interface.
type VPCEndpoint struct {
	ID                  string
	VPCID               string
	ServiceName         string
	EndpointType        string
	State               string
	PrivateDNSEnabled   bool
	RequesterManaged    bool
	OwnerID             string
	RouteTableIDs       []string
	SubnetIDs           []string
	SecurityGroupIDs    []string
	NetworkInterfaceIDs []string
	DNSEntries          []VPCEndpointDNSEntry
	CreatedAt           time.Time
	Tags                map[string]string
}

// VPCEndpointDNSEntry captures one DNS entry reported for an interface endpoint.
type VPCEndpointDNSEntry struct {
	DNSName      string
	HostedZoneID string
}

// ElasticIP is the scanner-owned representation of one allocated Elastic IP.
type ElasticIP struct {
	AllocationID            string
	AssociationID           string
	Domain                  string
	PublicIP                string
	PublicIPv4Pool          string
	NetworkBorderGroup      string
	InstanceID              string
	NetworkInterfaceID      string
	NetworkInterfaceOwnerID string
	PrivateIP               string
	Tags                    map[string]string
}

// DHCPOptions is the scanner-owned representation of one DHCP option set.
type DHCPOptions struct {
	ID            string
	OwnerID       string
	Configuration []DHCPConfigurationEntry
	Tags          map[string]string
}

// DHCPConfigurationEntry captures one DHCP-option key/value list.
type DHCPConfigurationEntry struct {
	Key    string
	Values []string
}

// CustomerGateway is the scanner-owned representation of one VPN customer
// gateway.
type CustomerGateway struct {
	ID             string
	State          string
	Type           string
	IPAddress      string
	BGPASN         string
	DeviceName     string
	CertificateARN string
	Tags           map[string]string
}

// VPNGateway is the scanner-owned representation of one virtual private
// gateway.
type VPNGateway struct {
	ID               string
	State            string
	Type             string
	AvailabilityZone string
	AmazonSideASN    int64
	VPCAttachments   []VPNGatewayAttachment
	Tags             map[string]string
}

// VPNGatewayAttachment captures one VPN gateway VPC attachment.
type VPNGatewayAttachment struct {
	VPCID string
	State string
}

// VPNConnection is the scanner-owned representation of one site-to-site VPN
// connection. Tunnel pre-shared keys are intentionally outside the contract.
type VPNConnection struct {
	ID                 string
	State              string
	Type               string
	Category           string
	CustomerGatewayID  string
	VPNGatewayID       string
	TransitGatewayID   string
	CoreNetworkARN     string
	StaticRoutesOnly   bool
	Tags               map[string]string
	TelemetrySummaries []VPNTunnelTelemetry
}

// VPNTunnelTelemetry captures non-secret tunnel telemetry the scanner persists.
// The scanner intentionally never reads or stores tunnel pre-shared keys.
type VPNTunnelTelemetry struct {
	OutsideIPAddress   string
	Status             string
	StatusMessage      string
	AcceptedRouteCount int32
	LastStatusChange   time.Time
	CertificateARN     string
}
