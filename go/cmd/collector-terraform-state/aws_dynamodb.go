package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
)

type dynamoDBGetItemAPI interface {
	GetItem(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
}

type awsDynamoDBLockMetadataClient struct {
	roleARN string
	mu      sync.Mutex
	clients map[string]dynamoDBGetItemAPI
	now     func() time.Time
}

func newAWSDynamoDBLockMetadataClient(roleARN string) terraformstate.LockMetadataClient {
	return &awsDynamoDBLockMetadataClient{
		roleARN: strings.TrimSpace(roleARN),
		clients: make(map[string]dynamoDBGetItemAPI),
		now:     time.Now,
	}
}

func (c *awsDynamoDBLockMetadataClient) ReadLockMetadata(
	ctx context.Context,
	input terraformstate.LockMetadataInput,
) (terraformstate.LockMetadataOutput, error) {
	client, err := c.clientForRegion(ctx, input.Region)
	if err != nil {
		return terraformstate.LockMetadataOutput{}, err
	}

	output, err := client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: awsv2.String(input.TableName),
		Key: map[string]types.AttributeValue{
			"LockID": &types.AttributeValueMemberS{Value: input.LockID},
		},
		ProjectionExpression:     awsv2.String("#D"),
		ExpressionAttributeNames: map[string]string{"#D": "Digest"},
		ConsistentRead:           awsv2.Bool(true),
	})
	if err != nil {
		return terraformstate.LockMetadataOutput{}, fmt.Errorf("read terraform state dynamodb lock metadata: %w", safeDynamoDBGetItemError(err))
	}

	return terraformstate.LockMetadataOutput{
		Digest:     dynamoDBStringAttribute(output, "Digest"),
		ObservedAt: c.currentTime(),
	}, nil
}

func (c *awsDynamoDBLockMetadataClient) clientForRegion(ctx context.Context, region string) (dynamoDBGetItemAPI, error) {
	region = strings.TrimSpace(region)
	if region == "" {
		return nil, fmt.Errorf("terraform state dynamodb region must not be blank")
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if client := c.clients[region]; client != nil {
		return client, nil
	}

	config, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration: %w", safeDynamoDBGetItemError(err))
	}
	if c.roleARN != "" {
		stsClient := sts.NewFromConfig(config)
		config.Credentials = awsv2.NewCredentialsCache(stscreds.NewAssumeRoleProvider(stsClient, c.roleARN))
	}
	client := dynamodb.NewFromConfig(config)
	c.clients[region] = client
	return client, nil
}

func (c *awsDynamoDBLockMetadataClient) currentTime() time.Time {
	if c.now != nil {
		return c.now().UTC()
	}
	return time.Now().UTC()
}

func dynamoDBStringAttribute(output *dynamodb.GetItemOutput, name string) string {
	if output == nil || output.Item == nil {
		return ""
	}
	value, ok := output.Item[name].(*types.AttributeValueMemberS)
	if !ok {
		return ""
	}
	return value.Value
}

func safeDynamoDBGetItemError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return fmt.Errorf("aws dynamodb api error code=%s fault=%s", apiErr.ErrorCode(), apiErr.ErrorFault())
	}
	return fmt.Errorf("aws dynamodb request failed")
}
