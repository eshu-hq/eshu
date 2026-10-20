// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 CloudFormation calls into
// scanner-owned metadata.
//
// The adapter only calls read-only list-and-describe operations:
// DescribeStacks, ListStacks (status-filtered for recently deleted stacks),
// ListStackResources, ListStackSets, DescribeStackSet, ListChangeSets,
// DescribeStackResourceDrifts, ListStackInstances, and ListTypes. It must never
// call GetTemplate or GetTemplateSummary (template body), DescribeChangeSet
// (change-set body), GetStackPolicy (policy body), any Detect*Drift API
// (mutation), or any stack/stack-set/change-set/instance/type mutation API.
//
// Parameter values are dropped during mapping (only keys survive), the
// stack-set TemplateBody is never carried into the scanner type, and drift
// property documents are reduced to per-status counts.
// TestAPIClientInterfaceExcludesTemplateAndMutationAPIs proves the accepted SDK
// surface lists none of the forbidden methods.
package awssdk
