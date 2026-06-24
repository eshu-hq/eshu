// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsappsync "github.com/aws/aws-sdk-go-v2/service/appsync"
	awsappsynctypes "github.com/aws/aws-sdk-go-v2/service/appsync/types"

	appsyncservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/appsync"
)

// listGraphQLAPIs paginates ListGraphqlApis and maps each API's control-plane
// metadata.
func (c *Client) listGraphQLAPIs(ctx context.Context) ([]appsyncservice.GraphQLAPI, error) {
	var apis []appsyncservice.GraphQLAPI
	var nextToken *string
	for {
		var page *awsappsync.ListGraphqlApisOutput
		err := c.recordAPICall(ctx, "ListGraphqlApis", func(callCtx context.Context) error {
			var err error
			page, err = c.api.ListGraphqlApis(callCtx, &awsappsync.ListGraphqlApisInput{
				MaxResults: listPageSize,
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return apis, nil
		}
		for _, api := range page.GraphqlApis {
			apis = append(apis, mapGraphQLAPI(api))
		}
		nextToken = page.NextToken
		if !hasNextToken(nextToken) {
			return apis, nil
		}
	}
}

// listDataSources paginates ListDataSources for one API.
func (c *Client) listDataSources(ctx context.Context, apiID string) ([]appsyncservice.DataSource, error) {
	var dataSources []appsyncservice.DataSource
	var nextToken *string
	for {
		var page *awsappsync.ListDataSourcesOutput
		err := c.recordAPICall(ctx, "ListDataSources", func(callCtx context.Context) error {
			var err error
			page, err = c.api.ListDataSources(callCtx, &awsappsync.ListDataSourcesInput{
				ApiId:      aws.String(apiID),
				MaxResults: listPageSize,
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return dataSources, nil
		}
		for _, ds := range page.DataSources {
			dataSources = append(dataSources, mapDataSource(ds))
		}
		nextToken = page.NextToken
		if !hasNextToken(nextToken) {
			return dataSources, nil
		}
	}
}

// listFunctions paginates ListFunctions for one API. The function code body and
// mapping templates returned by AWS are never mapped.
func (c *Client) listFunctions(ctx context.Context, apiID string) ([]appsyncservice.Function, error) {
	var functions []appsyncservice.Function
	var nextToken *string
	for {
		var page *awsappsync.ListFunctionsOutput
		err := c.recordAPICall(ctx, "ListFunctions", func(callCtx context.Context) error {
			var err error
			page, err = c.api.ListFunctions(callCtx, &awsappsync.ListFunctionsInput{
				ApiId:      aws.String(apiID),
				MaxResults: listPageSize,
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return functions, nil
		}
		for _, function := range page.Functions {
			functions = append(functions, mapFunction(function))
		}
		nextToken = page.NextToken
		if !hasNextToken(nextToken) {
			return functions, nil
		}
	}
}

// listAPIKeys paginates ListApiKeys for one API. AppSync ListApiKeys never
// returns the key value, and the scanner-owned APIKey type has no field for it.
func (c *Client) listAPIKeys(ctx context.Context, apiID string) ([]appsyncservice.APIKey, error) {
	var keys []appsyncservice.APIKey
	var nextToken *string
	for {
		var page *awsappsync.ListApiKeysOutput
		err := c.recordAPICall(ctx, "ListApiKeys", func(callCtx context.Context) error {
			var err error
			page, err = c.api.ListApiKeys(callCtx, &awsappsync.ListApiKeysInput{
				ApiId:      aws.String(apiID),
				MaxResults: listPageSize,
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return keys, nil
		}
		for _, key := range page.ApiKeys {
			keys = append(keys, mapAPIKey(key))
		}
		nextToken = page.NextToken
		if !hasNextToken(nextToken) {
			return keys, nil
		}
	}
}

// listResolversForTypes paginates ListResolvers for each provided type name.
// ListResolvers requires a type name, so the caller supplies the type names
// listed once (without reading any type definition body). The resolver
// request/response mapping templates and JS code returned by AWS are never
// mapped.
func (c *Client) listResolversForTypes(ctx context.Context, apiID string, typeNames []string) ([]appsyncservice.Resolver, error) {
	var resolvers []appsyncservice.Resolver
	for _, typeName := range typeNames {
		typeResolvers, err := c.listResolversForType(ctx, apiID, typeName)
		if err != nil {
			return nil, err
		}
		resolvers = append(resolvers, typeResolvers...)
	}
	return resolvers, nil
}

func (c *Client) listResolversForType(ctx context.Context, apiID, typeName string) ([]appsyncservice.Resolver, error) {
	var resolvers []appsyncservice.Resolver
	var nextToken *string
	for {
		var page *awsappsync.ListResolversOutput
		err := c.recordAPICall(ctx, "ListResolvers", func(callCtx context.Context) error {
			var err error
			page, err = c.api.ListResolvers(callCtx, &awsappsync.ListResolversInput{
				ApiId:      aws.String(apiID),
				TypeName:   aws.String(typeName),
				MaxResults: listPageSize,
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return resolvers, nil
		}
		for _, resolver := range page.Resolvers {
			resolvers = append(resolvers, mapResolver(resolver))
		}
		nextToken = page.NextToken
		if !hasNextToken(nextToken) {
			return resolvers, nil
		}
	}
}

// listTypeNames paginates ListTypes and returns the type names only. The type
// Definition body (SDL) is never read.
func (c *Client) listTypeNames(ctx context.Context, apiID string) ([]string, error) {
	var names []string
	var nextToken *string
	for {
		var page *awsappsync.ListTypesOutput
		err := c.recordAPICall(ctx, "ListTypes", func(callCtx context.Context) error {
			var err error
			page, err = c.api.ListTypes(callCtx, &awsappsync.ListTypesInput{
				ApiId:      aws.String(apiID),
				Format:     awsappsynctypes.TypeDefinitionFormatSdl,
				MaxResults: listPageSize,
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return names, nil
		}
		for _, typ := range page.Types {
			if name := strings.TrimSpace(aws.ToString(typ.Name)); name != "" {
				names = append(names, name)
			}
		}
		nextToken = page.NextToken
		if !hasNextToken(nextToken) {
			return names, nil
		}
	}
}

// schemaMetadata returns the schema creation status and the bounded type count
// for one API. It never calls GetIntrospectionSchema and never reads the SDL
// body. The type names are listed once by the caller and passed in. When the API
// has no types and no schema-creation status, it returns nil.
func (c *Client) schemaMetadata(ctx context.Context, apiID string, typeNames []string) (*appsyncservice.SchemaMetadata, error) {
	var status *awsappsync.GetSchemaCreationStatusOutput
	err := c.recordAPICall(ctx, "GetSchemaCreationStatus", func(callCtx context.Context) error {
		var statusErr error
		status, statusErr = c.api.GetSchemaCreationStatus(callCtx, &awsappsync.GetSchemaCreationStatusInput{
			ApiId: aws.String(apiID),
		})
		return statusErr
	})
	if err != nil {
		return nil, err
	}
	statusValue := ""
	if status != nil {
		statusValue = string(status.Status)
	}
	if len(typeNames) == 0 && strings.TrimSpace(statusValue) == "" {
		return nil, nil
	}
	return &appsyncservice.SchemaMetadata{
		Status:    statusValue,
		TypeCount: len(typeNames),
	}, nil
}

// hasNextToken reports whether a pagination token should drive another page.
// AWS returns nil or empty to signal the final page; treating empty as
// continuation would loop forever.
func hasNextToken(token *string) bool {
	return strings.TrimSpace(aws.ToString(token)) != ""
}
