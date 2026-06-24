// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 WorkSpaces client into the
// metadata-only WorkSpaces scanner interface.
//
// The adapter uses DescribeWorkspaces, DescribeWorkspaceDirectories,
// DescribeWorkspaceBundles, DescribeIpGroups, and DescribeTags to read
// WorkSpaces, directory, bundle, and IP-access-control-group control-plane
// metadata and resource tags. It intentionally excludes every Create/Modify/
// Reboot/Rebuild/Start/Stop/Terminate mutation, every session and
// connection-status read, and any credential read, so the adapter cannot read
// desktop session contents or mutate WorkSpaces state.
package awssdk
