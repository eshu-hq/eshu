// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cicdstore persists claim-fenced CI/CD run watermarks in Postgres,
// closing the #5429 cross-cycle gap-detection watermark
// (go/internal/collector/cicdrun/runwatermark) across process restarts and
// collector replicas.
//
// CICDRunWatermarkStore mirrors AWSPaginationCheckpointStore's fencing
// pattern in the postgres root: a Save carrying a fencing token older than
// the stored row is rejected with runwatermark.ErrStaleFence, and a fencing
// token equal to the stored row's succeeds (idempotent redelivery). Load
// deliberately has no generation/fencing predicate: unlike an AWS pagination
// checkpoint, whose resume state is scoped to one generation's scan, a run
// watermark must be readable by a LATER generation to detect a gap against
// an EARLIER generation's progress.
//
// This package moved out of the postgres root under #6693; it must not
// import the parent postgres package. TestCICDRunWatermarkSchemaMatchesBootstrapMigration,
// which proves CICDRunWatermarkSchemaSQL stays in lockstep with the embedded
// bootstrap migration, stays in the postgres root because it asserts against
// root's BootstrapDefinitions bootstrap registry.
package cicdstore
