// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmgn "github.com/aws/aws-sdk-go-v2/service/mgn"
	awsmgntypes "github.com/aws/aws-sdk-go-v2/service/mgn/types"

	mgnservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/mgn"
)

// mapApplication maps an SDK MGN application into the scanner-owned metadata
// model, copying only non-secret control-plane fields.
func mapApplication(application awsmgntypes.Application) mgnservice.Application {
	mapped := mgnservice.Application{
		ARN:              strings.TrimSpace(aws.ToString(application.Arn)),
		ApplicationID:    strings.TrimSpace(aws.ToString(application.ApplicationID)),
		Name:             strings.TrimSpace(aws.ToString(application.Name)),
		Description:      strings.TrimSpace(aws.ToString(application.Description)),
		WaveID:           strings.TrimSpace(aws.ToString(application.WaveID)),
		IsArchived:       aws.ToBool(application.IsArchived),
		CreationTime:     parseAPITime(aws.ToString(application.CreationDateTime)),
		LastModifiedTime: parseAPITime(aws.ToString(application.LastModifiedDateTime)),
		Tags:             cloneTags(application.Tags),
	}
	if status := application.ApplicationAggregatedStatus; status != nil {
		mapped.HealthStatus = strings.TrimSpace(string(status.HealthStatus))
		mapped.ProgressStatus = strings.TrimSpace(string(status.ProgressStatus))
		mapped.TotalSourceServers = status.TotalSourceServers
	}
	return mapped
}

// mapSourceServer maps an SDK MGN source server into the scanner-owned metadata
// model. It copies lifecycle, replication state, recommended target type, the
// launched instance id, and non-secret identification hints only; it never
// copies replication-agent credentials or replicated disk contents.
func mapSourceServer(server awsmgntypes.SourceServer) mgnservice.SourceServer {
	mapped := mgnservice.SourceServer{
		ARN:             strings.TrimSpace(aws.ToString(server.Arn)),
		SourceServerID:  strings.TrimSpace(aws.ToString(server.SourceServerID)),
		ApplicationID:   strings.TrimSpace(aws.ToString(server.ApplicationID)),
		ReplicationType: strings.TrimSpace(string(server.ReplicationType)),
		IsArchived:      aws.ToBool(server.IsArchived),
		VcenterClientID: strings.TrimSpace(aws.ToString(server.VcenterClientID)),
		Tags:            cloneTags(server.Tags),
	}
	if lifecycle := server.LifeCycle; lifecycle != nil {
		mapped.LifeCycleState = strings.TrimSpace(string(lifecycle.State))
	}
	if replication := server.DataReplicationInfo; replication != nil {
		mapped.DataReplicationState = strings.TrimSpace(string(replication.DataReplicationState))
	}
	if launched := server.LaunchedInstance; launched != nil {
		mapped.LaunchedEC2InstanceID = strings.TrimSpace(aws.ToString(launched.Ec2InstanceID))
	}
	applySourceProperties(&mapped, server.SourceProperties)
	return mapped
}

// applySourceProperties copies the non-secret subset of source properties: the
// recommended instance type, OS full string, and identification hints. Disks,
// CPUs, RAM detail, and network interface detail are intentionally excluded.
func applySourceProperties(server *mgnservice.SourceServer, properties *awsmgntypes.SourceProperties) {
	if properties == nil {
		return
	}
	server.RecommendedInstanceType = strings.TrimSpace(aws.ToString(properties.RecommendedInstanceType))
	if os := properties.Os; os != nil {
		server.OS = strings.TrimSpace(aws.ToString(os.FullString))
	}
	if hints := properties.IdentificationHints; hints != nil {
		server.Hostname = strings.TrimSpace(aws.ToString(hints.Hostname))
		server.FQDN = strings.TrimSpace(aws.ToString(hints.Fqdn))
		server.AWSInstanceID = strings.TrimSpace(aws.ToString(hints.AwsInstanceID))
	}
}

// mapLaunchConfiguration maps an SDK GetLaunchConfiguration output into the
// scanner-owned launch configuration model for one source server.
func mapLaunchConfiguration(
	sourceServerID string,
	output *awsmgn.GetLaunchConfigurationOutput,
) *mgnservice.LaunchConfiguration {
	return &mgnservice.LaunchConfiguration{
		SourceServerID:                      strings.TrimSpace(sourceServerID),
		Name:                                strings.TrimSpace(aws.ToString(output.Name)),
		LaunchDisposition:                   strings.TrimSpace(string(output.LaunchDisposition)),
		BootMode:                            strings.TrimSpace(string(output.BootMode)),
		TargetInstanceTypeRightSizingMethod: strings.TrimSpace(string(output.TargetInstanceTypeRightSizingMethod)),
		EC2LaunchTemplateID:                 strings.TrimSpace(aws.ToString(output.Ec2LaunchTemplateID)),
		CopyPrivateIP:                       aws.ToBool(output.CopyPrivateIp),
		CopyTags:                            aws.ToBool(output.CopyTags),
	}
}

// mapJob maps an SDK MGN job into the scanner-owned metadata model, copying the
// job identity, type, status, initiator, timestamps, and the participating
// source server ids only.
func mapJob(job awsmgntypes.Job) mgnservice.Job {
	mapped := mgnservice.Job{
		ARN:          strings.TrimSpace(aws.ToString(job.Arn)),
		JobID:        strings.TrimSpace(aws.ToString(job.JobID)),
		Type:         strings.TrimSpace(string(job.Type)),
		Status:       strings.TrimSpace(string(job.Status)),
		InitiatedBy:  strings.TrimSpace(string(job.InitiatedBy)),
		CreationTime: parseAPITime(aws.ToString(job.CreationDateTime)),
		EndTime:      parseAPITime(aws.ToString(job.EndDateTime)),
		Tags:         cloneTags(job.Tags),
	}
	for _, participant := range job.ParticipatingServers {
		id := strings.TrimSpace(aws.ToString(participant.SourceServerID))
		if id == "" {
			continue
		}
		mapped.ParticipatingSourceServerIDs = append(mapped.ParticipatingSourceServerIDs, id)
	}
	return mapped
}

// parseAPITime parses an MGN RFC3339 timestamp string into a time.Time, or
// returns the zero time when the value is empty or unparseable so the scanner
// omits an unknown timestamp instead of emitting an epoch.
func parseAPITime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

// cloneTags returns a trimmed-key copy of the SDK tag map, or nil when empty, so
// the scanner-owned model never aliases the SDK response map.
func cloneTags(input map[string]string) map[string]string {
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
