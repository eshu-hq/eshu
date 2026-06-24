// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Security Hub client into the
// metadata-only Security Hub scanner interface.
//
// The adapter reads DescribeHub, GetAdministratorAccount, ListMembers,
// GetEnabledStandards, DescribeStandardsControls, DescribeActionTargets,
// GetInsights, GetInsightResults, GetFindings, and ListTagsForResource. It
// reduces GetFindings responses to bounded aggregate counts and intentionally
// excludes finding bodies, insight filter expressions, resource details,
// remediation text, notes, product fields, user-defined fields, network
// details, process details, and mutation APIs.
package awssdk
