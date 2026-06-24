// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package storagegateway

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// volumeOnGatewayRelationship records a volume's parent gateway. The edge keys
// the gateway by ARN, matching the gateway node's published resource_id, so the
// volume->gateway join resolves instead of dangling. It returns nil when AWS
// reports no gateway ARN or no volume identity.
func volumeOnGatewayRelationship(boundary awscloud.Boundary, volume Volume) *awscloud.RelationshipObservation {
	volumeID := volumeResourceID(volume)
	gatewayARN := strings.TrimSpace(volume.GatewayARN)
	if volumeID == "" || !isARN(gatewayARN) {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipStorageGatewayVolumeOnGateway,
		SourceResourceID: volumeID,
		TargetResourceID: gatewayARN,
		TargetARN:        gatewayARN,
		TargetType:       awscloud.ResourceTypeStorageGatewayGateway,
		SourceRecordID:   volumeID + "->" + awscloud.RelationshipStorageGatewayVolumeOnGateway + ":" + gatewayARN,
	}
}

// fileShareOnGatewayRelationship records a file share's parent gateway, keyed by
// the gateway ARN the gateway node publishes as its resource_id. It returns nil
// when AWS reports no gateway ARN or no file-share identity.
func fileShareOnGatewayRelationship(boundary awscloud.Boundary, share FileShare) *awscloud.RelationshipObservation {
	shareID := fileShareResourceID(share)
	gatewayARN := strings.TrimSpace(share.GatewayARN)
	if shareID == "" || !isARN(gatewayARN) {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipStorageGatewayFileShareOnGateway,
		SourceResourceID: shareID,
		TargetResourceID: gatewayARN,
		TargetARN:        gatewayARN,
		TargetType:       awscloud.ResourceTypeStorageGatewayGateway,
		SourceRecordID:   shareID + "->" + awscloud.RelationshipStorageGatewayFileShareOnGateway + ":" + gatewayARN,
	}
}

// fileShareS3BucketRelationship records the S3 bucket a file share stores into.
// The target ARN is reduced to the bucket-only, partition-aware ARN the S3
// scanner publishes as its resource_id so the edge joins the bucket node. It
// returns nil when LocationARN is not a recognizable S3 bucket ARN (for example
// an access-point ARN).
func fileShareS3BucketRelationship(boundary awscloud.Boundary, share FileShare) *awscloud.RelationshipObservation {
	shareID := fileShareResourceID(share)
	bucketARN, bucket, prefix, ok := s3BucketARNFromLocation(share.LocationARN)
	if shareID == "" || !ok {
		return nil
	}
	attributes := map[string]any{
		"location_arn": strings.TrimSpace(share.LocationARN),
		"bucket":       bucket,
	}
	if prefix != "" {
		attributes["object_key_prefix"] = prefix
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipStorageGatewayFileShareStoresInS3Bucket,
		SourceResourceID: shareID,
		TargetResourceID: bucketARN,
		TargetARN:        bucketARN,
		TargetType:       awscloud.ResourceTypeS3Bucket,
		Attributes:       attributes,
		SourceRecordID:   shareID + "->" + awscloud.RelationshipStorageGatewayFileShareStoresInS3Bucket + ":" + bucketARN,
	}
}

// fileShareRoleRelationship records the IAM role a file share assumes to access
// its S3 storage, keyed by the role ARN the IAM scanner publishes as its
// resource_id. It returns nil when AWS reports no ARN-shaped role.
func fileShareRoleRelationship(boundary awscloud.Boundary, share FileShare) *awscloud.RelationshipObservation {
	shareID := fileShareResourceID(share)
	roleARN := strings.TrimSpace(share.Role)
	if shareID == "" || !isARN(roleARN) {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipStorageGatewayFileShareUsesIAMRole,
		SourceResourceID: shareID,
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       awscloud.ResourceTypeIAMRole,
		SourceRecordID:   shareID + "->" + awscloud.RelationshipStorageGatewayFileShareUsesIAMRole + ":" + roleARN,
	}
}

// fileShareKMSKeyRelationship records the KMS key a file share uses for
// server-side encryption. The KMS node publishes its bare key ID as resource_id
// but carries the key ARN as a correlation anchor, and existing cross-service
// KMS edges (efs, fsx) key the target by ARN; this edge follows that pattern. It
// returns nil when AWS reports no ARN-shaped key.
func fileShareKMSKeyRelationship(boundary awscloud.Boundary, share FileShare) *awscloud.RelationshipObservation {
	shareID := fileShareResourceID(share)
	kmsKeyARN := strings.TrimSpace(share.KMSKey)
	if shareID == "" || !isARN(kmsKeyARN) {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipStorageGatewayFileShareUsesKMSKey,
		SourceResourceID: shareID,
		TargetResourceID: kmsKeyARN,
		TargetARN:        kmsKeyARN,
		TargetType:       awscloud.ResourceTypeKMSKey,
		SourceRecordID:   shareID + "->" + awscloud.RelationshipStorageGatewayFileShareUsesKMSKey + ":" + kmsKeyARN,
	}
}

// fileShareLogGroupRelationship records the CloudWatch Logs log group a file
// share delivers audit logs to, keyed by the log-group ARN the CloudWatch Logs
// scanner publishes as its resource_id. It returns nil when AWS reports no
// ARN-shaped audit destination.
func fileShareLogGroupRelationship(boundary awscloud.Boundary, share FileShare) *awscloud.RelationshipObservation {
	shareID := fileShareResourceID(share)
	logGroupARN := strings.TrimSpace(share.AuditDestinationARN)
	if shareID == "" || !isARN(logGroupARN) {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipStorageGatewayFileShareLogsToCloudWatch,
		SourceResourceID: shareID,
		TargetResourceID: logGroupARN,
		TargetARN:        logGroupARN,
		TargetType:       awscloud.ResourceTypeCloudWatchLogsLogGroup,
		SourceRecordID:   shareID + "->" + awscloud.RelationshipStorageGatewayFileShareLogsToCloudWatch + ":" + logGroupARN,
	}
}

// gatewayVPCEndpointRelationship records the VPC endpoint a gateway is activated
// against. The VPC scanner publishes the endpoint node's resource_id as the bare
// `vpce-` ID, and DescribeGatewayInformation reports VPCEndpoint as a free-form
// configuration string, so the edge is emitted only when the value is the bare
// `vpce-` ID that joins that node. It returns nil otherwise.
func gatewayVPCEndpointRelationship(boundary awscloud.Boundary, gateway Gateway) *awscloud.RelationshipObservation {
	gatewayARN := strings.TrimSpace(gateway.ARN)
	endpointID, ok := vpcEndpointID(gateway.VPCEndpoint)
	if gatewayARN == "" || !ok {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipStorageGatewayGatewayUsesVPCEndpoint,
		SourceResourceID: gatewayARN,
		SourceARN:        gatewayARN,
		TargetResourceID: endpointID,
		TargetType:       awscloud.ResourceTypeVPCEndpoint,
		SourceRecordID:   gatewayARN + "->" + awscloud.RelationshipStorageGatewayGatewayUsesVPCEndpoint + ":" + endpointID,
	}
}

// volumeResourceID returns the stable identity a volume node publishes and that
// volume->gateway edges source on: the volume ARN, falling back to the bare
// volume ID when AWS reports no ARN.
func volumeResourceID(volume Volume) string {
	return firstNonEmpty(volume.ARN, volume.ID)
}

// fileShareResourceID returns the stable identity a file-share node publishes
// and that file-share edges source on: the file-share ARN, falling back to the
// bare file-share ID when AWS reports no ARN.
func fileShareResourceID(share FileShare) string {
	return firstNonEmpty(share.ARN, share.ID)
}
