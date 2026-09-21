// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package networkmanager

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only AWS Network Manager observations for one AWS
// claim. Implementations read control-plane metadata through the Network
// Manager Describe/Get/List management APIs and never mutate Network Manager
// state.
type Client interface {
	// Snapshot returns every global network visible to the configured AWS
	// credentials, each carrying its sites, devices, links, connections,
	// device-to-link associations, and transit gateway registrations, plus the
	// account's core networks. Network Manager is a global service, so the
	// adapter pins the control-plane region regardless of the claim region.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures Network Manager global-network and core-network metadata
// plus non-fatal scan warnings.
type Snapshot struct {
	// GlobalNetworks is the metadata-only set of global networks, each carrying
	// its child resources.
	GlobalNetworks []GlobalNetwork
	// CoreNetworks is the metadata-only set of core networks across every global
	// network in the account.
	CoreNetworks []CoreNetwork
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// GlobalNetwork is the scanner-owned Network Manager global-network model and
// the parent container for sites, devices, links, connections, and transit
// gateway registrations.
type GlobalNetwork struct {
	// ARN is the Amazon Resource Name that uniquely identifies the global
	// network. Network Manager ARNs carry no region segment.
	ARN string
	// ID is the global network id (global-network-...).
	ID string
	// Description is the operator-supplied description.
	Description string
	// State is the global network lifecycle state (for example AVAILABLE).
	State string
	// CreatedAt is when the global network was created.
	CreatedAt time.Time
	// Tags carries the global network resource tags.
	Tags map[string]string
	// Sites are the metadata-only sites within this global network.
	Sites []Site
	// Devices are the metadata-only devices within this global network.
	Devices []Device
	// Links are the metadata-only links within this global network.
	Links []Link
	// Connections are the metadata-only connections within this global network.
	Connections []Connection
	// LinkAssociations are the device-to-link associations within this global
	// network.
	LinkAssociations []LinkAssociation
	// TransitGatewayRegistrations are the transit gateways registered with this
	// global network.
	TransitGatewayRegistrations []TransitGatewayRegistration
}

// Site is the scanner-owned Network Manager site model: a physical location
// within a global network.
type Site struct {
	// ARN is the Amazon Resource Name that uniquely identifies the site.
	ARN string
	// ID is the site id (site-...).
	ID string
	// GlobalNetworkID is the id of the parent global network.
	GlobalNetworkID string
	// Description is the operator-supplied description.
	Description string
	// State is the site lifecycle state.
	State string
	// Address is the operator-supplied physical address, when reported.
	Address string
	// Latitude is the operator-supplied latitude, when reported.
	Latitude string
	// Longitude is the operator-supplied longitude, when reported.
	Longitude string
	// CreatedAt is when the site was created.
	CreatedAt time.Time
	// Tags carries the site resource tags.
	Tags map[string]string
}

// Device is the scanner-owned Network Manager device model: an appliance within
// a global network. The serial number is operator inventory metadata and is
// intentionally excluded from the scanner-owned model.
type Device struct {
	// ARN is the Amazon Resource Name that uniquely identifies the device.
	ARN string
	// ID is the device id (device-...).
	ID string
	// GlobalNetworkID is the id of the parent global network.
	GlobalNetworkID string
	// SiteID is the id of the site the device is placed at, when reported.
	SiteID string
	// Description is the operator-supplied description.
	Description string
	// Type is the device type.
	Type string
	// Vendor is the device vendor.
	Vendor string
	// Model is the device model.
	Model string
	// State is the device lifecycle state.
	State string
	// SubnetARN is the AWS subnet the device reports as its location, when set.
	// It is retained as context metadata only; Eshu does not yet publish a VPC
	// subnet resource node, so no graph edge is keyed to it.
	SubnetARN string
	// Zone is the reported Availability Zone, Local Zone, Wavelength Zone, or
	// Outpost id, when set.
	Zone string
	// Address is the operator-supplied physical address, when reported.
	Address string
	// Latitude is the operator-supplied latitude, when reported.
	Latitude string
	// Longitude is the operator-supplied longitude, when reported.
	Longitude string
	// CreatedAt is when the device was created.
	CreatedAt time.Time
	// Tags carries the device resource tags.
	Tags map[string]string
}

// Link is the scanner-owned Network Manager link model: a connection medium at
// a site within a global network.
type Link struct {
	// ARN is the Amazon Resource Name that uniquely identifies the link.
	ARN string
	// ID is the link id (link-...).
	ID string
	// GlobalNetworkID is the id of the parent global network.
	GlobalNetworkID string
	// SiteID is the id of the site the link belongs to, when reported.
	SiteID string
	// Description is the operator-supplied description.
	Description string
	// Type is the link type.
	Type string
	// Provider is the link provider.
	Provider string
	// UploadSpeedMbps is the reported upload bandwidth in Mbps.
	UploadSpeedMbps int32
	// DownloadSpeedMbps is the reported download bandwidth in Mbps.
	DownloadSpeedMbps int32
	// State is the link lifecycle state.
	State string
	// CreatedAt is when the link was created.
	CreatedAt time.Time
	// Tags carries the link resource tags.
	Tags map[string]string
}

// Connection is the scanner-owned Network Manager connection model: a cabling
// association between two devices within a global network.
type Connection struct {
	// ARN is the Amazon Resource Name that uniquely identifies the connection.
	ARN string
	// ID is the connection id (connection-...).
	ID string
	// GlobalNetworkID is the id of the parent global network.
	GlobalNetworkID string
	// DeviceID is the id of the first device in the connection.
	DeviceID string
	// ConnectedDeviceID is the id of the second device in the connection.
	ConnectedDeviceID string
	// LinkID is the id of the link for the first device, when reported.
	LinkID string
	// ConnectedLinkID is the id of the link for the second device, when reported.
	ConnectedLinkID string
	// Description is the operator-supplied description.
	Description string
	// State is the connection lifecycle state.
	State string
	// CreatedAt is when the connection was created.
	CreatedAt time.Time
	// Tags carries the connection resource tags.
	Tags map[string]string
}

// LinkAssociation is the scanner-owned Network Manager device-to-link
// association model reported by GetLinkAssociations.
type LinkAssociation struct {
	// GlobalNetworkID is the id of the global network the association lives in.
	GlobalNetworkID string
	// DeviceID is the associated device id.
	DeviceID string
	// LinkID is the associated link id.
	LinkID string
	// State is the association state.
	State string
}

// TransitGatewayRegistration is the scanner-owned Network Manager transit
// gateway registration model reported by GetTransitGatewayRegistrations.
type TransitGatewayRegistration struct {
	// GlobalNetworkID is the id of the global network the transit gateway is
	// registered with.
	GlobalNetworkID string
	// TransitGatewayARN is the ARN of the registered transit gateway. The scanner
	// extracts the bare transit gateway id from it to key the edge to the transit
	// gateway node.
	TransitGatewayARN string
	// State is the registration state code (for example AVAILABLE).
	State string
}

// CoreNetwork is the scanner-owned Network Manager core-network model: the
// AWS-managed Cloud WAN backbone of a global network. Routing policy documents
// are intentionally excluded.
type CoreNetwork struct {
	// ARN is the Amazon Resource Name that uniquely identifies the core network.
	ARN string
	// ID is the core network id (core-network-...).
	ID string
	// GlobalNetworkID is the id of the parent global network.
	GlobalNetworkID string
	// Description is the operator-supplied description.
	Description string
	// State is the core network lifecycle state.
	State string
	// SegmentNames are the segment names within the core network.
	SegmentNames []string
	// EdgeLocations are the edge location regions within the core network.
	EdgeLocations []string
	// CreatedAt is when the core network was created.
	CreatedAt time.Time
	// Tags carries the core network resource tags.
	Tags map[string]string
}
