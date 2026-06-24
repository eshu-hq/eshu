// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssg "github.com/aws/aws-sdk-go-v2/service/storagegateway"
	sgtypes "github.com/aws/aws-sdk-go-v2/service/storagegateway/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	sgservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/storagegateway"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the minimal, read-only Storage Gateway API surface the adapter
// depends on. It exposes only list and describe operations; no activation,
// deletion, shutdown, reboot, cache-refresh, volume/share create, or tape API
// appears here, so the metadata-only contract is enforced by the type itself.
type apiClient interface {
	ListGateways(context.Context, *awssg.ListGatewaysInput, ...func(*awssg.Options)) (*awssg.ListGatewaysOutput, error)
	DescribeGatewayInformation(context.Context, *awssg.DescribeGatewayInformationInput, ...func(*awssg.Options)) (*awssg.DescribeGatewayInformationOutput, error)
	ListVolumes(context.Context, *awssg.ListVolumesInput, ...func(*awssg.Options)) (*awssg.ListVolumesOutput, error)
	ListFileShares(context.Context, *awssg.ListFileSharesInput, ...func(*awssg.Options)) (*awssg.ListFileSharesOutput, error)
	DescribeNFSFileShares(context.Context, *awssg.DescribeNFSFileSharesInput, ...func(*awssg.Options)) (*awssg.DescribeNFSFileSharesOutput, error)
	DescribeSMBFileShares(context.Context, *awssg.DescribeSMBFileSharesInput, ...func(*awssg.Options)) (*awssg.DescribeSMBFileSharesOutput, error)
}

// Client adapts AWS SDK Storage Gateway pagination into scanner-owned metadata.
// The adapter never activates, deletes, or reboots a gateway, never refreshes a
// file-share cache, and never creates or deletes volumes or shares.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Storage Gateway SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awssg.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListGateways reads the gateways in the claimed boundary and enriches each one
// with the safe subset of DescribeGatewayInformation (endpoint type, VPC
// endpoint, audit log group). Network-interface IP addresses are reduced to a
// count and never persisted as raw values.
func (c *Client) ListGateways(ctx context.Context) ([]sgservice.Gateway, error) {
	infos, err := c.listGatewayInfos(ctx)
	if err != nil {
		return nil, err
	}
	gateways := make([]sgservice.Gateway, 0, len(infos))
	for _, info := range infos {
		arn := strings.TrimSpace(aws.ToString(info.GatewayARN))
		gateway := sgservice.Gateway{
			ARN:               arn,
			ID:                strings.TrimSpace(aws.ToString(info.GatewayId)),
			Name:              strings.TrimSpace(aws.ToString(info.GatewayName)),
			Type:              strings.TrimSpace(aws.ToString(info.GatewayType)),
			State:             strings.TrimSpace(aws.ToString(info.GatewayOperationalState)),
			OperationalState:  strings.TrimSpace(aws.ToString(info.GatewayOperationalState)),
			EC2InstanceID:     strings.TrimSpace(aws.ToString(info.Ec2InstanceId)),
			EC2InstanceRegion: strings.TrimSpace(aws.ToString(info.Ec2InstanceRegion)),
		}
		if arn != "" {
			detail, err := c.describeGateway(ctx, arn)
			if err != nil {
				return nil, err
			}
			applyGatewayDetail(&gateway, detail)
		}
		gateways = append(gateways, gateway)
	}
	return gateways, nil
}

func (c *Client) listGatewayInfos(ctx context.Context) ([]sgtypes.GatewayInfo, error) {
	var infos []sgtypes.GatewayInfo
	var marker *string
	for {
		var page *awssg.ListGatewaysOutput
		err := c.recordAPICall(ctx, "ListGateways", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListGateways(callCtx, &awssg.ListGatewaysInput{Marker: marker})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return infos, nil
		}
		infos = append(infos, page.Gateways...)
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return infos, nil
		}
	}
}

func (c *Client) describeGateway(ctx context.Context, gatewayARN string) (*awssg.DescribeGatewayInformationOutput, error) {
	var output *awssg.DescribeGatewayInformationOutput
	err := c.recordAPICall(ctx, "DescribeGatewayInformation", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeGatewayInformation(callCtx, &awssg.DescribeGatewayInformationInput{
			GatewayARN: aws.String(gatewayARN),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return output, nil
}

// ListVolumes reads the cached/stored iSCSI volumes for every gateway in the
// claimed boundary.
func (c *Client) ListVolumes(ctx context.Context) ([]sgservice.Volume, error) {
	infos, err := c.listGatewayInfos(ctx)
	if err != nil {
		return nil, err
	}
	var volumes []sgservice.Volume
	for _, info := range infos {
		arn := strings.TrimSpace(aws.ToString(info.GatewayARN))
		if arn == "" {
			continue
		}
		gatewayVolumes, err := c.listGatewayVolumes(ctx, arn)
		if err != nil {
			return nil, err
		}
		volumes = append(volumes, gatewayVolumes...)
	}
	return volumes, nil
}

func (c *Client) listGatewayVolumes(ctx context.Context, gatewayARN string) ([]sgservice.Volume, error) {
	var volumes []sgservice.Volume
	var marker *string
	for {
		var page *awssg.ListVolumesOutput
		err := c.recordAPICall(ctx, "ListVolumes", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListVolumes(callCtx, &awssg.ListVolumesInput{
				GatewayARN: aws.String(gatewayARN),
				Marker:     marker,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return volumes, nil
		}
		for _, info := range page.VolumeInfos {
			volumes = append(volumes, mapVolume(info))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return volumes, nil
		}
	}
}

// ListFileShares reads the NFS and SMB S3 file shares for the claimed boundary.
// It lists the file-share ARNs, groups them by protocol, and resolves each
// group through DescribeNFSFileShares / DescribeSMBFileShares for the safe
// metadata subset. NFS client allow lists and SMB admin/user lists never leave
// the adapter.
func (c *Client) ListFileShares(ctx context.Context) ([]sgservice.FileShare, error) {
	nfsARNs, smbARNs, err := c.listFileShareARNs(ctx)
	if err != nil {
		return nil, err
	}
	var shares []sgservice.FileShare
	nfs, err := c.describeNFSFileShares(ctx, nfsARNs)
	if err != nil {
		return nil, err
	}
	shares = append(shares, nfs...)
	smb, err := c.describeSMBFileShares(ctx, smbARNs)
	if err != nil {
		return nil, err
	}
	shares = append(shares, smb...)
	return shares, nil
}

func (c *Client) listFileShareARNs(ctx context.Context) (nfs, smb []string, err error) {
	var marker *string
	for {
		var page *awssg.ListFileSharesOutput
		callErr := c.recordAPICall(ctx, "ListFileShares", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListFileShares(callCtx, &awssg.ListFileSharesInput{Marker: marker})
			return err
		})
		if callErr != nil {
			return nil, nil, callErr
		}
		if page == nil {
			return nfs, smb, nil
		}
		for _, info := range page.FileShareInfoList {
			arn := strings.TrimSpace(aws.ToString(info.FileShareARN))
			if arn == "" {
				continue
			}
			switch info.FileShareType {
			case sgtypes.FileShareTypeNfs:
				nfs = append(nfs, arn)
			case sgtypes.FileShareTypeSmb:
				smb = append(smb, arn)
			}
		}
		marker = page.NextMarker
		if aws.ToString(marker) == "" {
			return nfs, smb, nil
		}
	}
}

func (c *Client) describeNFSFileShares(ctx context.Context, arns []string) ([]sgservice.FileShare, error) {
	var shares []sgservice.FileShare
	for _, batch := range batchARNs(arns) {
		var output *awssg.DescribeNFSFileSharesOutput
		err := c.recordAPICall(ctx, "DescribeNFSFileShares", func(callCtx context.Context) error {
			var err error
			output, err = c.client.DescribeNFSFileShares(callCtx, &awssg.DescribeNFSFileSharesInput{
				FileShareARNList: batch,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			continue
		}
		for _, info := range output.NFSFileShareInfoList {
			shares = append(shares, mapNFSFileShare(info))
		}
	}
	return shares, nil
}

func (c *Client) describeSMBFileShares(ctx context.Context, arns []string) ([]sgservice.FileShare, error) {
	var shares []sgservice.FileShare
	for _, batch := range batchARNs(arns) {
		var output *awssg.DescribeSMBFileSharesOutput
		err := c.recordAPICall(ctx, "DescribeSMBFileShares", func(callCtx context.Context) error {
			var err error
			output, err = c.client.DescribeSMBFileShares(callCtx, &awssg.DescribeSMBFileSharesInput{
				FileShareARNList: batch,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			continue
		}
		for _, info := range output.SMBFileShareInfoList {
			shares = append(shares, mapSMBFileShare(info))
		}
	}
	return shares, nil
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

var _ sgservice.Client = (*Client)(nil)

var _ apiClient = (*awssg.Client)(nil)
