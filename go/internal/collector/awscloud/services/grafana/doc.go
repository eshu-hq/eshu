// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package grafana maps Amazon Managed Grafana workspace metadata into AWS cloud
// collector facts.
//
// The scanner emits reported-confidence resources for Managed Grafana
// workspaces plus relationships for workspace-to-IAM-role and, when a
// vpcConfiguration is present, workspace-to-subnet and
// workspace-to-security-group evidence. Dashboards, panels, alert rules, query
// results, SAML and IAM Identity Center authentication secrets, and workspace
// API keys stay outside this package contract: the scanner is metadata-only.
package grafana
