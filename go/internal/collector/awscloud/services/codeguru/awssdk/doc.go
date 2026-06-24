// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 CodeGuru Reviewer and CodeGuru
// Profiler clients into the metadata-only CodeGuru scanner interface.
//
// The adapter uses ListRepositoryAssociations and DescribeRepositoryAssociation
// (Reviewer) to read repository-association metadata, encryption-key and
// S3-backing references, and resource tags, and ListProfilingGroups with
// descriptions (Profiler) to read profiling-group metadata and tags. It
// intentionally excludes every code-review and recommendation read
// (ListCodeReviews, DescribeCodeReview, ListRecommendations,
// ListRecommendationFeedback), every profiling-data read (GetProfile,
// ListFindingsReports, ListProfileTimes, BatchGetFrameMetricData), and all
// Associate/Disassociate/Create/Update/Delete/Put/Configure mutation APIs, so
// the adapter cannot read findings, recommendation content, or profiling samples
// or write CodeGuru state.
package awssdk
