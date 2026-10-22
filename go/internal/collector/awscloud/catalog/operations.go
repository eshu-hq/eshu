// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

const (
	// ServiceCloudTrail identifies the regional AWS CloudTrail metadata scan
	// slice. CloudTrail is the audit-config service; the scanner emits trail
	// and Lake event-data-store configuration only. CloudTrail event payloads
	// are the protected data class and must never be read through this
	// service kind.
	ServiceCloudTrail = "cloudtrail"
)

const (
	// ResourceTypeCloudTrailChannel identifies a CloudTrail channel metadata
	// resource. The scanner emits channel identity and destination metadata
	// only; event payloads are not associated with this resource.
	ResourceTypeCloudTrailChannel = "aws_cloudtrail_channel"
	// ResourceTypeCloudTrailDashboardConfig identifies a CloudTrail Lake
	// dashboard configuration metadata resource. The scanner emits dashboard
	// identity and status only; widget query bodies and result rows are not
	// part of the contract.
	ResourceTypeCloudTrailDashboardConfig = "aws_cloudtrail_dashboard_config"
	// ResourceTypeCloudTrailEventDataStore identifies a CloudTrail Lake event
	// data store metadata resource. The scanner emits store identity,
	// retention, and selector-count summary only; advanced event selector
	// bodies, Lake query strings, and query result rows are not part of the
	// contract.
	ResourceTypeCloudTrailEventDataStore = "aws_cloudtrail_event_data_store"
	// ResourceTypeCloudTrailTrail identifies a CloudTrail trail metadata
	// resource. The scanner emits trail configuration only; the audit event
	// payload itself is the protected data class and is never persisted by
	// this scanner.
	ResourceTypeCloudTrailTrail = "aws_cloudtrail_trail"
)

const (
	// RelationshipCloudTrailEventDataStoreUsesKMSKey records a CloudTrail
	// event data store's reported KMS key dependency.
	RelationshipCloudTrailEventDataStoreUsesKMSKey = "cloudtrail_event_data_store_uses_kms_key"
	// RelationshipCloudTrailTrailLogsToCloudWatchLogs records a CloudTrail
	// trail's reported CloudWatch Logs log group destination.
	RelationshipCloudTrailTrailLogsToCloudWatchLogs = "cloudtrail_trail_logs_to_cloudwatch_logs"
	// RelationshipCloudTrailTrailLogsToS3Bucket records a CloudTrail trail's
	// reported S3 destination bucket.
	RelationshipCloudTrailTrailLogsToS3Bucket = "cloudtrail_trail_logs_to_s3_bucket"
	// RelationshipCloudTrailTrailNotifiesSNSTopic records a CloudTrail trail's
	// reported SNS topic delivery target.
	RelationshipCloudTrailTrailNotifiesSNSTopic = "cloudtrail_trail_notifies_sns_topic"
	// RelationshipCloudTrailTrailUsesKMSKey records a CloudTrail trail's
	// reported KMS key dependency.
	RelationshipCloudTrailTrailUsesKMSKey = "cloudtrail_trail_uses_kms_key"
)

const (
	// ServiceCloudWatch identifies the regional Amazon CloudWatch metadata-only
	// scan slice covering metric alarms, composite alarms, dashboard
	// identities, Contributor Insights rule identities, and metric streams.
	// Dashboard body JSON and Contributor Insights rule definitions stay
	// outside the scan slice; the CloudWatch Logs log group surface lives in
	// ServiceCloudWatchLogs.
	ServiceCloudWatch = "cloudwatch"
)

const (
	// ResourceTypeCloudWatchAlarm identifies a CloudWatch metric alarm metadata
	// resource.
	ResourceTypeCloudWatchAlarm = "aws_cloudwatch_alarm"
	// ResourceTypeCloudWatchCompositeAlarm identifies a CloudWatch composite
	// alarm metadata resource.
	ResourceTypeCloudWatchCompositeAlarm = "aws_cloudwatch_composite_alarm"
	// ResourceTypeCloudWatchDashboard identifies a CloudWatch dashboard
	// metadata resource. The scanner persists the dashboard name and
	// last-modified timestamp only; the dashboard body JSON is never
	// persisted because widget bodies often reveal internal infrastructure
	// naming and KPI thresholds.
	ResourceTypeCloudWatchDashboard = "aws_cloudwatch_dashboard"
	// ResourceTypeCloudWatchInsightRule identifies a CloudWatch Contributor
	// Insights rule metadata resource. The scanner persists the rule name
	// and state only; the rule definition is never persisted because the
	// SQL-like grammar may encode customer query patterns.
	ResourceTypeCloudWatchInsightRule = "aws_cloudwatch_insight_rule"
	// ResourceTypeCloudWatchMetricStream identifies a CloudWatch metric stream
	// metadata resource.
	ResourceTypeCloudWatchMetricStream = "aws_cloudwatch_metric_stream"
)

const (
	// RelationshipCloudWatchAlarmNotifiesSNSTopic records an SNS topic listed
	// in a CloudWatch alarm's alarm, ok, or insufficient-data actions.
	RelationshipCloudWatchAlarmNotifiesSNSTopic = "cloudwatch_alarm_notifies_sns_topic"
	// RelationshipCloudWatchCompositeAlarmHasChildAlarm records a child alarm
	// referenced by a CloudWatch composite alarm's AlarmRule expression.
	RelationshipCloudWatchCompositeAlarmHasChildAlarm = "cloudwatch_composite_alarm_has_child_alarm"
	// RelationshipCloudWatchMetricStreamDeliversToFirehose records a metric
	// stream's reported Kinesis Data Firehose delivery destination.
	RelationshipCloudWatchMetricStreamDeliversToFirehose = "cloudwatch_metric_stream_delivers_to_firehose"
	// RelationshipCloudWatchAlarmObservesMetric records a metric identity
	// (namespace, metric name, and dimension summary) observed by a CloudWatch
	// metric alarm. Customer-tag-named dimension values route through the
	// shared redact library before persistence.
	RelationshipCloudWatchAlarmObservesMetric = "cloudwatch_alarm_observes_metric"
)

const (
	// ServiceCloudWatchLogs identifies the regional Amazon CloudWatch Logs log
	// group metadata scan slice.
	ServiceCloudWatchLogs = "cloudwatchlogs"
)

const (
	// ResourceTypeCloudWatchLogsLogGroup identifies a CloudWatch Logs log group
	// metadata resource.
	ResourceTypeCloudWatchLogsLogGroup = "aws_cloudwatch_logs_log_group"
)

const (
	// RelationshipCloudWatchLogsLogGroupUsesKMSKey records a CloudWatch Logs
	// log group's reported KMS key dependency.
	RelationshipCloudWatchLogsLogGroupUsesKMSKey = "cloudwatch_logs_log_group_uses_kms_key"
)

const (
	// ServiceXRay identifies the regional AWS X-Ray metadata-only scan slice.
	// The scanner emits X-Ray configuration only: trace groups, sampling rules,
	// and the account-region encryption configuration. It never reads or
	// persists observability payload — traces, trace summaries, segments, or
	// service-graph (service-map) data are outside the scan slice by
	// construction (the SDK adapter read surface excludes GetTraceSummaries,
	// BatchGetTraces, GetTraceGraph, GetServiceGraph, GetTimeSeriesService
	// Statistics, GetInsight*, and every Put*/Create*/Update*/Delete* mutation).
	ServiceXRay = "xray"
)

const (
	// ResourceTypeXRayGroup identifies an X-Ray group configuration resource.
	// The scanner persists the group name, ARN, and trace filter expression
	// (a configuration string, not trace data) plus insights-enablement flags.
	// Traces selected by the filter expression are never read.
	ResourceTypeXRayGroup = "aws_xray_group"
	// ResourceTypeXRaySamplingRule identifies an X-Ray sampling rule
	// configuration resource. The scanner persists the rule name, ARN,
	// priority, reservoir size, fixed rate, and the service name/type/host
	// match criteria. These describe which requests X-Ray samples; no sampled
	// trace, segment, or summary is read.
	ResourceTypeXRaySamplingRule = "aws_xray_sampling_rule"
	// ResourceTypeXRayEncryptionConfig identifies the account-region X-Ray
	// encryption configuration resource. X-Ray exposes a single encryption
	// configuration per account and region (no ARN), so the scanner keys it by
	// a synthetic "<account>/<region>/xray-encryption-config" resource id. It
	// carries the encryption type (NONE or KMS), status, and the configured
	// KMS key reference only.
	ResourceTypeXRayEncryptionConfig = "aws_xray_encryption_config"
	// ResourceTypeXRayServiceCorrelation identifies the synthetic service
	// identity an X-Ray sampling rule matches by service name and service
	// type. X-Ray sampling rules name a service by the string it reports in
	// segments (ServiceName) and its origin type (ServiceType); this is a
	// labeled correlation anchor, not a scanned AWS resource, so reducers join
	// it to the real service node by name during materialization. Declaring it
	// as a ResourceType constant keeps the relationship graph-join contract
	// satisfied without a fabricated ARN.
	ResourceTypeXRayServiceCorrelation = "aws_xray_service_correlation"
)

const (
	// RelationshipXRayEncryptionConfigUsesKMSKey records the KMS key the X-Ray
	// account-region encryption configuration uses, emitted only when the
	// encryption type is KMS and AWS reports a key reference. The target is the
	// KMS key family (aws_kms_key); the join key is the reported key
	// id/ARN/alias, matching how the KMS scanner publishes its key resource id.
	RelationshipXRayEncryptionConfigUsesKMSKey = "xray_encryption_config_uses_kms_key"
	// RelationshipXRaySamplingRuleMatchesService records the service identity a
	// sampling rule matches by service name and service type. It is a labeled
	// correlation anchor (target_type aws_xray_service_correlation): the rule
	// names the service by the string it reports in segments, and the reducer
	// resolves it to the real service node by name during materialization.
	RelationshipXRaySamplingRuleMatchesService = "xray_sampling_rule_matches_service"
)

const (
	// ServiceApplicationAutoScaling identifies the regional AWS Application Auto
	// Scaling metadata-only scan slice. The scanner reads scalable target,
	// scaling policy, and scheduled action control-plane metadata through the
	// Application Auto Scaling Describe APIs (DescribeScalableTargets,
	// DescribeScalingPolicies, DescribeScheduledActions) across every supported
	// service namespace. It never registers, deregisters, or mutates a scalable
	// target, never puts or deletes a scaling policy or scheduled action, and
	// never invokes a scaling action.
	ServiceApplicationAutoScaling = "applicationautoscaling"
)

const (
	// ResourceTypeApplicationAutoScalingScalableTarget identifies an Application
	// Auto Scaling scalable target metadata resource. The scanner emits the
	// service namespace, scalable dimension, the scaled resource id, the IAM
	// role ARN Application Auto Scaling assumes, and the min/max capacity bounds.
	ResourceTypeApplicationAutoScalingScalableTarget = "aws_application_autoscaling_scalable_target"
	// ResourceTypeApplicationAutoScalingScalingPolicy identifies an Application
	// Auto Scaling scaling policy metadata resource. The scanner emits identity,
	// policy type, the owning service namespace/dimension/resource id, and the
	// names of the CloudWatch alarms the policy is bound to. Step-scaling and
	// target-tracking configuration bodies are intentionally excluded.
	ResourceTypeApplicationAutoScalingScalingPolicy = "aws_application_autoscaling_scaling_policy"
	// ResourceTypeApplicationAutoScalingScheduledAction identifies an Application
	// Auto Scaling scheduled action metadata resource. The scanner emits
	// identity, the owning service namespace/dimension/resource id, the schedule
	// expression, the time zone, and the start/end window. The min/max target
	// capacity carried by the action is recorded; no other body is read.
	ResourceTypeApplicationAutoScalingScheduledAction = "aws_application_autoscaling_scheduled_action"
)

const (
	// RelationshipApplicationAutoScalingPolicyForScalableTarget records that a
	// scaling policy governs a scalable target. The target is keyed by the same
	// composite scalable-target resource_id the scalable-target node publishes,
	// so the edge joins that node exactly.
	RelationshipApplicationAutoScalingPolicyForScalableTarget = "application_autoscaling_policy_for_scalable_target"
	// RelationshipApplicationAutoScalingScheduledActionForScalableTarget records
	// that a scheduled action targets a scalable target, keyed by the composite
	// scalable-target resource_id the scalable-target node publishes.
	RelationshipApplicationAutoScalingScheduledActionForScalableTarget = "application_autoscaling_scheduled_action_for_scalable_target"
	// RelationshipApplicationAutoScalingTargetScalesDynamoDBTable records that a
	// scalable target governs a DynamoDB table's provisioned capacity. The target
	// is keyed by the partition-aware DynamoDB table ARN the DynamoDB scanner
	// publishes as its table resource_id.
	RelationshipApplicationAutoScalingTargetScalesDynamoDBTable = "application_autoscaling_target_scales_dynamodb_table"
	// RelationshipApplicationAutoScalingTargetScalesECSService records that a
	// scalable target governs an ECS service's desired task count. The target is
	// keyed by the partition-aware ECS service ARN the ECS scanner publishes as
	// its service resource_id.
	RelationshipApplicationAutoScalingTargetScalesECSService = "application_autoscaling_target_scales_ecs_service"
	// RelationshipApplicationAutoScalingTargetScalesRDSCluster records that a
	// scalable target governs an Aurora DB cluster's read-replica count. The
	// target is keyed by the partition-aware RDS cluster ARN the RDS scanner
	// publishes as its cluster resource_id.
	RelationshipApplicationAutoScalingTargetScalesRDSCluster = "application_autoscaling_target_scales_rds_cluster"
	// RelationshipApplicationAutoScalingTargetScalesLambdaFunction records that a
	// scalable target governs a Lambda function's provisioned concurrency. The
	// target is keyed by the partition-aware base Lambda function ARN the Lambda
	// scanner publishes as its function resource_id; the version/alias qualifier
	// carried by the scaling resource id is dropped to match the function node.
	RelationshipApplicationAutoScalingTargetScalesLambdaFunction = "application_autoscaling_target_scales_lambda_function"
	// RelationshipApplicationAutoScalingPolicyTriggersCloudWatchAlarm records that
	// a scaling policy is bound to a CloudWatch alarm. The target is keyed by the
	// alarm ARN Application Auto Scaling reports, which matches the CloudWatch
	// scanner's published alarm resource_id.
	RelationshipApplicationAutoScalingPolicyTriggersCloudWatchAlarm = "application_autoscaling_policy_triggers_cloudwatch_alarm"
)

const (
	// ServiceComputeOptimizer identifies the regional AWS Compute Optimizer
	// metadata-only scan slice. The scanner reads recommendation control-plane
	// metadata through the Compute Optimizer get APIs (GetRecommendationSummaries,
	// GetEC2InstanceRecommendations, GetAutoScalingGroupRecommendations,
	// GetEBSVolumeRecommendations, GetLambdaFunctionRecommendations) and never
	// mutates Compute Optimizer state, never enrolls or unenrolls an account, and
	// never reads CloudWatch metric data points behind a recommendation.
	ServiceComputeOptimizer = "computeoptimizer"
)

const (
	// ResourceTypeComputeOptimizerRecommendationSummary identifies a Compute
	// Optimizer recommendation summary metadata resource. The scanner emits one
	// summary per recommendation resource type (EC2 instance, Auto Scaling group,
	// EBS volume, Lambda function) for the claimed account and region, carrying the
	// finding-class counts and aggregated savings-opportunity percentage only.
	ResourceTypeComputeOptimizerRecommendationSummary = "aws_compute_optimizer_recommendation_summary"
	// ResourceTypeComputeOptimizerRecommendation identifies a single Compute
	// Optimizer per-resource recommendation metadata resource. The scanner emits
	// one recommendation per analyzed EC2 instance, Auto Scaling group, EBS volume,
	// and Lambda function, carrying the finding, current configuration shape, and
	// top recommended-option shape only. It never persists the underlying
	// CloudWatch utilization metric data points.
	ResourceTypeComputeOptimizerRecommendation = "aws_compute_optimizer_recommendation"
)

const (
	// RelationshipComputeOptimizerRecommendationTargetsInstance records that a
	// Compute Optimizer recommendation analyzes an EC2 instance. The target is
	// keyed by the bare EC2 instance id (i-...), matching how EC2 instance
	// relationship targets are published, so the edge joins the instance identity
	// instead of dangling.
	RelationshipComputeOptimizerRecommendationTargetsInstance = "compute_optimizer_recommendation_targets_instance"
	// RelationshipComputeOptimizerRecommendationTargetsAutoScalingGroup records
	// that a Compute Optimizer recommendation analyzes an Auto Scaling group. The
	// target is keyed by the Auto Scaling group name, matching how the autoscaling
	// scanner publishes its group resource_id.
	RelationshipComputeOptimizerRecommendationTargetsAutoScalingGroup = "compute_optimizer_recommendation_targets_auto_scaling_group"
	// RelationshipComputeOptimizerRecommendationTargetsFunction records that a
	// Compute Optimizer recommendation analyzes a Lambda function. The target is
	// keyed by the function ARN, matching how the lambda scanner publishes its
	// function resource_id.
	RelationshipComputeOptimizerRecommendationTargetsFunction = "compute_optimizer_recommendation_targets_function"
)

const (
	// ServiceMWAA identifies the regional Amazon Managed Workflows for Apache
	// Airflow (MWAA) metadata-only scan slice covering environments and their
	// S3, VPC, IAM, KMS, and CloudWatch Logs dependency edges.
	ServiceMWAA = "mwaa"
)

const (
	// ResourceTypeMWAAEnvironment identifies an Amazon MWAA environment metadata
	// resource. Apache Airflow configuration option values, connection strings,
	// and CLI/web login tokens stay outside the scanner contract.
	ResourceTypeMWAAEnvironment = "aws_mwaa_environment"
)

const (
	// RelationshipMWAAEnvironmentUsesS3Bucket records the Amazon S3 bucket that
	// stores an MWAA environment's DAGs and supporting files. The target is the
	// AWS-reported source-bucket ARN.
	RelationshipMWAAEnvironmentUsesS3Bucket = "mwaa_environment_uses_s3_bucket"
	// RelationshipMWAAEnvironmentUsesSubnet records a subnet an MWAA environment
	// is attached to through its VPC network configuration.
	RelationshipMWAAEnvironmentUsesSubnet = "mwaa_environment_uses_subnet"
	// RelationshipMWAAEnvironmentUsesSecurityGroup records a security group an
	// MWAA environment is attached to through its VPC network configuration.
	RelationshipMWAAEnvironmentUsesSecurityGroup = "mwaa_environment_uses_security_group"
	// RelationshipMWAAEnvironmentUsesIAMRole records the IAM execution role an
	// MWAA environment assumes to access Amazon Web Services resources.
	RelationshipMWAAEnvironmentUsesIAMRole = "mwaa_environment_uses_iam_role"
	// RelationshipMWAAEnvironmentUsesKMSKey records the KMS key an MWAA
	// environment uses to encrypt its data at rest.
	RelationshipMWAAEnvironmentUsesKMSKey = "mwaa_environment_uses_kms_key"
	// RelationshipMWAAEnvironmentLogsToCloudWatchLogGroup records a CloudWatch
	// Logs log group an MWAA environment publishes one Airflow log module to.
	RelationshipMWAAEnvironmentLogsToCloudWatchLogGroup = "mwaa_environment_logs_to_cloudwatch_log_group"
)
