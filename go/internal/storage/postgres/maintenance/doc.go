// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package maintenancestore persists operator-triggered ingester requests,
// tracked per ingester in the runtime_ingester_control table. A scan request
// moves from idle to pending, pending to running on claim, and running to
// completed or failed on completion. A reindex request is a fleet watermark:
// RequestReindex stamps reindex_request_requested_at monotonically from the
// database clock, git ingesters read it through GetReindexState, and nothing
// claims or completes it.
//
// RepositoryReindexStore holds the per-repository reindex watermarks in
// repository_reindex_requests, one row per git scope ID. RequestRepositoryReindex
// stamps each row monotonically from the database clock, taking row locks in
// scope ID order so concurrent overlapping requests cannot deadlock, and
// RepositoryReindexWatermarks reads the rows later than the fleet watermark.
// Rows are never claimed or deleted.
//
// StatusRequestStore implements runtime.StatusRequestStore over an injected
// db.ExecQueryer. It carries no queue leasing, no fencing token, and no
// dependency on the rest of the postgres root: every statement addresses one
// ingester row keyed by its primary key. This package must not import the
// parent postgres package.
package maintenancestore
