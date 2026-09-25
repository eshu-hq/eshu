// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"reflect"
	"strings"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsappsync "github.com/aws/aws-sdk-go-v2/service/appsync"
	awsappsynctypes "github.com/aws/aws-sdk-go-v2/service/appsync/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// TestAppSyncAPIExcludesForbiddenMethods proves the adapter's accepted SDK
// surface cannot reach mapping-template evaluation, code evaluation, schema-body
// reads, schema-creation, introspection, or mutation operations. Adding any such
// method to the interface fails the build's contract test before a scan can call
// it.
func TestAppSyncAPIExcludesForbiddenMethods(t *testing.T) {
	apiType := reflect.TypeOf((*appsyncAPI)(nil)).Elem()
	forbidden := []string{
		"EvaluateMappingTemplate",
		"EvaluateCode",
		"GetIntrospectionSchema",
		"StartSchemaCreation",
		"GetDataSourceIntrospection",
		"GetResolver",
		"GetFunction",
		"GetGraphqlApi",
		"GetDataSource",
		"CreateGraphqlApi",
		"UpdateGraphqlApi",
		"DeleteGraphqlApi",
		"CreateResolver",
		"UpdateResolver",
		"DeleteResolver",
		"CreateDataSource",
		"UpdateDataSource",
		"DeleteDataSource",
		"CreateFunction",
		"UpdateFunction",
		"DeleteFunction",
		"CreateApiKey",
		"UpdateApiKey",
		"DeleteApiKey",
		"CreateType",
		"UpdateType",
		"DeleteType",
	}
	for _, method := range forbidden {
		if _, ok := apiType.MethodByName(method); ok {
			t.Fatalf("appsyncAPI declares forbidden AppSync method %q; the adapter must stay metadata-only", method)
		}
	}
}

func TestSnapshotMapsMetadataAndExcludesPayloads(t *testing.T) {
	fake := &fakeAPI{
		apis: []awsappsynctypes.GraphqlApi{{
			ApiId:              awsv2.String("api-1"),
			Arn:                awsv2.String("arn:aws:appsync:us-east-1:123456789012:apis/api-1"),
			Name:               awsv2.String("orders"),
			AuthenticationType: awsappsynctypes.AuthenticationTypeAmazonCognitoUserPools,
			XrayEnabled:        true,
			UserPoolConfig: &awsappsynctypes.UserPoolConfig{
				UserPoolId: awsv2.String("us-east-1_abc123"),
				AwsRegion:  awsv2.String("us-east-1"),
			},
			OpenIDConnectConfig: &awsappsynctypes.OpenIDConnectConfig{
				Issuer: awsv2.String("https://issuer.example.com"),
			},
			LogConfig: &awsappsynctypes.LogConfig{
				FieldLogLevel:         awsappsynctypes.FieldLogLevelError,
				CloudWatchLogsRoleArn: awsv2.String("arn:aws:iam::123456789012:role/logs"),
			},
		}},
		dataSources: []awsappsynctypes.DataSource{{
			Name:          awsv2.String("orders-lambda"),
			DataSourceArn: awsv2.String("arn:aws:appsync:us-east-1:123456789012:apis/api-1/datasources/orders-lambda"),
			Type:          awsappsynctypes.DataSourceTypeAwsLambda,
			LambdaConfig:  &awsappsynctypes.LambdaDataSourceConfig{LambdaFunctionArn: awsv2.String("arn:aws:lambda:us-east-1:123456789012:function:orders")},
		}},
		types: []awsappsynctypes.Type{
			{Name: awsv2.String("Query"), Definition: awsv2.String("type Query { getOrder: Order }")},
			{Name: awsv2.String("Order"), Definition: awsv2.String("type Order { id: ID! }")},
		},
		resolversByType: map[string][]awsappsynctypes.Resolver{
			"Query": {{
				TypeName:                awsv2.String("Query"),
				FieldName:               awsv2.String("getOrder"),
				Kind:                    awsappsynctypes.ResolverKindUnit,
				DataSourceName:          awsv2.String("orders-lambda"),
				ResolverArn:             awsv2.String("arn:aws:appsync:us-east-1:123456789012:apis/api-1/types/Query/resolvers/getOrder"),
				RequestMappingTemplate:  awsv2.String("#set($x = $ctx.identity.sub)"),
				ResponseMappingTemplate: awsv2.String("$util.toJson($ctx.result)"),
				Code:                    awsv2.String("export function request(ctx){ return {} }"),
			}},
		},
		functions: []awsappsynctypes.FunctionConfiguration{{
			FunctionId:              awsv2.String("func-1"),
			Name:                    awsv2.String("fetchOrder"),
			FunctionArn:             awsv2.String("arn:aws:appsync:us-east-1:123456789012:apis/api-1/functions/func-1"),
			DataSourceName:          awsv2.String("orders-lambda"),
			Code:                    awsv2.String("export function request(ctx){ return {} }"),
			RequestMappingTemplate:  awsv2.String("#set($y = 1)"),
			ResponseMappingTemplate: awsv2.String("$util.toJson($ctx.result)"),
			Runtime:                 &awsappsynctypes.AppSyncRuntime{Name: awsappsynctypes.RuntimeNameAppsyncJs, RuntimeVersion: awsv2.String("1.0.0")},
		}},
		apiKeys: []awsappsynctypes.ApiKey{{
			Id:          awsv2.String("da2-secretvalue"),
			Description: awsv2.String("default"),
			Expires:     1893456000,
		}},
		schemaStatus: awsappsynctypes.SchemaStatusSuccess,
	}

	client := &Client{api: fake, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.APIs) != 1 {
		t.Fatalf("APIs = %d, want 1", len(snapshot.APIs))
	}
	api := snapshot.APIs[0]
	if len(api.UserPools) != 1 || api.UserPools[0].UserPoolID != "us-east-1_abc123" {
		t.Fatalf("user pools = %#v, want bare pool id us-east-1_abc123", api.UserPools)
	}
	if len(api.OIDCIssuers) != 1 || api.OIDCIssuers[0] != "https://issuer.example.com" {
		t.Fatalf("oidc issuers = %#v", api.OIDCIssuers)
	}
	if len(api.DataSources) != 1 || api.DataSources[0].LambdaFunctionARN != "arn:aws:lambda:us-east-1:123456789012:function:orders" {
		t.Fatalf("data sources = %#v", api.DataSources)
	}
	if len(api.Resolvers) != 1 {
		t.Fatalf("resolvers = %d, want 1", len(api.Resolvers))
	}
	if len(api.Functions) != 1 || api.Functions[0].RuntimeName != "APPSYNC_JS" {
		t.Fatalf("functions = %#v", api.Functions)
	}
	if api.Schema == nil || api.Schema.TypeCount != 2 || api.Schema.Status != "SUCCESS" {
		t.Fatalf("schema = %#v, want type_count 2 status SUCCESS", api.Schema)
	}
	if len(api.APIKeys) != 1 || api.APIKeys[0].ID != "da2-secretvalue" {
		t.Fatalf("api keys = %#v", api.APIKeys)
	}

	// The mapped resolver, function, and API key must carry no template, code,
	// SDL, or key-value body. The scanner-owned types have no such field, so a
	// reflection scan of the mapped values proves the payloads are unreachable.
	assertNoForbiddenStringValue(t, api.Resolvers[0], "#set", "$util", "export function")
	assertNoForbiddenStringValue(t, api.Functions[0], "#set", "$util", "export function")
	assertNoForbiddenStringValue(t, *api.Schema, "type Query", "type Order")
}

// TestSnapshotStopsPaginationOnNilPage proves a nil page terminates a paginated
// list rather than looping.
func TestSnapshotStopsPaginationOnNilPage(t *testing.T) {
	fake := &fakeAPI{returnNilAPIPage: true}
	client := &Client{api: fake, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.APIs) != 0 {
		t.Fatalf("APIs = %d, want 0", len(snapshot.APIs))
	}
}

// TestSnapshotPaginatesGraphQLAPIs proves pagination follows nextToken and stops
// when the token is empty.
func TestSnapshotPaginatesGraphQLAPIs(t *testing.T) {
	fake := &fakeAPI{
		apiPages: [][]awsappsynctypes.GraphqlApi{
			{{ApiId: awsv2.String("api-1"), Name: awsv2.String("a")}},
			{{ApiId: awsv2.String("api-2"), Name: awsv2.String("b")}},
		},
		schemaStatus: awsappsynctypes.SchemaStatusNotApplicable,
	}
	client := &Client{api: fake, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.APIs) != 2 {
		t.Fatalf("APIs = %d, want 2", len(snapshot.APIs))
	}
}

func assertNoForbiddenStringValue(t *testing.T, value any, needles ...string) {
	t.Helper()
	v := reflect.ValueOf(value)
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if field.Kind() != reflect.String {
			continue
		}
		lowered := strings.ToLower(field.String())
		for _, needle := range needles {
			if strings.Contains(lowered, strings.ToLower(needle)) {
				t.Fatalf("%s.%s carries forbidden payload %q", v.Type().Name(), v.Type().Field(i).Name, needle)
			}
		}
	}
}

func testBoundary() aws.Boundary {
	return aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceAppSync}
}

// fakeAPI is a minimal in-memory appsyncAPI. It returns a single page per list
// call (or two pages for the explicit pagination test).
type fakeAPI struct {
	apis             []awsappsynctypes.GraphqlApi
	apiPages         [][]awsappsynctypes.GraphqlApi
	apiPageIndex     int
	returnNilAPIPage bool
	dataSources      []awsappsynctypes.DataSource
	types            []awsappsynctypes.Type
	resolversByType  map[string][]awsappsynctypes.Resolver
	functions        []awsappsynctypes.FunctionConfiguration
	apiKeys          []awsappsynctypes.ApiKey
	schemaStatus     awsappsynctypes.SchemaStatus
}

func (f *fakeAPI) ListGraphqlApis(_ context.Context, _ *awsappsync.ListGraphqlApisInput, _ ...func(*awsappsync.Options)) (*awsappsync.ListGraphqlApisOutput, error) {
	if f.returnNilAPIPage {
		return nil, nil
	}
	if len(f.apiPages) > 0 {
		page := f.apiPages[f.apiPageIndex]
		out := &awsappsync.ListGraphqlApisOutput{GraphqlApis: page}
		if f.apiPageIndex < len(f.apiPages)-1 {
			out.NextToken = awsv2.String("next")
			f.apiPageIndex++
		}
		return out, nil
	}
	return &awsappsync.ListGraphqlApisOutput{GraphqlApis: f.apis}, nil
}

func (f *fakeAPI) ListDataSources(_ context.Context, _ *awsappsync.ListDataSourcesInput, _ ...func(*awsappsync.Options)) (*awsappsync.ListDataSourcesOutput, error) {
	return &awsappsync.ListDataSourcesOutput{DataSources: f.dataSources}, nil
}

func (f *fakeAPI) ListTypes(_ context.Context, _ *awsappsync.ListTypesInput, _ ...func(*awsappsync.Options)) (*awsappsync.ListTypesOutput, error) {
	return &awsappsync.ListTypesOutput{Types: f.types}, nil
}

func (f *fakeAPI) ListResolvers(_ context.Context, in *awsappsync.ListResolversInput, _ ...func(*awsappsync.Options)) (*awsappsync.ListResolversOutput, error) {
	return &awsappsync.ListResolversOutput{Resolvers: f.resolversByType[awsv2.ToString(in.TypeName)]}, nil
}

func (f *fakeAPI) ListFunctions(_ context.Context, _ *awsappsync.ListFunctionsInput, _ ...func(*awsappsync.Options)) (*awsappsync.ListFunctionsOutput, error) {
	return &awsappsync.ListFunctionsOutput{Functions: f.functions}, nil
}

func (f *fakeAPI) ListApiKeys(_ context.Context, _ *awsappsync.ListApiKeysInput, _ ...func(*awsappsync.Options)) (*awsappsync.ListApiKeysOutput, error) {
	return &awsappsync.ListApiKeysOutput{ApiKeys: f.apiKeys}, nil
}

func (f *fakeAPI) GetSchemaCreationStatus(_ context.Context, _ *awsappsync.GetSchemaCreationStatusInput, _ ...func(*awsappsync.Options)) (*awsappsync.GetSchemaCreationStatusOutput, error) {
	return &awsappsync.GetSchemaCreationStatusOutput{Status: f.schemaStatus}, nil
}
