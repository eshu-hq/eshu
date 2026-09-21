// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceConfig identifies the regional AWS Config metadata scan slice.
	ServiceConfig = "config"
)

const (
	// ResourceTypeConfigConfigurationRecorder identifies an AWS Config
	// configuration recorder. The recorder names the resource types Config
	// records; it never carries recorded configuration item bodies.
	ResourceTypeConfigConfigurationRecorder = "aws_config_configuration_recorder"
	// ResourceTypeConfigDeliveryChannel identifies an AWS Config delivery
	// channel: the S3 bucket and optional SNS topic Config delivers snapshots
	// and history files to.
	ResourceTypeConfigDeliveryChannel = "aws_config_delivery_channel"
	// ResourceTypeConfigRule identifies an AWS Config rule. The rule carries its
	// owner (AWS managed, custom Lambda, or custom policy), the managed-rule
	// source identifier or custom-Lambda function ARN, and the resource-type
	// scope it evaluates. Compliance evaluation result bodies are never stored.
	ResourceTypeConfigRule = "aws_config_rule"
	// ResourceTypeConfigConformancePack identifies an AWS Config conformance
	// pack: a named collection of Config rules with a deployment status and a
	// member-rule count. Conformance pack template bodies are never stored.
	ResourceTypeConfigConformancePack = "aws_config_conformance_pack"
	// ResourceTypeConfigConfigurationAggregator identifies an AWS Config
	// configuration aggregator that aggregates Config data from source accounts
	// and regions or from an organization.
	ResourceTypeConfigConfigurationAggregator = "aws_config_configuration_aggregator"
	// ResourceTypeConfigRetentionConfiguration identifies an AWS Config
	// retention configuration: the number of days Config retains configuration
	// item history.
	ResourceTypeConfigRetentionConfiguration = "aws_config_retention_configuration"
)

const (
	// RelationshipConfigConformancePackContainsRule records that an AWS Config
	// conformance pack contains a member Config rule. The target is the
	// aws_config_rule resource keyed by rule name.
	RelationshipConfigConformancePackContainsRule = "config_conformance_pack_contains_rule"
	// RelationshipConfigRuleEvaluatedByLambda records that an AWS Config custom
	// Lambda rule is evaluated by a Lambda function. The target is the
	// aws_lambda_function resource keyed by the Lambda function ARN.
	RelationshipConfigRuleEvaluatedByLambda = "config_rule_evaluated_by_lambda"
	// RelationshipConfigAggregatorSourcesAccount records that an AWS Config
	// configuration aggregator aggregates data from a source AWS account. The
	// target is the aws_account resource keyed by the account root ARN.
	RelationshipConfigAggregatorSourcesAccount = "config_aggregator_sources_account"
)
