// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appsync

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceAppSync,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:appsync:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	snapshot Snapshot
	err      error
}

func (c fakeClient) Snapshot(context.Context) (Snapshot, error) {
	return c.snapshot, c.err
}

func sampleAPI() GraphQLAPI {
	return GraphQLAPI{
		ID:                 "api-123",
		ARN:                "arn:aws:appsync:us-east-1:123456789012:apis/api-123",
		Name:               "orders",
		AuthenticationType: "AMAZON_COGNITO_USER_POOLS",
		XrayEnabled:        true,
		APIType:            "GRAPHQL",
		Visibility:         "GLOBAL",
		WAFWebACLARN:       "arn:aws:wafv2:us-east-1:123456789012:regional/webacl/orders/abc",
		LogConfig: &LogConfig{
			FieldLogLevel:         "ERROR",
			CloudWatchLogsRoleARN: "arn:aws:iam::123456789012:role/appsync-logs",
		},
		UserPools:   []UserPoolRef{{UserPoolID: "us-east-1_abc123", AwsRegion: "us-east-1"}},
		OIDCIssuers: []string{"https://issuer.example.com"},
		Tags:        map[string]string{"Environment": "prod"},
		DataSources: []DataSource{
			{Name: "orders-lambda", ARN: "arn:aws:appsync:us-east-1:123456789012:apis/api-123/datasources/orders-lambda", Type: "AWS_LAMBDA", LambdaFunctionARN: "arn:aws:lambda:us-east-1:123456789012:function:orders"},
			{Name: "orders-table", Type: "AMAZON_DYNAMODB", DynamoDBTableName: "Orders", DynamoDBAwsRegion: "us-east-1"},
			{Name: "search", Type: "AMAZON_OPENSEARCH_SERVICE", OpenSearchEndpoint: "https://search-orders.us-east-1.es.amazonaws.com"},
			{Name: "legacy-http", Type: "HTTP", HTTPEndpoint: "https://api.partner.example.com"},
			{Name: "orders-rds", Type: "RELATIONAL_DATABASE", RDSClusterARN: "arn:aws:rds:us-east-1:123456789012:cluster:orders", RDSAwsRegion: "us-east-1"},
		},
		Resolvers: []Resolver{{
			TypeName:       "Query",
			FieldName:      "getOrder",
			Kind:           "UNIT",
			DataSourceName: "orders-table",
			ARN:            "arn:aws:appsync:us-east-1:123456789012:apis/api-123/types/Query/resolvers/getOrder",
			RuntimeName:    "APPSYNC_JS",
		}},
		Functions: []Function{{
			ID:             "func-1",
			Name:           "fetchOrder",
			ARN:            "arn:aws:appsync:us-east-1:123456789012:apis/api-123/functions/func-1",
			DataSourceName: "orders-table",
			RuntimeName:    "APPSYNC_JS",
		}},
		Schema:  &SchemaMetadata{Status: "SUCCESS", TypeCount: 7},
		APIKeys: []APIKey{{ID: "da2-abc", Description: "default", Expires: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)}},
	}
}

func TestScannerEmitsGraphQLAPIMetadataAndRelationships(t *testing.T) {
	envelopes, err := Scanner{Client: fakeClient{snapshot: Snapshot{APIs: []GraphQLAPI{sampleAPI()}}}}.
		Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	api := resourceByType(t, envelopes, awscloud.ResourceTypeAppSyncGraphQLAPI)
	if got := payloadString(t, api, "resource_id"); got != "api-123" {
		t.Fatalf("api resource_id = %q, want api-123", got)
	}
	apiAttrs := attributesOf(t, api)
	if got, want := apiAttrs["authentication_type"], "AMAZON_COGNITO_USER_POOLS"; got != want {
		t.Fatalf("authentication_type = %#v, want %q", got, want)
	}
	if got, want := apiAttrs["xray_enabled"], true; got != want {
		t.Fatalf("xray_enabled = %#v, want %v", got, want)
	}
	if _, ok := apiAttrs["log_config"].(map[string]any); !ok {
		t.Fatalf("log_config = %#v, want map", apiAttrs["log_config"])
	}

	ds := resourceByType(t, envelopes, awscloud.ResourceTypeAppSyncDataSource)
	if got := payloadString(t, ds, "resource_id"); !strings.HasPrefix(got, "api-123/datasources/") {
		t.Fatalf("data source resource_id = %q, want api-123/datasources/ prefix", got)
	}

	resourceByType(t, envelopes, awscloud.ResourceTypeAppSyncResolver)
	resourceByType(t, envelopes, awscloud.ResourceTypeAppSyncFunction)
	resourceByType(t, envelopes, awscloud.ResourceTypeAppSyncSchema)
	resourceByType(t, envelopes, awscloud.ResourceTypeAppSyncAPIKey)

	// API -> user pool must target the bare pool ID, matching the Cognito
	// scanner's published user pool resource_id.
	userPoolEdge := relationshipByType(t, envelopes, awscloud.RelationshipAppSyncAPIUsesUserPool)
	if got := payloadString(t, userPoolEdge, "target_resource_id"); got != "us-east-1_abc123" {
		t.Fatalf("user pool target_resource_id = %q, want bare pool id us-east-1_abc123", got)
	}
	if got := payloadString(t, userPoolEdge, "target_type"); got != awscloud.ResourceTypeCognitoUserPool {
		t.Fatalf("user pool target_type = %q, want %q", got, awscloud.ResourceTypeCognitoUserPool)
	}

	oidcEdge := relationshipByType(t, envelopes, awscloud.RelationshipAppSyncAPIUsesOIDCIssuer)
	if got := payloadString(t, oidcEdge, "target_resource_id"); got != "https://issuer.example.com" {
		t.Fatalf("oidc target_resource_id = %q", got)
	}

	assertRelationship(t, envelopes, awscloud.RelationshipAppSyncAPIHasDataSource)
	assertRelationship(t, envelopes, awscloud.RelationshipAppSyncResolverUsesDataSource)
	assertRelationship(t, envelopes, awscloud.RelationshipAppSyncFunctionUsesDataSource)
}

func TestScannerDataSourceTargetJoinKeys(t *testing.T) {
	envelopes, err := Scanner{Client: fakeClient{snapshot: Snapshot{APIs: []GraphQLAPI{sampleAPI()}}}}.
		Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	targets := dataSourceTargetEdges(t, envelopes)
	cases := []struct {
		targetType string
		wantID     string
	}{
		{awscloud.ResourceTypeLambdaFunction, "arn:aws:lambda:us-east-1:123456789012:function:orders"},
		{awscloud.ResourceTypeDynamoDBTable, "arn:aws:dynamodb:us-east-1:123456789012:table/Orders"},
		{awscloud.ResourceTypeRDSDBCluster, "arn:aws:rds:us-east-1:123456789012:cluster:orders"},
		{awscloud.AppSyncDataSourceTargetTypeOpenSearch, "https://search-orders.us-east-1.es.amazonaws.com"},
		{awscloud.AppSyncDataSourceTargetTypeHTTPEndpoint, "https://api.partner.example.com"},
	}
	for _, tc := range cases {
		got, ok := targets[tc.targetType]
		if !ok {
			t.Fatalf("missing data-source target edge for target_type %q", tc.targetType)
		}
		if got != tc.wantID {
			t.Fatalf("target_type %q target_resource_id = %q, want %q", tc.targetType, got, tc.wantID)
		}
	}

	// Every target edge must carry a non-empty target_type so it cannot dangle.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if rt, _ := envelope.Payload["relationship_type"].(string); rt != awscloud.RelationshipAppSyncDataSourceTargetsResource {
			continue
		}
		if tt, _ := envelope.Payload["target_type"].(string); strings.TrimSpace(tt) == "" {
			t.Fatalf("data-source target edge has empty target_type: %#v", envelope.Payload)
		}
	}
}

// TestDynamoDBTargetDerivesPartitionFromSourceARN proves the synthesized table
// ARN reuses the partition from a source ARN rather than hardcoding arn:aws.
func TestDynamoDBTargetDerivesPartitionFromSourceARN(t *testing.T) {
	api := sampleAPI()
	api.ARN = "arn:aws-us-gov:appsync:us-gov-west-1:123456789012:apis/api-123"
	api.DataSources = []DataSource{{Name: "orders-table", Type: "AMAZON_DYNAMODB", DynamoDBTableName: "Orders", DynamoDBAwsRegion: "us-gov-west-1"}}
	api.Resolvers = nil
	api.Functions = nil

	envelopes, err := Scanner{Client: fakeClient{snapshot: Snapshot{APIs: []GraphQLAPI{api}}}}.
		Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	targets := dataSourceTargetEdges(t, envelopes)
	got := targets[awscloud.ResourceTypeDynamoDBTable]
	want := "arn:aws-us-gov:dynamodb:us-gov-west-1:123456789012:table/Orders"
	if got != want {
		t.Fatalf("dynamodb target = %q, want %q (partition must derive from source ARN)", got, want)
	}
}

// TestDynamoDBTargetFallsBackToTableNameWithoutSourceARN proves the edge still
// carries the bare table name as a join key when no ARN can be synthesized,
// rather than dangling with an empty target.
func TestDynamoDBTargetFallsBackToTableNameWithoutSourceARN(t *testing.T) {
	api := GraphQLAPI{
		ID:          "api-123",
		Name:        "orders",
		DataSources: []DataSource{{Name: "orders-table", Type: "AMAZON_DYNAMODB", DynamoDBTableName: "Orders"}},
	}
	envelopes, err := Scanner{Client: fakeClient{snapshot: Snapshot{APIs: []GraphQLAPI{api}}}}.
		Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	targets := dataSourceTargetEdges(t, envelopes)
	if got := targets[awscloud.ResourceTypeDynamoDBTable]; got != "Orders" {
		t.Fatalf("dynamodb fallback target = %q, want bare table name Orders", got)
	}
}

func TestScannerSurfacesSnapshotError(t *testing.T) {
	_, err := Scanner{Client: fakeClient{err: fmt.Errorf("boom")}}.Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want wrapped snapshot error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Scan() error = %v, want it to wrap %q", err, "boom")
	}
}

func TestScannerRejectsWrongServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = "kms"
	_, err := Scanner{Client: fakeClient{}}.Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service_kind rejection")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := Scanner{}.Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

// TestClientInterfaceExcludesForbiddenAppSyncAPIs proves the scanner Client
// interface cannot reach mapping-template evaluation, code evaluation,
// schema-body reads, or any mutation. Adding such a method fails this contract
// test before a scan can call it.
func TestClientInterfaceExcludesForbiddenAppSyncAPIs(t *testing.T) {
	clientType := reflect.TypeOf((*Client)(nil)).Elem()
	forbidden := []string{
		"EvaluateMappingTemplate",
		"EvaluateCode",
		"GetIntrospectionSchema",
		"StartSchemaCreation",
		"GetDataSourceIntrospection",
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
	}
	for _, method := range forbidden {
		if _, ok := clientType.MethodByName(method); ok {
			t.Fatalf("Client declares forbidden AppSync API %q; the scanner must stay metadata-only", method)
		}
	}
}

// TestScannerTypesCannotPersistHighIPPayloads feeds the scanner-owned types
// through reflection and proves no string-bearing field could carry the schema
// SDL body, resolver/function mapping templates, function code body, or API key
// value. High-IP payloads are always free-text bodies, so the check targets
// string and []byte fields (a structural pointer such as GraphQLAPI.Schema, a
// *SchemaMetadata that holds only a status and a count, is allowed). The test
// fails the moment a body-shaped field is added.
func TestScannerTypesCannotPersistHighIPPayloads(t *testing.T) {
	forbiddenFields := []string{
		"definition", // schema SDL body
		"sdl",        // schema SDL body
		"requestmapping",
		"responsemapping",
		"mappingtemplate",
		"code",   // resolver/function JS code body
		"apikey", // raw API key value
		"value",  // raw key value
		"secret",
	}
	types := []reflect.Type{
		reflect.TypeOf(GraphQLAPI{}),
		reflect.TypeOf(DataSource{}),
		reflect.TypeOf(Resolver{}),
		reflect.TypeOf(Function{}),
		reflect.TypeOf(SchemaMetadata{}),
		reflect.TypeOf(APIKey{}),
		reflect.TypeOf(LogConfig{}),
		reflect.TypeOf(UserPoolRef{}),
	}
	for _, typ := range types {
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if !isStringLike(field.Type) {
				continue
			}
			name := strings.ToLower(field.Name)
			for _, forbidden := range forbiddenFields {
				if strings.Contains(name, forbidden) {
					t.Fatalf("%s.%s could persist forbidden AppSync payload (matched %q); high-IP bodies must have no string field", typ.Name(), field.Name, forbidden)
				}
			}
		}
	}
}

// isStringLike reports whether a field type can hold free-text body content
// (string, []byte, or slices of those), which is where a high-IP payload would
// land if accidentally mapped.
func isStringLike(typ reflect.Type) bool {
	switch typ.Kind() {
	case reflect.String:
		return true
	case reflect.Slice:
		return isStringLike(typ.Elem())
	case reflect.Uint8:
		return true
	default:
		return false
	}
}

// TestScannerNeverEmitsForbiddenAttributeKeys feeds a fully populated snapshot
// and proves no emitted resource attribute key exposes a high-IP payload, even
// if a future change adds one.
func TestScannerNeverEmitsForbiddenAttributeKeys(t *testing.T) {
	envelopes, err := Scanner{Client: fakeClient{snapshot: Snapshot{APIs: []GraphQLAPI{sampleAPI()}}}}.
		Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	forbidden := []string{"definition", "sdl", "request_mapping_template", "response_mapping_template", "mapping_template", "code", "api_key", "key_value", "secret"}
	for _, envelope := range envelopes {
		attrs, ok := envelope.Payload["attributes"].(map[string]any)
		if !ok {
			continue
		}
		for key := range attrs {
			for _, bad := range forbidden {
				if strings.Contains(strings.ToLower(key), bad) {
					t.Fatalf("emitted attribute %q matches forbidden payload key %q", key, bad)
				}
			}
		}
	}
}

func dataSourceTargetEdges(t *testing.T, envelopes []facts.Envelope) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if rt, _ := envelope.Payload["relationship_type"].(string); rt != awscloud.RelationshipAppSyncDataSourceTargetsResource {
			continue
		}
		targetType, _ := envelope.Payload["target_type"].(string)
		targetID, _ := envelope.Payload["target_resource_id"].(string)
		out[targetType] = targetID
	}
	return out
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q", resourceType)
	return facts.Envelope{}
}

func assertRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return
		}
	}
	t.Fatalf("missing relationship_type %q", relationshipType)
}

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q", relationshipType)
	return facts.Envelope{}
}

func payloadString(t *testing.T, envelope facts.Envelope, key string) string {
	t.Helper()
	value, ok := envelope.Payload[key].(string)
	if !ok {
		t.Fatalf("payload[%q] = %#v, want string", key, envelope.Payload[key])
	}
	return value
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}
