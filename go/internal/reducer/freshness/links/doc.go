// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package links is the changed_since_link reducer domain (#7127 PR-3a). Its
// Runner turns generation activations into changed-since links. A later read
// path answers get_changed_since from those links in O(changes) instead of
// diffing two whole generations.
//
// Each Runner cycle does four things. It journals activations the Ack path
// has not recorded: a sweeper row with an unknown prior for every active
// generation, plus a bounded backfill of retained chains. It deletes the
// ledger rows of deleted scopes. It links each backlog scope, with at most
// Config.Workers scopes at once and at most Config.MaxLinksPerScope links per
// scope. Finally it samples the backlog, lag and state-table gauges.
//
// The storage package (storage/postgres/freshness/links) owns every
// statement, the per-scope cursor fence and the database-wide full-link
// slots. A non-counting miss (cursor_locked, generation_locked,
// generation_lock_timeout, slot_busy)
// stops that scope for the cycle and writes nothing. A counting failure
// (statement_timeout, connection_lost, sql_error, internal) is recorded on
// the cursor with backoff, and at Config.MaxAttempts the activation becomes a
// link_poisoned chain break (#7127 ruling 8.10). There is no work-item queue:
// the journal and the cursor are the durable queue.
//
// The domain is dark. cmd/reducer builds a Runner only when
// ESHU_CHANGED_SINCE_LINK_ENABLED=true, and a nil Runner issues no SQL.
package links
