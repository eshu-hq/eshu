// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmwaatypes "github.com/aws/aws-sdk-go-v2/service/mwaa/types"

	mwaaservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/mwaa"
)

// mapEnvironment converts an AWS SDK MWAA environment into the scanner-owned
// view. It deliberately never reads AirflowConfigurationOptions (the Apache
// Airflow configuration option values), CeleryExecutorQueue,
// DatabaseVpcEndpointService, WebserverUrl, or WebserverVpcEndpointService, so
// configuration values, internal queue ARNs, and webserver endpoints can never
// be persisted. Only safe identity, placement, and dependency metadata is
// mapped.
func mapEnvironment(raw awsmwaatypes.Environment) mwaaservice.Environment {
	environment := mwaaservice.Environment{
		Name:                strings.TrimSpace(aws.ToString(raw.Name)),
		ARN:                 strings.TrimSpace(aws.ToString(raw.Arn)),
		Status:              strings.TrimSpace(string(raw.Status)),
		AirflowVersion:      strings.TrimSpace(aws.ToString(raw.AirflowVersion)),
		WebserverAccessMode: strings.TrimSpace(string(raw.WebserverAccessMode)),
		EnvironmentClass:    strings.TrimSpace(aws.ToString(raw.EnvironmentClass)),
		EndpointManagement:  strings.TrimSpace(string(raw.EndpointManagement)),
		Schedulers:          aws.ToInt32(raw.Schedulers),
		MinWorkers:          aws.ToInt32(raw.MinWorkers),
		MaxWorkers:          aws.ToInt32(raw.MaxWorkers),
		MinWebservers:       aws.ToInt32(raw.MinWebservers),
		MaxWebservers:       aws.ToInt32(raw.MaxWebservers),
		CreatedAt:           aws.ToTime(raw.CreatedAt),
		SourceBucketARN:     strings.TrimSpace(aws.ToString(raw.SourceBucketArn)),
		ExecutionRoleARN:    strings.TrimSpace(aws.ToString(raw.ExecutionRoleArn)),
		ServiceRoleARN:      strings.TrimSpace(aws.ToString(raw.ServiceRoleArn)),
		KMSKey:              strings.TrimSpace(aws.ToString(raw.KmsKey)),
		Tags:                cloneStringMap(raw.Tags),
	}
	if raw.NetworkConfiguration != nil {
		environment.SubnetIDs = cloneStringSlice(raw.NetworkConfiguration.SubnetIds)
		environment.SecurityGroupIDs = cloneStringSlice(raw.NetworkConfiguration.SecurityGroupIds)
	}
	environment.LogGroups = mapLogGroups(raw.LoggingConfiguration)
	return environment
}

// mapLogGroups flattens the per-module Airflow logging configuration into the
// scanner-owned log group references. Only the module name, the CloudWatch
// Logs log group ARN, the enabled flag, and the non-secret log level are
// mapped; Airflow log record contents are never read.
func mapLogGroups(config *awsmwaatypes.LoggingConfiguration) []mwaaservice.LogGroup {
	if config == nil {
		return nil
	}
	modules := []struct {
		name   string
		module *awsmwaatypes.ModuleLoggingConfiguration
	}{
		{name: "DagProcessingLogs", module: config.DagProcessingLogs},
		{name: "SchedulerLogs", module: config.SchedulerLogs},
		{name: "TaskLogs", module: config.TaskLogs},
		{name: "WebserverLogs", module: config.WebserverLogs},
		{name: "WorkerLogs", module: config.WorkerLogs},
	}
	logGroups := make([]mwaaservice.LogGroup, 0, len(modules))
	for _, entry := range modules {
		if entry.module == nil {
			continue
		}
		arn := strings.TrimSpace(aws.ToString(entry.module.CloudWatchLogGroupArn))
		if arn == "" {
			continue
		}
		logGroups = append(logGroups, mwaaservice.LogGroup{
			Module:   entry.name,
			ARN:      arn,
			Enabled:  aws.ToBool(entry.module.Enabled),
			LogLevel: strings.TrimSpace(string(entry.module.LogLevel)),
		})
	}
	if len(logGroups) == 0 {
		return nil
	}
	return logGroups
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneStringSlice(input []string) []string {
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
