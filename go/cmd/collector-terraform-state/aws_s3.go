package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
)

type awsS3ObjectClient struct {
	roleARN string
	mu      sync.Mutex
	clients map[string]*s3.Client
}

func newAWSS3ObjectClient(roleARN string) terraformstate.S3ObjectClient {
	return &awsS3ObjectClient{
		roleARN: strings.TrimSpace(roleARN),
		clients: make(map[string]*s3.Client),
	}
}

func (c *awsS3ObjectClient) GetObject(
	ctx context.Context,
	input terraformstate.S3GetObjectInput,
) (terraformstate.S3GetObjectOutput, error) {
	client, err := c.clientForRegion(ctx, input.Region)
	if err != nil {
		return terraformstate.S3GetObjectOutput{}, err
	}
	request := &s3.GetObjectInput{
		Bucket: awsv2.String(input.Bucket),
		Key:    awsv2.String(input.Key),
	}
	if strings.TrimSpace(input.VersionID) != "" {
		request.VersionId = awsv2.String(input.VersionID)
	}
	if strings.TrimSpace(input.IfNoneMatch) != "" {
		request.IfNoneMatch = awsv2.String(input.IfNoneMatch)
	}

	output, err := client.GetObject(ctx, request)
	if err != nil {
		return terraformstate.S3GetObjectOutput{}, fmt.Errorf("read terraform state s3 object: %w", safeS3GetObjectError(err))
	}
	if output.Body == nil {
		return terraformstate.S3GetObjectOutput{}, fmt.Errorf("read terraform state s3 object: body is nil")
	}

	var size int64
	if output.ContentLength != nil {
		size = *output.ContentLength
	}
	var lastModified = timeZeroUTC()
	if output.LastModified != nil {
		lastModified = output.LastModified.UTC()
	}
	return terraformstate.S3GetObjectOutput{
		Body:         output.Body,
		Size:         size,
		ETag:         s3OutputETag(output),
		LastModified: lastModified,
	}, nil
}

func s3OutputETag(output *s3.GetObjectOutput) string {
	if output == nil {
		return ""
	}
	return awsv2.ToString(output.ETag)
}

func (c *awsS3ObjectClient) clientForRegion(ctx context.Context, region string) (*s3.Client, error) {
	region = strings.TrimSpace(region)
	if region == "" {
		return nil, fmt.Errorf("terraform state s3 region must not be blank")
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if client := c.clients[region]; client != nil {
		return client, nil
	}

	config, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration: %w", safeS3GetObjectError(err))
	}
	if c.roleARN != "" {
		stsClient := sts.NewFromConfig(config)
		config.Credentials = awsv2.NewCredentialsCache(stscreds.NewAssumeRoleProvider(stsClient, c.roleARN))
	}
	client := s3.NewFromConfig(config)
	c.clients[region] = client
	return client, nil
}

func safeS3GetObjectError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		if strings.EqualFold(apiErr.ErrorCode(), "NotModified") {
			return terraformstate.ErrStateNotModified
		}
		return fmt.Errorf("aws s3 api error code=%s fault=%s", apiErr.ErrorCode(), apiErr.ErrorFault())
	}
	return fmt.Errorf("aws s3 request failed")
}
