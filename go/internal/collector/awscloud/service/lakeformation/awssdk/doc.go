// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Lake Formation client into the
// metadata-only Lake Formation scanner interface.
//
// The adapter uses only GetDataLakeSettings, ListResources, and ListPermissions.
// It intentionally excludes GrantPermissions, RevokePermissions,
// BatchGrantPermissions, BatchRevokePermissions, RegisterResource,
// DeregisterResource, UpdateResource, PutDataLakeSettings, every LF-Tag and
// data-cells-filter mutation, and the credential-vending and governed-data
// readers (GetTemporaryGlueTableCredentials, GetTemporaryGluePartitionCredentials,
// GetTemporaryDataLocationCredentials, GetTableObjects, GetWorkUnits,
// GetWorkUnitResults, StartQueryPlanning). It drops every permission condition
// (LF-Tag) expression, LF-Tag value, and AdditionalDetails payload so only grant
// identities, principal identifiers, and resource ARNs leave the adapter.
package awssdk
