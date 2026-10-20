// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 Directory Service calls into
// scanner-owned metadata across AWS Managed Microsoft AD, Simple AD, and AD
// Connector directories.
//
// The adapter only calls describe-class reads: DescribeDirectories,
// DescribeTrusts, DescribeSharedDirectories, DescribeLDAPSSettings, and
// ListTagsForResource. It must not call any mutation API (ResetUserPassword,
// Create/Delete/Update/Enable/Disable/Register/Accept/Reject/Share/...). The
// apiClient interface plus the reflection guard in client_test.go enforce that
// contract.
//
// The mapping layer reads only safe metadata. It never maps the directory admin
// password (DescribeDirectories does not return it), the RADIUS shared secret
// (RadiusSettings is not read), or the AD Connector service-account user name
// (ConnectSettings.CustomerUserName is not read). VPC placement is read from
// VpcSettings for Simple AD and Managed Microsoft AD, and from ConnectSettings
// for AD Connector. LDAPS settings are queried only for Managed Microsoft AD
// directories, which are the only type that supports LDAPS.
package awssdk
