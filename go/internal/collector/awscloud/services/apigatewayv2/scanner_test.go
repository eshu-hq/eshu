// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apigatewayv2

import (
	"context"
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
		ServiceKind:         awscloud.ServiceAPIGatewayV2,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:apigatewayv2:1",
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

func boolPtr(b bool) *bool { return &b }

func sampleSnapshot() Snapshot {
	return Snapshot{
		APIs: []API{{
			ID:                        "api-123",
			Name:                      "orders-http",
			ProtocolType:              "HTTP",
			Endpoint:                  "https://api-123.execute-api.us-east-1.amazonaws.com",
			DisableExecuteAPIEndpoint: true,
			Tags:                      map[string]string{"Environment": "prod"},
			Stages: []Stage{{
				APIID:      "api-123",
				Name:       "$default",
				AutoDeploy: boolPtr(true),
			}},
			Authorizers: []Authorizer{
				{
					APIID:          "api-123",
					AuthorizerID:   "auth-cognito",
					Name:           "cognito-jwt",
					Type:           "JWT",
					IdentitySource: []string{"$request.header.Authorization"},
					JWTIssuer:      "https://cognito-idp.us-east-1.amazonaws.com/us-east-1_abc123",
					JWTAudience:    []string{"client-app"},
				},
				{
					APIID:          "api-123",
					AuthorizerID:   "auth-oidc",
					Name:           "okta-jwt",
					Type:           "JWT",
					IdentitySource: []string{"$request.header.Authorization"},
					JWTIssuer:      "https://example.okta.com/oauth2/default",
				},
			},
			Integrations: []Integration{
				{
					APIID:                "api-123",
					IntegrationID:        "int-lambda",
					Type:                 "AWS_PROXY",
					URI:                  "arn:aws:lambda:us-east-1:123456789012:function:orders",
					PayloadFormatVersion: "2.0",
				},
				{
					APIID:         "api-123",
					IntegrationID: "int-http",
					Type:          "HTTP_PROXY",
					URI:           "https://api.partner.example.com/orders",
					Method:        "POST",
				},
				{
					APIID:          "api-123",
					IntegrationID:  "int-private",
					Type:           "HTTP_PROXY",
					ConnectionType: "VPC_LINK",
					ConnectionID:   "vpclink-1",
					URI:            "arn:aws:elasticloadbalancing:us-east-1:123456789012:listener/app/orders/abc/def",
				},
			},
			Routes: []Route{
				{APIID: "api-123", RouteID: "route-1", RouteKey: "POST /orders", Target: "integrations/int-lambda"},
				{APIID: "api-123", RouteID: "route-2", RouteKey: "$default"},
			},
		}},
		VPCLinks: []VPCLink{{
			ID:               "vpclink-1",
			Name:             "orders-link",
			Status:           "AVAILABLE",
			SubnetIDs:        []string{"subnet-aaa", "subnet-bbb"},
			SecurityGroupIDs: []string{"sg-111"},
		}},
		Domains: []DomainName{{
			Name:            "api.example.com",
			ARN:             "arn:aws:apigateway:us-east-1::/domainnames/api.example.com",
			Status:          "AVAILABLE",
			CertificateARNs: []string{"arn:aws:acm:us-east-1:123456789012:certificate/abc-123"},
			Mappings:        []Mapping{{Domain: "api.example.com", ID: "map-1", Key: "v1", APIID: "api-123", Stage: "$default"}},
		}},
	}
}

// TestScannerEmitsAPIGatewayV2Metadata is the positive proof: the scanner emits
// each resource type and each relationship with a non-empty target_type and the
// join key the target scanner publishes.
func TestScannerEmitsAPIGatewayV2Metadata(t *testing.T) {
	envelopes, err := Scanner{Client: fakeClient{snapshot: sampleSnapshot()}}.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	api := resourceByType(t, envelopes, awscloud.ResourceTypeAPIGatewayV2API)
	if got := payloadString(t, api, "resource_id"); got != "api-123" {
		t.Fatalf("api resource_id = %q, want api-123", got)
	}
	if got := attributesOf(t, api)["protocol_type"]; got != "HTTP" {
		t.Fatalf("api protocol_type = %#v, want HTTP", got)
	}
	if got := attributesOf(t, api)["disable_execute_api_endpoint"]; got != true {
		t.Fatalf("api disable_execute_api_endpoint = %#v, want true", got)
	}

	stage := resourceByType(t, envelopes, awscloud.ResourceTypeAPIGatewayStage)
	if got := payloadString(t, stage, "resource_id"); got != "api-123/stages/$default" {
		t.Fatalf("stage resource_id = %q", got)
	}
	if got := attributesOf(t, stage)["auto_deploy"]; got != true {
		t.Fatalf("stage auto_deploy = %#v, want true", got)
	}

	route := resourceByType(t, envelopes, awscloud.ResourceTypeAPIGatewayV2Route)
	if got := attributesOf(t, route)["route_key"]; got != "POST /orders" && got != "$default" {
		t.Fatalf("route route_key = %#v", got)
	}

	resourceByType(t, envelopes, awscloud.ResourceTypeAPIGatewayV2Integration)
	resourceByType(t, envelopes, awscloud.ResourceTypeAPIGatewayV2Authorizer)
	resourceByType(t, envelopes, awscloud.ResourceTypeAPIGatewayV2VPCLink)
	resourceByType(t, envelopes, awscloud.ResourceTypeAPIGatewayDomainName)

	// API -> stage.
	apiStage := relationshipByType(t, envelopes, awscloud.RelationshipAPIGatewayV2APIHasStage)
	if got := payloadString(t, apiStage, "target_resource_id"); got != "api-123/stages/$default" {
		t.Fatalf("api->stage target = %q", got)
	}

	// API -> route, route -> integration.
	apiRoute := relationshipByType(t, envelopes, awscloud.RelationshipAPIGatewayV2APIHasRoute)
	if got := payloadString(t, apiRoute, "source_resource_id"); got != "api-123" {
		t.Fatalf("api->route source = %q", got)
	}
	routeInt := relationshipByType(t, envelopes, awscloud.RelationshipAPIGatewayV2RouteUsesIntegration)
	if got := payloadString(t, routeInt, "source_resource_id"); got != "api-123/routes/route-1" {
		t.Fatalf("route->integration source = %q", got)
	}
	if got := payloadString(t, routeInt, "target_resource_id"); got != "api-123/integrations/int-lambda" {
		t.Fatalf("route->integration target = %q", got)
	}

	// integration -> Lambda joins by the function ARN the Lambda scanner publishes.
	lambdaEdge := relationshipByType(t, envelopes, awscloud.RelationshipAPIGatewayV2IntegrationTargetsLambda)
	if got := payloadString(t, lambdaEdge, "target_resource_id"); got != "arn:aws:lambda:us-east-1:123456789012:function:orders" {
		t.Fatalf("integration->lambda target = %q", got)
	}
	if got := payloadString(t, lambdaEdge, "target_type"); got != awscloud.ResourceTypeLambdaFunction {
		t.Fatalf("integration->lambda target_type = %q", got)
	}

	// integration -> HTTP endpoint.
	httpEdge := relationshipByType(t, envelopes, awscloud.RelationshipAPIGatewayV2IntegrationTargetsHTTP)
	if got := payloadString(t, httpEdge, "target_resource_id"); got != "https://api.partner.example.com/orders" {
		t.Fatalf("integration->http target = %q", got)
	}

	// integration -> VPC link.
	vpcEdge := relationshipByType(t, envelopes, awscloud.RelationshipAPIGatewayV2IntegrationUsesVPCLink)
	if got := payloadString(t, vpcEdge, "target_resource_id"); got != "vpclink-1" {
		t.Fatalf("integration->vpc-link target = %q", got)
	}

	// API -> Cognito user pool joins by the BARE pool id (the Cognito scanner's
	// resource_id), not the full issuer URL.
	poolEdge := relationshipByType(t, envelopes, awscloud.RelationshipAPIGatewayV2APIUsesUserPool)
	if got := payloadString(t, poolEdge, "target_resource_id"); got != "us-east-1_abc123" {
		t.Fatalf("api->user-pool target = %q, want bare pool id us-east-1_abc123", got)
	}
	if got := payloadString(t, poolEdge, "target_type"); got != awscloud.ResourceTypeCognitoUserPool {
		t.Fatalf("api->user-pool target_type = %q", got)
	}

	// Non-Cognito JWT issuer stays a jwt_issuer edge, not a dangling user pool.
	jwtEdge := relationshipByType(t, envelopes, awscloud.RelationshipAPIGatewayV2APIUsesJWTIssuer)
	if got := payloadString(t, jwtEdge, "target_resource_id"); got != "https://example.okta.com/oauth2/default" {
		t.Fatalf("api->jwt-issuer target = %q", got)
	}

	// domain -> ACM certificate joins by the certificate ARN.
	acmEdge := relationshipByType(t, envelopes, awscloud.RelationshipAPIGatewayV2DomainUsesACMCertificate)
	if got := payloadString(t, acmEdge, "target_resource_id"); got != "arn:aws:acm:us-east-1:123456789012:certificate/abc-123" {
		t.Fatalf("domain->acm target = %q", got)
	}

	// domain -> API mapping.
	mapEdge := relationshipByType(t, envelopes, awscloud.RelationshipAPIGatewayV2DomainMapsToAPI)
	if got := payloadString(t, mapEdge, "target_resource_id"); got != "api-123" {
		t.Fatalf("domain->api target = %q", got)
	}

	// vpc link -> subnet / security group join by the bare ids the EC2 scanner publishes.
	subnetEdge := relationshipByType(t, envelopes, awscloud.RelationshipAPIGatewayV2VPCLinkUsesSubnet)
	if got := payloadString(t, subnetEdge, "target_type"); got != awscloud.ResourceTypeEC2Subnet {
		t.Fatalf("vpc-link->subnet target_type = %q", got)
	}
	sgEdge := relationshipByType(t, envelopes, awscloud.RelationshipAPIGatewayV2VPCLinkUsesSecurityGroup)
	if got := payloadString(t, sgEdge, "target_resource_id"); got != "sg-111" {
		t.Fatalf("vpc-link->sg target = %q", got)
	}

	// Every relationship must carry a non-empty target_type so no edge dangles.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if strings.TrimSpace(payloadString(t, envelope, "target_type")) == "" {
			t.Fatalf("relationship %q has empty target_type", payloadString(t, envelope, "relationship_type"))
		}
	}
}

// TestScannerRejectsForeignServiceKind proves the scanner refuses a boundary
// claimed for another service rather than silently scanning it.
func TestScannerRejectsForeignServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceAPIGateway
	if _, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary); err == nil {
		t.Fatalf("Scan() error = nil, want service_kind rejection")
	}
}

// TestScannerRequiresClient proves a nil client is a hard error, not a silent
// empty scan.
func TestScannerRequiresClient(t *testing.T) {
	if _, err := (Scanner{}).Scan(context.Background(), testBoundary()); err == nil {
		t.Fatalf("Scan() error = nil, want missing-client error")
	}
}

// TestScannerPropagatesSnapshotError proves a snapshot failure is surfaced, not
// swallowed.
func TestScannerPropagatesSnapshotError(t *testing.T) {
	_, err := Scanner{Client: fakeClient{err: context.DeadlineExceeded}}.Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want propagated snapshot error")
	}
}

// TestScannerTypesCarryNoForbiddenPayload proves the scanner-owned types have no
// field that could carry a request/response mapping template, an authorizer
// invocation URI, or an authorizer credential ARN. Adding such a field fails
// this guard before a scan could persist it.
func TestScannerTypesCarryNoForbiddenPayload(t *testing.T) {
	forbiddenFields := map[string]struct{}{
		"requesttemplates":             {},
		"responsetemplates":            {},
		"requestparameters":            {},
		"responseparameters":           {},
		"requestmodels":                {},
		"authorizeruri":                {},
		"authorizercredentialsarn":     {},
		"identityvalidationexpression": {},
		"stagevariables":               {},
	}
	types := []reflect.Type{
		reflect.TypeOf(API{}),
		reflect.TypeOf(Stage{}),
		reflect.TypeOf(Route{}),
		reflect.TypeOf(Integration{}),
		reflect.TypeOf(Authorizer{}),
		reflect.TypeOf(DomainName{}),
		reflect.TypeOf(VPCLink{}),
	}
	for _, typ := range types {
		for i := 0; i < typ.NumField(); i++ {
			name := strings.ToLower(typ.Field(i).Name)
			if _, bad := forbiddenFields[name]; bad {
				t.Fatalf("%s carries forbidden field %q; the scanner must stay metadata-only", typ.Name(), typ.Field(i).Name)
			}
		}
	}
}
