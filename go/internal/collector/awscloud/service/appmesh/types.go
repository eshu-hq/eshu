// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appmesh

import (
	"context"
	"time"
)

// Client is the App Mesh read surface consumed by Scanner. Runtime adapters
// MUST translate AWS SDK responses into these scanner-owned metadata records.
//
// The interface is read-only by construction. It exposes one method that
// returns the whole mesh inventory already resolved. The adapter owns
// pagination and per-resource Describe fan-out so the scanner sees a flat,
// metadata-only view. The interface deliberately excludes every App Mesh
// mutation API (Create/Update/Delete for Mesh, VirtualService, VirtualNode,
// VirtualRouter, Route, VirtualGateway, GatewayRoute); a reflection test on the
// adapter asserts the exclusion so a future SDK change cannot smuggle one in.
type Client interface {
	// ListMeshInventory returns every mesh visible to the configured
	// credentials, each with its virtual services, virtual nodes, virtual
	// routers, routes, virtual gateways, and gateway routes already resolved
	// to metadata-only records.
	ListMeshInventory(context.Context) ([]Mesh, error)
}

// Mesh is the scanner-owned representation of one App Mesh service mesh and its
// child resources. It contains metadata only.
type Mesh struct {
	// ARN is the mesh ARN App Mesh reports. It is the join key children use to
	// reference their parent mesh.
	ARN string
	// Name is the mesh name.
	Name string
	// MeshOwner is the IAM account ID App Mesh reports as the mesh owner. For a
	// shared mesh this differs from the scanning account.
	MeshOwner string
	// ResourceOwner is the IAM account ID App Mesh reports as the resource
	// owner.
	ResourceOwner string
	// EgressFilterType is the mesh egress filter type (DROP_ALL or ALLOW_ALL).
	EgressFilterType string
	// IPPreference is the mesh service discovery IP preference, when set.
	IPPreference string
	// Status is the mesh lifecycle status App Mesh reports.
	Status string
	// CreatedAt is the App Mesh-reported creation time.
	CreatedAt time.Time
	// LastUpdatedAt is the App Mesh-reported last-update time.
	LastUpdatedAt time.Time
	// Tags carries App Mesh resource tags as raw evidence. Do not infer
	// environment, owner, workload, or deployable-unit truth from tags here.
	Tags map[string]string

	// VirtualServices are the virtual services that reside in this mesh.
	VirtualServices []VirtualService
	// VirtualNodes are the virtual nodes that reside in this mesh.
	VirtualNodes []VirtualNode
	// VirtualRouters are the virtual routers that reside in this mesh.
	VirtualRouters []VirtualRouter
	// VirtualGateways are the virtual gateways that reside in this mesh.
	VirtualGateways []VirtualGateway
}

// VirtualService is the scanner-owned representation of one App Mesh virtual
// service.
type VirtualService struct {
	// ARN is the virtual service ARN App Mesh reports.
	ARN string
	// Name is the virtual service name. App Mesh virtual service names are DNS
	// hostnames such as "checkout.apps.local".
	Name string
	// MeshName is the parent mesh name.
	MeshName string
	// ProviderKind reports whether the virtual service is backed by a virtual
	// node or a virtual router ("virtual_node", "virtual_router", or "").
	ProviderKind string
	// ProviderName is the provider virtual node or virtual router name, when a
	// provider is configured.
	ProviderName string
	// Status is the virtual service lifecycle status App Mesh reports.
	Status string
	// CreatedAt is the App Mesh-reported creation time.
	CreatedAt time.Time
	// LastUpdatedAt is the App Mesh-reported last-update time.
	LastUpdatedAt time.Time
	// Tags carries App Mesh resource tags as raw evidence.
	Tags map[string]string
}

// VirtualNode is the scanner-owned representation of one App Mesh virtual node.
//
// The node carries service discovery and backend references only. It never
// carries a literal client TLS certificate body: TLS validation is reduced to
// the ACM Private CA certificate authority ARNs the trust references, which are
// safe metadata references and the join key for the trust relationship.
type VirtualNode struct {
	// ARN is the virtual node ARN App Mesh reports.
	ARN string
	// Name is the virtual node name.
	Name string
	// MeshName is the parent mesh name.
	MeshName string
	// ServiceDiscoveryKind reports the service discovery type ("dns",
	// "aws_cloud_map", or "").
	ServiceDiscoveryKind string
	// DNSHostname is the DNS service discovery hostname, when the node uses DNS
	// service discovery.
	DNSHostname string
	// CloudMapNamespaceName is the AWS Cloud Map namespace name, when the node
	// uses Cloud Map service discovery.
	CloudMapNamespaceName string
	// CloudMapServiceName is the AWS Cloud Map service name, when the node uses
	// Cloud Map service discovery.
	CloudMapServiceName string
	// BackendVirtualServiceNames are the virtual service names this node sends
	// outbound traffic to.
	BackendVirtualServiceNames []string
	// ClientTLSCertificateAuthorityARNs are the ACM Private CA (acm-pca)
	// certificate authority ARNs referenced by client TLS validation trust
	// across backend and default client policies. These are ARN references only;
	// the literal certificate body and private key material are never read.
	ClientTLSCertificateAuthorityARNs []string
	// Status is the virtual node lifecycle status App Mesh reports.
	Status string
	// CreatedAt is the App Mesh-reported creation time.
	CreatedAt time.Time
	// LastUpdatedAt is the App Mesh-reported last-update time.
	LastUpdatedAt time.Time
	// Tags carries App Mesh resource tags as raw evidence.
	Tags map[string]string
}

// VirtualRouter is the scanner-owned representation of one App Mesh virtual
// router and the routes it owns.
type VirtualRouter struct {
	// ARN is the virtual router ARN App Mesh reports.
	ARN string
	// Name is the virtual router name.
	Name string
	// MeshName is the parent mesh name.
	MeshName string
	// Listeners are the router listener port/protocol pairs.
	Listeners []Listener
	// Status is the virtual router lifecycle status App Mesh reports.
	Status string
	// CreatedAt is the App Mesh-reported creation time.
	CreatedAt time.Time
	// LastUpdatedAt is the App Mesh-reported last-update time.
	LastUpdatedAt time.Time
	// Tags carries App Mesh resource tags as raw evidence.
	Tags map[string]string
	// Routes are the routes that reside in this virtual router.
	Routes []Route
}

// VirtualGateway is the scanner-owned representation of one App Mesh virtual
// gateway and the gateway routes it owns.
type VirtualGateway struct {
	// ARN is the virtual gateway ARN App Mesh reports.
	ARN string
	// Name is the virtual gateway name.
	Name string
	// MeshName is the parent mesh name.
	MeshName string
	// Listeners are the gateway listener port/protocol pairs.
	Listeners []Listener
	// Status is the virtual gateway lifecycle status App Mesh reports.
	Status string
	// CreatedAt is the App Mesh-reported creation time.
	CreatedAt time.Time
	// LastUpdatedAt is the App Mesh-reported last-update time.
	LastUpdatedAt time.Time
	// Tags carries App Mesh resource tags as raw evidence.
	Tags map[string]string
	// GatewayRoutes are the gateway routes that reside in this virtual gateway.
	GatewayRoutes []GatewayRoute
}

// Listener captures a single App Mesh listener port and protocol.
type Listener struct {
	// Port is the listener port number.
	Port int32
	// Protocol is the listener protocol (http, http2, grpc, tcp).
	Protocol string
}

// Route is the scanner-owned representation of one App Mesh route.
//
// Route carries the route shape only: protocol kind, priority, path prefix,
// method, and header match descriptors. Header match values are kept on
// HeaderMatch and classified by the scanner; sensitive values are redacted
// through the shared redact library before emission. The header NAME is always
// preserved.
type Route struct {
	// ARN is the route ARN App Mesh reports.
	ARN string
	// Name is the route name.
	Name string
	// MeshName is the parent mesh name.
	MeshName string
	// VirtualRouterName is the parent virtual router name.
	VirtualRouterName string
	// VirtualRouterARN is the parent virtual router ARN, the join key for the
	// route-in-virtual-router relationship.
	VirtualRouterARN string
	// ProtocolKind reports the route protocol ("http", "http2", "grpc", "tcp").
	ProtocolKind string
	// Priority is the route match priority App Mesh reports, when set.
	Priority *int32
	// PathPrefix is the HTTP path prefix match, when set.
	PathPrefix string
	// PathExact is the HTTP exact path match, when set.
	PathExact string
	// Method is the HTTP method match, when set.
	Method string
	// HeaderMatches are the HTTP header match descriptors.
	HeaderMatches []HeaderMatch
	// Status is the route lifecycle status App Mesh reports.
	Status string
	// CreatedAt is the App Mesh-reported creation time.
	CreatedAt time.Time
	// LastUpdatedAt is the App Mesh-reported last-update time.
	LastUpdatedAt time.Time
	// Tags carries App Mesh resource tags as raw evidence.
	Tags map[string]string
}

// HeaderMatch describes one App Mesh HTTP header match.
//
// The header Name is non-sensitive routing shape and is always emitted. Value
// holds the literal match string App Mesh reports (e.g. an exact or prefix
// value). The scanner classifies Value: when the header name looks sensitive
// (Authorization, Cookie, X-Api-Key shaped) or the value matches a shared
// sensitive-key pattern, the scanner emits a redaction marker instead of the
// literal value.
type HeaderMatch struct {
	// Name is the HTTP header name to match on.
	Name string
	// MatchType is the match operator ("exact", "prefix", "suffix", "regex",
	// "range", or "" when only presence is matched).
	MatchType string
	// Value is the literal match string App Mesh reports, when the match type
	// carries one. The scanner redacts this before emission when it is
	// sensitive; it is never persisted verbatim for sensitive headers.
	Value string
	// Invert reports whether the match is inverted (match anything except the
	// criteria).
	Invert bool
}

// GatewayRoute is the scanner-owned representation of one App Mesh gateway
// route.
type GatewayRoute struct {
	// ARN is the gateway route ARN App Mesh reports.
	ARN string
	// Name is the gateway route name.
	Name string
	// MeshName is the parent mesh name.
	MeshName string
	// VirtualGatewayName is the parent virtual gateway name.
	VirtualGatewayName string
	// VirtualGatewayARN is the parent virtual gateway ARN.
	VirtualGatewayARN string
	// ProtocolKind reports the gateway route protocol ("http", "http2",
	// "grpc").
	ProtocolKind string
	// TargetVirtualServiceName is the virtual service the gateway route routes
	// to, when set.
	TargetVirtualServiceName string
	// Status is the gateway route lifecycle status App Mesh reports.
	Status string
	// CreatedAt is the App Mesh-reported creation time.
	CreatedAt time.Time
	// LastUpdatedAt is the App Mesh-reported last-update time.
	LastUpdatedAt time.Time
	// Tags carries App Mesh resource tags as raw evidence.
	Tags map[string]string
}
