// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Resource Access Manager client to
// the RAM scanner contract.
//
// The package owns RAM pagination, SDK response mapping, AWS API telemetry,
// throttle detection, and pagination spans. It is metadata-only: the accepted
// apiClient surface names only GetResourceShares, ListResources, ListPrincipals,
// and ListResourceSharePermissions, and excludes every Create/Delete/Update,
// Associate/Disassociate, Accept/Reject, Enable/Disable, Promote/Replace,
// Tag/Untag, and SetDefaultPermissionVersion operation by construction. It never
// reaches GetPermission, so the permission policy document body never crosses
// the adapter boundary. A reflective guard test fails the build if any
// forbidden operation becomes reachable.
package awssdk
