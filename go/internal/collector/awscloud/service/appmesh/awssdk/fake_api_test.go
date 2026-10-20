// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsappmesh "github.com/aws/aws-sdk-go-v2/service/appmesh"
	appmeshtypes "github.com/aws/aws-sdk-go-v2/service/appmesh/types"
)

var errProbe = errors.New("describe probe error")

// fakeAppMeshAPI is a read-only App Mesh API double for adapter tests. List
// operations return one page (or the configured mesh pages); Describe and tag
// lookups are keyed by resource name/ARN.
type fakeAppMeshAPI struct {
	meshPages       []*awsappmesh.ListMeshesOutput
	listMeshCalls   int
	meshDescribe    map[string]*appmeshtypes.MeshData
	meshDescribeErr error

	virtualServices        map[string][]appmeshtypes.VirtualServiceRef
	virtualServiceDescribe map[string]*appmeshtypes.VirtualServiceData

	virtualNodes        map[string][]appmeshtypes.VirtualNodeRef
	virtualNodeDescribe map[string]*appmeshtypes.VirtualNodeData

	virtualRouters        map[string][]appmeshtypes.VirtualRouterRef
	virtualRouterDescribe map[string]*appmeshtypes.VirtualRouterData

	routes        map[string][]appmeshtypes.RouteRef
	routeDescribe map[string]*appmeshtypes.RouteData

	virtualGateways        map[string][]appmeshtypes.VirtualGatewayRef
	virtualGatewayDescribe map[string]*appmeshtypes.VirtualGatewayData

	gatewayRoutes        map[string][]appmeshtypes.GatewayRouteRef
	gatewayRouteDescribe map[string]*appmeshtypes.GatewayRouteData

	tags map[string][]appmeshtypes.TagRef
}

func (f *fakeAppMeshAPI) ListMeshes(_ context.Context, in *awsappmesh.ListMeshesInput, _ ...func(*awsappmesh.Options)) (*awsappmesh.ListMeshesOutput, error) {
	page := f.listMeshCalls
	f.listMeshCalls++
	if page >= len(f.meshPages) {
		return &awsappmesh.ListMeshesOutput{}, nil
	}
	_ = in
	return f.meshPages[page], nil
}

func (f *fakeAppMeshAPI) DescribeMesh(_ context.Context, in *awsappmesh.DescribeMeshInput, _ ...func(*awsappmesh.Options)) (*awsappmesh.DescribeMeshOutput, error) {
	if f.meshDescribeErr != nil {
		return nil, f.meshDescribeErr
	}
	return &awsappmesh.DescribeMeshOutput{Mesh: f.meshDescribe[aws.ToString(in.MeshName)]}, nil
}

func (f *fakeAppMeshAPI) ListVirtualServices(_ context.Context, in *awsappmesh.ListVirtualServicesInput, _ ...func(*awsappmesh.Options)) (*awsappmesh.ListVirtualServicesOutput, error) {
	return &awsappmesh.ListVirtualServicesOutput{VirtualServices: f.virtualServices[aws.ToString(in.MeshName)]}, nil
}

func (f *fakeAppMeshAPI) DescribeVirtualService(_ context.Context, in *awsappmesh.DescribeVirtualServiceInput, _ ...func(*awsappmesh.Options)) (*awsappmesh.DescribeVirtualServiceOutput, error) {
	return &awsappmesh.DescribeVirtualServiceOutput{VirtualService: f.virtualServiceDescribe[aws.ToString(in.VirtualServiceName)]}, nil
}

func (f *fakeAppMeshAPI) ListVirtualNodes(_ context.Context, in *awsappmesh.ListVirtualNodesInput, _ ...func(*awsappmesh.Options)) (*awsappmesh.ListVirtualNodesOutput, error) {
	return &awsappmesh.ListVirtualNodesOutput{VirtualNodes: f.virtualNodes[aws.ToString(in.MeshName)]}, nil
}

func (f *fakeAppMeshAPI) DescribeVirtualNode(_ context.Context, in *awsappmesh.DescribeVirtualNodeInput, _ ...func(*awsappmesh.Options)) (*awsappmesh.DescribeVirtualNodeOutput, error) {
	return &awsappmesh.DescribeVirtualNodeOutput{VirtualNode: f.virtualNodeDescribe[aws.ToString(in.VirtualNodeName)]}, nil
}

func (f *fakeAppMeshAPI) ListVirtualRouters(_ context.Context, in *awsappmesh.ListVirtualRoutersInput, _ ...func(*awsappmesh.Options)) (*awsappmesh.ListVirtualRoutersOutput, error) {
	return &awsappmesh.ListVirtualRoutersOutput{VirtualRouters: f.virtualRouters[aws.ToString(in.MeshName)]}, nil
}

func (f *fakeAppMeshAPI) DescribeVirtualRouter(_ context.Context, in *awsappmesh.DescribeVirtualRouterInput, _ ...func(*awsappmesh.Options)) (*awsappmesh.DescribeVirtualRouterOutput, error) {
	return &awsappmesh.DescribeVirtualRouterOutput{VirtualRouter: f.virtualRouterDescribe[aws.ToString(in.VirtualRouterName)]}, nil
}

func (f *fakeAppMeshAPI) ListRoutes(_ context.Context, in *awsappmesh.ListRoutesInput, _ ...func(*awsappmesh.Options)) (*awsappmesh.ListRoutesOutput, error) {
	return &awsappmesh.ListRoutesOutput{Routes: f.routes[aws.ToString(in.VirtualRouterName)]}, nil
}

func (f *fakeAppMeshAPI) DescribeRoute(_ context.Context, in *awsappmesh.DescribeRouteInput, _ ...func(*awsappmesh.Options)) (*awsappmesh.DescribeRouteOutput, error) {
	return &awsappmesh.DescribeRouteOutput{Route: f.routeDescribe[aws.ToString(in.RouteName)]}, nil
}

func (f *fakeAppMeshAPI) ListVirtualGateways(_ context.Context, in *awsappmesh.ListVirtualGatewaysInput, _ ...func(*awsappmesh.Options)) (*awsappmesh.ListVirtualGatewaysOutput, error) {
	return &awsappmesh.ListVirtualGatewaysOutput{VirtualGateways: f.virtualGateways[aws.ToString(in.MeshName)]}, nil
}

func (f *fakeAppMeshAPI) DescribeVirtualGateway(_ context.Context, in *awsappmesh.DescribeVirtualGatewayInput, _ ...func(*awsappmesh.Options)) (*awsappmesh.DescribeVirtualGatewayOutput, error) {
	return &awsappmesh.DescribeVirtualGatewayOutput{VirtualGateway: f.virtualGatewayDescribe[aws.ToString(in.VirtualGatewayName)]}, nil
}

func (f *fakeAppMeshAPI) ListGatewayRoutes(_ context.Context, in *awsappmesh.ListGatewayRoutesInput, _ ...func(*awsappmesh.Options)) (*awsappmesh.ListGatewayRoutesOutput, error) {
	return &awsappmesh.ListGatewayRoutesOutput{GatewayRoutes: f.gatewayRoutes[aws.ToString(in.VirtualGatewayName)]}, nil
}

func (f *fakeAppMeshAPI) DescribeGatewayRoute(_ context.Context, in *awsappmesh.DescribeGatewayRouteInput, _ ...func(*awsappmesh.Options)) (*awsappmesh.DescribeGatewayRouteOutput, error) {
	return &awsappmesh.DescribeGatewayRouteOutput{GatewayRoute: f.gatewayRouteDescribe[aws.ToString(in.GatewayRouteName)]}, nil
}

func (f *fakeAppMeshAPI) ListTagsForResource(_ context.Context, in *awsappmesh.ListTagsForResourceInput, _ ...func(*awsappmesh.Options)) (*awsappmesh.ListTagsForResourceOutput, error) {
	return &awsappmesh.ListTagsForResourceOutput{Tags: f.tags[aws.ToString(in.ResourceArn)]}, nil
}
