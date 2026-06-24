// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsfsx "github.com/aws/aws-sdk-go-v2/service/fsx"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	fsxservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/fsx"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the FSx SDK seam the adapter consumes. It lists only the
// describe-style metadata reads the metadata-only scanner is permitted to call
// across every FSx flavor (Windows File Server, Lustre, NetApp ONTAP, OpenZFS).
// It deliberately excludes every mutation API
// (Create/Delete/Update/Restore/Copy/Release/Tag/Untag/Associate/Disassociate)
// and every file-content or alias read. client_test.go asserts this shape with
// reflection.
type apiClient interface {
	DescribeFileSystems(context.Context, *awsfsx.DescribeFileSystemsInput, ...func(*awsfsx.Options)) (*awsfsx.DescribeFileSystemsOutput, error)
	DescribeBackups(context.Context, *awsfsx.DescribeBackupsInput, ...func(*awsfsx.Options)) (*awsfsx.DescribeBackupsOutput, error)
	DescribeStorageVirtualMachines(context.Context, *awsfsx.DescribeStorageVirtualMachinesInput, ...func(*awsfsx.Options)) (*awsfsx.DescribeStorageVirtualMachinesOutput, error)
	DescribeVolumes(context.Context, *awsfsx.DescribeVolumesInput, ...func(*awsfsx.Options)) (*awsfsx.DescribeVolumesOutput, error)
	DescribeSnapshots(context.Context, *awsfsx.DescribeSnapshotsInput, ...func(*awsfsx.Options)) (*awsfsx.DescribeSnapshotsOutput, error)
}

// Client adapts AWS SDK FSx describe calls into scanner-owned metadata. It
// never maps Active Directory self-managed credentials, the ONTAP fsxadmin
// password, or the SVM admin password.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an FSx SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsfsx.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListFileSystems returns FSx file system metadata for every flavor visible to
// the configured credentials. It never maps AD passwords or the fsxadmin
// password.
func (c *Client) ListFileSystems(ctx context.Context) ([]fsxservice.FileSystem, error) {
	paginator := awsfsx.NewDescribeFileSystemsPaginator(c.client, &awsfsx.DescribeFileSystemsInput{})
	var systems []fsxservice.FileSystem
	for paginator.HasMorePages() {
		var page *awsfsx.DescribeFileSystemsOutput
		err := c.recordAPICall(ctx, "DescribeFileSystems", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			continue
		}
		for _, description := range page.FileSystems {
			systems = append(systems, mapFileSystem(description))
		}
	}
	return systems, nil
}

// ListBackups returns FSx backup metadata for the scanned account and region.
func (c *Client) ListBackups(ctx context.Context) ([]fsxservice.Backup, error) {
	paginator := awsfsx.NewDescribeBackupsPaginator(c.client, &awsfsx.DescribeBackupsInput{})
	var backups []fsxservice.Backup
	for paginator.HasMorePages() {
		var page *awsfsx.DescribeBackupsOutput
		err := c.recordAPICall(ctx, "DescribeBackups", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			continue
		}
		for _, description := range page.Backups {
			backups = append(backups, mapBackup(description))
		}
	}
	return backups, nil
}

// ListStorageVirtualMachines returns FSx for NetApp ONTAP storage virtual
// machine metadata. It never maps the SVM admin password or self-managed AD
// credentials.
func (c *Client) ListStorageVirtualMachines(ctx context.Context) ([]fsxservice.StorageVirtualMachine, error) {
	paginator := awsfsx.NewDescribeStorageVirtualMachinesPaginator(c.client, &awsfsx.DescribeStorageVirtualMachinesInput{})
	var svms []fsxservice.StorageVirtualMachine
	for paginator.HasMorePages() {
		var page *awsfsx.DescribeStorageVirtualMachinesOutput
		err := c.recordAPICall(ctx, "DescribeStorageVirtualMachines", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			continue
		}
		for _, description := range page.StorageVirtualMachines {
			svms = append(svms, mapStorageVirtualMachine(description))
		}
	}
	return svms, nil
}

// ListVolumes returns FSx for NetApp ONTAP and OpenZFS volume metadata.
func (c *Client) ListVolumes(ctx context.Context) ([]fsxservice.Volume, error) {
	paginator := awsfsx.NewDescribeVolumesPaginator(c.client, &awsfsx.DescribeVolumesInput{})
	var volumes []fsxservice.Volume
	for paginator.HasMorePages() {
		var page *awsfsx.DescribeVolumesOutput
		err := c.recordAPICall(ctx, "DescribeVolumes", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			continue
		}
		for _, description := range page.Volumes {
			volumes = append(volumes, mapVolume(description))
		}
	}
	return volumes, nil
}

// ListSnapshots returns FSx volume snapshot metadata.
func (c *Client) ListSnapshots(ctx context.Context) ([]fsxservice.Snapshot, error) {
	paginator := awsfsx.NewDescribeSnapshotsPaginator(c.client, &awsfsx.DescribeSnapshotsInput{})
	var snapshots []fsxservice.Snapshot
	for paginator.HasMorePages() {
		var page *awsfsx.DescribeSnapshotsOutput
		err := c.recordAPICall(ctx, "DescribeSnapshots", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			continue
		}
		for _, description := range page.Snapshots {
			snapshots = append(snapshots, mapSnapshot(description))
		}
	}
	return snapshots, nil
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

var _ fsxservice.Client = (*Client)(nil)

var _ apiClient = (*awsfsx.Client)(nil)
