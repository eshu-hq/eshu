// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package storagegateway maps AWS Storage Gateway gateway, iSCSI volume, and
// NFS/SMB S3 file-share metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for gateways, volumes, and
// file shares plus relationships for volume-to-gateway, file-share-to-gateway,
// file-share-to-S3-bucket, file-share-to-IAM-role, file-share-to-KMS-key,
// file-share-to-CloudWatch-log-group, and gateway-to-VPC-endpoint evidence.
// File-share object contents, NFS client allow lists, SMB admin/user lists, and
// every gateway/volume/share mutation API (activation, deletion, shutdown,
// reboot, cache refresh, create, update) stay outside this package contract.
package storagegateway
