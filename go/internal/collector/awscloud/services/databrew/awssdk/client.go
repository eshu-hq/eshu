// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdatabrew "github.com/aws/aws-sdk-go-v2/service/databrew"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	databrewservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/databrew"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS Glue DataBrew API the adapter
// calls. It is deliberately limited to the List reads for datasets, recipes,
// jobs, and projects. It exposes no Describe, Create, Update, Delete, Start,
// Stop, Publish, or Send operation, so the adapter cannot fetch recipe step
// expressions, custom SQL query strings, sample data, or mutate DataBrew state.
// The exclusion_test reflects over this interface to enforce that contract at
// build time.
type apiClient interface {
	ListDatasets(
		context.Context,
		*awsdatabrew.ListDatasetsInput,
		...func(*awsdatabrew.Options),
	) (*awsdatabrew.ListDatasetsOutput, error)
	ListRecipes(
		context.Context,
		*awsdatabrew.ListRecipesInput,
		...func(*awsdatabrew.Options),
	) (*awsdatabrew.ListRecipesOutput, error)
	ListJobs(
		context.Context,
		*awsdatabrew.ListJobsInput,
		...func(*awsdatabrew.Options),
	) (*awsdatabrew.ListJobsOutput, error)
	ListProjects(
		context.Context,
		*awsdatabrew.ListProjectsInput,
		...func(*awsdatabrew.Options),
	) (*awsdatabrew.ListProjectsOutput, error)
}

// Client adapts AWS SDK Glue DataBrew control-plane list calls into
// scanner-owned metadata. It never reads recipe step expressions, custom SQL
// query strings, sample data, or any data-plane payload, and never calls a
// mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a DataBrew SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsdatabrew.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns DataBrew dataset, recipe, job, and project metadata visible
// to the configured AWS credentials in one region. Recipe step expressions,
// custom SQL query strings, and sample data are never read.
func (c *Client) Snapshot(ctx context.Context) (databrewservice.Snapshot, error) {
	datasets, err := c.listDatasets(ctx)
	if err != nil {
		return databrewservice.Snapshot{}, err
	}
	recipes, err := c.listRecipes(ctx)
	if err != nil {
		return databrewservice.Snapshot{}, err
	}
	jobs, err := c.listJobs(ctx)
	if err != nil {
		return databrewservice.Snapshot{}, err
	}
	projects, err := c.listProjects(ctx)
	if err != nil {
		return databrewservice.Snapshot{}, err
	}
	return databrewservice.Snapshot{
		Datasets: datasets,
		Recipes:  recipes,
		Jobs:     jobs,
		Projects: projects,
	}, nil
}

func (c *Client) listDatasets(ctx context.Context) ([]databrewservice.Dataset, error) {
	var datasets []databrewservice.Dataset
	var nextToken *string
	for {
		var page *awsdatabrew.ListDatasetsOutput
		err := c.recordAPICall(ctx, "ListDatasets", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListDatasets(callCtx, &awsdatabrew.ListDatasetsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return datasets, nil
		}
		for _, dataset := range page.Datasets {
			datasets = append(datasets, mapDataset(dataset))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return datasets, nil
		}
	}
}

func (c *Client) listRecipes(ctx context.Context) ([]databrewservice.Recipe, error) {
	var recipes []databrewservice.Recipe
	var nextToken *string
	for {
		var page *awsdatabrew.ListRecipesOutput
		err := c.recordAPICall(ctx, "ListRecipes", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListRecipes(callCtx, &awsdatabrew.ListRecipesInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return recipes, nil
		}
		for _, recipe := range page.Recipes {
			recipes = append(recipes, mapRecipe(recipe))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return recipes, nil
		}
	}
}

func (c *Client) listJobs(ctx context.Context) ([]databrewservice.Job, error) {
	var jobs []databrewservice.Job
	var nextToken *string
	for {
		var page *awsdatabrew.ListJobsOutput
		err := c.recordAPICall(ctx, "ListJobs", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListJobs(callCtx, &awsdatabrew.ListJobsInput{
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
		for _, job := range page.Jobs {
			jobs = append(jobs, mapJob(job))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return jobs, nil
		}
	}
}

func (c *Client) listProjects(ctx context.Context) ([]databrewservice.Project, error) {
	var projects []databrewservice.Project
	var nextToken *string
	for {
		var page *awsdatabrew.ListProjectsOutput
		err := c.recordAPICall(ctx, "ListProjects", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListProjects(callCtx, &awsdatabrew.ListProjectsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return projects, nil
		}
		for _, project := range page.Projects {
			projects = append(projects, mapProject(project))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return projects, nil
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

var _ databrewservice.Client = (*Client)(nil)

var _ apiClient = (*awsdatabrew.Client)(nil)
