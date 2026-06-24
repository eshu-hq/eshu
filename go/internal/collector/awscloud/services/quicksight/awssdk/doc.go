// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 QuickSight client into the
// metadata-only QuickSight scanner interface.
//
// The adapter uses ListDataSources, DescribeDataSource, ListDataSets,
// DescribeDataSet, ListDashboards, DescribeDashboard, ListAnalyses,
// DescribeAnalysis, ListVPCConnections, and ListTagsForResource to read
// QuickSight control-plane metadata. It threads the boundary AWS account id
// (required by nearly every QuickSight API) into each call. It intentionally
// excludes every Create/Update/Delete mutation, the permissions reads,
// ingestion and job control, and any credential or secret read, so the adapter
// cannot mutate QuickSight state or read data-source credentials, connection
// passwords, secret connection parameters, SQL query bodies, or visual
// definitions. An account that is not signed up for QuickSight is mapped to an
// empty snapshot with a warning rather than a failed scan.
package awssdk
