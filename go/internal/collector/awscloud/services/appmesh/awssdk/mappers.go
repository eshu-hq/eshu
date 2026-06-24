// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	appmeshtypes "github.com/aws/aws-sdk-go-v2/service/appmesh/types"

	appmeshservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/appmesh"
)

func mapMesh(meshName string, detail *appmeshtypes.MeshData) appmeshservice.Mesh {
	mesh := appmeshservice.Mesh{Name: meshName}
	if detail == nil {
		return mesh
	}
	if metadata := detail.Metadata; metadata != nil {
		mesh.ARN = strings.TrimSpace(aws.ToString(metadata.Arn))
		mesh.MeshOwner = strings.TrimSpace(aws.ToString(metadata.MeshOwner))
		mesh.ResourceOwner = strings.TrimSpace(aws.ToString(metadata.ResourceOwner))
		mesh.CreatedAt = timeOrZero(metadata.CreatedAt)
		mesh.LastUpdatedAt = timeOrZero(metadata.LastUpdatedAt)
	}
	if spec := detail.Spec; spec != nil {
		if spec.EgressFilter != nil {
			mesh.EgressFilterType = string(spec.EgressFilter.Type)
		}
		if spec.ServiceDiscovery != nil {
			mesh.IPPreference = string(spec.ServiceDiscovery.IpPreference)
		}
	}
	if detail.Status != nil {
		mesh.Status = string(detail.Status.Status)
	}
	return mesh
}

func mapVirtualService(meshName, name string, detail *appmeshtypes.VirtualServiceData) appmeshservice.VirtualService {
	service := appmeshservice.VirtualService{Name: name, MeshName: meshName}
	if detail == nil {
		return service
	}
	if metadata := detail.Metadata; metadata != nil {
		service.ARN = strings.TrimSpace(aws.ToString(metadata.Arn))
		service.CreatedAt = timeOrZero(metadata.CreatedAt)
		service.LastUpdatedAt = timeOrZero(metadata.LastUpdatedAt)
	}
	if detail.Spec != nil {
		service.ProviderKind, service.ProviderName = providerKindAndName(detail.Spec.Provider)
	}
	if detail.Status != nil {
		service.Status = string(detail.Status.Status)
	}
	return service
}

func providerKindAndName(provider appmeshtypes.VirtualServiceProvider) (kind, name string) {
	switch typed := provider.(type) {
	case *appmeshtypes.VirtualServiceProviderMemberVirtualNode:
		return "virtual_node", strings.TrimSpace(aws.ToString(typed.Value.VirtualNodeName))
	case *appmeshtypes.VirtualServiceProviderMemberVirtualRouter:
		return "virtual_router", strings.TrimSpace(aws.ToString(typed.Value.VirtualRouterName))
	default:
		return "", ""
	}
}

func mapVirtualNode(meshName, name string, detail *appmeshtypes.VirtualNodeData) appmeshservice.VirtualNode {
	node := appmeshservice.VirtualNode{Name: name, MeshName: meshName}
	if detail == nil {
		return node
	}
	if metadata := detail.Metadata; metadata != nil {
		node.ARN = strings.TrimSpace(aws.ToString(metadata.Arn))
		node.CreatedAt = timeOrZero(metadata.CreatedAt)
		node.LastUpdatedAt = timeOrZero(metadata.LastUpdatedAt)
	}
	if spec := detail.Spec; spec != nil {
		applyServiceDiscovery(&node, spec.ServiceDiscovery)
		node.BackendVirtualServiceNames = backendVirtualServiceNames(spec.Backends)
		node.ClientTLSCertificateAuthorityARNs = clientTLSCertificateAuthorityARNs(spec)
	}
	if detail.Status != nil {
		node.Status = string(detail.Status.Status)
	}
	return node
}

func applyServiceDiscovery(node *appmeshservice.VirtualNode, discovery appmeshtypes.ServiceDiscovery) {
	switch typed := discovery.(type) {
	case *appmeshtypes.ServiceDiscoveryMemberDns:
		node.ServiceDiscoveryKind = "dns"
		node.DNSHostname = strings.TrimSpace(aws.ToString(typed.Value.Hostname))
	case *appmeshtypes.ServiceDiscoveryMemberAwsCloudMap:
		node.ServiceDiscoveryKind = "aws_cloud_map"
		node.CloudMapNamespaceName = strings.TrimSpace(aws.ToString(typed.Value.NamespaceName))
		node.CloudMapServiceName = strings.TrimSpace(aws.ToString(typed.Value.ServiceName))
	}
}

func backendVirtualServiceNames(backends []appmeshtypes.Backend) []string {
	var names []string
	for _, backend := range backends {
		typed, ok := backend.(*appmeshtypes.BackendMemberVirtualService)
		if !ok {
			continue
		}
		if name := strings.TrimSpace(aws.ToString(typed.Value.VirtualServiceName)); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// clientTLSCertificateAuthorityARNs extracts only the ACM Private CA (acm-pca)
// certificate authority ARNs from every client TLS validation trust the node
// references (backend client policies and backend defaults). File and SDS trust
// shapes carry certificate chains and secret names, which are intentionally NOT
// read: the adapter never returns a literal certificate body. Only ARN
// references survive.
func clientTLSCertificateAuthorityARNs(spec *appmeshtypes.VirtualNodeSpec) []string {
	var arns []string
	collect := func(policy *appmeshtypes.ClientPolicy) {
		arns = append(arns, acmTrustARNs(policy)...)
	}
	if spec.BackendDefaults != nil {
		collect(spec.BackendDefaults.ClientPolicy)
	}
	for _, backend := range spec.Backends {
		typed, ok := backend.(*appmeshtypes.BackendMemberVirtualService)
		if !ok {
			continue
		}
		collect(typed.Value.ClientPolicy)
	}
	return dedupe(arns)
}

func acmTrustARNs(policy *appmeshtypes.ClientPolicy) []string {
	if policy == nil || policy.Tls == nil || policy.Tls.Validation == nil {
		return nil
	}
	acm, ok := policy.Tls.Validation.Trust.(*appmeshtypes.TlsValidationContextTrustMemberAcm)
	if !ok {
		return nil
	}
	var arns []string
	for _, arn := range acm.Value.CertificateAuthorityArns {
		if trimmed := strings.TrimSpace(arn); trimmed != "" {
			arns = append(arns, trimmed)
		}
	}
	return arns
}

func mapVirtualRouter(meshName, name string, detail *appmeshtypes.VirtualRouterData) appmeshservice.VirtualRouter {
	router := appmeshservice.VirtualRouter{Name: name, MeshName: meshName}
	if detail == nil {
		return router
	}
	if metadata := detail.Metadata; metadata != nil {
		router.ARN = strings.TrimSpace(aws.ToString(metadata.Arn))
		router.CreatedAt = timeOrZero(metadata.CreatedAt)
		router.LastUpdatedAt = timeOrZero(metadata.LastUpdatedAt)
	}
	if detail.Spec != nil {
		for _, listener := range detail.Spec.Listeners {
			if listener.PortMapping == nil {
				continue
			}
			router.Listeners = append(router.Listeners, appmeshservice.Listener{
				Port:     aws.ToInt32(listener.PortMapping.Port),
				Protocol: string(listener.PortMapping.Protocol),
			})
		}
	}
	if detail.Status != nil {
		router.Status = string(detail.Status.Status)
	}
	return router
}

func mapVirtualGateway(meshName, name string, detail *appmeshtypes.VirtualGatewayData) appmeshservice.VirtualGateway {
	gateway := appmeshservice.VirtualGateway{Name: name, MeshName: meshName}
	if detail == nil {
		return gateway
	}
	if metadata := detail.Metadata; metadata != nil {
		gateway.ARN = strings.TrimSpace(aws.ToString(metadata.Arn))
		gateway.CreatedAt = timeOrZero(metadata.CreatedAt)
		gateway.LastUpdatedAt = timeOrZero(metadata.LastUpdatedAt)
	}
	if detail.Spec != nil {
		for _, listener := range detail.Spec.Listeners {
			if listener.PortMapping == nil {
				continue
			}
			gateway.Listeners = append(gateway.Listeners, appmeshservice.Listener{
				Port:     aws.ToInt32(listener.PortMapping.Port),
				Protocol: string(listener.PortMapping.Protocol),
			})
		}
	}
	if detail.Status != nil {
		gateway.Status = string(detail.Status.Status)
	}
	return gateway
}

func tagsToMap(tags []appmeshtypes.TagRef) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = aws.ToString(tag.Value)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func dedupe(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	output := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		output = append(output, value)
	}
	return output
}
