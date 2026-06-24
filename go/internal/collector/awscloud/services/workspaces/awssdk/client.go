// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsworkspaces "github.com/aws/aws-sdk-go-v2/service/workspaces"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	workspacesservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/workspaces"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS WorkSpaces API the adapter
// calls. It is deliberately limited to the control-plane describe reads and the
// resource-tag read. It exposes no Create/Modify/Reboot/Rebuild/Start/Stop/
// Terminate mutation and no session, connection-status, or credential read, so
// the adapter cannot read desktop session contents or mutate WorkSpaces state.
// The exclusion_test reflects over this interface to enforce that contract at
// build time.
type apiClient interface {
	DescribeWorkspaces(
		context.Context,
		*awsworkspaces.DescribeWorkspacesInput,
		...func(*awsworkspaces.Options),
	) (*awsworkspaces.DescribeWorkspacesOutput, error)
	DescribeWorkspaceDirectories(
		context.Context,
		*awsworkspaces.DescribeWorkspaceDirectoriesInput,
		...func(*awsworkspaces.Options),
	) (*awsworkspaces.DescribeWorkspaceDirectoriesOutput, error)
	DescribeWorkspaceBundles(
		context.Context,
		*awsworkspaces.DescribeWorkspaceBundlesInput,
		...func(*awsworkspaces.Options),
	) (*awsworkspaces.DescribeWorkspaceBundlesOutput, error)
	DescribeIpGroups(
		context.Context,
		*awsworkspaces.DescribeIpGroupsInput,
		...func(*awsworkspaces.Options),
	) (*awsworkspaces.DescribeIpGroupsOutput, error)
	DescribeTags(
		context.Context,
		*awsworkspaces.DescribeTagsInput,
		...func(*awsworkspaces.Options),
	) (*awsworkspaces.DescribeTagsOutput, error)
}

// Client adapts AWS SDK WorkSpaces control-plane calls into scanner-owned
// metadata. It never reads desktop session contents, user credentials,
// connection state, or registration codes, and never calls a mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a WorkSpaces SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsworkspaces.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns WorkSpaces, registered directories, account-owned bundles,
// and IP access control groups visible to the configured AWS credentials.
// Desktop session contents, user credentials, and connection state are never
// read.
func (c *Client) Snapshot(ctx context.Context) (workspacesservice.Snapshot, error) {
	directories, err := c.listDirectories(ctx)
	if err != nil {
		return workspacesservice.Snapshot{}, err
	}
	bundles, err := c.listBundles(ctx)
	if err != nil {
		return workspacesservice.Snapshot{}, err
	}
	ipGroups, err := c.listIPGroups(ctx)
	if err != nil {
		return workspacesservice.Snapshot{}, err
	}
	workspaces, err := c.listWorkspaces(ctx)
	if err != nil {
		return workspacesservice.Snapshot{}, err
	}
	return workspacesservice.Snapshot{
		Workspaces:  workspaces,
		Directories: directories,
		Bundles:     bundles,
		IPGroups:    ipGroups,
	}, nil
}

func (c *Client) listWorkspaces(ctx context.Context) ([]workspacesservice.Workspace, error) {
	var workspaces []workspacesservice.Workspace
	var nextToken *string
	for {
		var page *awsworkspaces.DescribeWorkspacesOutput
		err := c.recordAPICall(ctx, "DescribeWorkspaces", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.DescribeWorkspaces(callCtx, &awsworkspaces.DescribeWorkspacesInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return workspaces, nil
		}
		for _, workspace := range page.Workspaces {
			mapped, err := c.mapWorkspace(ctx, workspace)
			if err != nil {
				return nil, err
			}
			workspaces = append(workspaces, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return workspaces, nil
		}
	}
}

func (c *Client) listDirectories(ctx context.Context) ([]workspacesservice.Directory, error) {
	var directories []workspacesservice.Directory
	var nextToken *string
	for {
		var page *awsworkspaces.DescribeWorkspaceDirectoriesOutput
		err := c.recordAPICall(ctx, "DescribeWorkspaceDirectories", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.DescribeWorkspaceDirectories(callCtx, &awsworkspaces.DescribeWorkspaceDirectoriesInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return directories, nil
		}
		for _, directory := range page.Directories {
			mapped, err := c.mapDirectory(ctx, directory)
			if err != nil {
				return nil, err
			}
			directories = append(directories, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return directories, nil
		}
	}
}

// listBundles pages the account-owned WorkSpaces bundles. The DescribeWorkspace
// Bundles API returns the account's own bundles when no Owner is set; the
// scanner does not enumerate the (very large) AMAZON-provided catalog, so only
// bundles owned by the account and the bundles its WorkSpaces reference are in
// scope here.
func (c *Client) listBundles(ctx context.Context) ([]workspacesservice.Bundle, error) {
	var bundles []workspacesservice.Bundle
	var nextToken *string
	for {
		var page *awsworkspaces.DescribeWorkspaceBundlesOutput
		err := c.recordAPICall(ctx, "DescribeWorkspaceBundles", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.DescribeWorkspaceBundles(callCtx, &awsworkspaces.DescribeWorkspaceBundlesInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return bundles, nil
		}
		for _, bundle := range page.Bundles {
			mapped, err := c.mapBundle(ctx, bundle)
			if err != nil {
				return nil, err
			}
			bundles = append(bundles, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return bundles, nil
		}
	}
}

func (c *Client) listIPGroups(ctx context.Context) ([]workspacesservice.IPGroup, error) {
	var groups []workspacesservice.IPGroup
	var nextToken *string
	for {
		var page *awsworkspaces.DescribeIpGroupsOutput
		err := c.recordAPICall(ctx, "DescribeIpGroups", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.DescribeIpGroups(callCtx, &awsworkspaces.DescribeIpGroupsInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return groups, nil
		}
		for _, group := range page.Result {
			mapped, err := c.mapIPGroup(ctx, group)
			if err != nil {
				return nil, err
			}
			groups = append(groups, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return groups, nil
		}
	}
}

// listTags reads the resource tags for one WorkSpaces resource id (the bare
// WorkSpace, directory, bundle, or IP-group id DescribeTags expects). It never
// reads any other resource detail.
func (c *Client) listTags(ctx context.Context, resourceID string) (map[string]string, error) {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return nil, nil
	}
	var output *awsworkspaces.DescribeTagsOutput
	err := c.recordAPICall(ctx, "DescribeTags", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.DescribeTags(callCtx, &awsworkspaces.DescribeTagsInput{
			ResourceId: aws.String(resourceID),
		})
		return callErr
	})
	if err != nil || output == nil {
		return nil, err
	}
	if len(output.TagList) == 0 {
		return nil, nil
	}
	tags := make(map[string]string, len(output.TagList))
	for _, tag := range output.TagList {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		tags[key] = aws.ToString(tag.Value)
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

var _ workspacesservice.Client = (*Client)(nil)

var _ apiClient = (*awsworkspaces.Client)(nil)
