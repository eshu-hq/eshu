// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsneptunegraph "github.com/aws/aws-sdk-go-v2/service/neptunegraph"

	neptuneservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/neptune"
)

// ListGraphs returns Neptune Analytics graph metadata visible to the configured
// AWS credentials. Each graph summary is resolved with GetGraph so the
// vector-search embedding dimension (carried only on the detail response) is
// available; graph vertex and edge contents are never read.
func (c *Client) ListGraphs(ctx context.Context) ([]neptuneservice.Graph, error) {
	var graphs []neptuneservice.Graph
	var token *string
	for {
		var page *awsneptunegraph.ListGraphsOutput
		err := c.recordAPICall(ctx, "ListGraphs", func(callCtx context.Context) error {
			var err error
			page, err = c.graph.ListGraphs(callCtx, &awsneptunegraph.ListGraphsInput{
				NextToken:  token,
				MaxResults: aws.Int32(describeMaxRecords),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return graphs, nil
		}
		for _, summary := range page.Graphs {
			graph, err := c.describeGraph(ctx, summary.Id)
			if err != nil {
				return nil, err
			}
			tags, err := c.listGraphTags(ctx, aws.ToString(summary.Arn))
			if err != nil {
				return nil, err
			}
			graph.Tags = tags
			graphs = append(graphs, graph)
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return graphs, nil
		}
	}
}

// describeGraph resolves one Neptune Analytics graph's detail, including the
// vector-search dimension that ListGraphs does not return. It never issues a
// graph query or reads graph data.
func (c *Client) describeGraph(ctx context.Context, id *string) (neptuneservice.Graph, error) {
	if aws.ToString(id) == "" {
		return neptuneservice.Graph{}, nil
	}
	var output *awsneptunegraph.GetGraphOutput
	err := c.recordAPICall(ctx, "GetGraph", func(callCtx context.Context) error {
		var err error
		output, err = c.graph.GetGraph(callCtx, &awsneptunegraph.GetGraphInput{
			GraphIdentifier: id,
		})
		return err
	})
	if err != nil || output == nil {
		return neptuneservice.Graph{}, err
	}
	return mapGraph(output), nil
}

// ListGraphSnapshots returns Neptune Analytics graph snapshot metadata visible
// to the configured AWS credentials. Snapshot contents are never read.
func (c *Client) ListGraphSnapshots(ctx context.Context) ([]neptuneservice.GraphSnapshot, error) {
	var snapshots []neptuneservice.GraphSnapshot
	var token *string
	for {
		var page *awsneptunegraph.ListGraphSnapshotsOutput
		err := c.recordAPICall(ctx, "ListGraphSnapshots", func(callCtx context.Context) error {
			var err error
			page, err = c.graph.ListGraphSnapshots(callCtx, &awsneptunegraph.ListGraphSnapshotsInput{
				NextToken:  token,
				MaxResults: aws.Int32(describeMaxRecords),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return snapshots, nil
		}
		for _, summary := range page.GraphSnapshots {
			tags, err := c.listGraphTags(ctx, aws.ToString(summary.Arn))
			if err != nil {
				return nil, err
			}
			snapshots = append(snapshots, mapGraphSnapshot(summary, tags))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return snapshots, nil
		}
	}
}

func (c *Client) listGraphTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsneptunegraph.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResourceGraph", func(callCtx context.Context) error {
		var err error
		output, err = c.graph.ListTagsForResource(callCtx, &awsneptunegraph.ListTagsForResourceInput{
			ResourceArn: aws.String(resourceARN),
		})
		return err
	})
	if err != nil || output == nil {
		return nil, err
	}
	return cloneTagMap(output.Tags), nil
}
