// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"reflect"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsappmesh "github.com/aws/aws-sdk-go-v2/service/appmesh"
	appmeshtypes "github.com/aws/aws-sdk-go-v2/service/appmesh/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	appmeshservice "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/appmesh"
)

// TestAPIClientInterfaceExcludesAllMutationAPIs is the security gate for the
// App Mesh SDK adapter. The scanner contract forbids every Create/Update/Delete
// mutation API. This test reflects over the adapter's internal apiClient
// interface and FAILS if a future SDK refactor adds any of them.
func TestAPIClientInterfaceExcludesAllMutationAPIs(t *testing.T) {
	apiClientType := reflect.TypeOf((*apiClient)(nil)).Elem()
	resources := []string{"Mesh", "VirtualService", "VirtualNode", "VirtualRouter", "Route", "VirtualGateway", "GatewayRoute"}
	verbs := []string{"Create", "Update", "Delete"}
	for _, verb := range verbs {
		for _, resource := range resources {
			name := verb + resource
			if _, ok := apiClientType.MethodByName(name); ok {
				t.Fatalf("apiClient interface exposes %q; App Mesh scanner forbids all mutation APIs", name)
			}
		}
	}
}

func meshOwner() *string { return awsv2.String("123456789012") }

func fakeAPIWithFullMesh() *fakeAppMeshAPI {
	return &fakeAppMeshAPI{
		meshPages: []*awsappmesh.ListMeshesOutput{{
			Meshes: []appmeshtypes.MeshRef{{
				Arn:      awsv2.String("arn:aws:appmesh:us-east-1:123456789012:mesh/checkout"),
				MeshName: awsv2.String("checkout"),
			}},
		}},
		meshDescribe: map[string]*appmeshtypes.MeshData{
			"checkout": {
				MeshName: awsv2.String("checkout"),
				Metadata: &appmeshtypes.ResourceMetadata{
					Arn:           awsv2.String("arn:aws:appmesh:us-east-1:123456789012:mesh/checkout"),
					MeshOwner:     meshOwner(),
					ResourceOwner: meshOwner(),
				},
				Spec: &appmeshtypes.MeshSpec{
					EgressFilter: &appmeshtypes.EgressFilter{Type: appmeshtypes.EgressFilterTypeDropAll},
				},
				Status: &appmeshtypes.MeshStatus{Status: appmeshtypes.MeshStatusCodeActive},
			},
		},
		virtualServices: map[string][]appmeshtypes.VirtualServiceRef{
			"checkout": {{
				Arn:                awsv2.String("arn:aws:appmesh:us-east-1:123456789012:mesh/checkout/virtualService/checkout.apps.local"),
				VirtualServiceName: awsv2.String("checkout.apps.local"),
			}},
		},
		virtualServiceDescribe: map[string]*appmeshtypes.VirtualServiceData{
			"checkout.apps.local": {
				VirtualServiceName: awsv2.String("checkout.apps.local"),
				MeshName:           awsv2.String("checkout"),
				Metadata:           &appmeshtypes.ResourceMetadata{Arn: awsv2.String("arn:aws:appmesh:us-east-1:123456789012:mesh/checkout/virtualService/checkout.apps.local")},
				Spec: &appmeshtypes.VirtualServiceSpec{
					Provider: &appmeshtypes.VirtualServiceProviderMemberVirtualRouter{
						Value: appmeshtypes.VirtualRouterServiceProvider{VirtualRouterName: awsv2.String("checkout-router")},
					},
				},
				Status: &appmeshtypes.VirtualServiceStatus{Status: appmeshtypes.VirtualServiceStatusCodeActive},
			},
		},
		virtualNodes: map[string][]appmeshtypes.VirtualNodeRef{
			"checkout": {{
				Arn:             awsv2.String("arn:aws:appmesh:us-east-1:123456789012:mesh/checkout/virtualNode/checkout-node"),
				VirtualNodeName: awsv2.String("checkout-node"),
			}},
		},
		virtualNodeDescribe: map[string]*appmeshtypes.VirtualNodeData{
			"checkout-node": {
				VirtualNodeName: awsv2.String("checkout-node"),
				MeshName:        awsv2.String("checkout"),
				Metadata:        &appmeshtypes.ResourceMetadata{Arn: awsv2.String("arn:aws:appmesh:us-east-1:123456789012:mesh/checkout/virtualNode/checkout-node")},
				Spec: &appmeshtypes.VirtualNodeSpec{
					ServiceDiscovery: &appmeshtypes.ServiceDiscoveryMemberAwsCloudMap{
						Value: appmeshtypes.AwsCloudMapServiceDiscovery{
							NamespaceName: awsv2.String("apps.local"),
							ServiceName:   awsv2.String("checkout"),
						},
					},
					Backends: []appmeshtypes.Backend{
						&appmeshtypes.BackendMemberVirtualService{
							Value: appmeshtypes.VirtualServiceBackend{
								VirtualServiceName: awsv2.String("payments.apps.local"),
								ClientPolicy: &appmeshtypes.ClientPolicy{
									Tls: &appmeshtypes.ClientPolicyTls{
										Validation: &appmeshtypes.TlsValidationContext{
											Trust: &appmeshtypes.TlsValidationContextTrustMemberAcm{
												Value: appmeshtypes.TlsValidationContextAcmTrust{
													CertificateAuthorityArns: []string{"arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/abc"},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				Status: &appmeshtypes.VirtualNodeStatus{Status: appmeshtypes.VirtualNodeStatusCodeActive},
			},
		},
		virtualRouters: map[string][]appmeshtypes.VirtualRouterRef{
			"checkout": {{
				Arn:               awsv2.String("arn:aws:appmesh:us-east-1:123456789012:mesh/checkout/virtualRouter/checkout-router"),
				VirtualRouterName: awsv2.String("checkout-router"),
			}},
		},
		virtualRouterDescribe: map[string]*appmeshtypes.VirtualRouterData{
			"checkout-router": {
				VirtualRouterName: awsv2.String("checkout-router"),
				MeshName:          awsv2.String("checkout"),
				Metadata:          &appmeshtypes.ResourceMetadata{Arn: awsv2.String("arn:aws:appmesh:us-east-1:123456789012:mesh/checkout/virtualRouter/checkout-router")},
				Spec: &appmeshtypes.VirtualRouterSpec{
					Listeners: []appmeshtypes.VirtualRouterListener{{
						PortMapping: &appmeshtypes.PortMapping{Port: awsv2.Int32(8080), Protocol: appmeshtypes.PortProtocolHttp},
					}},
				},
				Status: &appmeshtypes.VirtualRouterStatus{Status: appmeshtypes.VirtualRouterStatusCodeActive},
			},
		},
		routes: map[string][]appmeshtypes.RouteRef{
			"checkout-router": {{
				Arn:               awsv2.String("arn:aws:appmesh:us-east-1:123456789012:mesh/checkout/virtualRouter/checkout-router/route/checkout-route"),
				RouteName:         awsv2.String("checkout-route"),
				VirtualRouterName: awsv2.String("checkout-router"),
			}},
		},
		routeDescribe: map[string]*appmeshtypes.RouteData{
			"checkout-route": {
				RouteName:         awsv2.String("checkout-route"),
				MeshName:          awsv2.String("checkout"),
				VirtualRouterName: awsv2.String("checkout-router"),
				Metadata:          &appmeshtypes.ResourceMetadata{Arn: awsv2.String("arn:aws:appmesh:us-east-1:123456789012:mesh/checkout/virtualRouter/checkout-router/route/checkout-route")},
				Spec: &appmeshtypes.RouteSpec{
					HttpRoute: &appmeshtypes.HttpRoute{
						Match: &appmeshtypes.HttpRouteMatch{
							Prefix: awsv2.String("/checkout"),
							Method: appmeshtypes.HttpMethodPost,
							Headers: []appmeshtypes.HttpRouteHeader{
								{Name: awsv2.String("x-tenant"), Match: &appmeshtypes.HeaderMatchMethodMemberExact{Value: "acme"}},
								{Name: awsv2.String("Authorization"), Match: &appmeshtypes.HeaderMatchMethodMemberExact{Value: "Bearer secret"}},
							},
						},
					},
				},
				Status: &appmeshtypes.RouteStatus{Status: appmeshtypes.RouteStatusCodeActive},
			},
		},
		virtualGateways: map[string][]appmeshtypes.VirtualGatewayRef{
			"checkout": {{
				Arn:                awsv2.String("arn:aws:appmesh:us-east-1:123456789012:mesh/checkout/virtualGateway/edge"),
				VirtualGatewayName: awsv2.String("edge"),
			}},
		},
		virtualGatewayDescribe: map[string]*appmeshtypes.VirtualGatewayData{
			"edge": {
				VirtualGatewayName: awsv2.String("edge"),
				MeshName:           awsv2.String("checkout"),
				Metadata:           &appmeshtypes.ResourceMetadata{Arn: awsv2.String("arn:aws:appmesh:us-east-1:123456789012:mesh/checkout/virtualGateway/edge")},
				Spec: &appmeshtypes.VirtualGatewaySpec{
					Listeners: []appmeshtypes.VirtualGatewayListener{{
						PortMapping: &appmeshtypes.VirtualGatewayPortMapping{Port: awsv2.Int32(443), Protocol: appmeshtypes.VirtualGatewayPortProtocolHttp2},
					}},
				},
				Status: &appmeshtypes.VirtualGatewayStatus{Status: appmeshtypes.VirtualGatewayStatusCodeActive},
			},
		},
		gatewayRoutes: map[string][]appmeshtypes.GatewayRouteRef{
			"edge": {{
				Arn:                awsv2.String("arn:aws:appmesh:us-east-1:123456789012:mesh/checkout/virtualGateway/edge/gatewayRoute/edge-route"),
				GatewayRouteName:   awsv2.String("edge-route"),
				VirtualGatewayName: awsv2.String("edge"),
			}},
		},
		gatewayRouteDescribe: map[string]*appmeshtypes.GatewayRouteData{
			"edge-route": {
				GatewayRouteName:   awsv2.String("edge-route"),
				MeshName:           awsv2.String("checkout"),
				VirtualGatewayName: awsv2.String("edge"),
				Metadata:           &appmeshtypes.ResourceMetadata{Arn: awsv2.String("arn:aws:appmesh:us-east-1:123456789012:mesh/checkout/virtualGateway/edge/gatewayRoute/edge-route")},
				Spec: &appmeshtypes.GatewayRouteSpec{
					HttpRoute: &appmeshtypes.HttpGatewayRoute{
						Action: &appmeshtypes.HttpGatewayRouteAction{
							Target: &appmeshtypes.GatewayRouteTarget{
								VirtualService: &appmeshtypes.GatewayRouteVirtualService{VirtualServiceName: awsv2.String("checkout.apps.local")},
							},
						},
					},
				},
				Status: &appmeshtypes.GatewayRouteStatus{Status: appmeshtypes.GatewayRouteStatusCodeActive},
			},
		},
		tags: map[string][]appmeshtypes.TagRef{
			"arn:aws:appmesh:us-east-1:123456789012:mesh/checkout": {{Key: awsv2.String("Environment"), Value: awsv2.String("prod")}},
		},
	}
}

func newTestAdapter(api *fakeAppMeshAPI) *Client {
	return &Client{
		client:   api,
		boundary: aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceAppMesh},
	}
}

func TestClientListMeshInventoryResolvesEveryResourceKind(t *testing.T) {
	api := fakeAPIWithFullMesh()
	meshes, err := newTestAdapter(api).ListMeshInventory(context.Background())
	if err != nil {
		t.Fatalf("ListMeshInventory() error = %v, want nil", err)
	}
	if len(meshes) != 1 {
		t.Fatalf("len(meshes) = %d, want 1", len(meshes))
	}
	mesh := meshes[0]
	if mesh.Name != "checkout" || mesh.EgressFilterType != string(appmeshtypes.EgressFilterTypeDropAll) {
		t.Fatalf("mesh = %+v", mesh)
	}
	if mesh.Tags["Environment"] != "prod" {
		t.Fatalf("mesh tags = %#v", mesh.Tags)
	}
	if len(mesh.VirtualServices) != 1 || mesh.VirtualServices[0].ProviderKind != "virtual_router" || mesh.VirtualServices[0].ProviderName != "checkout-router" {
		t.Fatalf("virtual services = %+v", mesh.VirtualServices)
	}
	if len(mesh.VirtualNodes) != 1 {
		t.Fatalf("virtual nodes = %+v", mesh.VirtualNodes)
	}
	node := mesh.VirtualNodes[0]
	if node.ServiceDiscoveryKind != "aws_cloud_map" || node.CloudMapNamespaceName != "apps.local" || node.CloudMapServiceName != "checkout" {
		t.Fatalf("node service discovery = %+v", node)
	}
	if len(node.BackendVirtualServiceNames) != 1 || node.BackendVirtualServiceNames[0] != "payments.apps.local" {
		t.Fatalf("node backends = %#v", node.BackendVirtualServiceNames)
	}
	if len(node.ClientTLSCertificateAuthorityARNs) != 1 || node.ClientTLSCertificateAuthorityARNs[0] != "arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/abc" {
		t.Fatalf("node CA arns = %#v", node.ClientTLSCertificateAuthorityARNs)
	}
	if len(mesh.VirtualRouters) != 1 || len(mesh.VirtualRouters[0].Routes) != 1 {
		t.Fatalf("routers/routes = %+v", mesh.VirtualRouters)
	}
	route := mesh.VirtualRouters[0].Routes[0]
	if route.ProtocolKind != "http" || route.PathPrefix != "/checkout" || route.Method != string(appmeshtypes.HttpMethodPost) {
		t.Fatalf("route = %+v", route)
	}
	if route.VirtualRouterARN != "arn:aws:appmesh:us-east-1:123456789012:mesh/checkout/virtualRouter/checkout-router" {
		t.Fatalf("route router ARN = %q", route.VirtualRouterARN)
	}
	if len(route.HeaderMatches) != 2 {
		t.Fatalf("route header matches = %+v", route.HeaderMatches)
	}
	if len(mesh.VirtualGateways) != 1 || len(mesh.VirtualGateways[0].GatewayRoutes) != 1 {
		t.Fatalf("gateways = %+v", mesh.VirtualGateways)
	}
	if mesh.VirtualGateways[0].GatewayRoutes[0].TargetVirtualServiceName != "checkout.apps.local" {
		t.Fatalf("gateway route target = %q", mesh.VirtualGateways[0].GatewayRoutes[0].TargetVirtualServiceName)
	}
}

// TestClientPreservesHeaderMatchValuesForScannerRedaction confirms the adapter
// passes the literal header match value through to the scanner. Redaction is
// the scanner's responsibility (it holds the redaction key); the adapter must
// not silently drop the value or the scanner cannot make the redaction
// decision.
func TestClientPreservesHeaderMatchValuesForScannerRedaction(t *testing.T) {
	api := fakeAPIWithFullMesh()
	meshes, err := newTestAdapter(api).ListMeshInventory(context.Background())
	if err != nil {
		t.Fatalf("ListMeshInventory() error = %v", err)
	}
	headers := meshes[0].VirtualRouters[0].Routes[0].HeaderMatches
	byName := map[string]appmeshservice.HeaderMatch{}
	for _, h := range headers {
		byName[h.Name] = h
	}
	if byName["x-tenant"].Value != "acme" {
		t.Fatalf("x-tenant value = %q, want acme", byName["x-tenant"].Value)
	}
	if byName["Authorization"].Value != "Bearer secret" {
		t.Fatalf("Authorization value = %q, want raw value preserved for scanner redaction", byName["Authorization"].Value)
	}
}

func TestClientListMeshesPaginates(t *testing.T) {
	api := fakeAPIWithFullMesh()
	api.meshPages = []*awsappmesh.ListMeshesOutput{
		{
			Meshes:    []appmeshtypes.MeshRef{{Arn: awsv2.String("arn:aws:appmesh:us-east-1:123456789012:mesh/a"), MeshName: awsv2.String("a")}},
			NextToken: awsv2.String("token-1"),
		},
		{
			Meshes: []appmeshtypes.MeshRef{{Arn: awsv2.String("arn:aws:appmesh:us-east-1:123456789012:mesh/checkout"), MeshName: awsv2.String("checkout")}},
		},
	}
	api.meshDescribe["a"] = &appmeshtypes.MeshData{
		MeshName: awsv2.String("a"),
		Metadata: &appmeshtypes.ResourceMetadata{Arn: awsv2.String("arn:aws:appmesh:us-east-1:123456789012:mesh/a")},
		Spec:     &appmeshtypes.MeshSpec{},
		Status:   &appmeshtypes.MeshStatus{Status: appmeshtypes.MeshStatusCodeActive},
	}

	meshes, err := newTestAdapter(api).ListMeshInventory(context.Background())
	if err != nil {
		t.Fatalf("ListMeshInventory() error = %v", err)
	}
	if len(meshes) != 2 {
		t.Fatalf("len(meshes) = %d, want 2", len(meshes))
	}
	if api.listMeshCalls != 2 {
		t.Fatalf("ListMeshes calls = %d, want 2", api.listMeshCalls)
	}
}

func TestClientSurfacesDescribeError(t *testing.T) {
	api := fakeAPIWithFullMesh()
	delete(api.meshDescribe, "checkout")
	api.meshDescribeErr = errProbe
	_, err := newTestAdapter(api).ListMeshInventory(context.Background())
	if err == nil {
		t.Fatalf("ListMeshInventory() error = nil, want wrapped describe error")
	}
}

func TestClientCompileTimeAssertions(t *testing.T) {
	var _ appmeshservice.Client = (*Client)(nil)
}
