// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS sso-admin and identitystore reads into the
// scanner-owned IAM Identity Center metadata snapshot.
//
// The adapter reaches AWS only through two narrow interfaces, ssoAdminAPI and
// identityStoreAPI, that expose List-, Describe-, and tag reads. They omit
// every mutation API, GetInlinePolicyForPermissionSet,
// GetPermissionsBoundaryForPermissionSet, GetApplicationAccessScope, and
// ListApplicationAccessScopes by construction; a reflection test asserts the
// interface shape so the forbidden APIs stay unreachable. Principal resolution
// reads only the identity store DisplayName for each unique assignment
// principal. Accounts with no Identity Center instance or without org access
// produce a warning rather than failing the claim.
package awssdk
