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
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	appmeshservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/appmesh"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the subset of the AWS SDK App Mesh client this adapter consumes.
// It is read-only: List and Describe operations plus ListTagsForResource. The
// interface deliberately excludes every Create/Update/Delete mutation API. A
// reflection test asserts the exclusion so a future SDK refactor cannot quietly
// broaden the contract.
type apiClient interface {
	ListMeshes(context.Context, *awsappmesh.ListMeshesInput, ...func(*awsappmesh.Options)) (*awsappmesh.ListMeshesOutput, error)
	DescribeMesh(context.Context, *awsappmesh.DescribeMeshInput, ...func(*awsappmesh.Options)) (*awsappmesh.DescribeMeshOutput, error)
	ListVirtualServices(context.Context, *awsappmesh.ListVirtualServicesInput, ...func(*awsappmesh.Options)) (*awsappmesh.ListVirtualServicesOutput, error)
	DescribeVirtualService(context.Context, *awsappmesh.DescribeVirtualServiceInput, ...func(*awsappmesh.Options)) (*awsappmesh.DescribeVirtualServiceOutput, error)
	ListVirtualNodes(context.Context, *awsappmesh.ListVirtualNodesInput, ...func(*awsappmesh.Options)) (*awsappmesh.ListVirtualNodesOutput, error)
	DescribeVirtualNode(context.Context, *awsappmesh.DescribeVirtualNodeInput, ...func(*awsappmesh.Options)) (*awsappmesh.DescribeVirtualNodeOutput, error)
	ListVirtualRouters(context.Context, *awsappmesh.ListVirtualRoutersInput, ...func(*awsappmesh.Options)) (*awsappmesh.ListVirtualRoutersOutput, error)
	DescribeVirtualRouter(context.Context, *awsappmesh.DescribeVirtualRouterInput, ...func(*awsappmesh.Options)) (*awsappmesh.DescribeVirtualRouterOutput, error)
	ListRoutes(context.Context, *awsappmesh.ListRoutesInput, ...func(*awsappmesh.Options)) (*awsappmesh.ListRoutesOutput, error)
	DescribeRoute(context.Context, *awsappmesh.DescribeRouteInput, ...func(*awsappmesh.Options)) (*awsappmesh.DescribeRouteOutput, error)
	ListVirtualGateways(context.Context, *awsappmesh.ListVirtualGatewaysInput, ...func(*awsappmesh.Options)) (*awsappmesh.ListVirtualGatewaysOutput, error)
	DescribeVirtualGateway(context.Context, *awsappmesh.DescribeVirtualGatewayInput, ...func(*awsappmesh.Options)) (*awsappmesh.DescribeVirtualGatewayOutput, error)
	ListGatewayRoutes(context.Context, *awsappmesh.ListGatewayRoutesInput, ...func(*awsappmesh.Options)) (*awsappmesh.ListGatewayRoutesOutput, error)
	DescribeGatewayRoute(context.Context, *awsappmesh.DescribeGatewayRouteInput, ...func(*awsappmesh.Options)) (*awsappmesh.DescribeGatewayRouteOutput, error)
	ListTagsForResource(context.Context, *awsappmesh.ListTagsForResourceInput, ...func(*awsappmesh.Options)) (*awsappmesh.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK App Mesh control-plane calls into scanner-owned
// metadata. It pages every List operation, fans out to the matching Describe
// per resource, reads tags per resource, and never calls a mutation API. Client
// TLS validation is reduced to ACM Private CA certificate authority ARNs;
// certificate bodies are never read.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an App Mesh SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsappmesh.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListMeshInventory returns the full App Mesh inventory visible to the
// configured credentials, with every child resource resolved to metadata-only
// records.
func (c *Client) ListMeshInventory(ctx context.Context) ([]appmeshservice.Mesh, error) {
	refs, err := c.listMeshes(ctx)
	if err != nil {
		return nil, err
	}
	meshes := make([]appmeshservice.Mesh, 0, len(refs))
	for _, ref := range refs {
		meshName := strings.TrimSpace(aws.ToString(ref.MeshName))
		if meshName == "" {
			continue
		}
		mesh, err := c.meshInventory(ctx, meshName)
		if err != nil {
			return nil, err
		}
		meshes = append(meshes, mesh)
	}
	return meshes, nil
}

func (c *Client) listMeshes(ctx context.Context) ([]appmeshtypes.MeshRef, error) {
	var refs []appmeshtypes.MeshRef
	var nextToken *string
	for {
		var page *awsappmesh.ListMeshesOutput
		err := c.recordAPICall(ctx, "ListMeshes", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListMeshes(callCtx, &awsappmesh.ListMeshesInput{NextToken: nextToken})
			return callErr
		})
		if err != nil {
			return nil, fmt.Errorf("list App Mesh meshes: %w", err)
		}
		if page == nil {
			return refs, nil
		}
		refs = append(refs, page.Meshes...)
		if aws.ToString(page.NextToken) == "" {
			return refs, nil
		}
		nextToken = page.NextToken
	}
}

func (c *Client) meshInventory(ctx context.Context, meshName string) (appmeshservice.Mesh, error) {
	detail, err := c.describeMesh(ctx, meshName)
	if err != nil {
		return appmeshservice.Mesh{}, err
	}
	mesh := mapMesh(meshName, detail)
	mesh.Tags = c.resourceTagsOrEmpty(ctx, mesh.ARN)

	if mesh.VirtualServices, err = c.virtualServices(ctx, meshName); err != nil {
		return appmeshservice.Mesh{}, err
	}
	if mesh.VirtualNodes, err = c.virtualNodes(ctx, meshName); err != nil {
		return appmeshservice.Mesh{}, err
	}
	if mesh.VirtualRouters, err = c.virtualRouters(ctx, meshName); err != nil {
		return appmeshservice.Mesh{}, err
	}
	if mesh.VirtualGateways, err = c.virtualGateways(ctx, meshName); err != nil {
		return appmeshservice.Mesh{}, err
	}
	return mesh, nil
}

func (c *Client) describeMesh(ctx context.Context, meshName string) (*appmeshtypes.MeshData, error) {
	var output *awsappmesh.DescribeMeshOutput
	err := c.recordAPICall(ctx, "DescribeMesh", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.DescribeMesh(callCtx, &awsappmesh.DescribeMeshInput{MeshName: aws.String(meshName)})
		return callErr
	})
	if err != nil {
		return nil, fmt.Errorf("describe App Mesh mesh %q: %w", meshName, err)
	}
	if output == nil {
		return nil, nil
	}
	return output.Mesh, nil
}

func (c *Client) virtualServices(ctx context.Context, meshName string) ([]appmeshservice.VirtualService, error) {
	var refs []appmeshtypes.VirtualServiceRef
	var nextToken *string
	for {
		var page *awsappmesh.ListVirtualServicesOutput
		err := c.recordAPICall(ctx, "ListVirtualServices", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListVirtualServices(callCtx, &awsappmesh.ListVirtualServicesInput{
				MeshName:  aws.String(meshName),
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, fmt.Errorf("list App Mesh virtual services for mesh %q: %w", meshName, err)
		}
		if page == nil {
			break
		}
		refs = append(refs, page.VirtualServices...)
		if aws.ToString(page.NextToken) == "" {
			break
		}
		nextToken = page.NextToken
	}

	services := make([]appmeshservice.VirtualService, 0, len(refs))
	for _, ref := range refs {
		name := strings.TrimSpace(aws.ToString(ref.VirtualServiceName))
		if name == "" {
			continue
		}
		detail, err := c.describeVirtualService(ctx, meshName, name)
		if err != nil {
			return nil, err
		}
		service := mapVirtualService(meshName, name, detail)
		service.Tags = c.resourceTagsOrEmpty(ctx, service.ARN)
		services = append(services, service)
	}
	return services, nil
}

func (c *Client) describeVirtualService(ctx context.Context, meshName, name string) (*appmeshtypes.VirtualServiceData, error) {
	var output *awsappmesh.DescribeVirtualServiceOutput
	err := c.recordAPICall(ctx, "DescribeVirtualService", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.DescribeVirtualService(callCtx, &awsappmesh.DescribeVirtualServiceInput{
			MeshName:           aws.String(meshName),
			VirtualServiceName: aws.String(name),
		})
		return callErr
	})
	if err != nil {
		return nil, fmt.Errorf("describe App Mesh virtual service %q: %w", name, err)
	}
	if output == nil {
		return nil, nil
	}
	return output.VirtualService, nil
}

// resourceTagsOrEmpty reads tags for one resource ARN, returning nil on a blank
// ARN. Tag reads page until exhausted.
func (c *Client) resourceTagsOrEmpty(ctx context.Context, arn string) map[string]string {
	arn = strings.TrimSpace(arn)
	if arn == "" {
		return nil
	}
	tags, err := c.resourceTags(ctx, arn)
	if err != nil {
		// Tag reads are best-effort metadata enrichment; a tag failure must not
		// drop the resource. The API-call counter already records the error
		// result for operator visibility.
		return nil
	}
	return tags
}

func (c *Client) resourceTags(ctx context.Context, arn string) (map[string]string, error) {
	var tags []appmeshtypes.TagRef
	var nextToken *string
	for {
		var page *awsappmesh.ListTagsForResourceOutput
		err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListTagsForResource(callCtx, &awsappmesh.ListTagsForResourceInput{
				ResourceArn: aws.String(arn),
				NextToken:   nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		tags = append(tags, page.Tags...)
		if aws.ToString(page.NextToken) == "" {
			break
		}
		nextToken = page.NextToken
	}
	return tagsToMap(tags), nil
}
