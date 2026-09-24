// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awsstore persists AWS collector claim-fencing state: paginated
// scan resume markers and per-tuple scan status.
//
// AWSPaginationCheckpointStore implements checkpoint.Store
// (internal/collector/awscloud/checkpoint): it lets a paginated AWS API scan
// resume from its last page after a worker restart, guarded by a
// generation/fencing-token pair so a stale worker cannot overwrite a newer
// claim's progress.
//
// AWSScanStatusStore records per-(collector instance, account, region,
// service) scan lifecycle for the admin status surface: running/succeeded/
// failed status, commit outcome, and observability counters (API calls,
// throttles, resources, relationships, tag observations). Its fencing guard
// widens across workflow generations only for terminal or orphaned prior
// rows -- see startAWSScanStatusQuery's comment for the exact widening
// conditions and issue #612, the orphaned-row bug it fixes.
//
// This package must not import the parent postgres package.
package awsstore
