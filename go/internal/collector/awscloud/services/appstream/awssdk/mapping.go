// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsappstreamtypes "github.com/aws/aws-sdk-go-v2/service/appstream/types"

	appstreamservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/appstream"
)

// mapFleet converts one SDK fleet into scanner-owned metadata. It reads only
// control-plane fields and copies the VPC subnet/security-group ids and the IAM
// role and image ARNs that drive relationship edges; it never reads session,
// user, or session-script content.
func (c *Client) mapFleet(
	ctx context.Context,
	fleet awsappstreamtypes.Fleet,
) (appstreamservice.Fleet, error) {
	arn := strings.TrimSpace(aws.ToString(fleet.Arn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return appstreamservice.Fleet{}, err
	}
	subnetIDs, securityGroupIDs := vpcConfig(fleet.VpcConfig)
	return appstreamservice.Fleet{
		ARN:                         arn,
		Name:                        strings.TrimSpace(aws.ToString(fleet.Name)),
		DisplayName:                 strings.TrimSpace(aws.ToString(fleet.DisplayName)),
		Description:                 strings.TrimSpace(aws.ToString(fleet.Description)),
		State:                       strings.TrimSpace(string(fleet.State)),
		FleetType:                   strings.TrimSpace(string(fleet.FleetType)),
		InstanceType:                strings.TrimSpace(aws.ToString(fleet.InstanceType)),
		Platform:                    strings.TrimSpace(string(fleet.Platform)),
		StreamView:                  strings.TrimSpace(string(fleet.StreamView)),
		IAMRoleARN:                  strings.TrimSpace(aws.ToString(fleet.IamRoleArn)),
		ImageARN:                    strings.TrimSpace(aws.ToString(fleet.ImageArn)),
		ImageName:                   strings.TrimSpace(aws.ToString(fleet.ImageName)),
		EnableDefaultInternetAccess: aws.ToBool(fleet.EnableDefaultInternetAccess),
		MaxConcurrentSessions:       aws.ToInt32(fleet.MaxConcurrentSessions),
		MaxUserDurationInSeconds:    aws.ToInt32(fleet.MaxUserDurationInSeconds),
		CreatedTime:                 aws.ToTime(fleet.CreatedTime),
		SubnetIDs:                   subnetIDs,
		SecurityGroupIDs:            securityGroupIDs,
		Tags:                        tags,
	}, nil
}

// mapStack converts one SDK stack into scanner-owned metadata. It records the
// persistent application-settings enablement and the S3 bucket NAMES AppStream
// reports for application settings and home-folders storage connectors; it never
// reads user settings detail or redirect URLs.
func (c *Client) mapStack(
	ctx context.Context,
	stack awsappstreamtypes.Stack,
) (appstreamservice.Stack, error) {
	arn := strings.TrimSpace(aws.ToString(stack.Arn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return appstreamservice.Stack{}, err
	}
	mapped := appstreamservice.Stack{
		ARN:                     arn,
		Name:                    strings.TrimSpace(aws.ToString(stack.Name)),
		DisplayName:             strings.TrimSpace(aws.ToString(stack.DisplayName)),
		Description:             strings.TrimSpace(aws.ToString(stack.Description)),
		StorageConnectorBuckets: storageConnectorBuckets(stack.StorageConnectors),
		CreatedTime:             aws.ToTime(stack.CreatedTime),
		Tags:                    tags,
	}
	if settings := stack.ApplicationSettings; settings != nil {
		mapped.ApplicationSettingsEnabled = aws.ToBool(settings.Enabled)
		mapped.ApplicationSettingsS3Bucket = strings.TrimSpace(aws.ToString(settings.S3BucketName))
	}
	return mapped, nil
}

// mapImageBuilder converts one SDK image builder into scanner-owned metadata,
// copying the VPC and IAM role/image ARN dependency fields.
func (c *Client) mapImageBuilder(
	ctx context.Context,
	builder awsappstreamtypes.ImageBuilder,
) (appstreamservice.ImageBuilder, error) {
	arn := strings.TrimSpace(aws.ToString(builder.Arn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return appstreamservice.ImageBuilder{}, err
	}
	subnetIDs, securityGroupIDs := vpcConfig(builder.VpcConfig)
	return appstreamservice.ImageBuilder{
		ARN:                         arn,
		Name:                        strings.TrimSpace(aws.ToString(builder.Name)),
		DisplayName:                 strings.TrimSpace(aws.ToString(builder.DisplayName)),
		Description:                 strings.TrimSpace(aws.ToString(builder.Description)),
		State:                       strings.TrimSpace(string(builder.State)),
		InstanceType:                strings.TrimSpace(aws.ToString(builder.InstanceType)),
		Platform:                    strings.TrimSpace(string(builder.Platform)),
		IAMRoleARN:                  strings.TrimSpace(aws.ToString(builder.IamRoleArn)),
		ImageARN:                    strings.TrimSpace(aws.ToString(builder.ImageArn)),
		EnableDefaultInternetAccess: aws.ToBool(builder.EnableDefaultInternetAccess),
		CreatedTime:                 aws.ToTime(builder.CreatedTime),
		SubnetIDs:                   subnetIDs,
		SecurityGroupIDs:            securityGroupIDs,
		Tags:                        tags,
	}, nil
}

// mapImage converts one SDK image into scanner-owned identity, state, and
// visibility metadata. Installed applications, image-permission grants, and
// agent contents are intentionally excluded.
func (c *Client) mapImage(
	ctx context.Context,
	image awsappstreamtypes.Image,
) (appstreamservice.Image, error) {
	arn := strings.TrimSpace(aws.ToString(image.Arn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return appstreamservice.Image{}, err
	}
	return appstreamservice.Image{
		ARN:          arn,
		Name:         strings.TrimSpace(aws.ToString(image.Name)),
		DisplayName:  strings.TrimSpace(aws.ToString(image.DisplayName)),
		State:        strings.TrimSpace(string(image.State)),
		Visibility:   strings.TrimSpace(string(image.Visibility)),
		ImageType:    strings.TrimSpace(string(image.ImageType)),
		Platform:     strings.TrimSpace(string(image.Platform)),
		BaseImageARN: strings.TrimSpace(aws.ToString(image.BaseImageArn)),
		CreatedTime:  aws.ToTime(image.CreatedTime),
		Tags:         tags,
	}, nil
}

// vpcConfig extracts the trimmed subnet and security-group ids from an AppStream
// VPC configuration, returning nil slices when the configuration is absent or
// empty.
func vpcConfig(config *awsappstreamtypes.VpcConfig) (subnetIDs, securityGroupIDs []string) {
	if config == nil {
		return nil, nil
	}
	return trimmedNonEmpty(config.SubnetIds), trimmedNonEmpty(config.SecurityGroupIds)
}

// storageConnectorBuckets returns the S3 bucket NAMES AppStream reports for
// home-folders storage connectors. Only the HOMEFOLDERS connector type is backed
// by an S3 bucket whose ResourceIdentifier is a bucket name; Google Drive and
// OneDrive connectors carry domain identifiers, not buckets, and are skipped.
func storageConnectorBuckets(connectors []awsappstreamtypes.StorageConnector) []string {
	var buckets []string
	for _, connector := range connectors {
		if connector.ConnectorType != awsappstreamtypes.StorageConnectorTypeHomefolders {
			continue
		}
		if bucket := strings.TrimSpace(aws.ToString(connector.ResourceIdentifier)); bucket != "" {
			buckets = append(buckets, bucket)
		}
	}
	return buckets
}

// trimmedNonEmpty returns a trimmed copy of values with empty entries dropped,
// or nil when nothing survives.
func trimmedNonEmpty(values []string) []string {
	var output []string
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	return output
}
