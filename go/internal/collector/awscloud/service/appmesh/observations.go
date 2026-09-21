// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appmesh

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// meshObservation maps one mesh into an aws_resource observation keyed by the
// mesh ARN.
func meshObservation(boundary awscloud.Boundary, mesh Mesh) awscloud.ResourceObservation {
	meshARN := strings.TrimSpace(mesh.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          meshARN,
		ResourceID:   meshARN,
		ResourceType: awscloud.ResourceTypeAppMeshMesh,
		Name:         strings.TrimSpace(mesh.Name),
		State:        strings.TrimSpace(mesh.Status),
		Tags:         cloneStringMap(mesh.Tags),
		Attributes: map[string]any{
			"mesh_name":          strings.TrimSpace(mesh.Name),
			"mesh_owner":         strings.TrimSpace(mesh.MeshOwner),
			"resource_owner":     strings.TrimSpace(mesh.ResourceOwner),
			"egress_filter_type": strings.TrimSpace(mesh.EgressFilterType),
			"ip_preference":      strings.TrimSpace(mesh.IPPreference),
			"status":             strings.TrimSpace(mesh.Status),
			"created_at":         timeOrNil(mesh.CreatedAt),
			"last_updated_at":    timeOrNil(mesh.LastUpdatedAt),
		},
		CorrelationAnchors: []string{meshARN, strings.TrimSpace(mesh.Name)},
		SourceRecordID:     meshARN,
	}
}

// virtualServiceObservation maps one virtual service into an aws_resource
// observation keyed by its ARN.
func virtualServiceObservation(boundary awscloud.Boundary, service VirtualService) awscloud.ResourceObservation {
	arn := strings.TrimSpace(service.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   arn,
		ResourceType: awscloud.ResourceTypeAppMeshVirtualService,
		Name:         strings.TrimSpace(service.Name),
		State:        strings.TrimSpace(service.Status),
		Tags:         cloneStringMap(service.Tags),
		Attributes: map[string]any{
			"virtual_service_name": strings.TrimSpace(service.Name),
			"mesh_name":            strings.TrimSpace(service.MeshName),
			"provider_kind":        strings.TrimSpace(service.ProviderKind),
			"provider_name":        strings.TrimSpace(service.ProviderName),
			"status":               strings.TrimSpace(service.Status),
			"created_at":           timeOrNil(service.CreatedAt),
			"last_updated_at":      timeOrNil(service.LastUpdatedAt),
		},
		CorrelationAnchors: []string{arn, strings.TrimSpace(service.Name)},
		SourceRecordID:     arn,
	}
}

// virtualNodeObservation maps one virtual node into an aws_resource
// observation. Client TLS validation is reduced to ACM Private CA certificate
// authority ARNs; no certificate body is ever recorded.
func virtualNodeObservation(boundary awscloud.Boundary, node VirtualNode) awscloud.ResourceObservation {
	arn := strings.TrimSpace(node.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   arn,
		ResourceType: awscloud.ResourceTypeAppMeshVirtualNode,
		Name:         strings.TrimSpace(node.Name),
		State:        strings.TrimSpace(node.Status),
		Tags:         cloneStringMap(node.Tags),
		Attributes: map[string]any{
			"virtual_node_name":                     strings.TrimSpace(node.Name),
			"mesh_name":                             strings.TrimSpace(node.MeshName),
			"service_discovery_kind":                strings.TrimSpace(node.ServiceDiscoveryKind),
			"dns_hostname":                          strings.TrimSpace(node.DNSHostname),
			"cloud_map_namespace_name":              strings.TrimSpace(node.CloudMapNamespaceName),
			"cloud_map_service_name":                strings.TrimSpace(node.CloudMapServiceName),
			"backend_virtual_service_names":         cloneStrings(node.BackendVirtualServiceNames),
			"client_tls_certificate_authority_arns": cloneStrings(node.ClientTLSCertificateAuthorityARNs),
			"status":                                strings.TrimSpace(node.Status),
			"created_at":                            timeOrNil(node.CreatedAt),
			"last_updated_at":                       timeOrNil(node.LastUpdatedAt),
		},
		CorrelationAnchors: []string{arn, strings.TrimSpace(node.Name)},
		SourceRecordID:     arn,
	}
}

// virtualRouterObservation maps one virtual router into an aws_resource
// observation keyed by its ARN.
func virtualRouterObservation(boundary awscloud.Boundary, router VirtualRouter) awscloud.ResourceObservation {
	arn := strings.TrimSpace(router.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   arn,
		ResourceType: awscloud.ResourceTypeAppMeshVirtualRouter,
		Name:         strings.TrimSpace(router.Name),
		State:        strings.TrimSpace(router.Status),
		Tags:         cloneStringMap(router.Tags),
		Attributes: map[string]any{
			"virtual_router_name": strings.TrimSpace(router.Name),
			"mesh_name":           strings.TrimSpace(router.MeshName),
			"listeners":           listenerAttributes(router.Listeners),
			"status":              strings.TrimSpace(router.Status),
			"created_at":          timeOrNil(router.CreatedAt),
			"last_updated_at":     timeOrNil(router.LastUpdatedAt),
		},
		CorrelationAnchors: []string{arn, strings.TrimSpace(router.Name)},
		SourceRecordID:     arn,
	}
}

// routeObservation maps one route into an aws_resource observation. Sensitive
// HTTP header match values are redacted through the shared redact library; the
// header name and match shape are always preserved.
func (s Scanner) routeObservation(boundary awscloud.Boundary, route Route) awscloud.ResourceObservation {
	arn := strings.TrimSpace(route.ARN)
	attributes := map[string]any{
		"route_name":          strings.TrimSpace(route.Name),
		"mesh_name":           strings.TrimSpace(route.MeshName),
		"virtual_router_name": strings.TrimSpace(route.VirtualRouterName),
		"protocol_kind":       strings.TrimSpace(route.ProtocolKind),
		"path_prefix":         strings.TrimSpace(route.PathPrefix),
		"path_exact":          strings.TrimSpace(route.PathExact),
		"method":              strings.TrimSpace(route.Method),
		"header_matches":      s.headerMatchAttributes(route.HeaderMatches),
		"status":              strings.TrimSpace(route.Status),
		"created_at":          timeOrNil(route.CreatedAt),
		"last_updated_at":     timeOrNil(route.LastUpdatedAt),
	}
	if route.Priority != nil {
		attributes["priority"] = int64(*route.Priority)
	}
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ARN:                arn,
		ResourceID:         arn,
		ResourceType:       awscloud.ResourceTypeAppMeshRoute,
		Name:               strings.TrimSpace(route.Name),
		State:              strings.TrimSpace(route.Status),
		Tags:               cloneStringMap(route.Tags),
		Attributes:         attributes,
		CorrelationAnchors: []string{arn, strings.TrimSpace(route.Name)},
		SourceRecordID:     arn,
	}
}

// virtualGatewayObservation maps one virtual gateway into an aws_resource
// observation keyed by its ARN.
func virtualGatewayObservation(boundary awscloud.Boundary, gateway VirtualGateway) awscloud.ResourceObservation {
	arn := strings.TrimSpace(gateway.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   arn,
		ResourceType: awscloud.ResourceTypeAppMeshVirtualGateway,
		Name:         strings.TrimSpace(gateway.Name),
		State:        strings.TrimSpace(gateway.Status),
		Tags:         cloneStringMap(gateway.Tags),
		Attributes: map[string]any{
			"virtual_gateway_name": strings.TrimSpace(gateway.Name),
			"mesh_name":            strings.TrimSpace(gateway.MeshName),
			"listeners":            listenerAttributes(gateway.Listeners),
			"status":               strings.TrimSpace(gateway.Status),
			"created_at":           timeOrNil(gateway.CreatedAt),
			"last_updated_at":      timeOrNil(gateway.LastUpdatedAt),
		},
		CorrelationAnchors: []string{arn, strings.TrimSpace(gateway.Name)},
		SourceRecordID:     arn,
	}
}

// gatewayRouteObservation maps one gateway route into an aws_resource
// observation keyed by its ARN.
func gatewayRouteObservation(boundary awscloud.Boundary, route GatewayRoute) awscloud.ResourceObservation {
	arn := strings.TrimSpace(route.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   arn,
		ResourceType: awscloud.ResourceTypeAppMeshGatewayRoute,
		Name:         strings.TrimSpace(route.Name),
		State:        strings.TrimSpace(route.Status),
		Tags:         cloneStringMap(route.Tags),
		Attributes: map[string]any{
			"gateway_route_name":          strings.TrimSpace(route.Name),
			"mesh_name":                   strings.TrimSpace(route.MeshName),
			"virtual_gateway_name":        strings.TrimSpace(route.VirtualGatewayName),
			"protocol_kind":               strings.TrimSpace(route.ProtocolKind),
			"target_virtual_service_name": strings.TrimSpace(route.TargetVirtualServiceName),
			"status":                      strings.TrimSpace(route.Status),
			"created_at":                  timeOrNil(route.CreatedAt),
			"last_updated_at":             timeOrNil(route.LastUpdatedAt),
		},
		CorrelationAnchors: []string{arn, strings.TrimSpace(route.Name)},
		SourceRecordID:     arn,
	}
}

func listenerAttributes(listeners []Listener) []map[string]any {
	if len(listeners) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(listeners))
	for _, listener := range listeners {
		result = append(result, map[string]any{
			"port":     int64(listener.Port),
			"protocol": strings.TrimSpace(listener.Protocol),
		})
	}
	return result
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

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}
