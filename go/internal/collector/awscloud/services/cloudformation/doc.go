// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cloudformation maps AWS CloudFormation control-plane metadata into
// AWS cloud collector facts.
//
// CloudFormation is the highest template-body redaction surface in the AWS
// collector. The package owns scanner-level normalization only. It never calls
// the AWS SDK directly, never reads a template body (GetTemplate,
// GetTemplateSummary), never reads parameter values, never reads change-set
// bodies, and never persists drift property documents. SDK adapters provide
// Stack, StackSet, ChangeSet, StackDriftResult, StackInstance, and
// RegisteredType values; Scanner emits aws_resource facts plus
// stack-to-resource-type, stack-set-to-instance, stack-to-IAM-role, and
// stack-to-S3-template-URL relationship evidence.
//
// Secret-like stack output values are redacted by key through the shared
// awscloud redaction policy before emission, so Scanner requires a non-zero
// redaction key. TestClientInterfaceExcludesMutationAndTemplateAPIs proves the
// Client interface can never reach a template body or any mutation API.
package cloudformation
