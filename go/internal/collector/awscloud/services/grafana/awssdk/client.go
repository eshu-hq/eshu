// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsgrafana "github.com/aws/aws-sdk-go-v2/service/grafana"
	awsgrafanatypes "github.com/aws/aws-sdk-go-v2/service/grafana/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	grafanaservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/grafana"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the read-only subset of the AWS SDK Managed Grafana client the
// adapter uses. It exposes ListWorkspaces, DescribeWorkspace, and
// ListTagsForResource only. Every Create/Update/Delete workspace API, the
// CreateWorkspaceApiKey and service-account-token APIs, AssociateLicense, and
// the DescribeWorkspaceAuthentication API (which returns SAML/IAM Identity
// Center configuration) are absent by construction, so the adapter cannot
// mutate a workspace, mint an API key or token, or read an authentication
// secret. The exclusion_test reflects over this interface to enforce that
// contract at build time.
type apiClient interface {
	ListWorkspaces(
		context.Context,
		*awsgrafana.ListWorkspacesInput,
		...func(*awsgrafana.Options),
	) (*awsgrafana.ListWorkspacesOutput, error)
	DescribeWorkspace(
		context.Context,
		*awsgrafana.DescribeWorkspaceInput,
		...func(*awsgrafana.Options),
	) (*awsgrafana.DescribeWorkspaceOutput, error)
	ListTagsForResource(
		context.Context,
		*awsgrafana.ListTagsForResourceInput,
		...func(*awsgrafana.Options),
	) (*awsgrafana.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK Managed Grafana control-plane calls into scanner-owned
// metadata. It never reads dashboards, alert rules, or query results, never
// reads workspace authentication configuration, and never calls a mutation or
// token API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Managed Grafana SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsgrafana.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns Managed Grafana workspace metadata visible to the configured
// AWS credentials. It enumerates workspace summaries with ListWorkspaces, reads
// each one with DescribeWorkspace for the role and vpcConfiguration metadata,
// and reads resource tags with ListTagsForResource. Dashboards, alert rules,
// query results, and authentication secrets are never read.
func (c *Client) Snapshot(ctx context.Context) (grafanaservice.Snapshot, error) {
	summaries, err := c.listWorkspaceIDs(ctx)
	if err != nil {
		return grafanaservice.Snapshot{}, err
	}
	workspaces := make([]grafanaservice.Workspace, 0, len(summaries))
	for _, id := range summaries {
		workspace, err := c.describeWorkspace(ctx, id)
		if err != nil {
			return grafanaservice.Snapshot{}, err
		}
		if workspace == nil {
			continue
		}
		workspaces = append(workspaces, *workspace)
	}
	return grafanaservice.Snapshot{Workspaces: workspaces}, nil
}

func (c *Client) listWorkspaceIDs(ctx context.Context) ([]string, error) {
	var ids []string
	var nextToken *string
	for {
		var page *awsgrafana.ListWorkspacesOutput
		err := c.recordAPICall(ctx, "ListWorkspaces", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListWorkspaces(callCtx, &awsgrafana.ListWorkspacesInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return ids, nil
		}
		for _, summary := range page.Workspaces {
			if id := strings.TrimSpace(aws.ToString(summary.Id)); id != "" {
				ids = append(ids, id)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return ids, nil
		}
	}
}

func (c *Client) describeWorkspace(ctx context.Context, workspaceID string) (*grafanaservice.Workspace, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, nil
	}
	var output *awsgrafana.DescribeWorkspaceOutput
	err := c.recordAPICall(ctx, "DescribeWorkspace", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeWorkspace(callCtx, &awsgrafana.DescribeWorkspaceInput{
			WorkspaceId: aws.String(workspaceID),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil || output.Workspace == nil {
		return &grafanaservice.Workspace{ID: workspaceID}, nil
	}
	workspace := c.mapWorkspace(*output.Workspace)
	if strings.TrimSpace(workspace.ID) == "" {
		workspace.ID = workspaceID
	}
	arn := workspaceARN(c.boundary, workspace.ID)
	workspace.ARN = arn
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return nil, err
	}
	workspace.Tags = tags
	return &workspace, nil
}

func (c *Client) mapWorkspace(workspace awsgrafanatypes.WorkspaceDescription) grafanaservice.Workspace {
	mapped := grafanaservice.Workspace{
		ID:                       strings.TrimSpace(aws.ToString(workspace.Id)),
		Name:                     strings.TrimSpace(aws.ToString(workspace.Name)),
		Description:              strings.TrimSpace(aws.ToString(workspace.Description)),
		Status:                   strings.TrimSpace(string(workspace.Status)),
		GrafanaVersion:           strings.TrimSpace(aws.ToString(workspace.GrafanaVersion)),
		Endpoint:                 strings.TrimSpace(aws.ToString(workspace.Endpoint)),
		AccountAccessType:        strings.TrimSpace(string(workspace.AccountAccessType)),
		PermissionType:           strings.TrimSpace(string(workspace.PermissionType)),
		WorkspaceRoleARN:         strings.TrimSpace(aws.ToString(workspace.WorkspaceRoleArn)),
		DataSources:              dataSourceNames(workspace.DataSources),
		NotificationDestinations: notificationDestinationNames(workspace.NotificationDestinations),
		AuthenticationProviders:  authenticationProviders(workspace.Authentication),
		Created:                  aws.ToTime(workspace.Created),
		Modified:                 aws.ToTime(workspace.Modified),
	}
	if vpc := workspace.VpcConfiguration; vpc != nil {
		mapped.SubnetIDs = trimmedStrings(vpc.SubnetIds)
		mapped.SecurityGroupIDs = trimmedStrings(vpc.SecurityGroupIds)
	}
	return mapped
}

func dataSourceNames(values []awsgrafanatypes.DataSourceType) []string {
	if len(values) == 0 {
		return nil
	}
	names := make([]string, 0, len(values))
	for _, value := range values {
		if name := strings.TrimSpace(string(value)); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func notificationDestinationNames(values []awsgrafanatypes.NotificationDestinationType) []string {
	if len(values) == 0 {
		return nil
	}
	names := make([]string, 0, len(values))
	for _, value := range values {
		if name := strings.TrimSpace(string(value)); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

// authenticationProviders returns only the authentication provider names (for
// example AWS_SSO, SAML). It never reads SAML assertion configuration or IAM
// Identity Center details; only the provider type names are mapped.
func authenticationProviders(summary *awsgrafanatypes.AuthenticationSummary) []string {
	if summary == nil || len(summary.Providers) == 0 {
		return nil
	}
	providers := make([]string, 0, len(summary.Providers))
	for _, provider := range summary.Providers {
		if name := strings.TrimSpace(string(provider)); name != "" {
			providers = append(providers, name)
		}
	}
	if len(providers) == 0 {
		return nil
	}
	return providers
}

func trimmedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	output := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsgrafana.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awsgrafana.ListTagsForResourceInput{
			ResourceArn: aws.String(resourceARN),
		})
		return err
	})
	if err != nil || output == nil {
		return nil, err
	}
	if len(output.Tags) == 0 {
		return nil, nil
	}
	tags := make(map[string]string, len(output.Tags))
	for key, value := range output.Tags {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		tags[trimmed] = value
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

var _ grafanaservice.Client = (*Client)(nil)

var _ apiClient = (*awsgrafana.Client)(nil)
