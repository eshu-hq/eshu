// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsathena "github.com/aws/aws-sdk-go-v2/service/athena"
	awsathenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	athenaservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/athena"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	listWorkGroupsLimit         int32 = 50
	listDataCatalogsLimit       int32 = 50
	listPreparedStatementsLimit int32 = 100
	listNamedQueriesLimit       int32 = 50
	batchGetNamedQueriesLimit         = 50
)

// apiClient is the bounded subset of the AWS Athena SDK consumed by the
// metadata-only adapter. The interface intentionally omits StartQueryExecution,
// StopQueryExecution, GetQueryResults, GetQueryExecution, ListQueryExecutions,
// GetNamedQuery, CreateNamedQuery, DeleteNamedQuery, UpdateNamedQuery,
// CreatePreparedStatement, UpdatePreparedStatement, DeletePreparedStatement,
// and GetPreparedStatement so the adapter cannot accidentally retrieve SQL
// bodies, result rows, or query history strings.
type apiClient interface {
	ListWorkGroups(context.Context, *awsathena.ListWorkGroupsInput, ...func(*awsathena.Options)) (*awsathena.ListWorkGroupsOutput, error)
	GetWorkGroup(context.Context, *awsathena.GetWorkGroupInput, ...func(*awsathena.Options)) (*awsathena.GetWorkGroupOutput, error)
	ListDataCatalogs(context.Context, *awsathena.ListDataCatalogsInput, ...func(*awsathena.Options)) (*awsathena.ListDataCatalogsOutput, error)
	GetDataCatalog(context.Context, *awsathena.GetDataCatalogInput, ...func(*awsathena.Options)) (*awsathena.GetDataCatalogOutput, error)
	ListPreparedStatements(context.Context, *awsathena.ListPreparedStatementsInput, ...func(*awsathena.Options)) (*awsathena.ListPreparedStatementsOutput, error)
	ListNamedQueries(context.Context, *awsathena.ListNamedQueriesInput, ...func(*awsathena.Options)) (*awsathena.ListNamedQueriesOutput, error)
	BatchGetNamedQuery(context.Context, *awsathena.BatchGetNamedQueryInput, ...func(*awsathena.Options)) (*awsathena.BatchGetNamedQueryOutput, error)
	ListTagsForResource(context.Context, *awsathena.ListTagsForResourceInput, ...func(*awsathena.Options)) (*awsathena.ListTagsForResourceOutput, error)
}

// arnBuilder builds an Athena resource ARN so the adapter can read tags. AWS
// Athena ListTagsForResource expects an ARN-shaped identifier. The builder is a
// field to keep production behavior testable without reaching into AWS account
// metadata at unit-test time.
type arnBuilder func(boundary awscloud.Boundary, name string) string

// Client adapts AWS SDK Athena control-plane calls into the scanner-owned
// metadata-only contract.
type Client struct {
	client         apiClient
	boundary       awscloud.Boundary
	tracer         trace.Tracer
	instruments    *telemetry.Instruments
	workGroupARN   arnBuilder
	dataCatalogARN arnBuilder
}

// NewClient builds an Athena SDK adapter for one claimed AWS boundary. The
// adapter only calls metadata-only Athena APIs; mutation APIs and SQL-body
// fetches are not part of the underlying interface and cannot be called.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:         awsathena.NewFromConfig(config),
		boundary:       boundary,
		tracer:         tracer,
		instruments:    instruments,
		workGroupARN:   workGroupARN,
		dataCatalogARN: dataCatalogARN,
	}
}

// ListWorkGroups returns Athena workgroup metadata for the configured boundary.
// The adapter never reads query result rows or workgroup query history.
func (c *Client) ListWorkGroups(ctx context.Context) ([]athenaservice.WorkGroup, error) {
	var workGroups []athenaservice.WorkGroup
	var nextToken *string
	for {
		var page *awsathena.ListWorkGroupsOutput
		err := c.recordAPICall(ctx, "ListWorkGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListWorkGroups(callCtx, &awsathena.ListWorkGroupsInput{
				MaxResults: aws.Int32(listWorkGroupsLimit),
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return workGroups, nil
		}
		for _, summary := range page.WorkGroups {
			name := strings.TrimSpace(aws.ToString(summary.Name))
			if name == "" {
				continue
			}
			mapped, err := c.workGroupMetadata(ctx, name, summary)
			if err != nil {
				return nil, err
			}
			workGroups = append(workGroups, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return workGroups, nil
		}
	}
}

func (c *Client) workGroupMetadata(
	ctx context.Context,
	name string,
	summary awsathenatypes.WorkGroupSummary,
) (athenaservice.WorkGroup, error) {
	var detail *awsathena.GetWorkGroupOutput
	err := c.recordAPICall(ctx, "GetWorkGroup", func(callCtx context.Context) error {
		var callErr error
		detail, callErr = c.client.GetWorkGroup(callCtx, &awsathena.GetWorkGroupInput{
			WorkGroup: aws.String(name),
		})
		return callErr
	})
	if err != nil {
		return athenaservice.WorkGroup{}, err
	}
	tags, err := c.listResourceTags(ctx, c.workGroupARN(c.boundary, name))
	if err != nil {
		return athenaservice.WorkGroup{}, err
	}
	return mapWorkGroup(name, summary, detail, tags), nil
}

// ListDataCatalogs returns Athena data catalog metadata for the configured
// boundary.
func (c *Client) ListDataCatalogs(ctx context.Context) ([]athenaservice.DataCatalog, error) {
	var catalogs []athenaservice.DataCatalog
	var nextToken *string
	for {
		var page *awsathena.ListDataCatalogsOutput
		err := c.recordAPICall(ctx, "ListDataCatalogs", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListDataCatalogs(callCtx, &awsathena.ListDataCatalogsInput{
				MaxResults: aws.Int32(listDataCatalogsLimit),
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return catalogs, nil
		}
		for _, summary := range page.DataCatalogsSummary {
			name := strings.TrimSpace(aws.ToString(summary.CatalogName))
			if name == "" {
				continue
			}
			mapped, err := c.dataCatalogMetadata(ctx, name)
			if err != nil {
				return nil, err
			}
			catalogs = append(catalogs, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return catalogs, nil
		}
	}
}

func (c *Client) dataCatalogMetadata(ctx context.Context, name string) (athenaservice.DataCatalog, error) {
	var detail *awsathena.GetDataCatalogOutput
	err := c.recordAPICall(ctx, "GetDataCatalog", func(callCtx context.Context) error {
		var callErr error
		detail, callErr = c.client.GetDataCatalog(callCtx, &awsathena.GetDataCatalogInput{
			Name: aws.String(name),
		})
		return callErr
	})
	if err != nil {
		return athenaservice.DataCatalog{}, err
	}
	tags, err := c.listResourceTags(ctx, c.dataCatalogARN(c.boundary, name))
	if err != nil {
		return athenaservice.DataCatalog{}, err
	}
	return mapDataCatalog(name, detail, tags), nil
}

// ListPreparedStatements returns prepared-statement names and last-modified
// timestamps for each workgroup. The adapter never calls GetPreparedStatement
// so the SQL `QueryStatement` body is never read into memory.
func (c *Client) ListPreparedStatements(
	ctx context.Context,
	workGroupNames []string,
) ([]athenaservice.PreparedStatement, error) {
	var statements []athenaservice.PreparedStatement
	for _, raw := range workGroupNames {
		workGroup := strings.TrimSpace(raw)
		if workGroup == "" {
			continue
		}
		var nextToken *string
		for {
			var page *awsathena.ListPreparedStatementsOutput
			err := c.recordAPICall(ctx, "ListPreparedStatements", func(callCtx context.Context) error {
				var callErr error
				page, callErr = c.client.ListPreparedStatements(callCtx, &awsathena.ListPreparedStatementsInput{
					WorkGroup:  aws.String(workGroup),
					MaxResults: aws.Int32(listPreparedStatementsLimit),
					NextToken:  nextToken,
				})
				return callErr
			})
			if err != nil {
				return nil, err
			}
			if page == nil {
				break
			}
			for _, summary := range page.PreparedStatements {
				statements = append(statements, athenaservice.PreparedStatement{
					WorkGroupName:    workGroup,
					StatementName:    strings.TrimSpace(aws.ToString(summary.StatementName)),
					LastModifiedTime: timeValue(summary.LastModifiedTime),
				})
			}
			nextToken = page.NextToken
			if aws.ToString(nextToken) == "" {
				break
			}
		}
	}
	return statements, nil
}

// ListNamedQueries returns named-query metadata for each workgroup. The adapter
// calls ListNamedQueries and BatchGetNamedQuery, copies the safe identity
// fields (id, name, database, workgroup, description) into the scanner type,
// and discards `QueryString` before returning. GetNamedQuery is not part of the
// underlying interface and is never reachable.
func (c *Client) ListNamedQueries(
	ctx context.Context,
	workGroupNames []string,
) ([]athenaservice.NamedQuery, error) {
	var queries []athenaservice.NamedQuery
	for _, raw := range workGroupNames {
		workGroup := strings.TrimSpace(raw)
		if workGroup == "" {
			continue
		}
		ids, err := c.listNamedQueryIDs(ctx, workGroup)
		if err != nil {
			return nil, err
		}
		batched, err := c.batchGetNamedQueries(ctx, ids)
		if err != nil {
			return nil, err
		}
		queries = append(queries, batched...)
	}
	return queries, nil
}

func (c *Client) listNamedQueryIDs(ctx context.Context, workGroup string) ([]string, error) {
	var ids []string
	var nextToken *string
	for {
		var page *awsathena.ListNamedQueriesOutput
		err := c.recordAPICall(ctx, "ListNamedQueries", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListNamedQueries(callCtx, &awsathena.ListNamedQueriesInput{
				WorkGroup:  aws.String(workGroup),
				MaxResults: aws.Int32(listNamedQueriesLimit),
				NextToken:  nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return ids, nil
		}
		ids = append(ids, page.NamedQueryIds...)
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return ids, nil
		}
	}
}

func (c *Client) batchGetNamedQueries(
	ctx context.Context,
	namedQueryIDs []string,
) ([]athenaservice.NamedQuery, error) {
	var queries []athenaservice.NamedQuery
	for start := 0; start < len(namedQueryIDs); start += batchGetNamedQueriesLimit {
		end := start + batchGetNamedQueriesLimit
		if end > len(namedQueryIDs) {
			end = len(namedQueryIDs)
		}
		chunk := namedQueryIDs[start:end]
		var output *awsathena.BatchGetNamedQueryOutput
		err := c.recordAPICall(ctx, "BatchGetNamedQuery", func(callCtx context.Context) error {
			var callErr error
			output, callErr = c.client.BatchGetNamedQuery(callCtx, &awsathena.BatchGetNamedQueryInput{
				NamedQueryIds: chunk,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			continue
		}
		for _, raw := range output.NamedQueries {
			queries = append(queries, mapNamedQuery(raw))
		}
	}
	return queries, nil
}

func (c *Client) listResourceTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var nextToken *string
	tags := map[string]string{}
	for {
		var output *awsathena.ListTagsForResourceOutput
		err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
			var callErr error
			output, callErr = c.client.ListTagsForResource(callCtx, &awsathena.ListTagsForResourceInput{
				ResourceARN: aws.String(resourceARN),
				NextToken:   nextToken,
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

var _ athenaservice.Client = (*Client)(nil)

var _ apiClient = (*awsathena.Client)(nil)
