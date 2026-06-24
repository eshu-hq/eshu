// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

// terraformImportMapping binds one AWS resource family to its Terraform resource
// type and a deterministic import-ID extractor. The ImportID function MUST return
// the empty string when the finding cannot yield an exact, non-guessed import
// identity so the caller refuses the candidate with missing_provider_import_id
// instead of emitting an unsafe import block.
type terraformImportMapping struct {
	ResourceType string
	ImportID     func(IaCManagementFindingRow) string
}

// terraformImportMappingForFinding resolves the supported Terraform import
// mapping for an AWS finding keyed by the ARN service token stored in
// ResourceType. Only families whose Terraform import ID is exactly derivable
// from the finding ARN/ResourceID are mapped; every other family stays
// unsupported so it is refused rather than guessed.
//
// Mapping table (family -> Terraform type -> import ID source):
//
//	s3       -> aws_s3_bucket            -> bucket name
//	lambda   -> aws_lambda_function      -> function name
//	sns      -> aws_sns_topic            -> topic ARN
//	dynamodb -> aws_dynamodb_table       -> table name
//	ecr      -> aws_ecr_repository       -> repository name
//	logs     -> aws_cloudwatch_log_group -> log group name
//
// Families that need a data-plane read, secret value, broad policy synthesis,
// or whose import identity is not exact (for example SQS queue URLs, SNS
// subscriptions, DynamoDB indexes, and CloudWatch log streams) are intentionally
// absent or refused inside the extractor. Security-sensitive families such as
// iam and kms still pass through the upstream safety gate, which refuses them
// for terraform_import_plan regardless of whether a mapping exists here.
func terraformImportMappingForFinding(finding IaCManagementFindingRow) (terraformImportMapping, bool) {
	switch strings.ToLower(strings.TrimSpace(finding.ResourceType)) {
	case "s3":
		return terraformImportMapping{ResourceType: "aws_s3_bucket", ImportID: terraformImportS3BucketID}, true
	case "lambda":
		return terraformImportMapping{ResourceType: "aws_lambda_function", ImportID: terraformImportLambdaFunctionID}, true
	case "sns":
		return terraformImportMapping{ResourceType: "aws_sns_topic", ImportID: terraformImportSNSTopicID}, true
	case "dynamodb":
		return terraformImportMapping{ResourceType: "aws_dynamodb_table", ImportID: terraformImportDynamoDBTableID}, true
	case "ecr":
		return terraformImportMapping{ResourceType: "aws_ecr_repository", ImportID: terraformImportECRRepositoryID}, true
	case "logs":
		return terraformImportMapping{ResourceType: "aws_cloudwatch_log_group", ImportID: terraformImportCloudWatchLogGroupID}, true
	default:
		return terraformImportMapping{}, false
	}
}

// terraformImportS3BucketID derives the aws_s3_bucket import ID, which is the
// bucket name. The finding ResourceID already holds the bucket name; the ARN
// (arn:aws:s3:::bucket) is a fallback.
func terraformImportS3BucketID(finding IaCManagementFindingRow) string {
	if id := strings.TrimSpace(finding.ResourceID); id != "" {
		return id
	}
	const s3ARNPrefix = "arn:aws:s3:::"
	return strings.TrimPrefix(strings.TrimSpace(finding.ARN), s3ARNPrefix)
}

// terraformImportLambdaFunctionID derives the aws_lambda_function import ID,
// which is the function name. The ARN resource segment is function:<name>.
func terraformImportLambdaFunctionID(finding IaCManagementFindingRow) string {
	resourceID := strings.TrimSpace(finding.ResourceID)
	return strings.TrimPrefix(resourceID, "function:")
}

// terraformImportSNSTopicID derives the aws_sns_topic import ID, which is the
// full topic ARN. SNS subscriptions share the sns service token but carry a
// subscription UUID in the resource segment (topic:uuid); those are ambiguous
// for a topic import and yield the empty string so the candidate is refused.
func terraformImportSNSTopicID(finding IaCManagementFindingRow) string {
	resourceID := strings.TrimSpace(finding.ResourceID)
	if strings.ContainsAny(resourceID, ":/") {
		return ""
	}
	return strings.TrimSpace(finding.ARN)
}

// terraformImportDynamoDBTableID derives the aws_dynamodb_table import ID, which
// is the table name. The ARN resource segment is table/<name>. Sub-resources
// such as table/<name>/index/<index> are not tables and are refused.
func terraformImportDynamoDBTableID(finding IaCManagementFindingRow) string {
	resourceID := strings.TrimSpace(finding.ResourceID)
	const tablePrefix = "table/"
	if !strings.HasPrefix(resourceID, tablePrefix) {
		return ""
	}
	name := strings.TrimPrefix(resourceID, tablePrefix)
	if name == "" || strings.Contains(name, "/") {
		return ""
	}
	return name
}

// terraformImportECRRepositoryID derives the aws_ecr_repository import ID, which
// is the repository name. The ARN resource segment is repository/<name>.
func terraformImportECRRepositoryID(finding IaCManagementFindingRow) string {
	resourceID := strings.TrimSpace(finding.ResourceID)
	const repoPrefix = "repository/"
	if !strings.HasPrefix(resourceID, repoPrefix) {
		return ""
	}
	name := strings.TrimPrefix(resourceID, repoPrefix)
	if name == "" {
		return ""
	}
	return name
}

// terraformImportCloudWatchLogGroupID derives the aws_cloudwatch_log_group
// import ID, which is the log group name. The ARN resource segment is
// log-group:<name> and may carry a trailing :* wildcard. A log-stream
// sub-resource (log-group:<name>:log-stream:<stream>) is ambiguous for a log
// group import and is refused.
func terraformImportCloudWatchLogGroupID(finding IaCManagementFindingRow) string {
	resourceID := strings.TrimSpace(finding.ResourceID)
	const logGroupPrefix = "log-group:"
	if !strings.HasPrefix(resourceID, logGroupPrefix) {
		return ""
	}
	name := strings.TrimPrefix(resourceID, logGroupPrefix)
	name = strings.TrimSuffix(name, ":*")
	if name == "" || strings.Contains(name, ":log-stream:") {
		return ""
	}
	return name
}
