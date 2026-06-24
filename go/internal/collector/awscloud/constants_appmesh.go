// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceAppMesh identifies the regional AWS App Mesh metadata-only scan
	// slice. The scanner covers meshes, virtual services, virtual nodes,
	// virtual routers, routes, virtual gateways, and gateway routes. It never
	// calls App Mesh mutation APIs and never persists client TLS certificate
	// bodies or sensitive HTTP header match values.
	ServiceAppMesh = "appmesh"
)

const (
	// ResourceTypeAppMeshMesh identifies an App Mesh service mesh metadata
	// resource. The fact carries mesh identity (ARN, name), the mesh owner and
	// resource owner account IDs App Mesh reports, the egress filter type, and
	// the IP preference. App Mesh-internal children join to this resource by its
	// ARN.
	ResourceTypeAppMeshMesh = "aws_appmesh_mesh"

	// ResourceTypeAppMeshVirtualService identifies an App Mesh virtual service
	// metadata resource. The fact carries the virtual service identity (ARN,
	// name), parent mesh name, and the provider kind (virtual node or virtual
	// router) the virtual service routes to.
	ResourceTypeAppMeshVirtualService = "aws_appmesh_virtual_service"

	// ResourceTypeAppMeshVirtualNode identifies an App Mesh virtual node
	// metadata resource. The fact carries node identity (ARN, name), parent mesh
	// name, the service discovery shape (DNS hostname or Cloud Map
	// namespace/service), and the backend virtual service names. Client TLS
	// validation references record ACM Private CA certificate authority ARNs
	// only; the scanner never persists a literal certificate body.
	ResourceTypeAppMeshVirtualNode = "aws_appmesh_virtual_node"

	// ResourceTypeAppMeshVirtualRouter identifies an App Mesh virtual router
	// metadata resource. The fact carries router identity (ARN, name), parent
	// mesh name, and listener port/protocol metadata.
	ResourceTypeAppMeshVirtualRouter = "aws_appmesh_virtual_router"

	// ResourceTypeAppMeshRoute identifies an App Mesh route metadata resource.
	// The fact carries route identity (ARN, name), parent mesh and virtual
	// router names, the route protocol kind, priority, and the route match
	// shape (path prefix, method, header names). Sensitive header match values
	// (Authorization, Cookie, X-Api-Key shaped) are redacted through the shared
	// redact library; the header name is always preserved.
	ResourceTypeAppMeshRoute = "aws_appmesh_route"

	// ResourceTypeAppMeshVirtualGateway identifies an App Mesh virtual gateway
	// metadata resource. The fact carries gateway identity (ARN, name), parent
	// mesh name, and listener port/protocol metadata.
	ResourceTypeAppMeshVirtualGateway = "aws_appmesh_virtual_gateway"

	// ResourceTypeAppMeshGatewayRoute identifies an App Mesh gateway route
	// metadata resource. The fact carries gateway route identity (ARN, name),
	// parent mesh and virtual gateway names, the route protocol kind, and the
	// target virtual service name.
	ResourceTypeAppMeshGatewayRoute = "aws_appmesh_gateway_route"
)

const (
	// RelationshipAppMeshVirtualServiceInMesh records that a virtual service
	// belongs to a service mesh. The target is the parent mesh resource keyed
	// by its App Mesh ARN.
	RelationshipAppMeshVirtualServiceInMesh = "appmesh_virtual_service_in_mesh"

	// RelationshipAppMeshVirtualNodeBackendVirtualService records an outbound
	// backend reference from a virtual node to a virtual service it sends
	// traffic to. The target is the backend virtual service resource keyed by
	// its App Mesh ARN.
	RelationshipAppMeshVirtualNodeBackendVirtualService = "appmesh_virtual_node_backend_virtual_service"

	// RelationshipAppMeshRouteInVirtualRouter records that a route belongs to a
	// virtual router. The target is the parent virtual router resource keyed by
	// its App Mesh ARN.
	RelationshipAppMeshRouteInVirtualRouter = "appmesh_route_in_virtual_router"

	// RelationshipAppMeshVirtualGatewayInMesh records that a virtual gateway
	// belongs to a service mesh. The target is the parent mesh resource keyed by
	// its App Mesh ARN.
	RelationshipAppMeshVirtualGatewayInMesh = "appmesh_virtual_gateway_in_mesh"

	// RelationshipAppMeshVirtualNodeTrustsCertificateAuthority records that a
	// virtual node client TLS policy validates peers against an ACM Private CA
	// certificate authority. App Mesh reports these trust anchors as ACM Private
	// CA ARNs (arn:{partition}:acm-pca:{region}:{account}:certificate-authority/
	// {id}), not public ACM certificate ARNs, so the target keys on the
	// certificate authority ARN. The literal certificate body is never read.
	RelationshipAppMeshVirtualNodeTrustsCertificateAuthority = "appmesh_virtual_node_trusts_certificate_authority"

	// RelationshipAppMeshVirtualNodeUsesCloudMapService records that a virtual
	// node discovers endpoints through an AWS Cloud Map namespace and service.
	// The target is keyed by the Cloud Map namespace/service identity App Mesh
	// reports.
	RelationshipAppMeshVirtualNodeUsesCloudMapService = "appmesh_virtual_node_uses_cloud_map_service"

	// RelationshipAppMeshVirtualNodeUsesDNSHostname records that a virtual node
	// discovers endpoints through a DNS hostname. The target is keyed by the
	// reported hostname.
	RelationshipAppMeshVirtualNodeUsesDNSHostname = "appmesh_virtual_node_uses_dns_hostname"
)

const (
	// ResourceTypeACMPCACertificateAuthority identifies an AWS Certificate
	// Manager Private CA certificate authority. App Mesh client TLS trust ARNs
	// point at ACM Private CA (acm-pca) certificate authorities, not public ACM
	// certificates, so the virtual-node trust edge targets this type. The
	// acm-pca scanner (ServiceACMPCA) publishes certificate authority resources
	// keyed by the certificate authority ARN, which is the resource_id this
	// constant joins against, so the trust edge resolves.
	ResourceTypeACMPCACertificateAuthority = "aws_acmpca_certificate_authority"

	// TargetTypeCloudMapService is the relationship target_type for an AWS Cloud
	// Map service discovered through an App Mesh virtual node. Cloud Map has no
	// Eshu scanner yet, so the target keys on the namespace/service identity App
	// Mesh reports rather than a Cloud Map ARN.
	TargetTypeCloudMapService = "aws_cloud_map_service"

	// TargetTypeDNSHostname is the relationship target_type for a DNS hostname
	// an App Mesh virtual node uses for service discovery. The hostname is an
	// external name, not an AWS ARN-addressable resource.
	TargetTypeDNSHostname = "dns_hostname"
)
