// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package transitgateway

import (
	"context"
	"time"
)

// Client is the Transit Gateway metadata read surface consumed by Scanner.
// Runtime adapters translate AWS SDK EC2 responses into these scanner-owned
// records. The interface intentionally exposes only list/read operations; no
// mutation method (Create/Delete/Modify/Associate/Disassociate/Enable/Disable/
// Accept/Reject) appears here, and the AWS SDK adapter must not gain access to
// any such method through its embedded paginator interfaces either.
type Client interface {
	ListTransitGateways(context.Context) ([]TransitGateway, error)
	ListTransitGatewayRouteTables(context.Context) ([]RouteTable, error)
	ListTransitGatewayAttachments(context.Context) ([]Attachment, error)
	ListTransitGatewayPeeringAttachments(context.Context) ([]PeeringAttachment, error)
	ListTransitGatewayMulticastDomains(context.Context) ([]MulticastDomain, error)
	ListTransitGatewayPolicyTables(context.Context) ([]PolicyTable, error)
}

// TransitGateway is the scanner-owned representation of one transit gateway.
// It carries identity, ownership, state, and the non-secret configuration
// options AWS reports. The scanner never reads routes or policy entries.
type TransitGateway struct {
	ID          string
	ARN         string
	OwnerID     string
	State       string
	Description string
	CreatedAt   time.Time
	Options     TransitGatewayOptions
	Tags        map[string]string
}

// TransitGatewayOptions captures the non-secret transit gateway option flags
// AWS reports. The default association and propagation route table IDs anchor
// route-table relationships even when no attachment association is reported.
type TransitGatewayOptions struct {
	AmazonSideASN                  int64
	AssociationDefaultRouteTableID string
	PropagationDefaultRouteTableID string
	AutoAcceptSharedAttachments    string
	DefaultRouteTableAssociation   string
	DefaultRouteTablePropagation   string
	DNSSupport                     string
	MulticastSupport               string
	VPNECMPSupport                 string
}

// RouteTable is the scanner-owned representation of one transit gateway route
// table. It is distinct from a VPC route table: a transit gateway route table
// routes between attachments rather than between subnets.
type RouteTable struct {
	ID                           string
	TransitGatewayID             string
	State                        string
	DefaultAssociationRouteTable bool
	DefaultPropagationRouteTable bool
	CreatedAt                    time.Time
	Tags                         map[string]string
}

// Attachment is the scanner-owned representation of one transit gateway
// attachment reported by DescribeTransitGatewayAttachments. AWS reports VPC,
// VPN, Direct Connect gateway, peering, and Connect attachments through this
// single API; ResourceType records the variant and ResourceID names the
// attached resource. The Association fields carry the route table the
// attachment is associated with, when AWS reports one.
type Attachment struct {
	ID                      string
	TransitGatewayID        string
	TransitGatewayOwnerID   string
	ResourceType            string
	ResourceID              string
	ResourceOwnerID         string
	State                   string
	AssociationRouteTableID string
	AssociationState        string
	CreatedAt               time.Time
	Tags                    map[string]string
}

// PeeringAttachment is the scanner-owned representation of one transit gateway
// peering attachment. Peering attachments link two transit gateways that can
// live in different accounts and Regions. The scanner emits both sides as AWS
// reports them and never resolves the remote account's identity.
type PeeringAttachment struct {
	ID            string
	State         string
	StatusCode    string
	StatusMessage string
	Requester     PeeringTransitGatewayInfo
	Accepter      PeeringTransitGatewayInfo
	CreatedAt     time.Time
	Tags          map[string]string
}

// PeeringTransitGatewayInfo captures one side of a peering attachment as AWS
// reports it. OwnerID and Region are surfaced as-is for downstream
// org-context joins; the scanner does not resolve the remote account.
type PeeringTransitGatewayInfo struct {
	TransitGatewayID string
	OwnerID          string
	Region           string
	CoreNetworkID    string
}

// MulticastDomain is the scanner-owned representation of one transit gateway
// multicast domain.
type MulticastDomain struct {
	ID               string
	ARN              string
	TransitGatewayID string
	OwnerID          string
	State            string
	CreatedAt        time.Time
	Options          MulticastDomainOptions
	Tags             map[string]string
}

// MulticastDomainOptions captures the non-secret multicast domain option flags
// AWS reports.
type MulticastDomainOptions struct {
	AutoAcceptSharedAssociations string
	IGMPv2Support                string
	StaticSourcesSupport         string
}

// PolicyTable is the scanner-owned representation of one transit gateway policy
// table. The scanner emits identity and state only; it never reads the policy
// rules, which can carry network policy detail outside the inventory contract.
type PolicyTable struct {
	ID               string
	TransitGatewayID string
	State            string
	CreatedAt        time.Time
	Tags             map[string]string
}
