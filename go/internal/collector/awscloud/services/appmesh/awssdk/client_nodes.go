// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsappmesh "github.com/aws/aws-sdk-go-v2/service/appmesh"
	appmeshtypes "github.com/aws/aws-sdk-go-v2/service/appmesh/types"

	appmeshservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/appmesh"
)

func (c *Client) virtualNodes(ctx context.Context, meshName string) ([]appmeshservice.VirtualNode, error) {
	var refs []appmeshtypes.VirtualNodeRef
	var nextToken *string
	for {
		var page *awsappmesh.ListVirtualNodesOutput
		err := c.recordAPICall(ctx, "ListVirtualNodes", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListVirtualNodes(callCtx, &awsappmesh.ListVirtualNodesInput{
				MeshName:  aws.String(meshName),
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, fmt.Errorf("list App Mesh virtual nodes for mesh %q: %w", meshName, err)
		}
		if page == nil {
			break
		}
		refs = append(refs, page.VirtualNodes...)
		if aws.ToString(page.NextToken) == "" {
			break
		}
		nextToken = page.NextToken
	}

	nodes := make([]appmeshservice.VirtualNode, 0, len(refs))
	for _, ref := range refs {
		name := strings.TrimSpace(aws.ToString(ref.VirtualNodeName))
		if name == "" {
			continue
		}
		detail, err := c.describeVirtualNode(ctx, meshName, name)
		if err != nil {
			return nil, err
		}
		node := mapVirtualNode(meshName, name, detail)
		node.Tags = c.resourceTagsOrEmpty(ctx, node.ARN)
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func (c *Client) describeVirtualNode(ctx context.Context, meshName, name string) (*appmeshtypes.VirtualNodeData, error) {
	var output *awsappmesh.DescribeVirtualNodeOutput
	err := c.recordAPICall(ctx, "DescribeVirtualNode", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.DescribeVirtualNode(callCtx, &awsappmesh.DescribeVirtualNodeInput{
			MeshName:        aws.String(meshName),
			VirtualNodeName: aws.String(name),
		})
		return callErr
	})
	if err != nil {
		return nil, fmt.Errorf("describe App Mesh virtual node %q: %w", name, err)
	}
	if output == nil {
		return nil, nil
	}
	return output.VirtualNode, nil
}

func (c *Client) virtualRouters(ctx context.Context, meshName string) ([]appmeshservice.VirtualRouter, error) {
	var refs []appmeshtypes.VirtualRouterRef
	var nextToken *string
	for {
		var page *awsappmesh.ListVirtualRoutersOutput
		err := c.recordAPICall(ctx, "ListVirtualRouters", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListVirtualRouters(callCtx, &awsappmesh.ListVirtualRoutersInput{
				MeshName:  aws.String(meshName),
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, fmt.Errorf("list App Mesh virtual routers for mesh %q: %w", meshName, err)
		}
		if page == nil {
			break
		}
		refs = append(refs, page.VirtualRouters...)
		if aws.ToString(page.NextToken) == "" {
			break
		}
		nextToken = page.NextToken
	}

	routers := make([]appmeshservice.VirtualRouter, 0, len(refs))
	for _, ref := range refs {
		name := strings.TrimSpace(aws.ToString(ref.VirtualRouterName))
		if name == "" {
			continue
		}
		detail, err := c.describeVirtualRouter(ctx, meshName, name)
		if err != nil {
			return nil, err
		}
		router := mapVirtualRouter(meshName, name, detail)
		router.Tags = c.resourceTagsOrEmpty(ctx, router.ARN)
		if router.Routes, err = c.routes(ctx, meshName, name, router.ARN); err != nil {
			return nil, err
		}
		routers = append(routers, router)
	}
	return routers, nil
}

func (c *Client) describeVirtualRouter(ctx context.Context, meshName, name string) (*appmeshtypes.VirtualRouterData, error) {
	var output *awsappmesh.DescribeVirtualRouterOutput
	err := c.recordAPICall(ctx, "DescribeVirtualRouter", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.DescribeVirtualRouter(callCtx, &awsappmesh.DescribeVirtualRouterInput{
			MeshName:          aws.String(meshName),
			VirtualRouterName: aws.String(name),
		})
		return callErr
	})
	if err != nil {
		return nil, fmt.Errorf("describe App Mesh virtual router %q: %w", name, err)
	}
	if output == nil {
		return nil, nil
	}
	return output.VirtualRouter, nil
}

func (c *Client) routes(ctx context.Context, meshName, routerName, routerARN string) ([]appmeshservice.Route, error) {
	var refs []appmeshtypes.RouteRef
	var nextToken *string
	for {
		var page *awsappmesh.ListRoutesOutput
		err := c.recordAPICall(ctx, "ListRoutes", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListRoutes(callCtx, &awsappmesh.ListRoutesInput{
				MeshName:          aws.String(meshName),
				VirtualRouterName: aws.String(routerName),
				NextToken:         nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, fmt.Errorf("list App Mesh routes for virtual router %q: %w", routerName, err)
		}
		if page == nil {
			break
		}
		refs = append(refs, page.Routes...)
		if aws.ToString(page.NextToken) == "" {
			break
		}
		nextToken = page.NextToken
	}

	routes := make([]appmeshservice.Route, 0, len(refs))
	for _, ref := range refs {
		name := strings.TrimSpace(aws.ToString(ref.RouteName))
		if name == "" {
			continue
		}
		detail, err := c.describeRoute(ctx, meshName, routerName, name)
		if err != nil {
			return nil, err
		}
		route := mapRoute(meshName, routerName, routerARN, name, detail)
		route.Tags = c.resourceTagsOrEmpty(ctx, route.ARN)
		routes = append(routes, route)
	}
	return routes, nil
}

func (c *Client) describeRoute(ctx context.Context, meshName, routerName, name string) (*appmeshtypes.RouteData, error) {
	var output *awsappmesh.DescribeRouteOutput
	err := c.recordAPICall(ctx, "DescribeRoute", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.DescribeRoute(callCtx, &awsappmesh.DescribeRouteInput{
			MeshName:          aws.String(meshName),
			VirtualRouterName: aws.String(routerName),
			RouteName:         aws.String(name),
		})
		return callErr
	})
	if err != nil {
		return nil, fmt.Errorf("describe App Mesh route %q: %w", name, err)
	}
	if output == nil {
		return nil, nil
	}
	return output.Route, nil
}
