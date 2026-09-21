// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 EC2 client into the metadata-only
// Amazon Verified Access scanner interface.
//
// The adapter uses DescribeVerifiedAccessInstances, DescribeVerifiedAccessGroups,
// DescribeVerifiedAccessEndpoints, and DescribeVerifiedAccessTrustProviders to
// read Verified Access control-plane metadata. It intentionally excludes every
// Create/Modify/Delete mutation API, the GetVerifiedAccess*Policy policy-body
// reads, and any data-plane operation, so the adapter cannot mutate Verified
// Access state, read policy documents, or read trust-provider client secrets. It
// copies only the OIDC issuer reference from a trust provider, never the OIDC
// client identifier, client secret, or token/userinfo endpoints.
package awssdk
