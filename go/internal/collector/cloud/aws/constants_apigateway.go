// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceAPIGateway identifies the regional Amazon API Gateway metadata scan
	// slice covering REST, HTTP, and WebSocket APIs.
	ServiceAPIGateway = "apigateway"
)

const (
	// ResourceTypeAPIGatewayRESTAPI identifies a REST API Gateway API.
	ResourceTypeAPIGatewayRESTAPI = "aws_apigateway_rest_api"
	// ResourceTypeAPIGatewayV2API identifies an HTTP or WebSocket API Gateway
	// API.
	ResourceTypeAPIGatewayV2API = "aws_apigatewayv2_api"
	// ResourceTypeAPIGatewayStage identifies an API Gateway stage.
	ResourceTypeAPIGatewayStage = "aws_apigateway_stage"
	// ResourceTypeAPIGatewayDomainName identifies an API Gateway custom domain.
	ResourceTypeAPIGatewayDomainName = "aws_apigateway_domain_name"
)

const (
	// RelationshipAPIGatewayAPIHasStage records stage membership on an API
	// Gateway API.
	RelationshipAPIGatewayAPIHasStage = "apigateway_api_has_stage"
	// RelationshipAPIGatewayDomainMapsToAPI records custom-domain mapping
	// evidence to an API Gateway API.
	RelationshipAPIGatewayDomainMapsToAPI = "apigateway_domain_maps_to_api"
	// RelationshipAPIGatewayDomainUsesACMCertificate records custom-domain TLS
	// certificate dependencies.
	RelationshipAPIGatewayDomainUsesACMCertificate = "apigateway_domain_uses_acm_certificate"
	// RelationshipAPIGatewayStageLogsToResource records stage access-log
	// destinations when AWS reports an ARN.
	RelationshipAPIGatewayStageLogsToResource = "apigateway_stage_logs_to_resource"
	// RelationshipAPIGatewayAPIIntegratesWithResource records API integration
	// targets when AWS reports an ARN-addressable backend.
	RelationshipAPIGatewayAPIIntegratesWithResource = "apigateway_api_integrates_with_resource"
)
