// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package unread persists the collector-owned read state of one
// security-alert organization target, one record per target generation
// (#8943).
//
// The security-alert collector reads at most max_pages x 100 alerts of an
// organization, then reads each allowlisted repository one by one when that
// stream was cut short. A repository the collector did not read in full has no
// alert fact, so without a record the query layer would report an empty answer
// for it as complete. Writer returns the collector.GenerationStateWriter that
// stores the target-level row (security_alert_target_read_states) and one child
// row per unread repository with its closed reason
// (security_alert_unread_repositories). The ingestion store runs it in the
// commit transaction of the generation, after the generation row exists, so
// the record commits with its generation or not at all. Both tables cascade
// from scope_generations, so generation retention removes them with the
// generation; nothing here owns a sweeper and nothing depends on
// workflow_work_items.
//
// The same row carries the org fan-out cursor (#8946, migration 232):
// fanout_start_repository, fanout_next_repository, and fanout_lap_repository
// name the allowlisted repository this generation's fan-out started at, the
// next claim starts at, and, when a failed read at the last position closed a
// lap, that last repository.
// CursorReader reads the cursor of the newest pending-or-active generation of a
// scope, so the cursor advances only when a generation commits and survives a
// restart.
//
// The package imports the collector contract and the shared value types only,
// never the parent postgres package.
package unread
