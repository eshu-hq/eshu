// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsds "github.com/aws/aws-sdk-go-v2/service/directoryservice"
	awsdstypes "github.com/aws/aws-sdk-go-v2/service/directoryservice/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	dsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ds"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const describeLimit int32 = 100

// apiClient is the single seam between the Directory Service adapter and the AWS
// SDK client. Client.client is typed as apiClient, pinned by
// var _ apiClient = (*awsds.Client)(nil), so it is the only place SDK methods can
// be called from. It is limited to describe-class metadata reads: no mutation API
// (ResetUserPassword, Create/Delete/Update/Enable/Disable/...) may appear here.
type apiClient interface {
	DescribeDirectories(
		context.Context,
		*awsds.DescribeDirectoriesInput,
		...func(*awsds.Options),
	) (*awsds.DescribeDirectoriesOutput, error)
	DescribeTrusts(
		context.Context,
		*awsds.DescribeTrustsInput,
		...func(*awsds.Options),
	) (*awsds.DescribeTrustsOutput, error)
	DescribeSharedDirectories(
		context.Context,
		*awsds.DescribeSharedDirectoriesInput,
		...func(*awsds.Options),
	) (*awsds.DescribeSharedDirectoriesOutput, error)
	DescribeLDAPSSettings(
		context.Context,
		*awsds.DescribeLDAPSSettingsInput,
		...func(*awsds.Options),
	) (*awsds.DescribeLDAPSSettingsOutput, error)
	ListTagsForResource(
		context.Context,
		*awsds.ListTagsForResourceInput,
		...func(*awsds.Options),
	) (*awsds.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK Directory Service control-plane calls into scanner-owned
// metadata. It never reads the directory admin password, the RADIUS shared
// secret, the AD Connector service-account credentials, or any mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Directory Service SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsds.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListDirectories returns Directory Service directory metadata visible to the
// configured AWS credentials. The adapter resolves each directory's LDAPS status
// for AWS Managed Microsoft AD directories only; SimpleAD and AD Connector
// directories do not support LDAPS, so the adapter does not call
// DescribeLDAPSSettings for them. The directory admin password and RADIUS shared
// secret are never read.
func (c *Client) ListDirectories(ctx context.Context) ([]dsservice.Directory, error) {
	var directories []dsservice.Directory
	var nextToken *string
	for {
		var page *awsds.DescribeDirectoriesOutput
		err := c.recordAPICall(ctx, "DescribeDirectories", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeDirectories(callCtx, &awsds.DescribeDirectoriesInput{
				Limit:     aws.Int32(describeLimit),
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return directories, nil
		}
		for _, raw := range page.DirectoryDescriptions {
			ldapsStatuses, err := c.listLDAPSStatuses(ctx, raw)
			if err != nil {
				return nil, err
			}
			tags, err := c.listTags(ctx, aws.ToString(raw.DirectoryId))
			if err != nil {
				return nil, err
			}
			directories = append(directories, mapDirectory(raw, ldapsStatuses, tags))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return directories, nil
		}
	}
}

// ListTrusts returns the trust relationships for one directory id.
func (c *Client) ListTrusts(ctx context.Context, directoryID string) ([]dsservice.Trust, error) {
	directoryID = strings.TrimSpace(directoryID)
	if directoryID == "" {
		return nil, nil
	}
	var trusts []dsservice.Trust
	var nextToken *string
	for {
		var page *awsds.DescribeTrustsOutput
		err := c.recordAPICall(ctx, "DescribeTrusts", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeTrusts(callCtx, &awsds.DescribeTrustsInput{
				DirectoryId: aws.String(directoryID),
				Limit:       aws.Int32(describeLimit),
				NextToken:   nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return trusts, nil
		}
		for _, raw := range page.Trusts {
			trusts = append(trusts, mapTrust(raw))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return trusts, nil
		}
	}
}

// ListSharedDirectories returns the share invitations owned by one directory id.
func (c *Client) ListSharedDirectories(ctx context.Context, ownerDirectoryID string) ([]dsservice.SharedDirectory, error) {
	ownerDirectoryID = strings.TrimSpace(ownerDirectoryID)
	if ownerDirectoryID == "" {
		return nil, nil
	}
	var shares []dsservice.SharedDirectory
	var nextToken *string
	for {
		var page *awsds.DescribeSharedDirectoriesOutput
		err := c.recordAPICall(ctx, "DescribeSharedDirectories", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeSharedDirectories(callCtx, &awsds.DescribeSharedDirectoriesInput{
				OwnerDirectoryId: aws.String(ownerDirectoryID),
				Limit:            aws.Int32(describeLimit),
				NextToken:        nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return shares, nil
		}
		for _, raw := range page.SharedDirectories {
			shares = append(shares, mapSharedDirectory(raw))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return shares, nil
		}
	}
}

// ListLDAPSSettings returns the client-side LDAPS settings for one directory id.
func (c *Client) ListLDAPSSettings(ctx context.Context, directoryID string) ([]dsservice.LDAPSSetting, error) {
	directoryID = strings.TrimSpace(directoryID)
	if directoryID == "" {
		return nil, nil
	}
	var settings []dsservice.LDAPSSetting
	var nextToken *string
	for {
		var page *awsds.DescribeLDAPSSettingsOutput
		err := c.recordAPICall(ctx, "DescribeLDAPSSettings", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeLDAPSSettings(callCtx, &awsds.DescribeLDAPSSettingsInput{
				DirectoryId: aws.String(directoryID),
				Type:        awsdstypes.LDAPSTypeClient,
				Limit:       aws.Int32(describeLimit),
				NextToken:   nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return settings, nil
		}
		for _, raw := range page.LDAPSSettingsInfo {
			settings = append(settings, mapLDAPSSetting(raw))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return settings, nil
		}
	}
}

// listLDAPSStatuses resolves the LDAPS statuses for a directory when its type
// supports LDAPS (AWS Managed Microsoft AD, including the shared variant).
// SimpleAD and AD Connector do not support LDAPS, so the adapter skips the call
// to avoid an UnsupportedOperationException.
func (c *Client) listLDAPSStatuses(ctx context.Context, raw awsdstypes.DirectoryDescription) ([]string, error) {
	if !supportsLDAPS(raw.Type) {
		return nil, nil
	}
	directoryID := aws.ToString(raw.DirectoryId)
	settings, err := c.ListLDAPSSettings(ctx, directoryID)
	if err != nil {
		return nil, err
	}
	statuses := make([]string, 0, len(settings))
	for _, setting := range settings {
		if status := strings.TrimSpace(setting.Status); status != "" {
			statuses = append(statuses, status)
		}
	}
	if len(statuses) == 0 {
		return nil, nil
	}
	return statuses, nil
}

func supportsLDAPS(directoryType awsdstypes.DirectoryType) bool {
	switch directoryType {
	case awsdstypes.DirectoryTypeMicrosoftAd, awsdstypes.DirectoryTypeSharedMicrosoftAd:
		return true
	default:
		return false
	}
}

func (c *Client) listTags(ctx context.Context, directoryID string) (map[string]string, error) {
	directoryID = strings.TrimSpace(directoryID)
	if directoryID == "" {
		return nil, nil
	}
	tags := map[string]string{}
	var nextToken *string
	for {
		var output *awsds.ListTagsForResourceOutput
		err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
			var err error
			output, err = c.client.ListTagsForResource(callCtx, &awsds.ListTagsForResourceInput{
				ResourceId: aws.String(directoryID),
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			break
		}
		for _, tag := range output.Tags {
			key := strings.TrimSpace(aws.ToString(tag.Key))
			if key == "" {
				continue
			}
			tags[key] = aws.ToString(tag.Value)
		}
		nextToken = output.NextToken
		if aws.ToString(nextToken) == "" {
			break
		}
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
}

func (c *Client) recordAPICall(ctx context.Context, operation string, call func(context.Context) error) error {
	if c.tracer != nil {
		var span trace.Span
		ctx, span = c.tracer.Start(ctx, telemetry.SpanAWSServicePaginationPage)
		span.SetAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
		)
		defer span.End()
	}
	err := call(ctx)
	result := "success"
	if err != nil {
		result = "error"
	}
	throttled := isThrottleError(err)
	awscloud.RecordAPICall(ctx, awscloud.APICallEvent{
		Boundary:  c.boundary,
		Operation: operation,
		Result:    result,
		Throttled: throttled,
	})
	if c.instruments != nil {
		c.instruments.AWSAPICalls.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
			telemetry.AttrResult(result),
		))
		if throttled {
			c.instruments.AWSThrottles.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrService(c.boundary.ServiceKind),
				telemetry.AttrAccount(c.boundary.AccountID),
				telemetry.AttrRegion(c.boundary.Region),
			))
		}
	}
	return err
}

func isThrottleError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := apiErr.ErrorCode()
	return strings.Contains(strings.ToLower(code), "throttl") ||
		code == "RequestLimitExceeded" ||
		code == "TooManyRequestsException"
}

var _ dsservice.Client = (*Client)(nil)

var _ apiClient = (*awsds.Client)(nil)
