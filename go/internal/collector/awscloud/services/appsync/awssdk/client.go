// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsappsync "github.com/aws/aws-sdk-go-v2/service/appsync"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	appsyncservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/appsync"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// listPageSize bounds each AppSync list page. The AppSync list APIs cap
// MaxResults at 25, so this is the service maximum.
const listPageSize int32 = 25

// appsyncAPI is the AppSync read surface the adapter uses. It deliberately omits
// EvaluateMappingTemplate, EvaluateCode, GetIntrospectionSchema,
// StartSchemaCreation, GetDataSourceIntrospection, and every Create/Update/Delete
// operation. ListResolvers and ListFunctions responses do include mapping-template
// and code fields, but the mapper deliberately never reads or copies them, so no
// template, code, or key-value body reaches a fact; GetSchemaCreationStatus returns
// only a status string. A reflection test asserts the forbidden methods stay absent
// from this interface, and a mapping test asserts those body fields are not persisted.
type appsyncAPI interface {
	ListGraphqlApis(context.Context, *awsappsync.ListGraphqlApisInput, ...func(*awsappsync.Options)) (*awsappsync.ListGraphqlApisOutput, error)
	ListDataSources(context.Context, *awsappsync.ListDataSourcesInput, ...func(*awsappsync.Options)) (*awsappsync.ListDataSourcesOutput, error)
	ListTypes(context.Context, *awsappsync.ListTypesInput, ...func(*awsappsync.Options)) (*awsappsync.ListTypesOutput, error)
	ListResolvers(context.Context, *awsappsync.ListResolversInput, ...func(*awsappsync.Options)) (*awsappsync.ListResolversOutput, error)
	ListFunctions(context.Context, *awsappsync.ListFunctionsInput, ...func(*awsappsync.Options)) (*awsappsync.ListFunctionsOutput, error)
	ListApiKeys(context.Context, *awsappsync.ListApiKeysInput, ...func(*awsappsync.Options)) (*awsappsync.ListApiKeysOutput, error)
	GetSchemaCreationStatus(context.Context, *awsappsync.GetSchemaCreationStatusInput, ...func(*awsappsync.Options)) (*awsappsync.GetSchemaCreationStatusOutput, error)
}

// Client adapts AWS SDK AppSync read-only calls into scanner-owned metadata. It
// never calls EvaluateMappingTemplate, EvaluateCode, GetIntrospectionSchema, any
// schema-creation, introspection, or mutation API, and never maps the schema SDL
// body, resolver/function mapping templates, function code, or API key values.
type Client struct {
	api         appsyncAPI
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an AppSync SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		api:         awsappsync.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns GraphQL API, data source, resolver, function, schema, and
// API key metadata visible to the configured AWS credentials.
func (c *Client) Snapshot(ctx context.Context) (appsyncservice.Snapshot, error) {
	apis, err := c.listGraphQLAPIs(ctx)
	if err != nil {
		return appsyncservice.Snapshot{}, err
	}
	for i := range apis {
		if err := c.hydrateAPI(ctx, &apis[i]); err != nil {
			return appsyncservice.Snapshot{}, err
		}
	}
	return appsyncservice.Snapshot{APIs: apis}, nil
}

// hydrateAPI loads the data sources, resolvers, functions, schema metadata, and
// API keys for one GraphQL API.
func (c *Client) hydrateAPI(ctx context.Context, api *appsyncservice.GraphQLAPI) error {
	apiID := strings.TrimSpace(api.ID)
	if apiID == "" {
		return nil
	}
	var err error
	if api.DataSources, err = c.listDataSources(ctx, apiID); err != nil {
		return err
	}
	// List type names once and reuse them for both resolver enumeration (each
	// ListResolvers call requires a type name) and schema type-count metadata.
	typeNames, err := c.listTypeNames(ctx, apiID)
	if err != nil {
		return err
	}
	if api.Resolvers, err = c.listResolversForTypes(ctx, apiID, typeNames); err != nil {
		return err
	}
	if api.Functions, err = c.listFunctions(ctx, apiID); err != nil {
		return err
	}
	if api.Schema, err = c.schemaMetadata(ctx, apiID, typeNames); err != nil {
		return err
	}
	if api.APIKeys, err = c.listAPIKeys(ctx, apiID); err != nil {
		return err
	}
	return nil
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
	if err == nil {
		return false
	}
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := strings.ToLower(apiErr.ErrorCode())
	return strings.Contains(code, "throttl") ||
		strings.Contains(code, "rate") ||
		code == "requestlimitexceeded" ||
		code == "toomanyrequestsexception" ||
		code == "slowdown"
}

var _ appsyncservice.Client = (*Client)(nil)

var _ appsyncAPI = (*awsappsync.Client)(nil)
