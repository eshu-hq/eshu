// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscodedeploy "github.com/aws/aws-sdk-go-v2/service/codedeploy"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	cdservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/codedeploy"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// recentDeploymentLimit bounds how many recent deployment IDs the adapter
// resolves per scan. It matches the BatchGetDeployments cap so the scanner
// makes a single batch call and stays metadata-sized regardless of account
// deployment history.
const recentDeploymentLimit = 25

// batchApplicationLimit caps BatchGetApplications input per the AWS contract.
const batchApplicationLimit = 100

// batchDeploymentGroupLimit caps BatchGetDeploymentGroups input per the AWS
// contract, which accepts at most 100 deployment group names per call. The
// adapter chunks larger applications so a deployment-group-heavy application
// does not fail the whole scan.
const batchDeploymentGroupLimit = 100

// apiClient is the metadata-only CodeDeploy SDK surface the adapter consumes.
// It intentionally omits every mutation API, deployment/instance data-plane
// API, and revision-body API. The reflection guard test asserts the omission.
type apiClient interface {
	ListApplications(context.Context, *awscodedeploy.ListApplicationsInput, ...func(*awscodedeploy.Options)) (*awscodedeploy.ListApplicationsOutput, error)
	BatchGetApplications(context.Context, *awscodedeploy.BatchGetApplicationsInput, ...func(*awscodedeploy.Options)) (*awscodedeploy.BatchGetApplicationsOutput, error)
	ListDeploymentGroups(context.Context, *awscodedeploy.ListDeploymentGroupsInput, ...func(*awscodedeploy.Options)) (*awscodedeploy.ListDeploymentGroupsOutput, error)
	BatchGetDeploymentGroups(context.Context, *awscodedeploy.BatchGetDeploymentGroupsInput, ...func(*awscodedeploy.Options)) (*awscodedeploy.BatchGetDeploymentGroupsOutput, error)
	ListDeploymentConfigs(context.Context, *awscodedeploy.ListDeploymentConfigsInput, ...func(*awscodedeploy.Options)) (*awscodedeploy.ListDeploymentConfigsOutput, error)
	GetDeploymentConfig(context.Context, *awscodedeploy.GetDeploymentConfigInput, ...func(*awscodedeploy.Options)) (*awscodedeploy.GetDeploymentConfigOutput, error)
	ListDeployments(context.Context, *awscodedeploy.ListDeploymentsInput, ...func(*awscodedeploy.Options)) (*awscodedeploy.ListDeploymentsOutput, error)
	BatchGetDeployments(context.Context, *awscodedeploy.BatchGetDeploymentsInput, ...func(*awscodedeploy.Options)) (*awscodedeploy.BatchGetDeploymentsOutput, error)
	ListTagsForResource(context.Context, *awscodedeploy.ListTagsForResourceInput, ...func(*awscodedeploy.Options)) (*awscodedeploy.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK CodeDeploy pagination into scanner-owned metadata. It
// redacts on-premises instance tag values before they reach scanner types.
type Client struct {
	client       apiClient
	boundary     awscloud.Boundary
	tracer       trace.Tracer
	instruments  *telemetry.Instruments
	redactionKey redact.Key
}

// NewClient builds a CodeDeploy SDK adapter for one claimed AWS boundary. The
// redaction key is required so on-premises instance tag values never persist
// raw; callers obtain it from the runtime scanner dependencies.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	redactionKey redact.Key,
) *Client {
	return &Client{
		client:       awscodedeploy.NewFromConfig(config),
		boundary:     boundary,
		tracer:       tracer,
		instruments:  instruments,
		redactionKey: redactionKey,
	}
}

// ListApplications returns CodeDeploy application metadata visible to the
// configured credentials. It lists application names, batch-resolves their
// metadata, and reads each application's tags. It never fetches revisions.
func (c *Client) ListApplications(ctx context.Context) ([]cdservice.Application, error) {
	names, err := c.listApplicationNames(ctx)
	if err != nil {
		return nil, err
	}
	var applications []cdservice.Application
	for start := 0; start < len(names); start += batchApplicationLimit {
		end := start + batchApplicationLimit
		if end > len(names) {
			end = len(names)
		}
		var output *awscodedeploy.BatchGetApplicationsOutput
		err := c.recordAPICall(ctx, "BatchGetApplications", func(callCtx context.Context) error {
			var callErr error
			output, callErr = c.client.BatchGetApplications(callCtx, &awscodedeploy.BatchGetApplicationsInput{
				ApplicationNames: names[start:end],
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			continue
		}
		for _, info := range output.ApplicationsInfo {
			application := mapApplication(info)
			arn := applicationARN(c.boundary, application.Name)
			tags, err := c.listTags(ctx, arn)
			if err != nil {
				return nil, err
			}
			application.Tags = tags
			applications = append(applications, application)
		}
	}
	return applications, nil
}

func (c *Client) listApplicationNames(ctx context.Context) ([]string, error) {
	var names []string
	var token *string
	for {
		var output *awscodedeploy.ListApplicationsOutput
		err := c.recordAPICall(ctx, "ListApplications", func(callCtx context.Context) error {
			var callErr error
			output, callErr = c.client.ListApplications(callCtx, &awscodedeploy.ListApplicationsInput{NextToken: token})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			break
		}
		names = append(names, output.Applications...)
		if output.NextToken == nil || strings.TrimSpace(*output.NextToken) == "" {
			break
		}
		token = output.NextToken
	}
	return names, nil
}

// ListDeploymentGroups returns deployment-group metadata for one application.
func (c *Client) ListDeploymentGroups(ctx context.Context, applicationName string) ([]cdservice.DeploymentGroup, error) {
	applicationName = strings.TrimSpace(applicationName)
	if applicationName == "" {
		return nil, nil
	}
	names, err := c.listDeploymentGroupNames(ctx, applicationName)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, nil
	}
	groups := make([]cdservice.DeploymentGroup, 0, len(names))
	for start := 0; start < len(names); start += batchDeploymentGroupLimit {
		end := start + batchDeploymentGroupLimit
		if end > len(names) {
			end = len(names)
		}
		var output *awscodedeploy.BatchGetDeploymentGroupsOutput
		err = c.recordAPICall(ctx, "BatchGetDeploymentGroups", func(callCtx context.Context) error {
			var callErr error
			output, callErr = c.client.BatchGetDeploymentGroups(callCtx, &awscodedeploy.BatchGetDeploymentGroupsInput{
				ApplicationName:      aws.String(applicationName),
				DeploymentGroupNames: names[start:end],
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			continue
		}
		for _, info := range output.DeploymentGroupsInfo {
			group := mapDeploymentGroup(info, c.redactionKey)
			arn := deploymentGroupARN(c.boundary, group.ApplicationName, group.Name)
			tags, err := c.listTags(ctx, arn)
			if err != nil {
				return nil, err
			}
			group.Tags = tags
			groups = append(groups, group)
		}
	}
	return groups, nil
}

func (c *Client) listDeploymentGroupNames(ctx context.Context, applicationName string) ([]string, error) {
	var names []string
	var token *string
	for {
		var output *awscodedeploy.ListDeploymentGroupsOutput
		err := c.recordAPICall(ctx, "ListDeploymentGroups", func(callCtx context.Context) error {
			var callErr error
			output, callErr = c.client.ListDeploymentGroups(callCtx, &awscodedeploy.ListDeploymentGroupsInput{
				ApplicationName: aws.String(applicationName),
				NextToken:       token,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			break
		}
		names = append(names, output.DeploymentGroups...)
		if output.NextToken == nil || strings.TrimSpace(*output.NextToken) == "" {
			break
		}
		token = output.NextToken
	}
	return names, nil
}

// ListDeploymentConfigs returns deployment-configuration metadata, resolving
// the minimum-healthy-hosts contract per config.
func (c *Client) ListDeploymentConfigs(ctx context.Context) ([]cdservice.DeploymentConfig, error) {
	names, err := c.listDeploymentConfigNames(ctx)
	if err != nil {
		return nil, err
	}
	configs := make([]cdservice.DeploymentConfig, 0, len(names))
	for _, name := range names {
		var output *awscodedeploy.GetDeploymentConfigOutput
		err := c.recordAPICall(ctx, "GetDeploymentConfig", func(callCtx context.Context) error {
			var callErr error
			output, callErr = c.client.GetDeploymentConfig(callCtx, &awscodedeploy.GetDeploymentConfigInput{
				DeploymentConfigName: aws.String(name),
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if output == nil || output.DeploymentConfigInfo == nil {
			continue
		}
		configs = append(configs, mapDeploymentConfig(*output.DeploymentConfigInfo))
	}
	return configs, nil
}

func (c *Client) listDeploymentConfigNames(ctx context.Context) ([]string, error) {
	var names []string
	var token *string
	for {
		var output *awscodedeploy.ListDeploymentConfigsOutput
		err := c.recordAPICall(ctx, "ListDeploymentConfigs", func(callCtx context.Context) error {
			var callErr error
			output, callErr = c.client.ListDeploymentConfigs(callCtx, &awscodedeploy.ListDeploymentConfigsInput{NextToken: token})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			break
		}
		names = append(names, output.DeploymentConfigsList...)
		if output.NextToken == nil || strings.TrimSpace(*output.NextToken) == "" {
			break
		}
		token = output.NextToken
	}
	return names, nil
}

// ListRecentDeployments returns the most recent deployment metadata for the
// boundary, bounded by recentDeploymentLimit so the scan stays metadata-sized.
// It never requests revision bodies; appspec.yml content is dropped in mapping.
func (c *Client) ListRecentDeployments(ctx context.Context) ([]cdservice.Deployment, error) {
	ids, err := c.listRecentDeploymentIDs(ctx)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var output *awscodedeploy.BatchGetDeploymentsOutput
	err = c.recordAPICall(ctx, "BatchGetDeployments", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.BatchGetDeployments(callCtx, &awscodedeploy.BatchGetDeploymentsInput{
			DeploymentIds: ids,
		})
		return callErr
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	deployments := make([]cdservice.Deployment, 0, len(output.DeploymentsInfo))
	for _, info := range output.DeploymentsInfo {
		deployments = append(deployments, mapDeployment(info))
	}
	return deployments, nil
}

func (c *Client) listRecentDeploymentIDs(ctx context.Context) ([]string, error) {
	var output *awscodedeploy.ListDeploymentsOutput
	err := c.recordAPICall(ctx, "ListDeployments", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.ListDeployments(callCtx, &awscodedeploy.ListDeploymentsInput{})
		return callErr
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	ids := output.Deployments
	if len(ids) > recentDeploymentLimit {
		ids = ids[:recentDeploymentLimit]
	}
	return ids, nil
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	tags := map[string]string{}
	var token *string
	for {
		var output *awscodedeploy.ListTagsForResourceOutput
		err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
			var callErr error
			output, callErr = c.client.ListTagsForResource(callCtx, &awscodedeploy.ListTagsForResourceInput{
				ResourceArn: aws.String(resourceARN),
				NextToken:   token,
			})
			return callErr
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
		if output.NextToken == nil || strings.TrimSpace(*output.NextToken) == "" {
			break
		}
		token = output.NextToken
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

func applicationARN(boundary awscloud.Boundary, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return fmt.Sprintf("arn:%s:codedeploy:%s:%s:application:%s", awscloud.PartitionForBoundary(boundary), boundary.Region, boundary.AccountID, name)
}

func deploymentGroupARN(boundary awscloud.Boundary, application, group string) string {
	application = strings.TrimSpace(application)
	group = strings.TrimSpace(group)
	if application == "" || group == "" {
		return ""
	}
	return fmt.Sprintf("arn:%s:codedeploy:%s:%s:deploymentgroup:%s/%s", awscloud.PartitionForBoundary(boundary), boundary.Region, boundary.AccountID, application, group)
}

var _ apiClient = (*awscodedeploy.Client)(nil)

var _ cdservice.Client = (*Client)(nil)
