// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 DAX control-plane client into the
// metadata-only Client the dax scanner consumes.
//
// The adapter exposes only DescribeClusters, DescribeSubnetGroups,
// DescribeParameterGroups, and ListTags through a narrow apiClient interface, so
// no mutation API and no DescribeParameters (individual parameter values) is
// reachable. It maps SDK types into scanner-owned metadata, records the
// server-side-encryption status without a KMS key (DAX reports none), pages every
// list response to exhaustion, and wraps each call in the shared AWS pagination
// span plus API-call and throttle counters. Cached DynamoDB item data, query
// results, and node endpoint payloads are never read.
package awssdk
