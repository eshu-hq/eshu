// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssg "github.com/aws/aws-sdk-go-v2/service/storagegateway"
	sgtypes "github.com/aws/aws-sdk-go-v2/service/storagegateway/types"

	sgservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/storagegateway"
)

// describeFileShareBatchSize bounds the FileShareARNList passed to
// DescribeNFSFileShares / DescribeSMBFileShares per call. The Storage Gateway
// API caps each describe at a small batch, so the adapter chunks the listed
// ARNs to stay within the limit.
const describeFileShareBatchSize = 10

// applyGatewayDetail folds the safe subset of DescribeGatewayInformation into a
// gateway view. Network interfaces are reduced to a count so raw IP addresses
// are never persisted, and the VPC endpoint, endpoint type, audit log group,
// host environment, timezone, and software version that anchor the gateway's
// resource and relationships are captured. A nil detail leaves the list-derived
// fields unchanged.
func applyGatewayDetail(gateway *sgservice.Gateway, detail *awssg.DescribeGatewayInformationOutput) {
	if gateway == nil || detail == nil {
		return
	}
	if name := strings.TrimSpace(aws.ToString(detail.GatewayName)); name != "" {
		gateway.Name = name
	}
	if state := strings.TrimSpace(aws.ToString(detail.GatewayState)); state != "" {
		gateway.State = state
	}
	if gatewayType := strings.TrimSpace(aws.ToString(detail.GatewayType)); gatewayType != "" {
		gateway.Type = gatewayType
	}
	gateway.EndpointType = strings.TrimSpace(aws.ToString(detail.EndpointType))
	gateway.HostEnvironment = strings.TrimSpace(string(detail.HostEnvironment))
	gateway.Timezone = strings.TrimSpace(aws.ToString(detail.GatewayTimezone))
	gateway.SoftwareVersion = strings.TrimSpace(aws.ToString(detail.SoftwareVersion))
	gateway.CloudWatchLogGroup = strings.TrimSpace(aws.ToString(detail.CloudWatchLogGroupARN))
	gateway.VPCEndpoint = strings.TrimSpace(aws.ToString(detail.VPCEndpoint))
	gateway.NetworkInterfaceCount = len(detail.GatewayNetworkInterfaces)
	gateway.Tags = tagMap(detail.Tags)
}

// tagMap converts AWS Storage Gateway tags into a scanner-owned string map,
// dropping blank keys. It returns nil when no usable tags remain.
func tagMap(tags []sgtypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		if key := strings.TrimSpace(aws.ToString(tag.Key)); key != "" {
			output[key] = aws.ToString(tag.Value)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func mapVolume(info sgtypes.VolumeInfo) sgservice.Volume {
	return sgservice.Volume{
		ARN:              strings.TrimSpace(aws.ToString(info.VolumeARN)),
		ID:               strings.TrimSpace(aws.ToString(info.VolumeId)),
		Type:             strings.TrimSpace(aws.ToString(info.VolumeType)),
		SizeInBytes:      info.VolumeSizeInBytes,
		AttachmentStatus: strings.TrimSpace(aws.ToString(info.VolumeAttachmentStatus)),
		GatewayARN:       strings.TrimSpace(aws.ToString(info.GatewayARN)),
		GatewayID:        strings.TrimSpace(aws.ToString(info.GatewayId)),
	}
}

func mapNFSFileShare(info sgtypes.NFSFileShareInfo) sgservice.FileShare {
	return sgservice.FileShare{
		ARN:                 strings.TrimSpace(aws.ToString(info.FileShareARN)),
		ID:                  strings.TrimSpace(aws.ToString(info.FileShareId)),
		Name:                strings.TrimSpace(aws.ToString(info.FileShareName)),
		Protocol:            "NFS",
		Type:                "NFS",
		Status:              strings.TrimSpace(aws.ToString(info.FileShareStatus)),
		GatewayARN:          strings.TrimSpace(aws.ToString(info.GatewayARN)),
		LocationARN:         strings.TrimSpace(aws.ToString(info.LocationARN)),
		BucketRegion:        strings.TrimSpace(aws.ToString(info.BucketRegion)),
		Role:                strings.TrimSpace(aws.ToString(info.Role)),
		KMSKey:              strings.TrimSpace(aws.ToString(info.KMSKey)),
		EncryptionType:      strings.TrimSpace(string(info.EncryptionType)),
		DefaultStorageClass: strings.TrimSpace(aws.ToString(info.DefaultStorageClass)),
		ObjectACL:           strings.TrimSpace(string(info.ObjectACL)),
		AuditDestinationARN: strings.TrimSpace(aws.ToString(info.AuditDestinationARN)),
		ReadOnly:            aws.ToBool(info.ReadOnly),
	}
}

func mapSMBFileShare(info sgtypes.SMBFileShareInfo) sgservice.FileShare {
	return sgservice.FileShare{
		ARN:                 strings.TrimSpace(aws.ToString(info.FileShareARN)),
		ID:                  strings.TrimSpace(aws.ToString(info.FileShareId)),
		Name:                strings.TrimSpace(aws.ToString(info.FileShareName)),
		Protocol:            "SMB",
		Type:                "SMB",
		Status:              strings.TrimSpace(aws.ToString(info.FileShareStatus)),
		GatewayARN:          strings.TrimSpace(aws.ToString(info.GatewayARN)),
		LocationARN:         strings.TrimSpace(aws.ToString(info.LocationARN)),
		BucketRegion:        strings.TrimSpace(aws.ToString(info.BucketRegion)),
		Role:                strings.TrimSpace(aws.ToString(info.Role)),
		KMSKey:              strings.TrimSpace(aws.ToString(info.KMSKey)),
		EncryptionType:      strings.TrimSpace(string(info.EncryptionType)),
		DefaultStorageClass: strings.TrimSpace(aws.ToString(info.DefaultStorageClass)),
		ObjectACL:           strings.TrimSpace(string(info.ObjectACL)),
		AuditDestinationARN: strings.TrimSpace(aws.ToString(info.AuditDestinationARN)),
		ReadOnly:            aws.ToBool(info.ReadOnly),
	}
}

// batchARNs splits arns into bounded slices the Describe*FileShares APIs accept
// in a single call. It returns nil for an empty input so the caller skips the
// describe entirely.
func batchARNs(arns []string) [][]string {
	if len(arns) == 0 {
		return nil
	}
	var batches [][]string
	for start := 0; start < len(arns); start += describeFileShareBatchSize {
		end := start + describeFileShareBatchSize
		if end > len(arns) {
			end = len(arns)
		}
		batches = append(batches, arns[start:end])
	}
	return batches
}
