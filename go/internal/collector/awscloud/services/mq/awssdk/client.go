// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmq "github.com/aws/aws-sdk-go-v2/service/mq"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	mqservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/mq"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the narrow Amazon MQ read surface the adapter is allowed to call.
// It deliberately excludes every broker, configuration, and user mutation API,
// RebootBroker, DescribeUser (the password-bearing user read), and
// DescribeConfigurationRevision (the configuration XML body read). The
// exclusion is enforced by TestAPIClientNeverIncludesForbiddenMethods.
type apiClient interface {
	ListBrokers(context.Context, *awsmq.ListBrokersInput, ...func(*awsmq.Options)) (*awsmq.ListBrokersOutput, error)
	DescribeBroker(context.Context, *awsmq.DescribeBrokerInput, ...func(*awsmq.Options)) (*awsmq.DescribeBrokerOutput, error)
	ListConfigurations(context.Context, *awsmq.ListConfigurationsInput, ...func(*awsmq.Options)) (*awsmq.ListConfigurationsOutput, error)
}

// Client adapts AWS SDK Amazon MQ responses to the metadata-only MQ scanner
// contract for both ActiveMQ and RabbitMQ broker engine types.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an Amazon MQ SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsmq.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListBrokers returns Amazon MQ brokers visible to the configured AWS
// credentials. ListBrokers reports only summary fields, so each broker is
// enriched with a DescribeBroker call. DescribeBroker returns broker usernames
// (UserSummary) but never passwords; the adapter records usernames only.
func (c *Client) ListBrokers(ctx context.Context) ([]mqservice.Broker, error) {
	var brokers []mqservice.Broker
	var nextToken *string
	for {
		var page *awsmq.ListBrokersOutput
		err := c.recordAPICall(ctx, "ListBrokers", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListBrokers(callCtx, &awsmq.ListBrokersInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return brokers, nil
		}
		for _, summary := range page.BrokerSummaries {
			brokerID := strings.TrimSpace(aws.ToString(summary.BrokerId))
			if brokerID == "" {
				continue
			}
			description, err := c.describeBroker(ctx, brokerID)
			if err != nil {
				return nil, fmt.Errorf("describe MQ broker %q: %w", brokerID, err)
			}
			brokers = append(brokers, mapBrokerDescription(description))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return brokers, nil
		}
	}
}

// ListConfigurations returns Amazon MQ broker configuration metadata. The
// adapter never calls DescribeConfigurationRevision, which would expose the raw
// configuration XML body.
func (c *Client) ListConfigurations(ctx context.Context) ([]mqservice.Configuration, error) {
	var configurations []mqservice.Configuration
	var nextToken *string
	for {
		var page *awsmq.ListConfigurationsOutput
		err := c.recordAPICall(ctx, "ListConfigurations", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListConfigurations(callCtx, &awsmq.ListConfigurationsInput{
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

func (c *Client) describeBroker(ctx context.Context, brokerID string) (*awsmq.DescribeBrokerOutput, error) {
	var output *awsmq.DescribeBrokerOutput
	err := c.recordAPICall(ctx, "DescribeBroker", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeBroker(callCtx, &awsmq.DescribeBrokerInput{
			BrokerId: aws.String(brokerID),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return &awsmq.DescribeBrokerOutput{}, nil
	}
	return output, nil
}

var _ mqservice.Client = (*Client)(nil)

var _ apiClient = (*awsmq.Client)(nil)
