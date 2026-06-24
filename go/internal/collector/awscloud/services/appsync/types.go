// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appsync

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client returns one bounded AppSync metadata snapshot for a claimed account
// and region. Runtime adapters translate AWS SDK responses into the
// scanner-owned types below.
//
// The interface deliberately exposes no mapping-template evaluation
// (EvaluateMappingTemplate), no code evaluation (EvaluateCode), no schema-body
// reads (GetIntrospectionSchema), and no mutation operations. A reflection test
// in this package and in the SDK adapter enforces that these forbidden methods
// stay absent.
type Client interface {
	Snapshot(context.Context) (Snapshot, error)
}

// Snapshot is the scanner-owned metadata view of AppSync GraphQL APIs, data
// sources, resolvers, functions, schema metadata, and API key metadata.
type Snapshot struct {
	Warnings []awscloud.WarningObservation
	APIs     []GraphQLAPI
}

// GraphQLAPI is the metadata-only scanner view of an AppSync GraphQL API. It
// carries control-plane configuration only; the schema SDL, resolver mapping
// templates, function code, and API key values are never present on any nested
// type.
type GraphQLAPI struct {
	ID                 string
	ARN                string
	Name               string
	AuthenticationType string
	XrayEnabled        bool
	APIType            string
	Visibility         string
	WAFWebACLARN       string
	LogConfig          *LogConfig
	UserPools          []UserPoolRef
	OIDCIssuers        []string
	Tags               map[string]string
	DataSources        []DataSource
	Resolvers          []Resolver
	Functions          []Function
	Schema             *SchemaMetadata
	APIKeys            []APIKey
}

// LogConfig summarizes the CloudWatch Logs configuration of a GraphQL API. It
// carries the bounded logging knobs and the publishing role ARN only.
type LogConfig struct {
	FieldLogLevel         string
	CloudWatchLogsRoleARN string
	ExcludeVerboseContent bool
}

// UserPoolRef references a Cognito user pool a GraphQL API authenticates
// against. UserPoolID is the bare pool ID, which matches the resource_id the
// Cognito scanner publishes for the user pool node.
type UserPoolRef struct {
	UserPoolID string
	AwsRegion  string
}

// DataSource is the metadata-only scanner view of an AppSync data source. It
// records the backing resource target without inlining any credentials. The
// service-role ARN and target identity are control-plane metadata, never secret
// material.
type DataSource struct {
	Name               string
	ARN                string
	Type               string
	ServiceRoleARN     string
	LambdaFunctionARN  string
	DynamoDBTableName  string
	DynamoDBAwsRegion  string
	OpenSearchEndpoint string
	HTTPEndpoint       string
	RDSClusterARN      string
	RDSAwsRegion       string
}

// Resolver is the metadata-only scanner view of an AppSync resolver. It records
// the type name, field name, kind, and data-source name. The request/response
// mapping template bodies and the JS resolver code are never present on this
// type.
type Resolver struct {
	TypeName            string
	FieldName           string
	Kind                string
	DataSourceName      string
	ARN                 string
	RuntimeName         string
	RuntimeVersion      string
	PipelineFunctionIDs []string
}

// Function is the metadata-only scanner view of an AppSync pipeline function.
// It records the name, data-source name, and runtime. The function code body
// and request/response mapping templates are never present on this type.
type Function struct {
	ID              string
	Name            string
	ARN             string
	DataSourceName  string
	RuntimeName     string
	RuntimeVersion  string
	FunctionVersion string
}

// SchemaMetadata summarizes an AppSync GraphQL schema. It records the creation
// status and the bounded type count only. The schema definition language (SDL)
// body is never present on this type because it exposes the data model and PII
// field names.
type SchemaMetadata struct {
	Status    string
	TypeCount int
}

// APIKey is the metadata-only scanner view of an AppSync API key. It records the
// key ID, description, and expiration. The API key value is never present on
// this type because it is a bearer credential.
type APIKey struct {
	ID          string
	Description string
	Expires     time.Time
	Deletes     time.Time
}
