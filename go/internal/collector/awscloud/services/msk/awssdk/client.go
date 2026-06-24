// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awskafka "github.com/aws/aws-sdk-go-v2/service/kafka"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	mskservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/msk"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type apiClient interface {
	ListClustersV2(context.Context, *awskafka.ListClustersV2Input, ...func(*awskafka.Options)) (*awskafka.ListClustersV2Output, error)
	ListConfigurations(context.Context, *awskafka.ListConfigurationsInput, ...func(*awskafka.Options)) (*awskafka.ListConfigurationsOutput, error)
	ListReplicators(context.Context, *awskafka.ListReplicatorsInput, ...func(*awskafka.Options)) (*awskafka.ListReplicatorsOutput, error)
	DescribeReplicator(context.Context, *awskafka.DescribeReplicatorInput, ...func(*awskafka.Options)) (*awskafka.DescribeReplicatorOutput, error)
}

// Client adapts AWS SDK MSK (kafka) responses to the metadata-only MSK scanner
// contract.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an MSK SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awskafka.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListClusters returns MSK clusters visible to the configured AWS credentials,
// covering both provisioned and serverless cluster types.
func (c *Client) ListClusters(ctx context.Context) ([]mskservice.Cluster, error) {
	var clusters []mskservice.Cluster
	var nextToken *string
	for {
		var page *awskafka.ListClustersV2Output
		err := c.recordAPICall(ctx, "ListClustersV2", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListClustersV2(callCtx, &awskafka.ListClustersV2Input{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return clusters, nil
		}
		for _, cluster := range page.ClusterInfoList {
			clusters = append(clusters, mapCluster(cluster))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return clusters, nil
		}
	}
}

// ListConfigurations returns MSK broker configuration metadata. The adapter
// never calls DescribeConfigurationRevision, which would expose the raw
// server.properties body.
func (c *Client) ListConfigurations(ctx context.Context) ([]mskservice.Configuration, error) {
	var configurations []mskservice.Configuration
	var nextToken *string
	for {
		var page *awskafka.ListConfigurationsOutput
		err := c.recordAPICall(ctx, "ListConfigurations", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListConfigurations(callCtx, &awskafka.ListConfigurationsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return configurations, nil
		}
		for _, configuration := range page.Configurations {
			configurations = append(configurations, mapConfiguration(configuration))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return configurations, nil
		}
	}
}

// ListReplicators returns MSK Replicator metadata. ListReplicators omits the
// service execution role and KafkaClusters detail, so each replicator is
// enriched with a DescribeReplicator call.
func (c *Client) ListReplicators(ctx context.Context) ([]mskservice.Replicator, error) {
	var replicators []mskservice.Replicator
	var nextToken *string
	for {
		var page *awskafka.ListReplicatorsOutput
		err := c.recordAPICall(ctx, "ListReplicators", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListReplicators(callCtx, &awskafka.ListReplicatorsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return replicators, nil
		}
		for _, summary := range page.Replicators {
			arn := strings.TrimSpace(aws.ToString(summary.ReplicatorArn))
			if arn == "" {
				continue
			}
			description, err := c.describeReplicator(ctx, arn)
			if err != nil {
				return nil, fmt.Errorf("describe MSK replicator %q: %w", arn, err)
			}
			replicators = append(replicators, mapReplicatorDescription(description))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return replicators, nil
		}
	}
}

func (c *Client) describeReplicator(ctx context.Context, arn string) (*awskafka.DescribeReplicatorOutput, error) {
	var output *awskafka.DescribeReplicatorOutput
	err := c.recordAPICall(ctx, "DescribeReplicator", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeReplicator(callCtx, &awskafka.DescribeReplicatorInput{
			ReplicatorArn: aws.String(arn),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return &awskafka.DescribeReplicatorOutput{}, nil
	}
	return output, nil
}

var _ mskservice.Client = (*Client)(nil)

var _ apiClient = (*awskafka.Client)(nil)
