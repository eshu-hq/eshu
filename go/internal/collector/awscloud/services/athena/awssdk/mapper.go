// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsathena "github.com/aws/aws-sdk-go-v2/service/athena"
	awsathenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	athenaservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/athena"
)

// mapWorkGroup converts an AWS Athena workgroup summary and detail response
// into the scanner-owned WorkGroup model. The mapper intentionally ignores any
// per-query result fields and only preserves the workgroup-level configuration
// the metadata-only scanner is allowed to emit.
func mapWorkGroup(
	name string,
	summary awsathenatypes.WorkGroupSummary,
	detail *awsathena.GetWorkGroupOutput,
	tags map[string]string,
) athenaservice.WorkGroup {
	mapped := athenaservice.WorkGroup{
		Name:                   strings.TrimSpace(name),
		State:                  strings.TrimSpace(string(summary.State)),
		Description:            strings.TrimSpace(aws.ToString(summary.Description)),
		CreationTime:           timeValue(summary.CreationTime),
		EffectiveEngineVersion: engineVersionEffective(summary.EngineVersion),
		EngineVersion:          engineVersionSelected(summary.EngineVersion),
		Tags:                   tags,
	}
	if detail == nil || detail.WorkGroup == nil {
		return mapped
	}
	workGroup := detail.WorkGroup
	if workGroup.Description != nil && strings.TrimSpace(aws.ToString(workGroup.Description)) != "" {
		mapped.Description = strings.TrimSpace(aws.ToString(workGroup.Description))
	}
	if workGroup.State != "" {
		mapped.State = strings.TrimSpace(string(workGroup.State))
	}
	if workGroup.CreationTime != nil {
		mapped.CreationTime = timeValue(workGroup.CreationTime)
	}
	config := workGroup.Configuration
	if config == nil {
		return mapped
	}
	mapped.EnforceWorkGroupConfiguration = aws.ToBool(config.EnforceWorkGroupConfiguration)
	mapped.PublishCloudWatchMetricsEnabled = aws.ToBool(config.PublishCloudWatchMetricsEnabled)
	mapped.RequesterPaysEnabled = aws.ToBool(config.RequesterPaysEnabled)
	mapped.BytesScannedCutoffPerQuery = aws.ToInt64(config.BytesScannedCutoffPerQuery)
	if config.EngineVersion != nil {
		if effective := engineVersionEffective(config.EngineVersion); effective != "" {
			mapped.EffectiveEngineVersion = effective
		}
		if selected := engineVersionSelected(config.EngineVersion); selected != "" {
			mapped.EngineVersion = selected
		}
	}
	if config.ResultConfiguration != nil {
		mapped.OutputLocation = strings.TrimSpace(aws.ToString(config.ResultConfiguration.OutputLocation))
		mapped.ExpectedBucketOwner = strings.TrimSpace(aws.ToString(config.ResultConfiguration.ExpectedBucketOwner))
		if config.ResultConfiguration.EncryptionConfiguration != nil {
			mapped.EncryptionOption = strings.TrimSpace(string(config.ResultConfiguration.EncryptionConfiguration.EncryptionOption))
			mapped.KMSKey = strings.TrimSpace(aws.ToString(config.ResultConfiguration.EncryptionConfiguration.KmsKey))
		}
	}
	return mapped
}

// mapDataCatalog converts an AWS Athena data catalog detail response into the
// scanner-owned DataCatalog model. The mapper does not capture Parameters
// because they can encode lambda function ARNs, connection strings, or other
// payload-shaped values; downstream relationships should add explicit edges
// when those references are needed.
func mapDataCatalog(
	name string,
	detail *awsathena.GetDataCatalogOutput,
	tags map[string]string,
) athenaservice.DataCatalog {
	mapped := athenaservice.DataCatalog{
		Name: strings.TrimSpace(name),
		Tags: tags,
	}
	if detail == nil || detail.DataCatalog == nil {
		return mapped
	}
	catalog := detail.DataCatalog
	if catalog.Name != nil && strings.TrimSpace(aws.ToString(catalog.Name)) != "" {
		mapped.Name = strings.TrimSpace(aws.ToString(catalog.Name))
	}
	mapped.Type = strings.TrimSpace(string(catalog.Type))
	mapped.Description = strings.TrimSpace(aws.ToString(catalog.Description))
	return mapped
}

// mapNamedQuery copies the scanner-safe identity attributes out of an AWS
// Athena named-query response. The QueryString SQL body is intentionally
// dropped on the floor here so it never reaches the scanner package.
func mapNamedQuery(raw awsathenatypes.NamedQuery) athenaservice.NamedQuery {
	return athenaservice.NamedQuery{
		NamedQueryID:  strings.TrimSpace(aws.ToString(raw.NamedQueryId)),
		Name:          strings.TrimSpace(aws.ToString(raw.Name)),
		Description:   strings.TrimSpace(aws.ToString(raw.Description)),
		Database:      strings.TrimSpace(aws.ToString(raw.Database)),
		WorkGroupName: strings.TrimSpace(aws.ToString(raw.WorkGroup)),
	}
}

func engineVersionEffective(version *awsathenatypes.EngineVersion) string {
	if version == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(version.EffectiveEngineVersion))
}

func engineVersionSelected(version *awsathenatypes.EngineVersion) string {
	if version == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(version.SelectedEngineVersion))
}

func timeValue(value *time.Time) time.Time {
	if value == nil || value.IsZero() {
		return time.Time{}
	}
	return value.UTC()
}

// workGroupARN builds the standard Athena workgroup ARN used by
// ListTagsForResource.
func workGroupARN(boundary awscloud.Boundary, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return "arn:" + awscloud.PartitionForBoundary(boundary) + ":athena:" + strings.TrimSpace(boundary.Region) +
		":" + strings.TrimSpace(boundary.AccountID) +
		":workgroup/" + name
}

// dataCatalogARN builds the standard Athena data catalog ARN used by
// ListTagsForResource.
func dataCatalogARN(boundary awscloud.Boundary, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return "arn:" + awscloud.PartitionForBoundary(boundary) + ":athena:" + strings.TrimSpace(boundary.Region) +
		":" + strings.TrimSpace(boundary.AccountID) +
		":datacatalog/" + name
}
