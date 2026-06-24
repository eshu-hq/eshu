// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscodebuild "github.com/aws/aws-sdk-go-v2/service/codebuild"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	cbservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/codebuild"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// batchProjectLimit caps BatchGetProjects input per the AWS contract, which
// accepts at most 100 project names per call. The adapter chunks larger name
// lists so a project-heavy account does not fail the whole scan.
const batchProjectLimit = 100

// batchReportGroupLimit caps BatchGetReportGroups input per the AWS contract,
// which accepts at most 100 report-group ARNs per call.
const batchReportGroupLimit = 100

// batchBuildLimit caps BatchGetBuilds input per the AWS contract, which accepts
// at most 100 build IDs per call.
const batchBuildLimit = 100

// recentBuildLimit bounds how many recent build IDs the adapter resolves per
// scan so the build resource set stays metadata-sized regardless of account
// build history. It matches one ListBuilds page of 100 IDs and one
// BatchGetBuilds call.
const recentBuildLimit = 100

// apiClient is the metadata-only CodeBuild SDK surface the adapter consumes. It
// intentionally omits every mutation API (CreateProject, UpdateProject,
// DeleteProject, StartBuild, StopBuild, RetryBuild, BatchDeleteBuilds,
// ImportSourceCredentials, DeleteSourceCredentials, and report-group/webhook
// mutation) and any source-credential or log-content reader. The reflection
// guard test asserts the omission.
type apiClient interface {
	ListProjects(context.Context, *awscodebuild.ListProjectsInput, ...func(*awscodebuild.Options)) (*awscodebuild.ListProjectsOutput, error)
	BatchGetProjects(context.Context, *awscodebuild.BatchGetProjectsInput, ...func(*awscodebuild.Options)) (*awscodebuild.BatchGetProjectsOutput, error)
	ListReportGroups(context.Context, *awscodebuild.ListReportGroupsInput, ...func(*awscodebuild.Options)) (*awscodebuild.ListReportGroupsOutput, error)
	BatchGetReportGroups(context.Context, *awscodebuild.BatchGetReportGroupsInput, ...func(*awscodebuild.Options)) (*awscodebuild.BatchGetReportGroupsOutput, error)
	ListBuilds(context.Context, *awscodebuild.ListBuildsInput, ...func(*awscodebuild.Options)) (*awscodebuild.ListBuildsOutput, error)
	BatchGetBuilds(context.Context, *awscodebuild.BatchGetBuildsInput, ...func(*awscodebuild.Options)) (*awscodebuild.BatchGetBuildsOutput, error)
}

// Client adapts AWS SDK CodeBuild pagination into scanner-owned metadata. It
// redacts PLAINTEXT environment-variable values before they reach scanner types
// and never reads buildspec bodies, build logs, or source credentials.
type Client struct {
	client       apiClient
	boundary     awscloud.Boundary
	tracer       trace.Tracer
	instruments  *telemetry.Instruments
	redactionKey redact.Key
}

// NewClient builds a CodeBuild SDK adapter for one claimed AWS boundary. The
// redaction key is required so PLAINTEXT environment-variable values never
// persist raw; callers obtain it from the runtime scanner dependencies.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	redactionKey redact.Key,
) *Client {
	return &Client{
		client:       awscodebuild.NewFromConfig(config),
		boundary:     boundary,
		tracer:       tracer,
		instruments:  instruments,
		redactionKey: redactionKey,
	}
}

// ListProjects returns CodeBuild build-project metadata visible to the
// configured credentials. It lists project names then batch-resolves their
// metadata in 100-name chunks. It never fetches buildspec bodies or source
// credentials.
func (c *Client) ListProjects(ctx context.Context) ([]cbservice.Project, error) {
	names, err := c.listProjectNames(ctx)
	if err != nil {
		return nil, err
	}
	var projects []cbservice.Project
	for start := 0; start < len(names); start += batchProjectLimit {
		end := start + batchProjectLimit
		if end > len(names) {
			end = len(names)
		}
		var output *awscodebuild.BatchGetProjectsOutput
		err := c.recordAPICall(ctx, "BatchGetProjects", func(callCtx context.Context) error {
			var callErr error
			output, callErr = c.client.BatchGetProjects(callCtx, &awscodebuild.BatchGetProjectsInput{
				Names: names[start:end],
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			continue
		}
		// Surface per-item resolution failures instead of silently dropping the
		// missing names. A name that lists but does not resolve indicates a
		// permissions or consistency problem the operator must see.
		if len(output.ProjectsNotFound) > 0 {
			return nil, fmt.Errorf("BatchGetProjects could not resolve %d project(s): %s",
				len(output.ProjectsNotFound), strings.Join(output.ProjectsNotFound, ", "))
		}
		for _, project := range output.Projects {
			projects = append(projects, mapProject(project, c.redactionKey))
		}
	}
	return projects, nil
}

func (c *Client) listProjectNames(ctx context.Context) ([]string, error) {
	var names []string
	var token *string
	for {
		var output *awscodebuild.ListProjectsOutput
		err := c.recordAPICall(ctx, "ListProjects", func(callCtx context.Context) error {
			var callErr error
			output, callErr = c.client.ListProjects(callCtx, &awscodebuild.ListProjectsInput{NextToken: token})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			break
		}
		names = append(names, output.Projects...)
		next := strings.TrimSpace(aws.ToString(output.NextToken))
		if next == "" {
			break
		}
		token = output.NextToken
	}
	return names, nil
}

// ListReportGroups returns CodeBuild report-group metadata. It lists report-
// group ARNs then batch-resolves their metadata in 100-ARN chunks.
func (c *Client) ListReportGroups(ctx context.Context) ([]cbservice.ReportGroup, error) {
	arns, err := c.listReportGroupARNs(ctx)
	if err != nil {
		return nil, err
	}
	var groups []cbservice.ReportGroup
	for start := 0; start < len(arns); start += batchReportGroupLimit {
		end := start + batchReportGroupLimit
		if end > len(arns) {
			end = len(arns)
		}
		var output *awscodebuild.BatchGetReportGroupsOutput
		err := c.recordAPICall(ctx, "BatchGetReportGroups", func(callCtx context.Context) error {
			var callErr error
			output, callErr = c.client.BatchGetReportGroups(callCtx, &awscodebuild.BatchGetReportGroupsInput{
				ReportGroupArns: arns[start:end],
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			continue
		}
		if len(output.ReportGroupsNotFound) > 0 {
			return nil, fmt.Errorf("BatchGetReportGroups could not resolve %d report group(s): %s",
				len(output.ReportGroupsNotFound), strings.Join(output.ReportGroupsNotFound, ", "))
		}
		for _, group := range output.ReportGroups {
			groups = append(groups, mapReportGroup(group))
		}
	}
	return groups, nil
}

func (c *Client) listReportGroupARNs(ctx context.Context) ([]string, error) {
	var arns []string
	var token *string
	for {
		var output *awscodebuild.ListReportGroupsOutput
		err := c.recordAPICall(ctx, "ListReportGroups", func(callCtx context.Context) error {
			var callErr error
			output, callErr = c.client.ListReportGroups(callCtx, &awscodebuild.ListReportGroupsInput{NextToken: token})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			break
		}
		arns = append(arns, output.ReportGroups...)
		next := strings.TrimSpace(aws.ToString(output.NextToken))
		if next == "" {
			break
		}
		token = output.NextToken
	}
	return arns, nil
}

// ListRecentBuilds returns the most recent build metadata for the boundary,
// bounded by recentBuildLimit. It lists build IDs newest-first, takes the
// bounded window, then batch-resolves their metadata. It never requests build
// logs; log references are dropped in mapping.
func (c *Client) ListRecentBuilds(ctx context.Context) ([]cbservice.Build, error) {
	ids, err := c.listRecentBuildIDs(ctx)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var builds []cbservice.Build
	for start := 0; start < len(ids); start += batchBuildLimit {
		end := start + batchBuildLimit
		if end > len(ids) {
			end = len(ids)
		}
		var output *awscodebuild.BatchGetBuildsOutput
		err := c.recordAPICall(ctx, "BatchGetBuilds", func(callCtx context.Context) error {
			var callErr error
			output, callErr = c.client.BatchGetBuilds(callCtx, &awscodebuild.BatchGetBuildsInput{
				Ids: ids[start:end],
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			continue
		}
		if len(output.BuildsNotFound) > 0 {
			return nil, fmt.Errorf("BatchGetBuilds could not resolve %d build(s): %s",
				len(output.BuildsNotFound), strings.Join(output.BuildsNotFound, ", "))
		}
		for _, build := range output.Builds {
			builds = append(builds, mapBuild(build))
		}
	}
	return builds, nil
}

// listRecentBuildIDs reads one ListBuilds page (IDs are returned newest-first)
// and bounds the result to recentBuildLimit so the scan stays metadata-sized.
func (c *Client) listRecentBuildIDs(ctx context.Context) ([]string, error) {
	var output *awscodebuild.ListBuildsOutput
	err := c.recordAPICall(ctx, "ListBuilds", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.ListBuilds(callCtx, &awscodebuild.ListBuildsInput{})
		return callErr
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	ids := output.Ids
	if len(ids) > recentBuildLimit {
		ids = ids[:recentBuildLimit]
	}
	return ids, nil
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

var _ apiClient = (*awscodebuild.Client)(nil)

var _ cbservice.Client = (*Client)(nil)
