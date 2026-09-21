// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Amazon Managed Service for
// Prometheus (aps) client into the metadata-only AMP scanner interface.
//
// The adapter uses ListWorkspaces, ListRuleGroupsNamespaces, and ListScrapers
// to read AMP workspace, rule-groups namespace (names only), and scraper
// control-plane metadata. It intentionally excludes DescribeRuleGroupsNamespace
// (the rule definition body), DescribeWorkspaceConfiguration, the alert-manager
// and scrape-configuration reads, and every Create/Update/Delete/Put mutation
// API, so the adapter cannot read ingested samples, rule definitions, or
// scrape-configuration bodies, and cannot mutate AMP state.
package awssdk
