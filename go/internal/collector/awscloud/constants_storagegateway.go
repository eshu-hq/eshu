// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceStorageGateway identifies the regional AWS Storage Gateway
	// metadata-only scan slice covering gateways, cached/stored volumes, and
	// NFS/SMB S3 file shares.
	ServiceStorageGateway = "storagegateway"
)

const (
	// ResourceTypeStorageGatewayGateway identifies an AWS Storage Gateway
	// gateway metadata resource. The scanner records gateway identity, type,
	// state, and endpoint type but never activates, deletes, or refreshes a
	// gateway.
	ResourceTypeStorageGatewayGateway = "aws_storagegateway_gateway"
	// ResourceTypeStorageGatewayVolume identifies an AWS Storage Gateway cached
	// or stored iSCSI volume metadata resource.
	ResourceTypeStorageGatewayVolume = "aws_storagegateway_volume"
	// ResourceTypeStorageGatewayFileShare identifies an AWS Storage Gateway
	// NFS or SMB S3 file share metadata resource. File-share object contents,
	// client allow lists, and admin/user lists stay outside the contract.
	ResourceTypeStorageGatewayFileShare = "aws_storagegateway_file_share"
)

const (
	// RelationshipStorageGatewayVolumeOnGateway records a Storage Gateway
	// volume's parent gateway when AWS reports the gateway ARN.
	RelationshipStorageGatewayVolumeOnGateway = "storagegateway_volume_on_gateway"
	// RelationshipStorageGatewayFileShareOnGateway records a Storage Gateway
	// file share's parent gateway when AWS reports the gateway ARN.
	RelationshipStorageGatewayFileShareOnGateway = "storagegateway_file_share_on_gateway"
	// RelationshipStorageGatewayFileShareStoresInS3Bucket records the S3 bucket
	// a file share's reported LocationARN resolves to.
	RelationshipStorageGatewayFileShareStoresInS3Bucket = "storagegateway_file_share_stores_in_s3_bucket"
	// RelationshipStorageGatewayFileShareUsesIAMRole records the IAM role a
	// file share assumes to access its S3 storage when AWS reports a role ARN.
	RelationshipStorageGatewayFileShareUsesIAMRole = "storagegateway_file_share_uses_iam_role"
	// RelationshipStorageGatewayFileShareUsesKMSKey records the KMS key a file
	// share uses for server-side encryption when AWS reports a key ARN.
	RelationshipStorageGatewayFileShareUsesKMSKey = "storagegateway_file_share_uses_kms_key"
	// RelationshipStorageGatewayFileShareLogsToCloudWatch records the
	// CloudWatch Logs log group a file share delivers audit logs to when AWS
	// reports a log-group ARN.
	RelationshipStorageGatewayFileShareLogsToCloudWatch = "storagegateway_file_share_logs_to_cloudwatch"
	// RelationshipStorageGatewayGatewayUsesVPCEndpoint records the VPC endpoint
	// a gateway is activated against when AWS reports a `vpce-`-shaped endpoint
	// identifier that the VPC scanner publishes as a node.
	RelationshipStorageGatewayGatewayUsesVPCEndpoint = "storagegateway_gateway_uses_vpc_endpoint"
)
