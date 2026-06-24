// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceAPIGatewayV2 identifies the regional Amazon API Gateway v2 metadata
	// scan slice covering HTTP and WebSocket APIs, their stages, routes,
	// integrations, authorizers, custom domains, and VPC links. The classic REST
	// (v1) surface is owned by the separate ServiceAPIGateway scan slice.
	ServiceAPIGatewayV2 = "apigatewayv2"
)

const (
	// ResourceTypeAPIGatewayV2Route identifies an API Gateway v2 route. The fact
	// carries the route key, route id, target reference, and authorization
	// summary only. Request models, request parameter mappings, and selection
	// expressions are never persisted because they describe request transformation
	// behavior, not topology.
	ResourceTypeAPIGatewayV2Route = "aws_apigatewayv2_route"
	// ResourceTypeAPIGatewayV2Integration identifies an API Gateway v2 integration.
	// The fact carries the integration id, type, backend URI/target, and
	// connection metadata only. Request templates, request/response parameter
	// mappings, and credential ARNs are never persisted.
	ResourceTypeAPIGatewayV2Integration = "aws_apigatewayv2_integration"
	// ResourceTypeAPIGatewayV2Authorizer identifies an API Gateway v2 authorizer.
	// The fact carries the authorizer name, type, and identity sources only. The
	// JWT issuer is surfaced for the Cognito user pool join. Lambda authorizer
	// invocation URIs, authorizer credential ARNs, and identity validation
	// expressions are never persisted.
	ResourceTypeAPIGatewayV2Authorizer = "aws_apigatewayv2_authorizer"
	// ResourceTypeAPIGatewayV2VPCLink identifies an API Gateway v2 VPC link used by
	// private integrations. The fact carries the link id, name, status, subnet
	// ids, and security group ids.
	ResourceTypeAPIGatewayV2VPCLink = "aws_apigatewayv2_vpc_link"
)

const (
	// RelationshipAPIGatewayV2APIHasStage records stage membership on an API
	// Gateway v2 API.
	RelationshipAPIGatewayV2APIHasStage = "apigatewayv2_api_has_stage"
	// RelationshipAPIGatewayV2APIHasRoute records route membership on an API
	// Gateway v2 API.
	RelationshipAPIGatewayV2APIHasRoute = "apigatewayv2_api_has_route"
	// RelationshipAPIGatewayV2RouteUsesIntegration records the integration a route
	// dispatches to, derived from the route target reference.
	RelationshipAPIGatewayV2RouteUsesIntegration = "apigatewayv2_route_uses_integration"
	// RelationshipAPIGatewayV2IntegrationTargetsLambda records the Lambda function
	// an AWS_PROXY integration invokes, joined by the function ARN the Lambda
	// scanner publishes as its resource_id.
	RelationshipAPIGatewayV2IntegrationTargetsLambda = "apigatewayv2_integration_targets_lambda"
	// RelationshipAPIGatewayV2IntegrationTargetsHTTP records the external HTTP
	// endpoint an HTTP_PROXY integration forwards to. The target is a URL, not an
	// AWS resource ARN.
	RelationshipAPIGatewayV2IntegrationTargetsHTTP = "apigatewayv2_integration_targets_http"
	// RelationshipAPIGatewayV2IntegrationUsesVPCLink records the VPC link a private
	// integration routes through.
	RelationshipAPIGatewayV2IntegrationUsesVPCLink = "apigatewayv2_integration_uses_vpc_link"
	// RelationshipAPIGatewayV2APIUsesUserPool records the Cognito user pool a JWT
	// authorizer trusts, joined by the bare user pool id parsed from the JWT
	// issuer URL. This matches the resource_id the Cognito scanner publishes for
	// the user pool node.
	RelationshipAPIGatewayV2APIUsesUserPool = "apigatewayv2_api_uses_user_pool"
	// RelationshipAPIGatewayV2APIUsesJWTIssuer records a JWT authorizer issuer that
	// is not an AWS Cognito user pool. The issuer is an external URL, not an AWS
	// resource ARN.
	RelationshipAPIGatewayV2APIUsesJWTIssuer = "apigatewayv2_api_uses_jwt_issuer"
	// RelationshipAPIGatewayV2DomainUsesACMCertificate records custom-domain TLS
	// certificate dependencies, joined by the certificate ARN the ACM scanner
	// publishes as its resource_id.
	RelationshipAPIGatewayV2DomainUsesACMCertificate = "apigatewayv2_domain_uses_acm_certificate"
	// RelationshipAPIGatewayV2DomainMapsToAPI records custom-domain API mapping
	// evidence to an API Gateway v2 API.
	RelationshipAPIGatewayV2DomainMapsToAPI = "apigatewayv2_domain_maps_to_api"
	// RelationshipAPIGatewayV2VPCLinkUsesSubnet records a subnet an API Gateway v2
	// VPC link spans, joined by the bare subnet id the EC2 scanner publishes.
	RelationshipAPIGatewayV2VPCLinkUsesSubnet = "apigatewayv2_vpc_link_uses_subnet"
	// RelationshipAPIGatewayV2VPCLinkUsesSecurityGroup records a security group an
	// API Gateway v2 VPC link applies, joined by the bare group id the EC2 scanner
	// publishes.
	RelationshipAPIGatewayV2VPCLinkUsesSecurityGroup = "apigatewayv2_vpc_link_uses_security_group"
)

const (
	// APIGatewayV2JWTIssuerTargetType labels a JWT authorizer issuer that is not a
	// Cognito user pool. The issuer is an external OpenID Connect URL, not an AWS
	// resource ARN.
	APIGatewayV2JWTIssuerTargetType = "jwt_issuer"
	// APIGatewayV2HTTPEndpointTargetType labels an HTTP_PROXY integration target.
	// The target is an external endpoint URL, not an AWS resource ARN.
	APIGatewayV2HTTPEndpointTargetType = "http_endpoint"
)
