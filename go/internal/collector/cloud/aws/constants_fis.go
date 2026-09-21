// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceFIS identifies the regional AWS Fault Injection Service (FIS)
	// metadata-only scan slice. The scanner reads experiment-template
	// control-plane metadata through the FIS management APIs
	// (ListExperimentTemplates, GetExperimentTemplate, ListTagsForResource) and
	// never starts, stops, or mutates an experiment, and never reads experiment
	// run results, resolved-target inventories, or any data-plane payload.
	ServiceFIS = "fis"
)

const (
	// ResourceTypeFISExperimentTemplate identifies an AWS FIS experiment
	// template metadata resource. The scanner emits identity (id, ARN, name from
	// the Name tag, description), the execution IAM role reference, the action
	// and target id/type inventory, the logging and stop-condition destinations,
	// and lifecycle timestamps only. Action parameter values, target filter
	// values, and experiment run outputs stay outside the contract.
	ResourceTypeFISExperimentTemplate = "aws_fis_experiment_template"
)

const (
	// RelationshipFISTemplateUsesIAMRole records the IAM role an FIS experiment
	// template assumes to inject faults. The target is keyed by the role ARN FIS
	// reports, matching the IAM scanner's published role resource_id.
	RelationshipFISTemplateUsesIAMRole = "fis_template_uses_iam_role"
	// RelationshipFISTemplateTargetsEC2Instance records an FIS experiment
	// template's explicit EC2 instance target. The target is keyed by the bare
	// instance id (i-...) extracted from the reported instance ARN, matching the
	// identity EC2 instance edges publish across the fleet.
	RelationshipFISTemplateTargetsEC2Instance = "fis_template_targets_ec2_instance"
	// RelationshipFISTemplateTargetsECSCluster records an FIS experiment
	// template's explicit ECS cluster target. The target is keyed by the cluster
	// ARN FIS reports, matching the ECS scanner's published cluster resource_id.
	RelationshipFISTemplateTargetsECSCluster = "fis_template_targets_ecs_cluster"
	// RelationshipFISTemplateTargetsRDSDBInstance records an FIS experiment
	// template's explicit RDS DB instance target. The target is keyed by the DB
	// instance ARN FIS reports, matching the RDS scanner's published instance
	// resource_id.
	RelationshipFISTemplateTargetsRDSDBInstance = "fis_template_targets_rds_db_instance"
	// RelationshipFISTemplateTargetsRDSDBCluster records an FIS experiment
	// template's explicit RDS DB cluster target. The target is keyed by the DB
	// cluster ARN FIS reports, matching the RDS scanner's published cluster
	// resource_id.
	RelationshipFISTemplateTargetsRDSDBCluster = "fis_template_targets_rds_db_cluster"
	// RelationshipFISTemplateLogsToCloudWatchLogGroup records the CloudWatch
	// Logs log group an FIS experiment template streams experiment logs to. The
	// target is keyed by the log group ARN FIS reports, matching the
	// cloudwatchlogs scanner's published log group resource_id.
	RelationshipFISTemplateLogsToCloudWatchLogGroup = "fis_template_logs_to_cloudwatch_log_group"
	// RelationshipFISTemplateLogsToS3 records the S3 bucket an FIS experiment
	// template writes experiment logs to. FIS reports a bucket name, so the
	// scanner synthesizes the partition-aware bucket ARN to match the S3
	// scanner's published bucket resource_id.
	RelationshipFISTemplateLogsToS3 = "fis_template_logs_to_s3"
	// RelationshipFISTemplateStopsOnCloudWatchAlarm records a CloudWatch alarm
	// an FIS experiment template halts on as a stop condition. The target is
	// keyed by the alarm ARN FIS reports, matching the cloudwatch scanner's
	// published alarm resource_id.
	RelationshipFISTemplateStopsOnCloudWatchAlarm = "fis_template_stops_on_cloudwatch_alarm"
)
