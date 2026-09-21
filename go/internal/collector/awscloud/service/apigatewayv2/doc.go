// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package apigatewayv2 owns the AWS API Gateway v2 metadata scanner for the AWS
// cloud collector. It converts HTTP and WebSocket APIs, their stages, routes,
// integrations, authorizers, custom domains, and VPC links into reported AWS
// facts and relationship evidence for one claimed account and region.
//
// The scanner is metadata-only. It never reads or persists request/response
// mapping templates, route request models, authorizer Lambda invocation URIs or
// credential ARNs, JWT secrets, or stage variable values, and it never mutates
// API Gateway resources. The classic REST (v1) surface is owned by the separate
// apigateway scanner; this package covers only the apigatewayv2 service.
//
// Callers handle AWS authorization, throttling, and partial service failures as
// ordinary scanner errors. Relationships always set a non-empty target_type and
// join by the resource_id the target scanner publishes: Lambda by function ARN,
// Cognito user pool by bare pool id parsed from the JWT issuer URL, ACM by
// certificate ARN, and EC2 subnet/security group by bare id.
package apigatewayv2
