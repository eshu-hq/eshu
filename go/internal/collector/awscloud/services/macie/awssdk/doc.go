// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Amazon Macie client into the
// metadata-only Macie scanner interface.
//
// The adapter owns Macie pagination, the session and administrator-account
// reads, member, classification-job, allow-list, custom-data-identifier, and
// findings-filter list reads, the aggregate finding-count-by-severity read,
// throttle classification, and per-call AWS API telemetry.
//
// It is the highest-redaction adapter in the collector. It intentionally
// excludes sensitive-data finding reads (GetSensitiveDataOccurrences,
// GetSensitiveDataOccurrencesAvailability, GetFindings, ListFindings), custom
// data identifier regular-expression reads (GetCustomDataIdentifier,
// TestCustomDataIdentifier, BatchGetCustomDataIdentifiers), allow-list content
// reads (GetAllowList), findings filter criteria reads (GetFindingsFilter),
// classification-job bucket-criteria reads (DescribeClassificationJob,
// DescribeBuckets, SearchResources), and every mutation API. A reflection test
// over the internal apiClient interface enforces that exclusion, and the
// scanner-owned types it returns have no field able to hold a regex body, list
// contents, finding detail, or criteria.
package awssdk
