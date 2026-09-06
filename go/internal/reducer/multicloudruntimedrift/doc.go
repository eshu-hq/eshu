// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package multicloudruntimedrift publishes admitted provider-neutral runtime
// drift findings -- orphaned, unmanaged, ambiguous, unknown, image-version
// drift, and value-comparison-inconclusive -- as durable reducer facts
// (issues #1997, #1998, #5759).
//
// The domain is additive: it registers only when both a
// [MultiCloudRuntimeDriftEvidenceLoader] and a [MultiCloudRuntimeDriftFindingWriter]
// are wired, via [MaterializationDomainDefinition]. Evidence rows are keyed on
// canonical cloud_resource_uid so GCP and Azure share one join and one drift
// rule pack with the AWS structural drift path; AWS itself is exclusively
// DomainAWSCloudRuntimeDrift's, so [MultiCloudRuntimeDriftHandler] drops any
// AWS-provider row the shared evidence loader also resolves
// (excludeAWSOwnedRows) before evaluation, never publishing a duplicate under
// reducer_multi_cloud_runtime_drift_finding.
//
// [PostgresMultiCloudRuntimeDriftWriter] persists one durable reducer fact per
// admitted candidate, keyed by a stable identity (scope, generation, finding
// kind, canonical uid) so retries and concurrent workers converge on the same
// row instead of duplicating findings. Writes go through the shared
// factwrite.BatchInsertVersionedFacts bounded chunked bulk insert rather than
// one ExecContext per candidate.
//
// This package never imports internal/reducer.
package multicloudruntimedrift
