// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// conformancePackRuleRelationship records that a conformance pack contains a
// member Config rule. The target is the aws_config_rule resource keyed by rule
// name, so the edge joins to the rule node the scanner emits (or would emit) in
// the same account and region. It returns false for an empty rule name so a
// blank entry does not produce a dangling edge.
func conformancePackRuleRelationship(
	boundary awscloud.Boundary,
	pack ConformancePack,
	ruleName string,
) (awscloud.RelationshipObservation, bool) {
	packID := firstNonEmpty(strings.TrimSpace(pack.ARN), conformancePackResourceID(pack.Name))
	name := strings.TrimSpace(ruleName)
	if packID == "" || name == "" {
		return awscloud.RelationshipObservation{}, false
	}
	ruleID := ruleResourceID(name)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipConfigConformancePackContainsRule,
		SourceResourceID: packID,
		SourceARN:        strings.TrimSpace(pack.ARN),
		TargetResourceID: ruleID,
		TargetType:       awscloud.ResourceTypeConfigRule,
		Attributes: map[string]any{
			"conformance_pack_name": strings.TrimSpace(pack.Name),
			"config_rule_name":      name,
		},
		SourceRecordID: packID + "#rule#" + ruleID,
	}, true
}

// ruleLambdaRelationship records that an AWS Config custom Lambda rule is
// evaluated by a Lambda function. The target is the aws_lambda_function
// resource keyed by the Lambda function ARN, matching the lambda scanner's
// resource_id convention (the function ARN when present). It returns false for
// managed or custom-policy rules, which carry no Lambda evaluator ARN.
func ruleLambdaRelationship(
	boundary awscloud.Boundary,
	rule ConfigRule,
) (awscloud.RelationshipObservation, bool) {
	lambdaARN := strings.TrimSpace(rule.LambdaFunctionARN)
	name := strings.TrimSpace(rule.Name)
	if name == "" || !isLambdaFunctionARN(lambdaARN) {
		return awscloud.RelationshipObservation{}, false
	}
	ruleID := ruleResourceID(name)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipConfigRuleEvaluatedByLambda,
		SourceResourceID: ruleID,
		SourceARN:        strings.TrimSpace(rule.ARN),
		TargetResourceID: lambdaARN,
		TargetARN:        lambdaARN,
		TargetType:       awscloud.ResourceTypeLambdaFunction,
		Attributes: map[string]any{
			"config_rule_name": name,
			"owner":            strings.TrimSpace(rule.Owner),
		},
		SourceRecordID: ruleID + "#lambda#" + lambdaARN,
	}, true
}

// aggregatorAccountRelationship records that an AWS Config configuration
// aggregator aggregates data from a source AWS account. The target is the
// aws_account resource keyed by the account root ARN. The partition is derived
// from the aggregator ARN rather than hardcoded so the edge stays correct in
// GovCloud and China partitions. It returns false when the partition cannot be
// derived or the account id is empty.
func aggregatorAccountRelationship(
	boundary awscloud.Boundary,
	aggregator ConfigurationAggregator,
	sourceAccountID string,
) (awscloud.RelationshipObservation, bool) {
	aggregatorID := firstNonEmpty(strings.TrimSpace(aggregator.ARN), aggregatorResourceID(aggregator.Name))
	accountID := strings.TrimSpace(sourceAccountID)
	if aggregatorID == "" || accountID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	partition, ok := partitionFromARN(aggregator.ARN)
	if !ok {
		return awscloud.RelationshipObservation{}, false
	}
	accountARN := "arn:" + partition + ":iam::" + accountID + ":root"
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipConfigAggregatorSourcesAccount,
		SourceResourceID: aggregatorID,
		SourceARN:        strings.TrimSpace(aggregator.ARN),
		TargetResourceID: accountARN,
		TargetARN:        accountARN,
		TargetType:       awscloud.ResourceTypeAWSAccount,
		Attributes: map[string]any{
			"source_account_id":             accountID,
			"configuration_aggregator_name": strings.TrimSpace(aggregator.Name),
		},
		SourceRecordID: aggregatorID + "#source-account#" + accountID,
	}, true
}

// partitionFromARN extracts the partition segment from an AWS ARN. It returns
// false when the value is not a well-formed ARN, so callers never synthesize a
// hardcoded "aws" partition for GovCloud or China resources.
func partitionFromARN(value string) (string, bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) < 6 || parts[0] != "arn" || strings.TrimSpace(parts[1]) == "" {
		return "", false
	}
	return parts[1], true
}

// isLambdaFunctionARN reports whether value is a Lambda function ARN, so the
// custom-rule-to-Lambda edge targets a real Lambda function node and never a
// non-Lambda string a custom rule might carry.
func isLambdaFunctionARN(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	parts := strings.Split(value, ":")
	return len(parts) >= 6 && parts[0] == "arn" && parts[2] == "lambda" && strings.HasPrefix(parts[5], "function")
}
