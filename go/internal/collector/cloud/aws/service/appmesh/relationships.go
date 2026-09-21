// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appmesh

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// virtualServiceRelationships emits the virtual-service-to-mesh edge. The mesh
// is keyed by its own ARN so the join lands on the mesh resource.
func virtualServiceRelationships(boundary aws.Boundary, mesh Mesh, service VirtualService) []aws.RelationshipObservation {
	serviceARN := strings.TrimSpace(service.ARN)
	meshARN := strings.TrimSpace(mesh.ARN)
	if serviceARN == "" || meshARN == "" {
		return nil
	}
	return []aws.RelationshipObservation{{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipAppMeshVirtualServiceInMesh,
		SourceResourceID: serviceARN,
		SourceARN:        serviceARN,
		TargetResourceID: meshARN,
		TargetARN:        meshARN,
		TargetType:       aws.ResourceTypeAppMeshMesh,
		SourceRecordID:   serviceARN + "->" + meshARN,
	}}
}

// virtualNodeRelationships emits the backend, certificate-authority trust, and
// service-discovery edges for one virtual node. Backend virtual service ARNs
// are synthesized from the node's own ARN so the partition is never hardcoded.
func virtualNodeRelationships(boundary aws.Boundary, node VirtualNode) []aws.RelationshipObservation {
	nodeARN := strings.TrimSpace(node.ARN)
	if nodeARN == "" {
		return nil
	}
	var relationships []aws.RelationshipObservation

	for _, backend := range node.BackendVirtualServiceNames {
		backendName := strings.TrimSpace(backend)
		if backendName == "" {
			continue
		}
		backendARN := siblingResourceARN(nodeARN, "virtualService", backendName)
		if backendARN == "" {
			continue
		}
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipAppMeshVirtualNodeBackendVirtualService,
			SourceResourceID: nodeARN,
			SourceARN:        nodeARN,
			TargetResourceID: backendARN,
			TargetARN:        backendARN,
			TargetType:       aws.ResourceTypeAppMeshVirtualService,
			Attributes:       map[string]any{"virtual_service_name": backendName},
			SourceRecordID:   nodeARN + "->" + backendARN,
		})
	}

	for _, ca := range node.ClientTLSCertificateAuthorityARNs {
		caARN := strings.TrimSpace(ca)
		if !strings.HasPrefix(caARN, "arn:") {
			continue
		}
		// App Mesh reports client TLS trust anchors as ACM Private CA (acm-pca)
		// certificate authority ARNs, not public ACM certificate ARNs. Keying
		// the target on the CA ARN with the acm-pca target type lets the edge
		// join an ACM Private CA certificate authority resource instead of
		// dangling against the public ACM scanner.
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipAppMeshVirtualNodeTrustsCertificateAuthority,
			SourceResourceID: nodeARN,
			SourceARN:        nodeARN,
			TargetResourceID: caARN,
			TargetARN:        caARN,
			TargetType:       aws.ResourceTypeACMPCACertificateAuthority,
			SourceRecordID:   nodeARN + "->" + caARN,
		})
	}

	if rel, ok := serviceDiscoveryRelationship(boundary, node, nodeARN); ok {
		relationships = append(relationships, rel)
	}

	return relationships
}

// serviceDiscoveryRelationship emits the Cloud Map or DNS service-discovery
// edge for a virtual node. Cloud Map keys on "namespace/service" because Cloud
// Map has no Eshu scanner; DNS keys on the hostname.
func serviceDiscoveryRelationship(boundary aws.Boundary, node VirtualNode, nodeARN string) (aws.RelationshipObservation, bool) {
	switch strings.TrimSpace(node.ServiceDiscoveryKind) {
	case "aws_cloud_map":
		namespace := strings.TrimSpace(node.CloudMapNamespaceName)
		serviceName := strings.TrimSpace(node.CloudMapServiceName)
		if namespace == "" || serviceName == "" {
			return aws.RelationshipObservation{}, false
		}
		target := namespace + "/" + serviceName
		return aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipAppMeshVirtualNodeUsesCloudMapService,
			SourceResourceID: nodeARN,
			SourceARN:        nodeARN,
			TargetResourceID: target,
			TargetType:       aws.TargetTypeCloudMapService,
			Attributes: map[string]any{
				"cloud_map_namespace_name": namespace,
				"cloud_map_service_name":   serviceName,
			},
			SourceRecordID: nodeARN + "->cloudmap:" + target,
		}, true
	case "dns":
		hostname := strings.TrimSpace(node.DNSHostname)
		if hostname == "" {
			return aws.RelationshipObservation{}, false
		}
		return aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipAppMeshVirtualNodeUsesDNSHostname,
			SourceResourceID: nodeARN,
			SourceARN:        nodeARN,
			TargetResourceID: hostname,
			TargetType:       aws.TargetTypeDNSHostname,
			SourceRecordID:   nodeARN + "->dns:" + hostname,
		}, true
	default:
		return aws.RelationshipObservation{}, false
	}
}

// routeRelationships emits the route-to-virtual-router edge. The router is
// keyed by the parent virtual router ARN App Mesh reports for the route.
func routeRelationships(boundary aws.Boundary, route Route) []aws.RelationshipObservation {
	routeARN := strings.TrimSpace(route.ARN)
	routerARN := strings.TrimSpace(route.VirtualRouterARN)
	if routeARN == "" || routerARN == "" {
		return nil
	}
	return []aws.RelationshipObservation{{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipAppMeshRouteInVirtualRouter,
		SourceResourceID: routeARN,
		SourceARN:        routeARN,
		TargetResourceID: routerARN,
		TargetARN:        routerARN,
		TargetType:       aws.ResourceTypeAppMeshVirtualRouter,
		SourceRecordID:   routeARN + "->" + routerARN,
	}}
}

// virtualGatewayRelationships emits the virtual-gateway-to-mesh edge.
func virtualGatewayRelationships(boundary aws.Boundary, mesh Mesh, gateway VirtualGateway) []aws.RelationshipObservation {
	gatewayARN := strings.TrimSpace(gateway.ARN)
	meshARN := strings.TrimSpace(mesh.ARN)
	if gatewayARN == "" || meshARN == "" {
		return nil
	}
	return []aws.RelationshipObservation{{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipAppMeshVirtualGatewayInMesh,
		SourceResourceID: gatewayARN,
		SourceARN:        gatewayARN,
		TargetResourceID: meshARN,
		TargetARN:        meshARN,
		TargetType:       aws.ResourceTypeAppMeshMesh,
		SourceRecordID:   gatewayARN + "->" + meshARN,
	}}
}

// siblingResourceARN synthesizes a sibling App Mesh resource ARN from a known
// resource ARN within the same mesh. App Mesh resource ARNs share the form
// arn:{partition}:appmesh:{region}:{account}:mesh/{meshName}/{kind}/{name}.
// Deriving the prefix from the source ARN keeps the partition, region, account,
// and mesh name consistent with what App Mesh reported, so we never hardcode
// "arn:aws:". It returns "" when the source ARN is not a recognizable mesh-child
// ARN.
func siblingResourceARN(sourceARN, kind, name string) string {
	const meshSegment = ":mesh/"
	index := strings.Index(sourceARN, meshSegment)
	if index < 0 {
		return ""
	}
	rest := sourceARN[index+len(meshSegment):]
	meshName := rest
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		meshName = rest[:slash]
	}
	if strings.TrimSpace(meshName) == "" {
		return ""
	}
	prefix := sourceARN[:index+len(meshSegment)]
	return prefix + meshName + "/" + kind + "/" + name
}
