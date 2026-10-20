// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Managed Grafana client into the
// metadata-only Grafana scanner interface.
//
// The adapter uses ListWorkspaces, DescribeWorkspace, and ListTagsForResource
// to read Managed Grafana workspace control-plane metadata and resource tags. It
// intentionally excludes every Create/Update/Delete workspace API, the
// CreateWorkspaceApiKey and workspace service-account-token APIs,
// AssociateLicense, and DescribeWorkspaceAuthentication (which returns SAML and
// IAM Identity Center configuration), so the adapter cannot mutate a workspace,
// mint an API key or token, or read an authentication secret.
package awssdk
