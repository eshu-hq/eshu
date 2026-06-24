// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdatazone "github.com/aws/aws-sdk-go-v2/service/datazone"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	datazoneservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/datazone"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS DataZone API the adapter
// calls. It is deliberately limited to domain/project/environment/data-source
// list reads, the single GetDomain and GetDataSource describe reads, and the
// resource-tag read. It exposes no GetAsset, GetGlossary, GetGlossaryTerm,
// GetListing, GetSubscription, time-series, or lineage read, and no
// Create/Update/Delete mutation, so the adapter cannot read catalog asset or
// glossary content or write DataZone state. The exclusion_test reflects over
// this interface to enforce that contract at build time.
type apiClient interface {
	ListDomains(
		context.Context,
		*awsdatazone.ListDomainsInput,
		...func(*awsdatazone.Options),
	) (*awsdatazone.ListDomainsOutput, error)
	GetDomain(
		context.Context,
		*awsdatazone.GetDomainInput,
		...func(*awsdatazone.Options),
	) (*awsdatazone.GetDomainOutput, error)
	ListProjects(
		context.Context,
		*awsdatazone.ListProjectsInput,
		...func(*awsdatazone.Options),
	) (*awsdatazone.ListProjectsOutput, error)
	ListEnvironments(
		context.Context,
		*awsdatazone.ListEnvironmentsInput,
		...func(*awsdatazone.Options),
	) (*awsdatazone.ListEnvironmentsOutput, error)
	ListDataSources(
		context.Context,
		*awsdatazone.ListDataSourcesInput,
		...func(*awsdatazone.Options),
	) (*awsdatazone.ListDataSourcesOutput, error)
	GetDataSource(
		context.Context,
		*awsdatazone.GetDataSourceInput,
		...func(*awsdatazone.Options),
	) (*awsdatazone.GetDataSourceOutput, error)
	ListTagsForResource(
		context.Context,
		*awsdatazone.ListTagsForResourceInput,
		...func(*awsdatazone.Options),
	) (*awsdatazone.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK DataZone control-plane calls into scanner-owned
// metadata. It never reads catalog asset or glossary content, never reads
// subscription, time-series, or lineage data, and never calls a mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a DataZone SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsdatazone.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns DataZone domain metadata and the projects, environments, and
// data sources under each domain visible to the configured AWS credentials.
// Catalog asset content, glossaries, subscriptions, time-series data, and
// lineage are never read.
func (c *Client) Snapshot(ctx context.Context) (datazoneservice.Snapshot, error) {
	summaries, err := c.listDomains(ctx)
	if err != nil {
		return datazoneservice.Snapshot{}, err
	}
	domains := make([]datazoneservice.Domain, 0, len(summaries))
	for _, summary := range summaries {
		domain, err := c.describeDomain(ctx, summary)
		if err != nil {
			return datazoneservice.Snapshot{}, err
		}
		domain.Projects, err = c.listProjects(ctx, domain.ID)
		if err != nil {
			return datazoneservice.Snapshot{}, err
		}
		domain.Environments, err = c.listEnvironments(ctx, domain.ID, domain.Projects)
		if err != nil {
			return datazoneservice.Snapshot{}, err
		}
		domain.DataSources, err = c.listDataSources(ctx, domain.ID, domain.Projects)
		if err != nil {
			return datazoneservice.Snapshot{}, err
		}
		domains = append(domains, domain)
	}
	return datazoneservice.Snapshot{Domains: domains}, nil
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsdatazone.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awsdatazone.ListTagsForResourceInput{
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

var _ datazoneservice.Client = (*Client)(nil)

var _ apiClient = (*awsdatazone.Client)(nil)
