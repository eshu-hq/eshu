// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceCloudFormation identifies the regional AWS CloudFormation metadata
	// scan slice. CloudFormation is the highest template-body redaction surface
	// in the AWS collector: stack and stack-set templates can carry inline IAM
	// policy bodies, NoEcho parameter values, and embedded secrets. The scanner
	// emits stack, stack-set, change-set, drift-result, stack-instance, and
	// registered-type configuration metadata only. Template bodies
	// (GetTemplate / TemplateBody), parameter values, change-set bodies, and
	// drift property documents are the protected data class and are never read
	// or persisted through this service kind.
	ServiceCloudFormation = "cloudformation"
)

const (
	// ResourceTypeCloudFormationStack identifies a CloudFormation stack metadata
	// resource. The scanner emits stack identity, status, capabilities, tags,
	// role ARN, drift status, and timestamps only; the template body, parameter
	// values, and secret-like output values are never persisted.
	ResourceTypeCloudFormationStack = "aws_cloudformation_stack"
	// ResourceTypeCloudFormationStackSet identifies a CloudFormation stack set
	// metadata resource. The scanner emits stack-set identity, status,
	// capabilities, permission model, and administration/execution role
	// references only; the stack-set template body and parameter values are
	// never persisted.
	ResourceTypeCloudFormationStackSet = "aws_cloudformation_stack_set"
	// ResourceTypeCloudFormationChangeSet identifies a CloudFormation change set
	// metadata resource. The scanner emits change-set identity, status, and
	// execution status only; the per-resource change body (action, replacement
	// detail) is never read or persisted.
	ResourceTypeCloudFormationChangeSet = "aws_cloudformation_change_set"
	// ResourceTypeCloudFormationStackDrift identifies a CloudFormation stack
	// drift detection result metadata resource. The scanner emits drift status
	// and per-status resource counts only; actual and expected property
	// documents and per-property differences are never persisted.
	ResourceTypeCloudFormationStackDrift = "aws_cloudformation_stack_drift"
	// ResourceTypeCloudFormationStackInstance identifies a CloudFormation stack
	// set instance metadata resource. The scanner emits account, region, status,
	// and drift status only.
	ResourceTypeCloudFormationStackInstance = "aws_cloudformation_stack_instance"
	// ResourceTypeCloudFormationType identifies a CloudFormation registered
	// extension (type) metadata resource. The scanner emits type identity,
	// kind, default version, publisher, and activation state only; type schema
	// and configuration bodies are never persisted.
	ResourceTypeCloudFormationType = "aws_cloudformation_type"
)

const (
	// RelationshipCloudFormationStackUsesResourceType records that a stack
	// manages at least one resource of a given CloudFormation resource type,
	// derived from ListStackResources. The relationship carries the resource
	// type and physical/logical identity only; no template body is read.
	RelationshipCloudFormationStackUsesResourceType = "cloudformation_stack_uses_resource_type"
	// RelationshipCloudFormationStackSetContainsStackInstance records that a
	// stack set owns a deployed stack instance in a target account and region.
	RelationshipCloudFormationStackSetContainsStackInstance = "cloudformation_stack_set_contains_stack_instance"
	// RelationshipCloudFormationStackUsesIAMRole records a stack's reported IAM
	// service role ARN dependency.
	RelationshipCloudFormationStackUsesIAMRole = "cloudformation_stack_uses_iam_role"
	// RelationshipCloudFormationStackUsesS3TemplateURL records a stack's reported
	// S3 template URL reference. The URL location is provenance evidence; the
	// referenced template object contents are never fetched.
	RelationshipCloudFormationStackUsesS3TemplateURL = "cloudformation_stack_uses_s3_template_url"
)
