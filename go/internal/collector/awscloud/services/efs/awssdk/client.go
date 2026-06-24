// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsefs "github.com/aws/aws-sdk-go-v2/service/efs"
	awsefstypes "github.com/aws/aws-sdk-go-v2/service/efs/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	efsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/efs"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the EFS SDK seam the adapter consumes. It lists only the
// describe-style metadata reads the metadata-only scanner is permitted to call.
// It deliberately excludes every mutation API (Create/Delete/Put/Update/Modify)
// and the NFS file system policy reads (DescribeFileSystemPolicy,
// DescribeBackupPolicy). client_test.go asserts this shape with reflection.
type apiClient interface {
	DescribeFileSystems(context.Context, *awsefs.DescribeFileSystemsInput, ...func(*awsefs.Options)) (*awsefs.DescribeFileSystemsOutput, error)
	DescribeAccessPoints(context.Context, *awsefs.DescribeAccessPointsInput, ...func(*awsefs.Options)) (*awsefs.DescribeAccessPointsOutput, error)
	DescribeMountTargets(context.Context, *awsefs.DescribeMountTargetsInput, ...func(*awsefs.Options)) (*awsefs.DescribeMountTargetsOutput, error)
	DescribeMountTargetSecurityGroups(context.Context, *awsefs.DescribeMountTargetSecurityGroupsInput, ...func(*awsefs.Options)) (*awsefs.DescribeMountTargetSecurityGroupsOutput, error)
	DescribeLifecycleConfiguration(context.Context, *awsefs.DescribeLifecycleConfigurationInput, ...func(*awsefs.Options)) (*awsefs.DescribeLifecycleConfigurationOutput, error)
	DescribeReplicationConfigurations(context.Context, *awsefs.DescribeReplicationConfigurationsInput, ...func(*awsefs.Options)) (*awsefs.DescribeReplicationConfigurationsOutput, error)
}

// Client adapts AWS SDK EFS describe calls into scanner-owned metadata.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an EFS SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsefs.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListFileSystems returns EFS file system metadata visible to the configured
// credentials, with each file system's access points, mount targets, and
// lifecycle policy summary. It never requests a file system policy body.
func (c *Client) ListFileSystems(ctx context.Context) ([]efsservice.FileSystem, error) {
	paginator := awsefs.NewDescribeFileSystemsPaginator(c.client, &awsefs.DescribeFileSystemsInput{})
	var systems []efsservice.FileSystem
	for paginator.HasMorePages() {
		var page *awsefs.DescribeFileSystemsOutput
		err := c.recordAPICall(ctx, "DescribeFileSystems", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, description := range page.FileSystems {
			system, err := c.fileSystemMetadata(ctx, description)
			if err != nil {
				return nil, err
			}
			systems = append(systems, system)
		}
	}
	return systems, nil
}

func (c *Client) fileSystemMetadata(
	ctx context.Context,
	description awsefstypes.FileSystemDescription,
) (efsservice.FileSystem, error) {
	fsID := aws.ToString(description.FileSystemId)
	accessPoints, err := c.accessPoints(ctx, fsID)
	if err != nil {
		return efsservice.FileSystem{}, err
	}
	mountTargets, err := c.mountTargets(ctx, fsID)
	if err != nil {
		return efsservice.FileSystem{}, err
	}
	lifecycle, err := c.lifecyclePolicy(ctx, fsID)
	if err != nil {
		return efsservice.FileSystem{}, err
	}
	return mapFileSystem(description, accessPoints, mountTargets, lifecycle), nil
}

func (c *Client) accessPoints(ctx context.Context, fsID string) ([]efsservice.AccessPoint, error) {
	paginator := awsefs.NewDescribeAccessPointsPaginator(c.client, &awsefs.DescribeAccessPointsInput{
		FileSystemId: aws.String(fsID),
	})
	var accessPoints []efsservice.AccessPoint
	for paginator.HasMorePages() {
		var page *awsefs.DescribeAccessPointsOutput
		err := c.recordAPICall(ctx, "DescribeAccessPoints", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, description := range page.AccessPoints {
			accessPoints = append(accessPoints, mapAccessPoint(description))
		}
	}
	return accessPoints, nil
}

func (c *Client) mountTargets(ctx context.Context, fsID string) ([]efsservice.MountTarget, error) {
	paginator := awsefs.NewDescribeMountTargetsPaginator(c.client, &awsefs.DescribeMountTargetsInput{
		FileSystemId: aws.String(fsID),
	})
	var mountTargets []efsservice.MountTarget
	for paginator.HasMorePages() {
		var page *awsefs.DescribeMountTargetsOutput
		err := c.recordAPICall(ctx, "DescribeMountTargets", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, description := range page.MountTargets {
			target := mapMountTarget(description)
			groups, err := c.mountTargetSecurityGroups(ctx, target.ID)
			if err != nil {
				return nil, err
			}
			target.SecurityGroupIDs = groups
			mountTargets = append(mountTargets, target)
		}
	}
	return mountTargets, nil
}

func (c *Client) mountTargetSecurityGroups(ctx context.Context, mountTargetID string) ([]string, error) {
	if strings.TrimSpace(mountTargetID) == "" {
		return nil, nil
	}
	var output *awsefs.DescribeMountTargetSecurityGroupsOutput
	err := c.recordAPICall(ctx, "DescribeMountTargetSecurityGroups", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeMountTargetSecurityGroups(callCtx, &awsefs.DescribeMountTargetSecurityGroupsInput{
			MountTargetId: aws.String(mountTargetID),
		})
		return err
	})
	if err != nil {
		// Mount targets that are not yet available report
		// IncorrectMountTargetState. Skip security group evidence rather than
		// failing the whole scan for a transient lifecycle state.
		if isIncorrectMountTargetState(err) {
			return nil, nil
		}
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	return cloneStrings(output.SecurityGroups), nil
}

func (c *Client) lifecyclePolicy(ctx context.Context, fsID string) (efsservice.LifecyclePolicySummary, error) {
	var output *awsefs.DescribeLifecycleConfigurationOutput
	err := c.recordAPICall(ctx, "DescribeLifecycleConfiguration", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeLifecycleConfiguration(callCtx, &awsefs.DescribeLifecycleConfigurationInput{
			FileSystemId: aws.String(fsID),
		})
		return err
	})
	if err != nil {
		return efsservice.LifecyclePolicySummary{}, err
	}
	if output == nil {
		return efsservice.LifecyclePolicySummary{}, nil
	}
	return summarizeLifecyclePolicies(output.LifecyclePolicies), nil
}

// ListReplicationConfigurations returns EFS replication configuration metadata
// for the scanned account and region.
func (c *Client) ListReplicationConfigurations(ctx context.Context) ([]efsservice.ReplicationConfiguration, error) {
	paginator := awsefs.NewDescribeReplicationConfigurationsPaginator(c.client, &awsefs.DescribeReplicationConfigurationsInput{})
	var configs []efsservice.ReplicationConfiguration
	for paginator.HasMorePages() {
		var page *awsefs.DescribeReplicationConfigurationsOutput
		err := c.recordAPICall(ctx, "DescribeReplicationConfigurations", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, description := range page.Replications {
			configs = append(configs, mapReplicationConfiguration(description))
		}
	}
	return configs, nil
}

func mapFileSystem(
	description awsefstypes.FileSystemDescription,
	accessPoints []efsservice.AccessPoint,
	mountTargets []efsservice.MountTarget,
	lifecycle efsservice.LifecyclePolicySummary,
) efsservice.FileSystem {
	return efsservice.FileSystem{
		ID:                   aws.ToString(description.FileSystemId),
		ARN:                  strings.TrimSpace(aws.ToString(description.FileSystemArn)),
		Name:                 strings.TrimSpace(aws.ToString(description.Name)),
		OwnerID:              aws.ToString(description.OwnerId),
		LifeCycleState:       string(description.LifeCycleState),
		PerformanceMode:      string(description.PerformanceMode),
		ThroughputMode:       string(description.ThroughputMode),
		Encrypted:            aws.ToBool(description.Encrypted),
		KMSKeyID:             strings.TrimSpace(aws.ToString(description.KmsKeyId)),
		AvailabilityZoneID:   aws.ToString(description.AvailabilityZoneId),
		NumberOfMountTargets: description.NumberOfMountTargets,
		LifecyclePolicy:      lifecycle,
		Tags:                 mapTags(description.Tags),
		AccessPoints:         accessPoints,
		MountTargets:         mountTargets,
	}
}

func mapAccessPoint(description awsefstypes.AccessPointDescription) efsservice.AccessPoint {
	accessPoint := efsservice.AccessPoint{
		ID:             aws.ToString(description.AccessPointId),
		ARN:            strings.TrimSpace(aws.ToString(description.AccessPointArn)),
		Name:           strings.TrimSpace(aws.ToString(description.Name)),
		FileSystemID:   aws.ToString(description.FileSystemId),
		LifeCycleState: string(description.LifeCycleState),
		Tags:           mapTags(description.Tags),
	}
	if description.RootDirectory != nil {
		accessPoint.RootDirectory = strings.TrimSpace(aws.ToString(description.RootDirectory.Path))
	}
	if description.PosixUser != nil {
		accessPoint.PosixUID = description.PosixUser.Uid
		accessPoint.PosixGID = description.PosixUser.Gid
	}
	return accessPoint
}

func mapMountTarget(description awsefstypes.MountTargetDescription) efsservice.MountTarget {
	return efsservice.MountTarget{
		ID:                 aws.ToString(description.MountTargetId),
		FileSystemID:       aws.ToString(description.FileSystemId),
		SubnetID:           aws.ToString(description.SubnetId),
		VPCID:              aws.ToString(description.VpcId),
		AvailabilityZoneID: aws.ToString(description.AvailabilityZoneId),
		LifeCycleState:     string(description.LifeCycleState),
		IPAddress:          aws.ToString(description.IpAddress),
		NetworkInterfaceID: aws.ToString(description.NetworkInterfaceId),
	}
}

func mapReplicationConfiguration(description awsefstypes.ReplicationConfigurationDescription) efsservice.ReplicationConfiguration {
	config := efsservice.ReplicationConfiguration{
		SourceFileSystemID:  aws.ToString(description.SourceFileSystemId),
		SourceFileSystemARN: strings.TrimSpace(aws.ToString(description.SourceFileSystemArn)),
	}
	for _, destination := range description.Destinations {
		config.Destinations = append(config.Destinations, efsservice.ReplicationDestination{
			FileSystemID: aws.ToString(destination.FileSystemId),
			Region:       aws.ToString(destination.Region),
			Status:       string(destination.Status),
		})
	}
	return config
}

func summarizeLifecyclePolicies(policies []awsefstypes.LifecyclePolicy) efsservice.LifecyclePolicySummary {
	var summary efsservice.LifecyclePolicySummary
	for _, policy := range policies {
		if policy.TransitionToIA != "" {
			summary.TransitionToIA = string(policy.TransitionToIA)
		}
		if policy.TransitionToArchive != "" {
			summary.TransitionToArchive = string(policy.TransitionToArchive)
		}
		if policy.TransitionToPrimaryStorageClass != "" {
			summary.TransitionToPrimaryStorageClass = string(policy.TransitionToPrimaryStorageClass)
		}
	}
	return summary
}

func mapTags(tags []awsefstypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = aws.ToString(tag.Value)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneStrings(input []string) []string {
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

func isIncorrectMountTargetState(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.ErrorCode() == "IncorrectMountTargetState"
}

func (c *Client) recordAPICall(ctx context.Context, operation string, call func(context.Context) error) error {
	if c.tracer != nil {
		var span trace.Span
		ctx, span = c.tracer.Start(ctx, telemetry.SpanAWSServicePaginationPage)
		span.SetAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
		)
		defer span.End()
	}
	err := call(ctx)
	result := "success"
	if err != nil {
		result = "error"
	}
	throttled := isThrottleError(err)
	awscloud.RecordAPICall(ctx, awscloud.APICallEvent{
		Boundary:  c.boundary,
		Operation: operation,
		Result:    result,
		Throttled: throttled,
	})
	if c.instruments != nil {
		c.instruments.AWSAPICalls.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
			telemetry.AttrResult(result),
		))
		if throttled {
			c.instruments.AWSThrottles.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrService(c.boundary.ServiceKind),
				telemetry.AttrAccount(c.boundary.AccountID),
				telemetry.AttrRegion(c.boundary.Region),
			))
		}
	}
	return err
}

func isThrottleError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := apiErr.ErrorCode()
	return strings.Contains(strings.ToLower(code), "throttl") ||
		code == "RequestLimitExceeded" ||
		code == "TooManyRequestsException"
}

var _ efsservice.Client = (*Client)(nil)

var _ apiClient = (*awsefs.Client)(nil)
