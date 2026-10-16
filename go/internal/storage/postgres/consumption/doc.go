// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package consumption rebuilds one scope's package-consumption sidecars
// (package_manifest_consumption_keys and package_registry_identity_keys) from
// its facts in short transactions, for the reducer-owned backfill in the
// parent postgres package (#7088, #7663).
//
// A Rebuilder runs a start transaction that locks the scope's ingestion_scopes
// row FOR NO KEY UPDATE, deletes the scope's sidecar rows, and writes the
// scope's dirty row as a readiness fence, keeping that row's xmin. It then
// commits one transaction per fact page, each taking the same row lock under a
// bounded lock_timeout. The last page deletes the dirty row only when its xmin
// still matches. An old writer's trigger or a concurrent rebuild that touches
// the row in between leaves the scope dirty for the next pass.
//
// The row lock excludes the ingester, the only writer that bypasses the dirty
// trigger, without blocking the FOR KEY SHARE that foreign-key checks take.
// The parent package supplies the page query and the sidecar refresh through
// the PageLoader and Refresher seams; this package never imports the parent.
package consumption
