// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceAppSync identifies the regional AWS AppSync metadata scan slice
	// covering GraphQL APIs, data sources, resolvers, functions, schema
	// metadata, and API key metadata.
	ServiceAppSync = "appsync"
)

const (
	// ResourceTypeAppSyncGraphQLAPI identifies an AppSync GraphQL API.
	ResourceTypeAppSyncGraphQLAPI = "aws_appsync_graphql_api"
	// ResourceTypeAppSyncDataSource identifies an AppSync data source.
	ResourceTypeAppSyncDataSource = "aws_appsync_datasource"
	// ResourceTypeAppSyncResolver identifies an AppSync resolver attached to a
	// GraphQL type field.
	ResourceTypeAppSyncResolver = "aws_appsync_resolver"
	// ResourceTypeAppSyncFunction identifies an AppSync pipeline function.
	ResourceTypeAppSyncFunction = "aws_appsync_function"
	// ResourceTypeAppSyncSchema identifies AppSync schema metadata for one API.
	ResourceTypeAppSyncSchema = "aws_appsync_schema"
	// ResourceTypeAppSyncAPIKey identifies AppSync API key metadata. The key
	// value is never observed or persisted.
	ResourceTypeAppSyncAPIKey = "aws_appsync_api_key" // #nosec G101 -- resource-type identifier for AppSync API key metadata, not a credential value
)

const (
	// RelationshipAppSyncAPIHasDataSource records data-source membership on an
	// AppSync GraphQL API.
	RelationshipAppSyncAPIHasDataSource = "appsync_api_has_data_source"
	// RelationshipAppSyncResolverUsesDataSource records the data source a
	// resolver invokes.
	RelationshipAppSyncResolverUsesDataSource = "appsync_resolver_uses_data_source"
	// RelationshipAppSyncFunctionUsesDataSource records the data source a
	// pipeline function invokes.
	RelationshipAppSyncFunctionUsesDataSource = "appsync_function_uses_data_source"
	// RelationshipAppSyncDataSourceTargetsResource records the backing AWS
	// resource a data source connects to (Lambda, DynamoDB table, OpenSearch
	// domain, HTTP endpoint, or RDS cluster).
	RelationshipAppSyncDataSourceTargetsResource = "appsync_data_source_targets_resource"
	// RelationshipAppSyncAPIUsesUserPool records the Cognito user pool a GraphQL
	// API authenticates against.
	RelationshipAppSyncAPIUsesUserPool = "appsync_api_uses_user_pool"
	// RelationshipAppSyncAPIUsesOIDCIssuer records the OpenID Connect issuer a
	// GraphQL API authenticates against.
	RelationshipAppSyncAPIUsesOIDCIssuer = "appsync_api_uses_oidc_issuer"
)

const (
	// AppSyncDataSourceTargetTypeOpenSearch labels an AppSync OpenSearch data
	// source target. AppSync reports an endpoint URL rather than a domain ARN,
	// and no OpenSearch scanner publishes a canonical node, so the edge carries
	// the endpoint as join evidence under this target type.
	AppSyncDataSourceTargetTypeOpenSearch = "aws_opensearch_domain"
	// AppSyncDataSourceTargetTypeHTTPEndpoint labels an AppSync HTTP data source
	// target. The target is an external endpoint URL, not an AWS resource ARN.
	AppSyncDataSourceTargetTypeHTTPEndpoint = "http_endpoint"
	// AppSyncOIDCIssuerTargetType labels the OpenID Connect issuer an AppSync API
	// trusts. The issuer is an external URL, not an AWS resource ARN.
	AppSyncOIDCIssuerTargetType = "oidc_issuer"
)
