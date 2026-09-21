// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceLambda identifies the regional AWS Lambda service scan slice.
	ServiceLambda = "lambda"
)

const (
	// ResourceTypeLambdaFunction identifies a Lambda function.
	ResourceTypeLambdaFunction = "aws_lambda_function"
	// ResourceTypeLambdaAlias identifies a Lambda alias.
	ResourceTypeLambdaAlias = "aws_lambda_alias"
	// ResourceTypeLambdaEventSourceMapping identifies a Lambda event source
	// mapping.
	ResourceTypeLambdaEventSourceMapping = "aws_lambda_event_source_mapping"
)

const (
	// RelationshipLambdaAliasTargetsFunction records alias routing to a Lambda
	// function version.
	RelationshipLambdaAliasTargetsFunction = "lambda_alias_targets_function"
	// RelationshipLambdaEventSourceMappingTargetsFunction records an event
	// source mapping target function.
	RelationshipLambdaEventSourceMappingTargetsFunction = "lambda_event_source_mapping_targets_function"
	// RelationshipLambdaFunctionUsesImage records a Lambda container image URI.
	RelationshipLambdaFunctionUsesImage = "lambda_function_uses_image"
	// RelationshipLambdaFunctionUsesExecutionRole records a Lambda execution
	// role.
	RelationshipLambdaFunctionUsesExecutionRole = "lambda_function_uses_execution_role"
	// RelationshipLambdaFunctionUsesSubnet records Lambda VPC subnet placement.
	RelationshipLambdaFunctionUsesSubnet = "lambda_function_uses_subnet"
	// RelationshipLambdaFunctionUsesSecurityGroup records Lambda VPC security
	// group attachment.
	RelationshipLambdaFunctionUsesSecurityGroup = "lambda_function_uses_security_group"
)
