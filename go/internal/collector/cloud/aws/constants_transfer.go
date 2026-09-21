// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceTransfer identifies the regional AWS Transfer Family metadata-only
	// scan slice covering SFTP/FTPS/FTP/AS2 servers and their service-managed
	// users. Host keys, SSH public keys, user policy JSON, and any credential
	// material stay outside the scan slice.
	ServiceTransfer = "transfer"
)

const (
	// ResourceTypeTransferServer identifies an AWS Transfer Family server
	// metadata resource. The fact carries server identity (ARN, server ID),
	// endpoint type, protocols, identity provider type, domain, and state. Host
	// key fingerprints and host key material are never persisted.
	ResourceTypeTransferServer = "aws_transfer_server"
	// ResourceTypeTransferUser identifies an AWS Transfer Family service-managed
	// user metadata resource. The fact carries user identity (ARN, user name),
	// home directory type, home directory path, and home directory mapping paths.
	// SSH public keys, user policy JSON, and POSIX credential material are never
	// persisted.
	ResourceTypeTransferUser = "aws_transfer_user"
)

const (
	// RelationshipTransferServerUsesVPCEndpoint records a Transfer server's
	// reported VPC endpoint placement when the server uses a VPC_ENDPOINT
	// endpoint type with an AWS-reported VPC endpoint ID.
	RelationshipTransferServerUsesVPCEndpoint = "transfer_server_uses_vpc_endpoint"
	// RelationshipTransferServerUsesElasticIP records a Transfer server's
	// reported Elastic IP allocation when the server's VPC endpoint attaches
	// address allocation IDs.
	RelationshipTransferServerUsesElasticIP = "transfer_server_uses_elastic_ip"
	// RelationshipTransferServerUsesACMCertificate records a Transfer server's
	// reported ACM certificate dependency when FTPS is enabled and AWS reports
	// an ARN-shaped certificate.
	RelationshipTransferServerUsesACMCertificate = "transfer_server_uses_acm_certificate"
	// RelationshipTransferServerLogsToLogGroup records a Transfer server's
	// reported CloudWatch Logs structured-log destination when AWS reports an
	// ARN-shaped log group.
	RelationshipTransferServerLogsToLogGroup = "transfer_server_logs_to_log_group"
	// RelationshipTransferServerUsesLoggingRole records a Transfer server's
	// reported CloudWatch logging IAM role when AWS reports an ARN-shaped role.
	RelationshipTransferServerUsesLoggingRole = "transfer_server_uses_logging_role"
	// RelationshipTransferUserUsesIAMRole records a Transfer user's reported IAM
	// role dependency when AWS reports an ARN-shaped role.
	RelationshipTransferUserUsesIAMRole = "transfer_user_uses_iam_role"
	// RelationshipTransferUserHomeDirectoryInS3Bucket records a Transfer user's
	// reported S3 home-directory backing bucket when the home directory resolves
	// to an S3 path. Only the path (bucket and key prefix) is recorded; object
	// contents are never read.
	RelationshipTransferUserHomeDirectoryInS3Bucket = "transfer_user_home_directory_in_s3_bucket"
	// RelationshipTransferUserHomeDirectoryInEFSFileSystem records a Transfer
	// user's reported EFS home-directory backing file system when the home
	// directory resolves to an EFS path. Only the path is recorded; file
	// contents are never read.
	RelationshipTransferUserHomeDirectoryInEFSFileSystem = "transfer_user_home_directory_in_efs_file_system"
)
