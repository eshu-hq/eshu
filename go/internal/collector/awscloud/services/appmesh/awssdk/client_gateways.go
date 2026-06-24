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

func (c *Client) virtualGateways(ctx context.Context, meshName string) ([]appmeshservice.VirtualGateway, error) {
	var refs []appmeshtypes.VirtualGatewayRef
	var nextToken *string
	for {
		var page *awsappmesh.ListVirtualGatewaysOutput
		err := c.recordAPICall(ctx, "ListVirtualGateways", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListVirtualGateways(callCtx, &awsappmesh.ListVirtualGatewaysInput{
				MeshName:  aws.String(meshName),
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, fmt.Errorf("list App Mesh virtual gateways for mesh %q: %w", meshName, err)
		}
		if page == nil {
			break
		}
		refs = append(refs, page.VirtualGateways...)
		if aws.ToString(page.NextToken) == "" {
			break
		}
		nextToken = page.NextToken
	}

	gateways := make([]appmeshservice.VirtualGateway, 0, len(refs))
	for _, ref := range refs {
		name := strings.TrimSpace(aws.ToString(ref.VirtualGatewayName))
		if name == "" {
			continue
		}
		detail, err := c.describeVirtualGateway(ctx, meshName, name)
		if err != nil {
			return nil, err
		}
		gateway := mapVirtualGateway(meshName, name, detail)
		gateway.Tags = c.resourceTagsOrEmpty(ctx, gateway.ARN)
		if gateway.GatewayRoutes, err = c.gatewayRoutes(ctx, meshName, name); err != nil {
			return nil, err
		}
		gateways = append(gateways, gateway)
	}
	return gateways, nil
}

func (c *Client) describeVirtualGateway(ctx context.Context, meshName, name string) (*appmeshtypes.VirtualGatewayData, error) {
	var output *awsappmesh.DescribeVirtualGatewayOutput
	err := c.recordAPICall(ctx, "DescribeVirtualGateway", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.DescribeVirtualGateway(callCtx, &awsappmesh.DescribeVirtualGatewayInput{
			MeshName:           aws.String(meshName),
			VirtualGatewayName: aws.String(name),
		})
		return callErr
	})
	if err != nil {
		return nil, fmt.Errorf("describe App Mesh virtual gateway %q: %w", name, err)
	}
	if output == nil {
		return nil, nil
	}
	return output.VirtualGateway, nil
}

func (c *Client) gatewayRoutes(ctx context.Context, meshName, gatewayName string) ([]appmeshservice.GatewayRoute, error) {
	var refs []appmeshtypes.GatewayRouteRef
	var nextToken *string
	for {
		var page *awsappmesh.ListGatewayRoutesOutput
		err := c.recordAPICall(ctx, "ListGatewayRoutes", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListGatewayRoutes(callCtx, &awsappmesh.ListGatewayRoutesInput{
				MeshName:           aws.String(meshName),
				VirtualGatewayName: aws.String(gatewayName),
				NextToken:          nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, fmt.Errorf("list App Mesh gateway routes for virtual gateway %q: %w", gatewayName, err)
		}
		if page == nil {
			break
		}
		refs = append(refs, page.GatewayRoutes...)
		if aws.ToString(page.NextToken) == "" {
			break
		}
		nextToken = page.NextToken
	}

	routes := make([]appmeshservice.GatewayRoute, 0, len(refs))
	for _, ref := range refs {
		name := strings.TrimSpace(aws.ToString(ref.GatewayRouteName))
		if name == "" {
			continue
		}
		detail, err := c.describeGatewayRoute(ctx, meshName, gatewayName, name)
		if err != nil {
			return nil, err
		}
		route := mapGatewayRoute(meshName, gatewayName, name, detail)
		route.Tags = c.resourceTagsOrEmpty(ctx, route.ARN)
		routes = append(routes, route)
	}
	return routes, nil
}

func (c *Client) describeGatewayRoute(ctx context.Context, meshName, gatewayName, name string) (*appmeshtypes.GatewayRouteData, error) {
	var output *awsappmesh.DescribeGatewayRouteOutput
	err := c.recordAPICall(ctx, "DescribeGatewayRoute", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.DescribeGatewayRoute(callCtx, &awsappmesh.DescribeGatewayRouteInput{
			MeshName:           aws.String(meshName),
			VirtualGatewayName: aws.String(gatewayName),
			GatewayRouteName:   aws.String(name),
		})
		return callErr
	})
	if err != nil {
		return nil, fmt.Errorf("describe App Mesh gateway route %q: %w", name, err)
	}
	if output == nil {
		return nil, nil
	}
	return output.GatewayRoute, nil
}
