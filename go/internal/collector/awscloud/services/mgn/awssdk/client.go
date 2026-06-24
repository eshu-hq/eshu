// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmgn "github.com/aws/aws-sdk-go-v2/service/mgn"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	mgnservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/mgn"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS Application Migration Service
// API the adapter calls. It is deliberately limited to the source-server,
// application, launch-configuration, and job control-plane reads. It exposes no
// replication-configuration read (which carries staging credentials), no
// replication-template read, no replication-agent or connector mutation, and no
// Create/Update/Delete/Start/Stop/Terminate operation, so the adapter cannot
// read replication secrets or write MGN state. The exclusion_test reflects over
// this interface to enforce that contract at build time.
type apiClient interface {
	DescribeSourceServers(
		context.Context,
		*awsmgn.DescribeSourceServersInput,
		...func(*awsmgn.Options),
	) (*awsmgn.DescribeSourceServersOutput, error)
	ListApplications(
		context.Context,
		*awsmgn.ListApplicationsInput,
		...func(*awsmgn.Options),
	) (*awsmgn.ListApplicationsOutput, error)
	GetLaunchConfiguration(
		context.Context,
		*awsmgn.GetLaunchConfigurationInput,
		...func(*awsmgn.Options),
	) (*awsmgn.GetLaunchConfigurationOutput, error)
	DescribeJobs(
		context.Context,
		*awsmgn.DescribeJobsInput,
		...func(*awsmgn.Options),
	) (*awsmgn.DescribeJobsOutput, error)
}

// Client adapts AWS SDK Application Migration Service control-plane calls into
// scanner-owned metadata. It never reads replication-agent credentials,
// replication configuration secrets, or replicated disk contents, and never
// calls a mutation or replication-control API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an MGN SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsmgn.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns MGN application, source server, launch configuration, and job
// metadata visible to the configured AWS credentials. Replication-agent
// credentials, replication configuration secrets, and replicated disk contents
// are never read.
func (c *Client) Snapshot(ctx context.Context) (mgnservice.Snapshot, error) {
	applications, err := c.listApplications(ctx)
	if err != nil {
		return mgnservice.Snapshot{}, err
	}
	servers, err := c.describeSourceServers(ctx)
	if err != nil {
		return mgnservice.Snapshot{}, err
	}
	for i := range servers {
		config, err := c.getLaunchConfiguration(ctx, servers[i].SourceServerID)
		if err != nil {
			return mgnservice.Snapshot{}, err
		}
		servers[i].LaunchConfiguration = config
	}
	jobs, err := c.describeJobs(ctx)
	if err != nil {
		return mgnservice.Snapshot{}, err
	}
	return mgnservice.Snapshot{
		Applications:  applications,
		SourceServers: servers,
		Jobs:          jobs,
	}, nil
}

func (c *Client) listApplications(ctx context.Context) ([]mgnservice.Application, error) {
	var applications []mgnservice.Application
	var nextToken *string
	for {
		var page *awsmgn.ListApplicationsOutput
		err := c.recordAPICall(ctx, "ListApplications", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListApplications(callCtx, &awsmgn.ListApplicationsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return applications, nil
		}
		for _, item := range page.Items {
			applications = append(applications, mapApplication(item))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return applications, nil
		}
	}
}

func (c *Client) describeSourceServers(ctx context.Context) ([]mgnservice.SourceServer, error) {
	var servers []mgnservice.SourceServer
	var nextToken *string
	for {
		var page *awsmgn.DescribeSourceServersOutput
		err := c.recordAPICall(ctx, "DescribeSourceServers", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeSourceServers(callCtx, &awsmgn.DescribeSourceServersInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return servers, nil
		}
		for _, item := range page.Items {
			servers = append(servers, mapSourceServer(item))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return servers, nil
		}
	}
}

// getLaunchConfiguration reads the launch configuration for one source server.
// GetLaunchConfiguration is a per-source-server read with no pagination. A
// source server without a launch configuration (for example one not yet ready)
// is recorded as nil rather than failing the scan; only access errors propagate.
func (c *Client) getLaunchConfiguration(
	ctx context.Context,
	sourceServerID string,
) (*mgnservice.LaunchConfiguration, error) {
	sourceServerID = strings.TrimSpace(sourceServerID)
	if sourceServerID == "" {
		return nil, nil
	}
	var output *awsmgn.GetLaunchConfigurationOutput
	err := c.recordAPICall(ctx, "GetLaunchConfiguration", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetLaunchConfiguration(callCtx, &awsmgn.GetLaunchConfigurationInput{
			SourceServerID: aws.String(sourceServerID),
		})
		return err
	})
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	return mapLaunchConfiguration(sourceServerID, output), nil
}

func (c *Client) describeJobs(ctx context.Context) ([]mgnservice.Job, error) {
	var jobs []mgnservice.Job
	var nextToken *string
	for {
		var page *awsmgn.DescribeJobsOutput
		err := c.recordAPICall(ctx, "DescribeJobs", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeJobs(callCtx, &awsmgn.DescribeJobsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return jobs, nil
		}
		for _, item := range page.Items {
			jobs = append(jobs, mapJob(item))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return jobs, nil
		}
	}
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

// isThrottleError reports whether err is an AWS throttle/rate-limit error so the
// shared throttle counter records it without swallowing the failure.
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

// isNotFound reports whether err is an MGN resource-not-found error so a source
// server without a launch configuration is treated as absent metadata rather
// than a scan failure.
func isNotFound(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := apiErr.ErrorCode()
	return code == "ResourceNotFoundException" || strings.Contains(strings.ToLower(code), "notfound")
}

var (
	_ mgnservice.Client = (*Client)(nil)
	_ apiClient         = (*awsmgn.Client)(nil)
)
