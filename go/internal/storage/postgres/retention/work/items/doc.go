// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package items deletes old terminal workflow_work_items rows in bounded
// batch transactions (#8829).
//
// Sweeper.PruneWorkItems walks the table in primary-key order, one window of
// WorkItemRetentionPolicy.BatchSize rows per transaction, and deletes the
// completed and failed_terminal rows of closed runs that are older than the
// policy age, except the newest row, the newest completed row and the newest
// failed_terminal row of each (collector_kind, collector_instance_id,
// scope_id) target and the row of a scope's active generation. Claim rows go with their work item through the
// workflow_claims ON DELETE CASCADE. Sweeper.WorkItemTableSize reads the
// table's catalog size for the operator gauges.
//
// Each batch locks its rows FOR UPDATE SKIP LOCKED under a local lock and
// statement timeout, so it never waits on a claim, heartbeat or completion.
// The workflow coordinator owns the schedule; this package owns only the SQL
// and the batch loop. It lives outside the parent postgres package because
// that directory is over the dirgate cap.
package items
