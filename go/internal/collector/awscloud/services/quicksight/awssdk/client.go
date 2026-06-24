// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsquicksight "github.com/aws/aws-sdk-go-v2/service/quicksight"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	quicksightservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/quicksight"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS QuickSight API the adapter
// calls. It is deliberately limited to list and describe reads for data
// sources, datasets, dashboards, analyses, VPC connections, and resource tags.
// It exposes no Create/Update/Delete mutation, no permissions read, no
// ingestion or job control, and no credential/secret read, so the adapter
// cannot mutate QuickSight state or read credentials. The exclusion_test
// reflects over this interface to enforce that contract at build time.
type apiClient interface {
	ListDataSources(
		context.Context,
		*awsquicksight.ListDataSourcesInput,
		...func(*awsquicksight.Options),
	) (*awsquicksight.ListDataSourcesOutput, error)
	DescribeDataSource(
		context.Context,
		*awsquicksight.DescribeDataSourceInput,
		...func(*awsquicksight.Options),
	) (*awsquicksight.DescribeDataSourceOutput, error)
	ListDataSets(
		context.Context,
		*awsquicksight.ListDataSetsInput,
		...func(*awsquicksight.Options),
	) (*awsquicksight.ListDataSetsOutput, error)
	DescribeDataSet(
		context.Context,
		*awsquicksight.DescribeDataSetInput,
		...func(*awsquicksight.Options),
	) (*awsquicksight.DescribeDataSetOutput, error)
	ListDashboards(
		context.Context,
		*awsquicksight.ListDashboardsInput,
		...func(*awsquicksight.Options),
	) (*awsquicksight.ListDashboardsOutput, error)
	DescribeDashboard(
		context.Context,
		*awsquicksight.DescribeDashboardInput,
		...func(*awsquicksight.Options),
	) (*awsquicksight.DescribeDashboardOutput, error)
	ListAnalyses(
		context.Context,
		*awsquicksight.ListAnalysesInput,
		...func(*awsquicksight.Options),
	) (*awsquicksight.ListAnalysesOutput, error)
	DescribeAnalysis(
		context.Context,
		*awsquicksight.DescribeAnalysisInput,
		...func(*awsquicksight.Options),
	) (*awsquicksight.DescribeAnalysisOutput, error)
	ListVPCConnections(
		context.Context,
		*awsquicksight.ListVPCConnectionsInput,
		...func(*awsquicksight.Options),
	) (*awsquicksight.ListVPCConnectionsOutput, error)
	ListTagsForResource(
		context.Context,
		*awsquicksight.ListTagsForResourceInput,
		...func(*awsquicksight.Options),
	) (*awsquicksight.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK QuickSight control-plane calls into scanner-owned
// metadata. It never reads data-source credentials, connection passwords,
// secret connection parameters, SQL query bodies, or visual definitions, and
// never calls a mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	accountID   string
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a QuickSight SDK adapter for one claimed AWS boundary. Nearly
// every QuickSight API requires the caller's AWS account id, which the adapter
// threads from boundary.AccountID into each call.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsquicksight.NewFromConfig(config),
		boundary:    boundary,
		accountID:   strings.TrimSpace(boundary.AccountID),
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns QuickSight data source, dataset, dashboard, and analysis
// metadata visible to the configured AWS credentials for the boundary account.
// When the account is not signed up for QuickSight the first list call fails
// with an account-scoped not-subscribed error; the adapter maps that single
// case to an empty snapshot with a warning instead of failing the scan. SQL
// bodies, credentials, and visual definitions are never read.
func (c *Client) Snapshot(ctx context.Context) (quicksightservice.Snapshot, error) {
	if c.accountID == "" {
		return quicksightservice.Snapshot{}, errors.New("quicksight adapter requires a boundary account id")
	}

	dataSources, err := c.listDataSources(ctx)
	if err != nil {
		if isNotSubscribed(err) {
			return quicksightservice.Snapshot{Warnings: []awscloud.WarningObservation{c.notSubscribedWarning(err)}}, nil
		}
		return quicksightservice.Snapshot{}, err
	}

	connections, err := c.listVPCConnections(ctx)
	if err != nil {
		return quicksightservice.Snapshot{}, err
	}
	dataSets, err := c.listDataSets(ctx)
	if err != nil {
		return quicksightservice.Snapshot{}, err
	}
	dashboards, err := c.listDashboards(ctx)
	if err != nil {
		return quicksightservice.Snapshot{}, err
	}
	analyses, err := c.listAnalyses(ctx)
	if err != nil {
		return quicksightservice.Snapshot{}, err
	}
	return quicksightservice.Snapshot{
		DataSources:    dataSources,
		DataSets:       dataSets,
		Dashboards:     dashboards,
		Analyses:       analyses,
		VPCConnections: connections,
	}, nil
}

func (c *Client) listDataSources(ctx context.Context) ([]quicksightservice.DataSource, error) {
	var dataSources []quicksightservice.DataSource
	var nextToken *string
	for {
		var page *awsquicksight.ListDataSourcesOutput
		err := c.recordAPICall(ctx, "ListDataSources", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListDataSources(callCtx, &awsquicksight.ListDataSourcesInput{
				AwsAccountId: aws.String(c.accountID),
				NextToken:    nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return dataSources, nil
		}
		for i := range page.DataSources {
			mapped, err := c.mapDataSource(ctx, page.DataSources[i])
			if err != nil {
				return nil, err
			}
			dataSources = append(dataSources, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return dataSources, nil
		}
	}
}

func (c *Client) listVPCConnections(ctx context.Context) (map[string]quicksightservice.VPCConnection, error) {
	connections := map[string]quicksightservice.VPCConnection{}
	var nextToken *string
	for {
		var page *awsquicksight.ListVPCConnectionsOutput
		err := c.recordAPICall(ctx, "ListVPCConnections", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListVPCConnections(callCtx, &awsquicksight.ListVPCConnectionsInput{
				AwsAccountId: aws.String(c.accountID),
				NextToken:    nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return connections, nil
		}
		for i := range page.VPCConnectionSummaries {
			id, resolved := mapVPCConnection(page.VPCConnectionSummaries[i])
			if id == "" {
				continue
			}
			connections[id] = resolved
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return connections, nil
		}
	}
}

func (c *Client) listDataSets(ctx context.Context) ([]quicksightservice.DataSet, error) {
	var dataSets []quicksightservice.DataSet
	var nextToken *string
	for {
		var page *awsquicksight.ListDataSetsOutput
		err := c.recordAPICall(ctx, "ListDataSets", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListDataSets(callCtx, &awsquicksight.ListDataSetsInput{
				AwsAccountId: aws.String(c.accountID),
				NextToken:    nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return dataSets, nil
		}
		for i := range page.DataSetSummaries {
			mapped, err := c.mapDataSet(ctx, page.DataSetSummaries[i])
			if err != nil {
				return nil, err
			}
			dataSets = append(dataSets, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return dataSets, nil
		}
	}
}

func (c *Client) listDashboards(ctx context.Context) ([]quicksightservice.Dashboard, error) {
	var dashboards []quicksightservice.Dashboard
	var nextToken *string
	for {
		var page *awsquicksight.ListDashboardsOutput
		err := c.recordAPICall(ctx, "ListDashboards", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListDashboards(callCtx, &awsquicksight.ListDashboardsInput{
				AwsAccountId: aws.String(c.accountID),
				NextToken:    nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return dashboards, nil
		}
		for i := range page.DashboardSummaryList {
			mapped, err := c.mapDashboard(ctx, page.DashboardSummaryList[i])
			if err != nil {
				return nil, err
			}
			dashboards = append(dashboards, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return dashboards, nil
		}
	}
}

func (c *Client) listAnalyses(ctx context.Context) ([]quicksightservice.Analysis, error) {
	var analyses []quicksightservice.Analysis
	var nextToken *string
	for {
		var page *awsquicksight.ListAnalysesOutput
		err := c.recordAPICall(ctx, "ListAnalyses", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListAnalyses(callCtx, &awsquicksight.ListAnalysesInput{
				AwsAccountId: aws.String(c.accountID),
				NextToken:    nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return analyses, nil
		}
		for i := range page.AnalysisSummaryList {
			mapped, err := c.mapAnalysis(ctx, page.AnalysisSummaryList[i])
			if err != nil {
				return nil, err
			}
			analyses = append(analyses, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return analyses, nil
		}
	}
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsquicksight.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.ListTagsForResource(callCtx, &awsquicksight.ListTagsForResourceInput{
			ResourceArn: aws.String(resourceARN),
		})
		return callErr
	})
	if err != nil {
		// A tag read denied for a single resource must not fail the whole scan;
		// the resource metadata is still valuable without its tags.
		if isAccessDenied(err) || isResourceNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	return tagsFromSDK(output.Tags), nil
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

func (c *Client) notSubscribedWarning(err error) awscloud.WarningObservation {
	return awscloud.WarningObservation{
		Boundary:       c.boundary,
		WarningKind:    "quicksight_not_subscribed",
		ErrorClass:     errorClass(err),
		Message:        "account is not signed up for Amazon QuickSight; no QuickSight resources observed",
		SourceRecordID: "quicksight_not_subscribed:" + c.accountID,
	}
}

var _ quicksightservice.Client = (*Client)(nil)

var _ apiClient = (*awsquicksight.Client)(nil)
