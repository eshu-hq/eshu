// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Storage Gateway client into the
// metadata-only Storage Gateway scanner interface.
//
// The adapter uses ListGateways, DescribeGatewayInformation, ListVolumes,
// ListFileShares, DescribeNFSFileShares, and DescribeSMBFileShares. It
// intentionally excludes ActivateGateway, DeleteGateway, ShutdownGateway,
// StartGateway, UpdateGatewayInformation, RefreshCache, CreateNFSFileShare,
// CreateSMBFileShare, DeleteFileShare, CreateCachediSCSIVolume,
// CreateStorediSCSIVolume, DeleteVolume, and every other mutation, cache, tape,
// or credential API. Object contents, NFS client allow lists, and SMB
// admin/user lists never leave the adapter.
package awssdk
