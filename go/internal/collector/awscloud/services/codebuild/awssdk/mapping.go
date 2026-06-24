// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	cbtypes "github.com/aws/aws-sdk-go-v2/service/codebuild/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	cbservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/codebuild"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// plaintextEnvValueReason labels redacted PLAINTEXT environment-variable values
// so downstream audit can trace why the value was replaced. CodeBuild PLAINTEXT
// values may carry secrets, so the raw value never reaches scanner records.
const plaintextEnvValueReason = "codebuild_environment_variable_value"

// mapProject converts an AWS SDK Project into the scanner-owned record. It never
// copies Source.Buildspec because that field carries buildspec.yml content the
// issue forbids persisting; the scanner-owned ProjectSource has no buildspec
// field. PLAINTEXT environment-variable values are redacted through the key.
func mapProject(project cbtypes.Project, key redact.Key) cbservice.Project {
	mapped := cbservice.Project{
		Name:               aws.ToString(project.Name),
		ARN:                aws.ToString(project.Arn),
		Description:        aws.ToString(project.Description),
		ServiceRoleARN:     aws.ToString(project.ServiceRole),
		EncryptionKeyID:    aws.ToString(project.EncryptionKey),
		TimeoutInMinutes:   aws.ToInt32(project.TimeoutInMinutes),
		QueuedTimeout:      aws.ToInt32(project.QueuedTimeoutInMinutes),
		ConcurrentBuilds:   aws.ToInt32(project.ConcurrentBuildLimit),
		Source:             mapSource(project.Source),
		SecondarySources:   mapSources(project.SecondarySources),
		Environment:        mapEnvironment(project.Environment, key),
		Artifacts:          mapArtifacts(project.Artifacts),
		SecondaryArtifacts: mapArtifactsList(project.SecondaryArtifacts),
		VPCConfig:          mapVPCConfig(project.VpcConfig),
		Tags:               mapTags(project.Tags),
	}
	if project.Created != nil {
		mapped.Created = project.Created.UTC()
	}
	if project.LastModified != nil {
		mapped.LastModified = project.LastModified.UTC()
	}
	return mapped
}

// mapSource keeps only safe source references. It must never copy
// source.Buildspec because that field carries an inline buildspec body or
// buildspec path the issue forbids persisting.
func mapSource(source *cbtypes.ProjectSource) cbservice.ProjectSource {
	if source == nil {
		return cbservice.ProjectSource{}
	}
	return cbservice.ProjectSource{
		Type:              string(source.Type),
		Location:          strings.TrimSpace(aws.ToString(source.Location)),
		SourceIdentifier:  strings.TrimSpace(aws.ToString(source.SourceIdentifier)),
		ReportBuildStatus: aws.ToBool(source.ReportBuildStatus),
	}
}

func mapSources(sources []cbtypes.ProjectSource) []cbservice.ProjectSource {
	if len(sources) == 0 {
		return nil
	}
	out := make([]cbservice.ProjectSource, 0, len(sources))
	for i := range sources {
		out = append(out, mapSource(&sources[i]))
	}
	return out
}

func mapEnvironment(environment *cbtypes.ProjectEnvironment, key redact.Key) cbservice.ProjectEnvironment {
	if environment == nil {
		return cbservice.ProjectEnvironment{}
	}
	return cbservice.ProjectEnvironment{
		Type:                 string(environment.Type),
		Image:                strings.TrimSpace(aws.ToString(environment.Image)),
		ComputeType:          string(environment.ComputeType),
		PrivilegedMode:       aws.ToBool(environment.PrivilegedMode),
		ImagePullCredentials: string(environment.ImagePullCredentialsType),
		EnvironmentVariables: mapEnvironmentVariables(environment.EnvironmentVariables, key),
	}
}

// mapEnvironmentVariables converts CodeBuild environment variables into
// scanner-owned records. PLAINTEXT values route through the redaction library
// because they may carry secrets the issue forbids persisting raw. For
// PARAMETER_STORE and SECRETS_MANAGER variables the Value is a resource
// reference (parameter name or secret ARN/name), so it is retained as Reference
// for relationship derivation, never as secret content.
func mapEnvironmentVariables(variables []cbtypes.EnvironmentVariable, key redact.Key) []cbservice.EnvironmentVariable {
	if len(variables) == 0 {
		return nil
	}
	out := make([]cbservice.EnvironmentVariable, 0, len(variables))
	for _, variable := range variables {
		entry := cbservice.EnvironmentVariable{
			Name: strings.TrimSpace(aws.ToString(variable.Name)),
			Type: string(variable.Type),
		}
		value := aws.ToString(variable.Value)
		switch variable.Type {
		case cbtypes.EnvironmentVariableTypePlaintext:
			if value != "" {
				entry.ValueMarker = awscloud.RedactString(value, plaintextEnvValueReason, key)
			}
		case cbtypes.EnvironmentVariableTypeParameterStore, cbtypes.EnvironmentVariableTypeSecretsManager:
			entry.Reference = strings.TrimSpace(value)
		default:
			// Unknown future type: redact the value to stay fail-safe so an
			// unmapped type can never persist a raw value.
			if value != "" {
				entry.ValueMarker = awscloud.RedactString(value, plaintextEnvValueReason, key)
			}
		}
		out = append(out, entry)
	}
	return out
}

func mapArtifacts(artifacts *cbtypes.ProjectArtifacts) cbservice.ProjectArtifacts {
	if artifacts == nil {
		return cbservice.ProjectArtifacts{}
	}
	return cbservice.ProjectArtifacts{
		Type:               string(artifacts.Type),
		Location:           strings.TrimSpace(aws.ToString(artifacts.Location)),
		ArtifactIdentifier: strings.TrimSpace(aws.ToString(artifacts.ArtifactIdentifier)),
		EncryptionDisabled: aws.ToBool(artifacts.EncryptionDisabled),
	}
}

func mapArtifactsList(artifacts []cbtypes.ProjectArtifacts) []cbservice.ProjectArtifacts {
	if len(artifacts) == 0 {
		return nil
	}
	out := make([]cbservice.ProjectArtifacts, 0, len(artifacts))
	for i := range artifacts {
		out = append(out, mapArtifacts(&artifacts[i]))
	}
	return out
}

func mapVPCConfig(config *cbtypes.VpcConfig) cbservice.VPCConfig {
	if config == nil {
		return cbservice.VPCConfig{}
	}
	return cbservice.VPCConfig{
		VPCID:            strings.TrimSpace(aws.ToString(config.VpcId)),
		SubnetIDs:        trimStrings(config.Subnets),
		SecurityGroupIDs: trimStrings(config.SecurityGroupIds),
	}
}

func mapReportGroup(group cbtypes.ReportGroup) cbservice.ReportGroup {
	mapped := cbservice.ReportGroup{
		Name:   aws.ToString(group.Name),
		ARN:    aws.ToString(group.Arn),
		Type:   string(group.Type),
		Status: string(group.Status),
		Tags:   mapTags(group.Tags),
	}
	if group.ExportConfig != nil {
		mapped.ExportType = string(group.ExportConfig.ExportConfigType)
		if group.ExportConfig.S3Destination != nil {
			mapped.ExportS3Bucket = strings.TrimSpace(aws.ToString(group.ExportConfig.S3Destination.Bucket))
		}
	}
	if group.Created != nil {
		mapped.Created = group.Created.UTC()
	}
	if group.LastModified != nil {
		mapped.LastModified = group.LastModified.UTC()
	}
	return mapped
}

// mapBuild keeps only build identity, status, and duration metadata. It must
// never copy build log group/stream references or log content; the
// scanner-owned Build record has no field for logs.
func mapBuild(build cbtypes.Build) cbservice.Build {
	mapped := cbservice.Build{
		ID:                    aws.ToString(build.Id),
		ARN:                   aws.ToString(build.Arn),
		ProjectName:           aws.ToString(build.ProjectName),
		BuildNumber:           aws.ToInt64(build.BuildNumber),
		Status:                string(build.BuildStatus),
		CurrentPhase:          strings.TrimSpace(aws.ToString(build.CurrentPhase)),
		Initiator:             strings.TrimSpace(aws.ToString(build.Initiator)),
		BuildComplete:         build.BuildComplete,
		ResolvedSourceVersion: strings.TrimSpace(aws.ToString(build.ResolvedSourceVersion)),
	}
	if build.StartTime != nil {
		mapped.StartTime = build.StartTime.UTC()
	}
	if build.EndTime != nil {
		mapped.EndTime = build.EndTime.UTC()
	}
	return mapped
}

func mapTags(tags []cbtypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		out[key] = aws.ToString(tag.Value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func trimStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
