// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsapigatewayv2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	awsapigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestAPIGatewayV2APIExcludesForbiddenMethods proves the adapter's accepted SDK
// surface cannot reach the full OpenAPI export, integration/route response
// readers, model/template readers, or any mutation operation. Adding any such
// method to the interface fails this contract test before a scan can call it.
func TestAPIGatewayV2APIExcludesForbiddenMethods(t *testing.T) {
	apiType := reflect.TypeOf((*apiClient)(nil)).Elem()
	forbidden := []string{
		"ExportApi",
		"GetIntegrationResponse",
		"GetIntegrationResponses",
		"GetRouteResponse",
		"GetRouteResponses",
		"GetModel",
		"GetModels",
		"GetModelTemplate",
		"ReimportApi",
		"ImportApi",
		"CreateApi",
		"UpdateApi",
		"DeleteApi",
		"CreateRoute",
		"UpdateRoute",
		"DeleteRoute",
		"CreateIntegration",
		"UpdateIntegration",
		"DeleteIntegration",
		"CreateAuthorizer",
		"UpdateAuthorizer",
		"DeleteAuthorizer",
		"CreateStage",
		"UpdateStage",
		"DeleteStage",
		"CreateVpcLink",
		"DeleteVpcLink",
		"CreateDeployment",
		"CreateDomainName",
		"DeleteDomainName",
	}
	for _, method := range forbidden {
		if _, ok := apiType.MethodByName(method); ok {
			t.Fatalf("apiClient declares forbidden API Gateway v2 method %q; the adapter must stay metadata-only", method)
		}
	}
}

// TestSnapshotMapsMetadataAndExcludesPayloads proves the adapter maps topology
// metadata while never copying request/response templates, request parameters,
// authorizer invocation URIs, or credential ARNs into a scanner-owned record.
func TestSnapshotMapsMetadataAndExcludesPayloads(t *testing.T) {
	fake := &fakeAPI{
		apis: []awsapigatewayv2types.Api{{
			ApiId:        aws.String("api-1"),
			Name:         aws.String("orders"),
			ProtocolType: awsapigatewayv2types.ProtocolTypeHttp,
			ApiEndpoint:  aws.String("https://api-1.execute-api.us-east-1.amazonaws.com"),
		}},
		stages: []awsapigatewayv2types.Stage{{
			StageName:  aws.String("$default"),
			AutoDeploy: aws.Bool(true),
		}},
		routes: []awsapigatewayv2types.Route{{
			RouteId:        aws.String("route-1"),
			RouteKey:       aws.String("POST /orders"),
			Target:         aws.String("integrations/int-1"),
			RequestModels:  map[string]string{"$default": "OrdersModel"},
			AuthorizerId:   aws.String("auth-1"),
			OperationName:  aws.String("createOrder"),
			ApiKeyRequired: aws.Bool(false),
		}},
		integrations: []awsapigatewayv2types.Integration{{
			IntegrationId:    aws.String("int-1"),
			IntegrationType:  awsapigatewayv2types.IntegrationTypeAwsProxy,
			IntegrationUri:   aws.String("arn:aws:lambda:us-east-1:123456789012:function:orders"),
			CredentialsArn:   aws.String("arn:aws:iam::123456789012:role/secret-invoke-role"),
			RequestTemplates: map[string]string{"application/json": "#set($x = $input.body)"},
			RequestParameters: map[string]string{
				"overwrite:header.Authorization": "stageVariables.secretToken",
			},
		}},
		authorizers: []awsapigatewayv2types.Authorizer{{
			AuthorizerId:                 aws.String("auth-1"),
			Name:                         aws.String("cognito"),
			AuthorizerType:               awsapigatewayv2types.AuthorizerTypeJwt,
			AuthorizerUri:                aws.String("arn:aws:apigateway:us-east-1:lambda:path/2015-03-31/functions/arn:aws:lambda:us-east-1:123456789012:function:secret-authorizer/invocations"),
			AuthorizerCredentialsArn:     aws.String("arn:aws:iam::123456789012:role/secret-auth-role"),
			IdentityValidationExpression: aws.String("^secretpattern$"),
			IdentitySource:               []string{"$request.header.Authorization"},
			JwtConfiguration: &awsapigatewayv2types.JWTConfiguration{
				Issuer:   aws.String("https://cognito-idp.us-east-1.amazonaws.com/us-east-1_abc123"),
				Audience: []string{"client-app"},
			},
		}},
		vpcLinks: []awsapigatewayv2types.VpcLink{{
			VpcLinkId:        aws.String("vpclink-1"),
			Name:             aws.String("orders-link"),
			VpcLinkStatus:    awsapigatewayv2types.VpcLinkStatusAvailable,
			SubnetIds:        []string{"subnet-aaa"},
			SecurityGroupIds: []string{"sg-111"},
		}},
		domains: []awsapigatewayv2types.DomainName{{
			DomainName:    aws.String("api.example.com"),
			DomainNameArn: aws.String("arn:aws:apigateway:us-east-1::/domainnames/api.example.com"),
			DomainNameConfigurations: []awsapigatewayv2types.DomainNameConfiguration{{
				CertificateArn:   aws.String("arn:aws:acm:us-east-1:123456789012:certificate/abc-123"),
				DomainNameStatus: awsapigatewayv2types.DomainNameStatusAvailable,
			}},
		}},
		mappings: []awsapigatewayv2types.ApiMapping{{
			ApiMappingId:  aws.String("map-1"),
			ApiId:         aws.String("api-1"),
			Stage:         aws.String("$default"),
			ApiMappingKey: aws.String("v1"),
		}},
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
	if len(api.Routes) != 1 || api.Routes[0].Target != "integrations/int-1" {
		t.Fatalf("routes = %#v", api.Routes)
	}
	if len(api.Integrations) != 1 || api.Integrations[0].URI != "arn:aws:lambda:us-east-1:123456789012:function:orders" {
		t.Fatalf("integrations = %#v", api.Integrations)
	}
	if len(api.Authorizers) != 1 || api.Authorizers[0].JWTIssuer != "https://cognito-idp.us-east-1.amazonaws.com/us-east-1_abc123" {
		t.Fatalf("authorizers = %#v", api.Authorizers)
	}
	if len(snapshot.VPCLinks) != 1 || snapshot.VPCLinks[0].ID != "vpclink-1" {
		t.Fatalf("vpc links = %#v", snapshot.VPCLinks)
	}
	if len(snapshot.Domains) != 1 || len(snapshot.Domains[0].Mappings) != 1 {
		t.Fatalf("domains = %#v", snapshot.Domains)
	}

	// The mapped route, integration, and authorizer must carry no request
	// template, request parameter, authorizer invocation URI, or credential ARN.
	// The scanner-owned types have no field for them, so a reflection scan of the
	// mapped values proves the payloads are unreachable.
	assertNoForbiddenStringValue(t, api.Routes[0], "OrdersModel", "#set")
	assertNoForbiddenStringValue(t, api.Integrations[0], "#set", "secret", "stageVariables.secretToken")
	assertNoForbiddenStringValue(t, api.Authorizers[0], "secret-authorizer", "secret-auth-role", "secretpattern")
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

// TestSnapshotPaginatesAPIs proves pagination follows nextToken and stops when
// the token is empty.
func TestSnapshotPaginatesAPIs(t *testing.T) {
	fake := &fakeAPI{
		apiPages: [][]awsapigatewayv2types.Api{
			{{ApiId: aws.String("api-1"), Name: aws.String("a")}},
			{{ApiId: aws.String("api-2"), Name: aws.String("b")}},
		},
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

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceAPIGatewayV2}
}

// fakeAPI is a minimal in-memory apiClient. It returns a single page per list
// call (or two pages for the explicit pagination test).
type fakeAPI struct {
	apis             []awsapigatewayv2types.Api
	apiPages         [][]awsapigatewayv2types.Api
	apiPageIndex     int
	returnNilAPIPage bool
	stages           []awsapigatewayv2types.Stage
	routes           []awsapigatewayv2types.Route
	integrations     []awsapigatewayv2types.Integration
	authorizers      []awsapigatewayv2types.Authorizer
	vpcLinks         []awsapigatewayv2types.VpcLink
	domains          []awsapigatewayv2types.DomainName
	mappings         []awsapigatewayv2types.ApiMapping
}

func (f *fakeAPI) GetApis(_ context.Context, _ *awsapigatewayv2.GetApisInput, _ ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetApisOutput, error) {
	if f.returnNilAPIPage {
		return nil, nil
	}
	if len(f.apiPages) > 0 {
		page := f.apiPages[f.apiPageIndex]
		out := &awsapigatewayv2.GetApisOutput{Items: page}
		if f.apiPageIndex < len(f.apiPages)-1 {
			out.NextToken = aws.String("next")
			f.apiPageIndex++
		}
		return out, nil
	}
	return &awsapigatewayv2.GetApisOutput{Items: f.apis}, nil
}

func (f *fakeAPI) GetStages(_ context.Context, _ *awsapigatewayv2.GetStagesInput, _ ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetStagesOutput, error) {
	return &awsapigatewayv2.GetStagesOutput{Items: f.stages}, nil
}

func (f *fakeAPI) GetRoutes(_ context.Context, _ *awsapigatewayv2.GetRoutesInput, _ ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetRoutesOutput, error) {
	return &awsapigatewayv2.GetRoutesOutput{Items: f.routes}, nil
}

func (f *fakeAPI) GetIntegrations(_ context.Context, _ *awsapigatewayv2.GetIntegrationsInput, _ ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetIntegrationsOutput, error) {
	return &awsapigatewayv2.GetIntegrationsOutput{Items: f.integrations}, nil
}

func (f *fakeAPI) GetAuthorizers(_ context.Context, _ *awsapigatewayv2.GetAuthorizersInput, _ ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetAuthorizersOutput, error) {
	return &awsapigatewayv2.GetAuthorizersOutput{Items: f.authorizers}, nil
}

func (f *fakeAPI) GetDomainNames(_ context.Context, _ *awsapigatewayv2.GetDomainNamesInput, _ ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetDomainNamesOutput, error) {
	return &awsapigatewayv2.GetDomainNamesOutput{Items: f.domains}, nil
}

func (f *fakeAPI) GetApiMappings(_ context.Context, _ *awsapigatewayv2.GetApiMappingsInput, _ ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetApiMappingsOutput, error) {
	return &awsapigatewayv2.GetApiMappingsOutput{Items: f.mappings}, nil
}

func (f *fakeAPI) GetVpcLinks(_ context.Context, _ *awsapigatewayv2.GetVpcLinksInput, _ ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetVpcLinksOutput, error) {
	return &awsapigatewayv2.GetVpcLinksOutput{Items: f.vpcLinks}, nil
}
