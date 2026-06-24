// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awskav2types "github.com/aws/aws-sdk-go-v2/service/kinesisanalyticsv2/types"

	kav2service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/kinesisanalyticsv2"
)

// logStreamSeparator marks the log-stream segment of a CloudWatch logging-option
// ARN. AWS reports a log STREAM ARN
// (arn:…:log-group:<name>:log-stream:<stream>) for an application's logging
// option, while the cloudwatchlogs scanner publishes the log GROUP ARN
// (arn:…:log-group:<name>). Trimming at this separator (and any trailing :*
// wildcard) yields the log group ARN that joins the cloudwatchlogs node.
const logStreamSeparator = ":log-stream:"

// mapApplication maps one DescribeApplication detail into the scanner-owned
// Application metadata view. It copies identity, runtime, mode, parallelism,
// posture, version counters, lifecycle timestamps, and join-relevant references
// only. Application code bodies, SQL text, environment property values, and
// run-configuration content are never copied.
func mapApplication(detail *awskav2types.ApplicationDetail) kav2service.Application {
	application := kav2service.Application{
		Name:                    strings.TrimSpace(aws.ToString(detail.ApplicationName)),
		ARN:                     strings.TrimSpace(aws.ToString(detail.ApplicationARN)),
		Status:                  strings.TrimSpace(string(detail.ApplicationStatus)),
		RuntimeEnvironment:      strings.TrimSpace(string(detail.RuntimeEnvironment)),
		Mode:                    strings.TrimSpace(string(detail.ApplicationMode)),
		Description:             strings.TrimSpace(aws.ToString(detail.ApplicationDescription)),
		VersionID:               aws.ToInt64(detail.ApplicationVersionId),
		ServiceExecutionRoleARN: strings.TrimSpace(aws.ToString(detail.ServiceExecutionRole)),
		CreateTimestamp:         aws.ToTime(detail.CreateTimestamp),
		LastUpdateTimestamp:     aws.ToTime(detail.LastUpdateTimestamp),
		LogGroupARNs:            logGroupARNs(detail.CloudWatchLoggingOptionDescriptions),
	}
	applyConfiguration(&application, detail.ApplicationConfigurationDescription)
	return application
}

// applyConfiguration copies the metadata-only fields from the application
// configuration description: snapshot posture, code content format and S3 code
// location reference, Flink parallelism, and the SQL input/output stream
// references. It never copies code bodies, SQL text, environment property
// values, or run-configuration content.
func applyConfiguration(
	application *kav2service.Application,
	config *awskav2types.ApplicationConfigurationDescription,
) {
	if config == nil {
		return
	}
	if snapshot := config.ApplicationSnapshotConfigurationDescription; snapshot != nil {
		application.SnapshotsEnabled = aws.ToBool(snapshot.SnapshotsEnabled)
	}
	applyCodeConfiguration(application, config.ApplicationCodeConfigurationDescription)
	applyFlinkConfiguration(application, config.FlinkApplicationConfigurationDescription)
	applySQLConfiguration(application, config.SqlApplicationConfigurationDescription)
	application.VPCConfigurations = vpcConfigurations(config.VpcConfigurationDescriptions)
}

// applyCodeConfiguration records the application code content FORMAT and the S3
// code location bucket ARN and object key only. The text-format code body and
// the zip checksum/size are deliberately not copied; the application code body
// is never read.
func applyCodeConfiguration(
	application *kav2service.Application,
	code *awskav2types.ApplicationCodeConfigurationDescription,
) {
	if code == nil {
		return
	}
	application.CodeContentType = strings.TrimSpace(string(code.CodeContentType))
	content := code.CodeContentDescription
	if content == nil || content.S3ApplicationCodeLocationDescription == nil {
		return
	}
	location := content.S3ApplicationCodeLocationDescription
	application.CodeS3BucketARN = strings.TrimSpace(aws.ToString(location.BucketARN))
	application.CodeS3FileKey = strings.TrimSpace(aws.ToString(location.FileKey))
}

// applyFlinkConfiguration records the Flink parallelism posture. The job plan
// description and checkpoint/monitoring config bodies are deliberately not
// copied.
func applyFlinkConfiguration(
	application *kav2service.Application,
	flink *awskav2types.FlinkApplicationConfigurationDescription,
) {
	if flink == nil || flink.ParallelismConfigurationDescription == nil {
		return
	}
	parallelism := flink.ParallelismConfigurationDescription
	application.AutoScalingEnabled = aws.ToBool(parallelism.AutoScalingEnabled)
	application.ParallelismConfigurationType = strings.TrimSpace(string(parallelism.ConfigurationType))
	application.Parallelism = aws.ToInt32(parallelism.Parallelism)
	application.ParallelismPerKPU = aws.ToInt32(parallelism.ParallelismPerKPU)
	application.CurrentParallelism = aws.ToInt32(parallelism.CurrentParallelism)
}

// applySQLConfiguration records the SQL input/output Kinesis data stream and
// Firehose delivery stream ARNs only. The in-application stream names, schemas,
// and input-processing (Lambda) configuration references are deliberately not
// copied.
func applySQLConfiguration(
	application *kav2service.Application,
	sql *awskav2types.SqlApplicationConfigurationDescription,
) {
	if sql == nil {
		return
	}
	for _, input := range sql.InputDescriptions {
		if input.KinesisStreamsInputDescription != nil {
			if arn := strings.TrimSpace(aws.ToString(input.KinesisStreamsInputDescription.ResourceARN)); arn != "" {
				application.InputKinesisStreamARNs = append(application.InputKinesisStreamARNs, arn)
			}
		}
		if input.KinesisFirehoseInputDescription != nil {
			if arn := strings.TrimSpace(aws.ToString(input.KinesisFirehoseInputDescription.ResourceARN)); arn != "" {
				application.InputFirehoseStreamARNs = append(application.InputFirehoseStreamARNs, arn)
			}
		}
	}
	for _, output := range sql.OutputDescriptions {
		if output.KinesisStreamsOutputDescription != nil {
			if arn := strings.TrimSpace(aws.ToString(output.KinesisStreamsOutputDescription.ResourceARN)); arn != "" {
				application.OutputKinesisStreamARNs = append(application.OutputKinesisStreamARNs, arn)
			}
		}
		if output.KinesisFirehoseOutputDescription != nil {
			if arn := strings.TrimSpace(aws.ToString(output.KinesisFirehoseOutputDescription.ResourceARN)); arn != "" {
				application.OutputFirehoseStreamARNs = append(application.OutputFirehoseStreamARNs, arn)
			}
		}
	}
}

// vpcConfigurations maps the application's VPC configuration descriptions into
// the scanner-owned join-relevant placement: the VPC id, the bare subnet ids,
// and the bare security group ids.
func vpcConfigurations(descriptions []awskav2types.VpcConfigurationDescription) []kav2service.VPCConfiguration {
	if len(descriptions) == 0 {
		return nil
	}
	configs := make([]kav2service.VPCConfiguration, 0, len(descriptions))
	for _, description := range descriptions {
		configs = append(configs, kav2service.VPCConfiguration{
			VPCConfigurationID: strings.TrimSpace(aws.ToString(description.VpcConfigurationId)),
			VPCID:              strings.TrimSpace(aws.ToString(description.VpcId)),
			SubnetIDs:          trimmedStrings(description.SubnetIds),
			SecurityGroupIDs:   trimmedStrings(description.SecurityGroupIds),
		})
	}
	return configs
}

// logGroupARNs maps each CloudWatch logging option's reported log STREAM ARN to
// the non-wildcard log GROUP ARN the cloudwatchlogs scanner publishes, dropping
// duplicates. Options that report no usable ARN are skipped.
func logGroupARNs(options []awskav2types.CloudWatchLoggingOptionDescription) []string {
	if len(options) == 0 {
		return nil
	}
	var arns []string
	seen := make(map[string]struct{}, len(options))
	for _, option := range options {
		groupARN := logGroupARNFromLogStreamARN(aws.ToString(option.LogStreamARN))
		if groupARN == "" {
			continue
		}
		if _, ok := seen[groupARN]; ok {
			continue
		}
		seen[groupARN] = struct{}{}
		arns = append(arns, groupARN)
	}
	return arns
}

// logGroupARNFromLogStreamARN converts a CloudWatch log STREAM ARN into the log
// GROUP ARN the cloudwatchlogs scanner publishes as its node resource_id. AWS
// reports arn:…:log-group:<name>:log-stream:<stream>; trimming the log-stream
// segment (and any trailing :* wildcard) yields arn:…:log-group:<name>. A value
// already in log-group form is returned with only the wildcard trimmed.
func logGroupARNFromLogStreamARN(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if index := strings.Index(value, logStreamSeparator); index >= 0 {
		value = value[:index]
	}
	value = strings.TrimSuffix(value, ":*")
	return strings.TrimSpace(value)
}

// trimmedStrings returns a trimmed copy of input with empty entries dropped, or
// nil when nothing survives.
func trimmedStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
